package rebrickable

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
	"github.com/spf13/viper"
)

// Error types
var (
	// ErrInventoryNotFound indicates that the set inventory was not found in Rebrickable.
	ErrInventoryNotFound = errors.New("inventory not found in rebrickable")

	// ErrMissingAPIKey indicates the Rebrickable API key was not configured.
	ErrMissingAPIKey = errors.New("rebrickable api key is not configured")
)

// Client handles all Rebrickable API interactions
type Client struct {
	httpClient *http.Client
	useMocks   bool
	throttler  *throttle.Throttler
	apiKey     string
	apiBaseURL string
}

// NewClient creates a new Rebrickable client instance
func NewClient() *Client {
	userAgents := viper.GetStringSlice("api_clients.rebrickable.user_agents")
	if len(userAgents) == 0 {
		userAgents = throttle.GetUserAgentForClient("rebrickable")
	}

	config := throttle.Config{
		DelayMinMs:              viper.GetInt("api_clients.rebrickable.delay_min_ms"),
		DelayMaxMs:              viper.GetInt("api_clients.rebrickable.delay_max_ms"),
		MaxRequests:             viper.GetInt("api_clients.rebrickable.max_requests"),
		WindowSeconds:           viper.GetInt("api_clients.rebrickable.window_seconds"),
		MaxAttempts:             viper.GetInt("api_clients.rebrickable.retry.max_attempts"),
		InitialBackoffMs:        viper.GetInt("api_clients.rebrickable.retry.initial_backoff_ms"),
		MaxBackoffMs:            viper.GetInt("api_clients.rebrickable.retry.max_backoff_ms"),
		BackoffMultiplier:       viper.GetFloat64("api_clients.rebrickable.retry.backoff_multiplier"),
		UserAgentEnabled:        viper.GetBool("api_clients.rebrickable.user_agent_enabled"),
		UserAgentRotation:       viper.GetBool("api_clients.rebrickable.user_agent_rotation"),
		UserAgents:              userAgents,
		AdaptiveEnabled:         viper.GetBool("api_clients.rebrickable.adaptive.enabled"),
		BaselineResponseTimeMs:  viper.GetInt("api_clients.rebrickable.adaptive.baseline_response_time_ms"),
		SlowThresholdMultiplier: viper.GetFloat64("api_clients.rebrickable.adaptive.slow_threshold_multiplier"),
	}

	apiBaseURL := viper.GetString("api_clients.rebrickable.api_base_url")
	if apiBaseURL == "" {
		apiBaseURL = "https://rebrickable.com/api/v3"
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		useMocks:   false,
		throttler:  throttle.New("rebrickable", config),
		apiKey:     viper.GetString("api_clients.rebrickable.api_key"),
		apiBaseURL: strings.TrimRight(apiBaseURL, "/"),
	}
}

// ThrottlerStatus returns the current status of the client's throttler.
func (c *Client) ThrottlerStatus() throttle.Status {
	return c.throttler.GetStatus()
}

// HasAPIKey reports whether an API key is configured. Inventory fetches fail without
// one, so this is used at startup to surface a misconfiguration early.
func (c *Client) HasAPIKey() bool {
	return c.apiKey != ""
}

var (
	_globalClient     *Client
	_globalClientOnce sync.Once
)

// C is used to access the global Client singleton
func C() *Client {
	_globalClientOnce.Do(func() {
		_globalClient = NewClient()
	})
	return _globalClient
}

// ReplaceGlobalClient affects a new client to the global client singleton
func ReplaceGlobalClient(client *Client) func() {
	prev := _globalClient
	_globalClient = client
	return func() { ReplaceGlobalClient(prev) }
}
