package database

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type RedisDB struct {
	Client  *redis.Client
	Redsync *redsync.Redsync
	TTLS    TTLS
}

type TTLS struct {
	Set        time.Duration
	SetPrice   time.Duration
	Brick      time.Duration
	BrickPrice time.Duration
}

// NewRedisDB creates a new Redis client.
func NewRedisDB() RedisDB {
	zap.L().Info("Connecting to Redis...")

	// Connect to Redis
	host := viper.GetString("redis.host")
	port := viper.GetInt("redis.port")
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
		PoolSize: viper.GetInt("redis.pool_size"),
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		zap.L().Error("Failed to connect to Redis", zap.Error(err))
		return RedisDB{
			Client:  nil,
			Redsync: nil,
		}
	}

	// Initialize Redsync for distributed locking
	pool := goredis.NewPool(client)
	rs := redsync.New(pool)

	// Load TTL settings, converted from seconds to time.Duration
	ttls := TTLS{
		Set:        viper.GetDuration("redis.ttls.set") * time.Second,
		SetPrice:   viper.GetDuration("redis.ttls.set_price") * time.Second,
		Brick:      viper.GetDuration("redis.ttls.brick") * time.Second,
		BrickPrice: viper.GetDuration("redis.ttls.brick_price") * time.Second,
	}

	zap.L().Info("Connected to Redis")

	return RedisDB{
		Client:  client,
		Redsync: rs,
		TTLS:    ttls,
	}
}

// IsHealthy checks if the Redis connection is healthy by running a ping command.
func (r RedisDB) IsHealthy() bool {
	// No Redis connection
	if r.Client == nil {
		return false
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to ping Redis
	_, err := r.Client.Ping(ctx).Result()
	return err == nil
}

// Close closes the Redis connection.
func (r RedisDB) Close() {
	if r.Client != nil {
		err := r.Client.Close()
		if err != nil {
			zap.L().Error("Failed to close Redis connection", zap.Error(err))
		}
	}
}
