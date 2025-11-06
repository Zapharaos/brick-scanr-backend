package app

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
)

// InitRedis init the redis database.
func InitRedis() {
	// Connect to Redis
	redis := database.NewRedisDB()

	// Verify connection
	if redis.IsHealthy() {
		database.DB().SetRedis(redis)
	}
}
