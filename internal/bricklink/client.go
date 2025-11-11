package bricklink

import (
	"net/http"
	"time"
)

// Client handles all BrickLink API interactions
type Client struct {
	httpClient *http.Client
	useMocks   bool
}

// NewClient creates a new BrickLink client instance
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		useMocks: false,
	}
}

// NewClientWithMocks creates a new BrickLink client instance with mock mode enabled
func NewClientWithMocks() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		useMocks: true,
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
