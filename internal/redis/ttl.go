package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
)

// IsTTLBelowThreshold checks if a given GetTTL is below a specified threshold
func IsTTLBelowThreshold(ttl time.Duration, treshold time.Duration) bool {
	return ttl > 0 && ttl < treshold
}

// GetTTL retrieves the GetTTL for a given Redis key
func GetTTL(ctx context.Context, key string) (time.Duration, error) {
	// Retrieve the key from Redis
	exists, err := database.DB().Redis().Client.Exists(ctx, key).Result()
	if err != nil {
		return KeyNotFound, fmt.Errorf("%w: %v", ErrFailedToCheckKey, err)
	}

	// Check if the key exists
	if exists == 1 {
		// If key exists, get its expiration time
		ttl, err := database.DB().Redis().Client.TTL(ctx, key).Result()
		if err != nil {
			return KeyNotFound, fmt.Errorf("%w: %v", ErrFailedToGetTTL, err)
		}

		return ttl, nil
	}

	return KeyNotFound, nil
}
