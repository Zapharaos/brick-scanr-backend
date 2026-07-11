package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/app"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"go.uber.org/zap"
)

// healthCacheTTL bounds how often the health endpoint actually probes its
// dependencies. A public, monitored endpoint may be polled frequently (uptime
// checks + a portfolio widget), so we serve a cached verdict in between to keep
// it cheap and to avoid hammering Redis with PINGs.
const healthCacheTTL = 5 * time.Second

// componentHealth is the public, sanitized status of a single dependency.
// It intentionally carries no error details or internal addresses.
type componentHealth struct {
	Status string `json:"status"` // "ok" or "down"
}

// healthResponse is the public payload, stable enough to be consumed by an
// uptime monitor (e.g. Uptime Kuma) and rendered on a status page.
type healthResponse struct {
	Status        string                     `json:"status"` // "ok" or "degraded"
	Version       string                     `json:"version,omitempty"`
	BuildDate     string                     `json:"buildDate,omitempty"`
	UptimeSeconds int64                      `json:"uptimeSeconds"`
	Timestamp     string                     `json:"timestamp"`
	Components    map[string]componentHealth `json:"components"`
}

var (
	healthMu       sync.Mutex
	healthCached   healthResponse
	healthCachedAt time.Time
	healthCachedOK bool
)

// statusWord maps a boolean health to its public label.
func statusWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "down"
}

// computeHealth probes the critical dependencies and builds the payload plus an
// overall-ok flag. Redis is treated as critical: when it is down the API cannot
// serve, so the overall status is "degraded" (HTTP 503).
func computeHealth() (healthResponse, bool) {
	redisOK := database.DB().Redis().IsHealthy()
	overallOK := redisOK

	resp := healthResponse{
		Status:    "ok",
		Version:   app.Version(),
		BuildDate: app.BuildDate(),
		Components: map[string]componentHealth{
			"redis": {Status: statusWord(redisOK)},
		},
	}
	if !overallOK {
		resp.Status = "degraded"
	}
	return resp, overallOK
}

// Health is a public health/readiness endpoint suitable for uptime monitors and
// status displays. It returns 200 when all critical dependencies are healthy and
// 503 when degraded, so a monitor treating non-2xx as "down" works out of the
// box. Dependency probes are cached for healthCacheTTL to stay cheap under
// frequent polling.
func Health(w http.ResponseWriter, r *http.Request) {
	healthMu.Lock()
	if healthCachedAt.IsZero() || time.Since(healthCachedAt) > healthCacheTTL {
		healthCached, healthCachedOK = computeHealth()
		healthCachedAt = time.Now()
	}
	resp, ok := healthCached, healthCachedOK
	healthMu.Unlock()

	// Uptime and timestamp always reflect "now", even on a cached verdict.
	resp.Timestamp = time.Now().UTC().Format(time.RFC3339)
	resp.UptimeSeconds = int64(app.Uptime().Seconds())

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if ok {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// HEAD requests only need the status code and headers.
	if r.Method == http.MethodHead {
		return
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("Failed to encode health response", zap.Error(err))
	}
}
