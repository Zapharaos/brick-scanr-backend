package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/go-redsync/redsync/v4"
	"go.uber.org/zap"
)

// BuildLockKey creates a consistent lock key for any Redis key
func BuildLockKey(key string) string {
	return fmt.Sprintf("lock:%s", key)
}

// AcquireRedisLock creates and acquires a distributed lock for a given key
// Returns the mutex which must be unlocked by the caller using defer
// Lock configuration (expiry, retry delay, tries) is loaded from redis.lock config
func AcquireRedisLock(ctx context.Context, lockKey string) (*redsync.Mutex, error) {
	lockConfig := database.DB().Redis().Lock

	/*zap.L().Debug("attempting to acquire redis lock",
		zap.String("lock_key", lockKey),
		zap.Duration("expiry", lockConfig.Expiry),
		zap.Duration("retry_delay", lockConfig.RetryDelay),
		zap.Int("tries", lockConfig.Tries),
	)*/

	mutex := database.DB().Redis().Redsync.NewMutex(lockKey,
		redsync.WithExpiry(lockConfig.Expiry),
		redsync.WithRetryDelay(lockConfig.RetryDelay),
		redsync.WithTries(lockConfig.Tries),
	)

	startTime := time.Now()
	if err := mutex.LockContext(ctx); err != nil {
		duration := time.Since(startTime)
		zap.L().Error("failed to acquire redis lock after retries",
			zap.String("lock_key", lockKey),
			zap.Duration("wait_time", duration),
			zap.Duration("expiry", lockConfig.Expiry),
			zap.Int("tries", lockConfig.Tries),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to acquire lock %s after %v: %w", lockKey, duration, err)
	}

	duration := time.Since(startTime)
	if duration > lockConfig.RetryDelay*time.Duration(lockConfig.Tries)/2 {
		zap.L().Warn("slow lock acquisition detected",
			zap.String("lock_key", lockKey),
			zap.Duration("wait_time", duration),
		)
	}

	/*zap.L().Debug("successfully acquired redis lock",
		zap.String("lock_key", lockKey),
		zap.Duration("wait_time", duration),
	)*/

	return mutex, nil
}

// ReleaseRedisLock safely releases a distributed lock
func ReleaseRedisLock(ctx context.Context, mutex *redsync.Mutex, lockKey string) {
	if mutex == nil {
		zap.L().Warn("attempted to release nil mutex",
			zap.String("lock_key", lockKey),
		)
		return
	}

	ok, err := mutex.UnlockContext(ctx)
	if err != nil {
		zap.L().Error("failed to release distributed lock",
			zap.Error(err),
			zap.String("lock_key", lockKey),
			zap.Bool("unlock_ok", ok),
		)
		return
	}

	if !ok {
		zap.L().Warn("lock was not held or already expired when releasing",
			zap.String("lock_key", lockKey),
		)
	} else {
		/*zap.L().Debug("successfully released redis lock",
			zap.String("lock_key", lockKey),
		)*/
	}
}
