package set

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

// IsTTLBelowThreshold checks if a given TTL is below a specified threshold
func IsTTLBelowThreshold(ttl time.Duration, treshold time.Duration) bool {
	return ttl > 0 && ttl < treshold
}

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

// GetTtlForRedisKey retrieves the TTL for a given Redis key
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

// DeleteRedisKey deletes a Redis key, logging a warning if it fails
func DeleteRedisKey(ctx context.Context, key string) error {
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

// MustDeleteRedisKey deletes a Redis key, ignoring any errors
func MustDeleteRedisKey(ctx context.Context, key string) {
	_ = DeleteRedisKey(ctx, key)
}

// ---------------------------------------
// Brick-specific Redis functions
// ---------------------------------------

func BuildKeyBrick(brickID BrickID) string {
	return fmt.Sprintf("brick:%s", brickID)
}

// GetRedisBrick retrieves a Brick from Redis by its BrickID and currency
func GetRedisBrick(ctx context.Context, brickID BrickID) (Brick, error) {
	key := BuildKeyBrick(brickID)
	data, err := GetRedisByKey(ctx, key, true)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		zap.L().Error(
			"failed to fetch brick data from redis",
			zap.String("brickID", string(brickID)),
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
			zap.Error(err),
		)
		return Brick{}, err
	}

	return cachedBrick, nil
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

	key := BuildKeyBrick(id)

	// Determine TTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Brick
	} else {
		ttl = redis.KeepTTL
	}

	return SetRedisByKey(ctx, key, brickJSON, ttl, true)
}

// ---------------------------------------
// Set-specific Redis functions
// ---------------------------------------

// BuildKeySet creates a Redis key for looking up Set by UUID
func BuildKeySet(identifier uuid.UUID) string {
	return fmt.Sprintf("set:%s", identifier)
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

// ---------------------------------------
// Bricklink-specific Redis functions
// ---------------------------------------

// BuildKeyBricklinkIDToSetID creates a Redis key for looking up Set by Bricklink ID
func BuildKeyBricklinkIDToSetID(bricklinkID string) string {
	return fmt.Sprintf("set:bricklink:%s", bricklinkID)
}

// GetRedisSetIDByBricklinkID retrieves the set uuid.UUID from Redis by its Bricklink ID
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func GetRedisSetIDByBricklinkID(ctx context.Context, bricklinkID string) (uuid.UUID, error) {
	key := BuildKeyBricklinkIDToSetID(bricklinkID)
	data, err := GetRedisByKey(ctx, key, true) // Use lock on cache miss
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("bricklinkID", bricklinkID),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	// Found cached data, unmarshal it
	var setID uuid.UUID
	if setID, err = uuid.Parse(data); err != nil {
		zap.L().Error(
			"failed to parse set ID",
			zap.String("bricklinkID", bricklinkID),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	return setID, nil
}

// GetRedisSetByBricklinkID retrieves the Set from Redis by its Bricklink ID
// Returns the Set, its remaining TTL, or an error if not found
func GetRedisSetByBricklinkID(ctx context.Context, bricklinkID string) (Set, time.Duration, error) {
	setID, err := GetRedisSetIDByBricklinkID(ctx, bricklinkID)
	if err != nil {
		return Set{}, RedisKeyNotFound, err
	} else if setID == uuid.Nil {
		return Set{}, RedisKeyNotFound, ErrKeyNotFound
	}

	// Fetch the full set by its UUID
	set, err := GetRedisSet(ctx, setID)
	if err != nil {
		zap.L().Error(
			"failed to fetch set data from redis by set ID",
			zap.String("bricklinkID", bricklinkID),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, RedisKeyNotFound, err
	}

	// Get remaining TTL for the set key
	ttl, err := GetTtlForRedisKey(ctx, BuildKeySet(setID))
	if err != nil {
		zap.L().Error(
			"failed to get TTL for cached data",
			zap.String("bricklinkID", bricklinkID),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, RedisKeyNotFound, err
	}

	return set, ttl, nil
}

// SetRedisSetIDForBricklinkID stores the set uuid.UUID in Redis by its Bricklink ID
// Uses distributed locking to ensure only one UUID is generated per BrickLink ID across concurrent requests
func SetRedisSetIDForBricklinkID(ctx context.Context, set Set, updateTTL bool) error {
	key := BuildKeyBricklinkIDToSetID(strconv.Itoa(set.BricklinkID))

	// Marshal set to JSON
	setIdJSON, err := json.Marshal(set.Id)
	if err != nil {
		zap.L().Error("failed to marshal setID to JSON",
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

	return SetRedisByKey(ctx, key, setIdJSON, ttl, true)
}

// SetRedisSetForBricklinkID stores both the set UUID mapping and the full set data in Redis by its Bricklink ID
func SetRedisSetForBricklinkID(ctx context.Context, set Set, updateTTL bool) error {
	// First, store the set ID mapping
	if err := SetRedisSetIDForBricklinkID(ctx, set, updateTTL); err != nil {
		return err
	}

	// Then, store the full set data
	if err := SetRedisSet(ctx, set, updateTTL); err != nil {
		return err
	}

	return nil
}

// ---------------------------------------
// Slug-specific Redis functions
// ---------------------------------------

// BuildKeySlug creates a Redis key for looking up Set by slug
func BuildKeySlug(slug string) string {
	return fmt.Sprintf("set:slug:%s", slug)
}

// GetRedisSetIDBySlug retrieves the set uuid.UUID from Redis by its slug
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func GetRedisSetIDBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	key := BuildKeySlug(slug)
	data, err := GetRedisByKey(ctx, key, true) // Use lock on cache miss
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("slug", slug),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	// Found cached data, unmarshal it
	var setID uuid.UUID
	if setID, err = uuid.Parse(data); err != nil {
		zap.L().Error(
			"failed to parse set ID",
			zap.String("slug", slug),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	return setID, nil
}

// GetRedisSetBySlug retrieves the Set from Redis by its slug
// Returns the Set, its remaining TTL, or an error if not found
func GetRedisSetBySlug(ctx context.Context, slug string) (Set, time.Duration, error) {
	setID, err := GetRedisSetIDBySlug(ctx, slug)
	if err != nil {
		return Set{}, RedisKeyNotFound, err
	} else if setID == uuid.Nil {
		return Set{}, RedisKeyNotFound, ErrKeyNotFound
	}

	// Fetch the full set by its UUID
	set, err := GetRedisSet(ctx, setID)
	if err != nil {
		zap.L().Error(
			"failed to fetch set data from redis by set ID",
			zap.String("slug", slug),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, RedisKeyNotFound, err
	}

	// Get remaining TTL for the set key
	ttl, err := GetTtlForRedisKey(ctx, BuildKeySet(setID))
	if err != nil {
		zap.L().Error(
			"failed to get TTL for cached data",
			zap.String("slug", slug),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Set{}, RedisKeyNotFound, err
	}

	return set, ttl, nil
}

// SetRedisSetIDForSlug stores the set uuid.UUID in Redis by its slug
// Uses distributed locking to ensure only one UUID is generated per slug across concurrent requests
func SetRedisSetIDForSlug(ctx context.Context, set Set, updateTTL bool) error {
	key := BuildKeySlug(set.Slug)

	// Marshal set to JSON
	setIdJSON, err := json.Marshal(set.Id)
	if err != nil {
		zap.L().Error("failed to marshal setID to JSON",
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

	return SetRedisByKey(ctx, key, setIdJSON, ttl, true)
}
