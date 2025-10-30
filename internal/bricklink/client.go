package bricklink

import (
	"net/http"
	"time"
)

// Client handles all BrickLink API interactions
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new BrickLink client instance
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}
