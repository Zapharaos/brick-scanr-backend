package set

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/go-redsync/redsync/v4"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const RedisKeyNotFound = -1

var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrFailedToGetKey   = errors.New("failed to get key")
	ErrFailedToSetKey   = errors.New("failed to set key")
	ErrFailedToCheckKey = errors.New("failed to check key existence")
	ErrFailedToGetTTL   = errors.New("failed to get TTL")
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

	zap.L().Debug("attempting to acquire redis lock",
		zap.String("lock_key", lockKey),
		zap.Duration("expiry", lockConfig.Expiry),
		zap.Duration("retry_delay", lockConfig.RetryDelay),
		zap.Int("tries", lockConfig.Tries),
	)

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

	zap.L().Debug("successfully acquired redis lock",
		zap.String("lock_key", lockKey),
		zap.Duration("wait_time", duration),
	)

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
		zap.L().Debug("successfully released redis lock",
			zap.String("lock_key", lockKey),
		)
	}
}

// GetRedisByKey retrieves a value from Redis by key
// If useLock is true, acquires a distributed lock when key is not found to ensure consistency during concurrent writes
func GetRedisByKey(ctx context.Context, key string, useLock bool) (string, error) {
	value, err := database.DB().Redis().Client.Get(ctx, key).Result()

	if err != nil && errors.Is(err, redis.Nil) {
		// Key not found
		if !useLock {
			return "", ErrKeyNotFound
		}

		// Acquire lock to prevent race with concurrent write
		lockKey := BuildLockKey(key)
		mutex, lockErr := AcquireRedisLock(ctx, lockKey)
		if lockErr != nil {
			zap.L().Error(
				"failed to acquire lock for redis key read",
				zap.String("key", key),
				zap.Error(lockErr),
			)
			return "", lockErr
		}
		defer ReleaseRedisLock(ctx, mutex, lockKey)

		// Double-check after acquiring lock
		value, err = database.DB().Redis().Client.Get(ctx, key).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return "", ErrKeyNotFound
			}
			return "", fmt.Errorf("%w: %v", ErrFailedToGetKey, err)
		}

		return value, nil
	} else if err != nil {
		// Other Redis error
		return "", fmt.Errorf("%w: %v", ErrFailedToGetKey, err)
	}

	// Key exists, return its value
	return value, nil
}

// SetRedisByKeyCustom executes a custom Redis operation with optional distributed locking
// If useLock is true, acquires a distributed lock before executing the custom operation
// The customOp function should perform the actual Redis operations and return an error if any
func SetRedisByKeyCustom(ctx context.Context, key string, useLock bool, customOp func(context.Context) error) error {
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

// SetRedisByKey stores a value in Redis by key with optional TTL
// If useLock is true, acquires a distributed lock to ensure consistency during concurrent operations
// If ttl is 0, uses redis.KeepTTL to maintain existing TTL
func SetRedisByKey(ctx context.Context, key string, value interface{}, ttl time.Duration, useLock bool) error {
	return SetRedisByKeyCustom(ctx, key, useLock, func(ctx context.Context) error {
		err := database.DB().Redis().Client.Set(ctx, key, value, ttl).Err()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFailedToSetKey, err)
		}
		return nil
	})
}

func GetTtlForRedisKey(ctx context.Context, key string) (time.Duration, error) {
	// Retrieve the key from Redis
	exists, err := database.DB().Redis().Client.Exists(ctx, key).Result()
	if err != nil {
		return RedisKeyNotFound, fmt.Errorf("%w: %v", ErrFailedToCheckKey, err)
	}

	// Check if the key exists
	if exists == 1 {
		// If key exists, get its expiration time
		ttl, err := database.DB().Redis().Client.TTL(ctx, key).Result()
		if err != nil {
			return RedisKeyNotFound, fmt.Errorf("%w: %v", ErrFailedToGetTTL, err)
		}

		return ttl, nil
	}

	return RedisKeyNotFound, nil
}

func CleanupRedisKey(ctx context.Context, key string) {
	if err := database.DB().Redis().Client.Del(ctx, key).Err(); err != nil {
		zap.L().Warn("failed to cleanup after failure", zap.Error(err))
	}
}

// BuildKeySet creates a Redis key for looking up Set by UUID
func BuildKeySet(identifier uuid.UUID) string {
	return fmt.Sprintf("set:%s", identifier)
}

// BuildKeyBricklinkIDToSet creates a Redis key for looking up Set by Bricklink ID
func BuildKeyBricklinkIDToSet(bricklinkID string) string {
	return fmt.Sprintf("set:bricklink:%s", bricklinkID)
}

// BuildKeySetIDToBricklinkID creates a Redis key for looking up Bricklink ID by Set ID
func BuildKeySetIDToBricklinkID(setID uuid.UUID) string {
	return fmt.Sprintf("set:%s:bricklink", setID)
}

func BuildKeyBrick(brickID BrickID, designID DesignID) string {
	return fmt.Sprintf("brick:%s:%s", designID, brickID)
}

// GetRedisSet retrieves a Set from Redis by its UUID
func GetRedisSet(ctx context.Context, setID uuid.UUID) (Set, error) {
	key := BuildKeySet(setID)
	data, err := GetRedisByKey(ctx, key, true)
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, err
	}

	// Found cached data, unmarshal it
	var cachedSet Set
	if err = json.Unmarshal([]byte(data), &cachedSet); err != nil {
		zap.L().Error(
			"failed to unmarshal cached data",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, err
	}

	return cachedSet, nil
}

// GetRedisBricklinkID retrieves a Bricklink ID from Redis by Set ID
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func GetRedisBricklinkID(ctx context.Context, setID uuid.UUID) (string, error) {
	key := BuildKeySetIDToBricklinkID(setID)
	data, err := GetRedisByKey(ctx, key, true) // Use lock on cache miss
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return "", err
	}

	return data, nil
}

// GetRedisBricklinkSet retrieves a Set from Redis by its Bricklink ID
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func GetRedisBricklinkSet(ctx context.Context, bricklinkID string) (Set, error) {
	key := BuildKeyBricklinkIDToSet(bricklinkID)
	data, err := GetRedisByKey(ctx, key, true) // Use lock on cache miss
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("bricklinkID", bricklinkID),
			zap.Error(err),
		)
		return Set{}, err
	}

	// Found cached data, unmarshal it
	var cachedSet Set
	if err = json.Unmarshal([]byte(data), &cachedSet); err != nil {
		zap.L().Error(
			"failed to unmarshal cached data",
			zap.String("bricklinkID", bricklinkID),
			zap.Error(err),
		)
		return Set{}, err
	}

	return cachedSet, nil
}

// GetRedisBricklinkSetFromSetID retrieves a Set from Redis by its Set ID using the Bricklink ID
func GetRedisBricklinkSetFromSetID(ctx context.Context, setID uuid.UUID) (Set, error) {
	// Retrieve BrickLink set ID from Redis
	bricklinkID, err := GetRedisBricklinkID(ctx, setID)
	if err != nil {
		return Set{}, err
	}

	cachedSet, err := GetRedisBricklinkSet(ctx, bricklinkID)
	if err != nil {
		return Set{}, err
	}

	return cachedSet, nil
}

// GetRedisBrick retrieves a Brick from Redis by its BrickID and currency
func GetRedisBrick(ctx context.Context, brickID BrickID, designID DesignID) (Brick, error) {
	key := BuildKeyBrick(brickID, designID)
	data, err := GetRedisByKey(ctx, key, true)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		zap.L().Error(
			"failed to fetch brick data from redis",
			zap.String("brickID", string(brickID)),
			zap.String("designID", string(designID)),
			zap.Error(err),
		)
		return Brick{}, err
	} else if data == "" || errors.Is(err, ErrKeyNotFound) {
		return Brick{}, ErrKeyNotFound
	}

	// Found cached data, unmarshal it
	var cachedBrick Brick
	if err = json.Unmarshal([]byte(data), &cachedBrick); err != nil {
		zap.L().Error(
			"failed to unmarshal cached brick data",
			zap.String("brickID", string(brickID)),
			zap.String("designID", string(designID)),
			zap.Error(err),
		)
		return Brick{}, err
	}

	return cachedBrick, nil
}

// SetRedisBricklinkSet stores a Set in Redis by its Bricklink ID and also maps the Set ID to the Bricklink ID
// Uses distributed locking to ensure only one UUID is generated per BrickLink ID across concurrent requests
// Returns the final Set (with consistent UUID) and a boolean indicating if this call created the entry
func SetRedisBricklinkSet(ctx context.Context, set Set) (Set, bool, error) {
	bricklinkID := fmt.Sprintf("%d", set.BricklinkID)
	key := BuildKeyBricklinkIDToSet(bricklinkID)

	var resultSet Set
	var created bool

	err := SetRedisByKeyCustom(ctx, key, true, func(ctx context.Context) error {
		// Now that we have the lock, check if the set already exists
		// IMPORTANT: Use useLock=false here because we already hold the lock!
		data, err := GetRedisByKey(ctx, key, false)

		if err == nil {
			// Set already exists (another goroutine created it while we were waiting for the lock)
			var existingSet Set
			if unmarshalErr := json.Unmarshal([]byte(data), &existingSet); unmarshalErr != nil {
				zap.L().Error("failed to unmarshal existing set",
					zap.Error(unmarshalErr),
					zap.Int("bricklink_id", set.BricklinkID),
				)
				return unmarshalErr
			}

			zap.L().Debug("Set already exists in cache, using existing UUID",
				zap.String("existing_id", existingSet.Id.String()),
				zap.String("new_id", set.Id.String()),
				zap.Int("bricklink_id", set.BricklinkID),
			)
			resultSet = existingSet
			created = false
			return nil
		} else if !errors.Is(err, ErrKeyNotFound) {
			// Unexpected error
			return err
		}

		// Set doesn't exist - create both mappings atomically using a transaction
		setJSON, err := json.Marshal(set)
		if err != nil {
			zap.L().Error("failed to marshal set to JSON",
				zap.Error(err),
				zap.String("set_id", set.Id.String()),
				zap.Int("bricklink_id", set.BricklinkID),
			)
			return err
		}

		// Use a transactional pipeline (MULTI/EXEC) to ensure both mappings are created atomically
		tx := database.DB().Redis().Client.TxPipeline()

		// Store BrickLink ID -> Set mapping
		keyBricklinkIDToSet := BuildKeyBricklinkIDToSet(bricklinkID)
		tx.Set(ctx, keyBricklinkIDToSet, setJSON, database.DB().Redis().TTLS.Set)

		// Store Set ID -> BrickLink ID mapping (for reverse lookup)
		keySetIDToBricklinkID := BuildKeySetIDToBricklinkID(set.Id)
		tx.Set(ctx, keySetIDToBricklinkID, set.BricklinkID, database.DB().Redis().TTLS.Set)

		// Execute transaction
		if _, err := tx.Exec(ctx); err != nil {
			zap.L().Error("failed to execute redis transaction for storing set",
				zap.Error(err),
				zap.String("set_id", set.Id.String()),
				zap.Int("bricklink_id", set.BricklinkID),
			)
			return err
		}

		zap.L().Debug("Successfully created new set in cache",
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)

		resultSet = set
		created = true
		return nil
	})

	if err != nil {
		return set, false, err
	}

	return resultSet, created, nil
}

// SetRedisSet stores a Set in Redis by its UUID
func SetRedisSet(ctx context.Context, set Set, updateTTL bool) error {
	key := BuildKeySet(set.Id)

	// Marshal set to JSON
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
		)
		return err
	}

	// Determine TTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Set
	} else {
		ttl = redis.KeepTTL
	}

	return SetRedisByKey(ctx, key, setJSON, ttl, true)
}

// SetRedisBrick stores a Brick in Redis by its BrickID and currency
func SetRedisBrick(ctx context.Context, brick Brick, updateTTL bool) error {
	id, err := brick.GetBrickIDForRedis()
	if err != nil {
		zap.L().Error("failed to get brick ID for redis",
			zap.Error(err),
		)
		return err
	}

	// Clean up any set related fields
	brick.Index = 0
	brick.Quantity = 0

	// Marshal brick to JSON
	brickJSON, err := json.Marshal(brick)
	if err != nil {
		zap.L().Error("failed to marshal brick to JSON",
			zap.Error(err),
			zap.String("brickID", string(id)),
			zap.String("designID", string(brick.DesignID)),
		)
		return err
	}

	key := BuildKeyBrick(id, brick.DesignID)

	// Determine TTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Brick
	} else {
		ttl = redis.KeepTTL
	}

	return SetRedisByKey(ctx, key, brickJSON, ttl, true)
}
