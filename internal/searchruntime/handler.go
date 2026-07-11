package searchruntime

import (
	"context"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/wsruntime"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// Handler manages all active search runtimes
type Handler struct {
	runtimes map[uuid.UUID]*Runtime
	// byKey maps "query_locale" → runtime, so we can reuse a healthy one
	byKey    map[string]*Runtime
	Upgrader *websocket.Upgrader

	wg    *sync.WaitGroup
	mutex sync.RWMutex
}

// NewHandler creates a new search runtime handler
func NewHandler(_ context.Context) *Handler {
	return &Handler{
		runtimes: make(map[uuid.UUID]*Runtime),
		byKey:    make(map[string]*Runtime),
		wg:       &sync.WaitGroup{},
		Upgrader: &websocket.Upgrader{
			CheckOrigin: wsruntime.CheckOrigin,
		},
	}
}

// wsThreshold returns the configured threshold above which a WebSocket is used
func wsThreshold() int {
	viper.SetDefault("search.ws_threshold", 1)
	return viper.GetInt("search.ws_threshold")
}

// NeedsWebSocket returns true when the total number of search results exceeds the threshold
func (h *Handler) NeedsWebSocket(totalResults int) bool {
	return totalResults > wsThreshold()
}

// RunSearch creates (or reuses) a Runtime for the given query and starts processing in the background.
// The caller must have already performed the BrickLink search and passes the raw results in.
func (h *Handler) RunSearch(
	ctx context.Context,
	query string,
	locale language.Tag,
	totalResults int,
	processFunc func(rt *Runtime),
) *Runtime {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	key := buildKey(query, locale)

	// Reuse a healthy running runtime for the same query+locale
	if rt, ok := h.byKey[key]; ok {
		if rt.IsAlive() {
			zap.L().Info("Reusing existing search runtime",
				zap.String("key", key),
				zap.String("runtime_id", rt.ID.String()),
			)
			return rt
		}
		// Stale / failed – remove it and create a new one
		h.removeRuntimeLocked(rt.ID)
	}

	rt := NewRuntime(query, locale, totalResults, h.wg, h.onRuntimeEnd)
	rt.Start()

	h.runtimes[rt.ID] = rt
	h.byKey[key] = rt

	zap.L().Info("Started new search runtime",
		zap.String("key", key),
		zap.String("runtime_id", rt.ID.String()),
		zap.Int("total_results", totalResults),
	)

	// Run the processing function in a background goroutine
	go processFunc(rt)

	return rt
}

// GetRuntime returns a runtime by its UUID, or nil if not found
func (h *Handler) GetRuntime(id uuid.UUID) *Runtime {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.runtimes[id]
}

// Shutdown gracefully stops all runtimes and waits for them to finish
func (h *Handler) Shutdown() {
	h.mutex.Lock()
	for _, rt := range h.runtimes {
		select {
		case rt.done <- struct{}{}:
		default:
		}
	}
	h.mutex.Unlock()
	h.wg.Wait()
}

// onRuntimeEnd is called by a Runtime when it stops
func (h *Handler) onRuntimeEnd(id uuid.UUID) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.removeRuntimeLocked(id)
}

func (h *Handler) removeRuntimeLocked(id uuid.UUID) {
	rt, ok := h.runtimes[id]
	if !ok {
		return
	}
	key := buildKey(rt.Query, rt.Locale)
	delete(h.runtimes, id)
	// Only remove the byKey entry if it still points to this runtime
	if existing, ok := h.byKey[key]; ok && existing.ID == id {
		delete(h.byKey, key)
	}
	zap.L().Info("Removed search runtime",
		zap.String("runtime_id", id.String()),
		zap.String("key", key),
	)
}

func buildKey(query string, locale language.Tag) string {
	return query + "_" + locale.String()
}
