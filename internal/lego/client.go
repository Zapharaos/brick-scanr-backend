package lego

import (
	"net/http"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
	"github.com/spf13/viper"
)

// Client handles all LEGO official API interactions
type Client struct {
	httpClient *http.Client
	useMocks   bool
	throttler  *throttle.Throttler
}

// NewClient creates a new LEGO client instance
func NewClient() *Client {
	// Load throttler config from viper
	userAgents := viper.GetStringSlice("api_clients.lego.user_agents")
	if len(userAgents) == 0 {
		// Use OS-appropriate user agents if none configured
		userAgents = throttle.GetUserAgentForClient("lego")
	}

	config := throttle.Config{
		DelayMinMs:              viper.GetInt("api_clients.lego.delay_min_ms"),
		DelayMaxMs:              viper.GetInt("api_clients.lego.delay_max_ms"),
		MaxRequests:             viper.GetInt("api_clients.lego.max_requests"),
		WindowSeconds:           viper.GetInt("api_clients.lego.window_seconds"),
		MaxAttempts:             viper.GetInt("api_clients.lego.retry.max_attempts"),
		InitialBackoffMs:        viper.GetInt("api_clients.lego.retry.initial_backoff_ms"),
		MaxBackoffMs:            viper.GetInt("api_clients.lego.retry.max_backoff_ms"),
		BackoffMultiplier:       viper.GetFloat64("api_clients.lego.retry.backoff_multiplier"),
		UserAgents:              userAgents,
		AdaptiveEnabled:         viper.GetBool("api_clients.lego.adaptive.enabled"),
		BaselineResponseTimeMs:  viper.GetInt("api_clients.lego.adaptive.baseline_response_time_ms"),
		SlowThresholdMultiplier: viper.GetFloat64("api_clients.lego.adaptive.slow_threshold_multiplier"),
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		useMocks:  false,
		throttler: throttle.New("lego", config),
	}
}

var _globalClient *Client

// C is used to access the global Client singleton
func C() *Client {
	if _globalClient == nil {
		_globalClient = NewClient()
	}
	return _globalClient
}

// ReplaceGlobalClient affects a new client to the global client singleton
func ReplaceGlobalClient(client *Client) func() {
	prev := _globalClient
	_globalClient = client
	return func() { ReplaceGlobalClient(prev) }
}
