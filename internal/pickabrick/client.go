package pickabrick

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
	"github.com/spf13/viper"
)

// Error types
var (
	// ErrBrickNotFound indicates that the brick was not found in Pick-a-Brick API
	ErrBrickNotFound = errors.New("brick not found in pick-a-brick")
)

// imageValidationCache stores whether CDN PNG images exist for element IDs
// true = CDN image exists, false = CDN image doesn't exist (use fallback)
type imageValidationCache struct {
	mu    sync.RWMutex
	cache map[string]bool
}

func newImageValidationCache() *imageValidationCache {
	return &imageValidationCache{
		cache: make(map[string]bool),
	}
}

func (c *imageValidationCache) Get(elementID string) (exists bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	exists, found = c.cache[elementID]
	return
}

func (c *imageValidationCache) Set(elementID string, exists bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[elementID] = exists
}

// Client handles all Pick-a-Brick API interactions
type Client struct {
	httpClient     *http.Client
	useMocks       bool
	throttler      *throttle.Throttler
	imageCache     *imageValidationCache
	validateImages bool // Whether to validate CDN images (can be toggled via config)
}

// NewClient creates a new Pick-a-Brick client instance
func NewClient() *Client {
	// Load throttler config from viper
	userAgents := viper.GetStringSlice("api_clients.pickabrick.user_agents")
	if len(userAgents) == 0 {
		// Use OS-appropriate user agents if none configured
		userAgents = throttle.GetUserAgentForClient("pickabrick")
	}

	config := throttle.Config{
		DelayMinMs:              viper.GetInt("api_clients.pickabrick.delay_min_ms"),
		DelayMaxMs:              viper.GetInt("api_clients.pickabrick.delay_max_ms"),
		MaxRequests:             viper.GetInt("api_clients.pickabrick.max_requests"),
		WindowSeconds:           viper.GetInt("api_clients.pickabrick.window_seconds"),
		MaxAttempts:             viper.GetInt("api_clients.pickabrick.retry.max_attempts"),
		InitialBackoffMs:        viper.GetInt("api_clients.pickabrick.retry.initial_backoff_ms"),
		MaxBackoffMs:            viper.GetInt("api_clients.pickabrick.retry.max_backoff_ms"),
		BackoffMultiplier:       viper.GetFloat64("api_clients.pickabrick.retry.backoff_multiplier"),
		UserAgents:              userAgents,
		AdaptiveEnabled:         viper.GetBool("api_clients.pickabrick.adaptive.enabled"),
		BaselineResponseTimeMs:  viper.GetInt("api_clients.pickabrick.adaptive.baseline_response_time_ms"),
		SlowThresholdMultiplier: viper.GetFloat64("api_clients.pickabrick.adaptive.slow_threshold_multiplier"),
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		useMocks:       false,
		throttler:      throttle.New("pickabrick", config),
		imageCache:     newImageValidationCache(),
		validateImages: viper.GetBool("api_clients.pickabrick.validate_cdn_images"),
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

// ValidateCDNImageURL checks if a CDN image URL exists using a HEAD request.
// Results are cached to avoid repeated validation attempts.
// Returns true if the image exists, false otherwise.
func (c *Client) ValidateCDNImageURL(url string, elementID string) bool {
	// Check cache first
	if exists, found := c.imageCache.Get(elementID); found {
		return exists
	}

	// Perform HEAD request to check if image exists
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		c.imageCache.Set(elementID, false)
		return false
	}

	// Use a separate client with short timeout for validation
	validationClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := validationClient.Do(req)
	if err != nil {
		c.imageCache.Set(elementID, false)
		return false
	}
	defer resp.Body.Close()

	// Consider 200 OK as existing, anything else as not existing
	exists := resp.StatusCode == http.StatusOK
	c.imageCache.Set(elementID, exists)
	return exists
}
