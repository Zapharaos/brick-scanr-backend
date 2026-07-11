package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Get retrieves a value from Redis by key
func Get(ctx context.Context, key string) (string, error) {
	value, err := database.DB().Redis().Client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrKeyNotFound
		}
		return "", fmt.Errorf("%w: %v", ErrFailedToGetKey, err)
	}
	return value, nil
}

// SetCustom executes a custom Redis operation with optional distributed locking
// If useLock is true, acquires a distributed lock before executing the custom operation
// The customOp function should perform the actual Redis operations and return an error if any
func SetCustom(ctx context.Context, key string, useLock bool, customOp func(context.Context) error) error {
	if !useLock {
		// Execute custom operation without lock
		return customOp(ctx)
	}

	// Acquire lock for consistent operation
	lockKey := BuildLockKey(key)
	mutex, lockErr := AcquireRedisLock(ctx, lockKey)
	if lockErr != nil {
		zap.L().Error(
			"failed to acquire lock for redis key write",
			zap.String("key", key),
			zap.Error(lockErr),
		)
		return lockErr
	}
	defer ReleaseRedisLock(ctx, mutex, lockKey)

	// Execute custom operation with lock held
	return customOp(ctx)
}

// Set stores a value in Redis by key with optional GetTTL
// If useLock is true, acquires a distributed lock to ensure consistency during concurrent operations
// If ttl is 0, uses redis.KeepTTL to maintain existing GetTTL
func Set(ctx context.Context, key string, value interface{}, ttl time.Duration, useLock bool) error {
	return SetCustom(ctx, key, useLock, func(ctx context.Context) error {
		err := database.DB().Redis().Client.Set(ctx, key, value, ttl).Err()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFailedToSetKey, err)
		}
		return nil
	})
}

// Delete deletes a Redis key, logging a warning if it fails
func Delete(ctx context.Context, key string) error {
	err := database.DB().Redis().Client.Del(ctx, key).Err()
	if err != nil {
		zap.L().Error("failed to delete redis key",
			zap.String("key", key),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// MustDelete deletes a Redis key, ignoring any errors
func MustDelete(ctx context.Context, key string) {
	_ = Delete(ctx, key)
}

// MGet retrieves multiple values from Redis by their keys using MGET command
// If allowPartial is true, returns a map of key -> value for found keys only (partial results)
// If allowPartial is false, returns ErrKeyNotFound if any key is missing
func MGet(ctx context.Context, keys []string, allowPartial bool) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}

	values, err := database.DB().Redis().Client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFailedToGetKey, err)
	}

	result := make(map[string]string)
	allFound := true
	for i, val := range values {
		if val == nil {
			// Key not found
			allFound = false
			if !allowPartial {
				return nil, ErrKeyNotFound
			}
			// Skip this key and continue with partial results
			continue
		}
		result[keys[i]] = val.(string)
	}

	// If allowPartial is false and not all keys were found, we already returned above
	// If allowPartial is true but no keys were found at all, return ErrKeyNotFound
	if allowPartial && len(result) == 0 && !allFound {
		return nil, ErrKeyNotFound
	}

	return result, nil
}

// Transaction executes multiple Redis operations atomically using a pipeline
// If useLock is true, acquires a distributed lock before executing the transaction
// The txFunc receives the pipeline and should add commands to it
func Transaction(ctx context.Context, lockKey string, useLock bool, txFunc func(goredis.Pipeliner) error) error {
	if !useLock {
		// Execute transaction without lock
		pipe := database.DB().Redis().Client.Pipeline()
		if err := txFunc(pipe); err != nil {
			return err
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to execute transaction: %w", err)
		}
		return nil
	}

	// Acquire lock for consistent operation
	mutex, lockErr := AcquireRedisLock(ctx, lockKey)
	if lockErr != nil {
		zap.L().Error(
			"failed to acquire lock for redis transaction",
			zap.String("lock_key", lockKey),
			zap.Error(lockErr),
		)
		return lockErr
	}
	defer ReleaseRedisLock(ctx, mutex, lockKey)

	// Execute transaction with lock held
	pipe := database.DB().Redis().Client.Pipeline()
	if err := txFunc(pipe); err != nil {
		return err
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}
	return nil
}
