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
	ErrFailedToCheckKey = errors.New("failed to check key existence")
	ErrFailedToGetTTL   = errors.New("failed to get TTL")
)

func GetRedisByKey(ctx context.Context, key string) (string, error) {
	value, err := database.DB().Redis().Client.Get(ctx, key).Result()
	if err != nil {
		// Differentiate between not found and other errors
		if errors.Is(err, redis.Nil) {
			return "", ErrKeyNotFound
		}
		return "", fmt.Errorf("%w: %v", ErrFailedToGetKey, err)
	}

	// If the key exists, return its value
	return value, nil
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
	return fmt.Sprintf("brick:%s:%s", brickID, designID)
}

// GetRedisSet retrieves a Set from Redis by its UUID
func GetRedisSet(ctx context.Context, setID uuid.UUID) (Set, error) {
	key := BuildKeySet(setID)
	data, err := GetRedisByKey(ctx, key)
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
func GetRedisBricklinkID(ctx context.Context, setID uuid.UUID) (string, error) {
	key := BuildKeySetIDToBricklinkID(setID)
	data, err := GetRedisByKey(ctx, key)
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
func GetRedisBricklinkSet(ctx context.Context, bricklinkID string) (Set, error) {
	key := BuildKeyBricklinkIDToSet(bricklinkID)
	data, err := GetRedisByKey(ctx, key)
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
	data, err := GetRedisByKey(ctx, key)
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
func SetRedisBricklinkSet(ctx context.Context, set Set, ttl time.Duration) (Set, bool, error) {
	bricklinkID := fmt.Sprintf("%d", set.BricklinkID)

	// Create a distributed lock for this specific BrickLink ID
	// This ensures only one goroutine can create the set for this BrickLink ID at a time
	lockKey := fmt.Sprintf("lock:%s", BuildKeyBricklinkIDToSet(bricklinkID))
	mutex := database.DB().Redis().Redsync.NewMutex(lockKey,
		// Lock expires after 5 seconds to prevent deadlocks
		redsync.WithExpiry(5*time.Second),
		// Retry acquiring the lock every 100ms
		redsync.WithRetryDelay(100*time.Millisecond),
		// Try for up to 3 seconds total
		redsync.WithTries(30),
	)

	// Acquire the lock
	if err := mutex.LockContext(ctx); err != nil {
		zap.L().Error("failed to acquire distributed lock for BrickLink set",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return set, false, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() {
		if _, err := mutex.UnlockContext(ctx); err != nil {
			zap.L().Warn("failed to release distributed lock",
				zap.Error(err),
				zap.String("bricklink_id", bricklinkID),
			)
		}
	}()

	// Now that we have the lock, check if the set already exists
	existingSet, err := GetRedisBricklinkSet(ctx, bricklinkID)
	if err == nil {
		// Set already exists (another goroutine created it while we were waiting for the lock)
		zap.L().Debug("Set already exists in cache, using existing UUID",
			zap.String("existing_id", existingSet.Id.String()),
			zap.String("new_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return existingSet, false, nil
	} else if !errors.Is(err, ErrKeyNotFound) {
		// Unexpected error
		return set, false, err
	}

	// Set doesn't exist - create both mappings atomically using a transaction
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return set, false, err
	}

	// Use a transactional pipeline (MULTI/EXEC) to ensure both mappings are created atomically
	tx := database.DB().Redis().Client.TxPipeline()

	// Store BrickLink ID -> Set mapping
	keyBricklinkIDToSet := BuildKeyBricklinkIDToSet(bricklinkID)
	tx.Set(ctx, keyBricklinkIDToSet, setJSON, ttl)

	// Store Set ID -> BrickLink ID mapping (for reverse lookup)
	keySetIDToBricklinkID := BuildKeySetIDToBricklinkID(set.Id)
	tx.Set(ctx, keySetIDToBricklinkID, set.BricklinkID, ttl)

	// Execute transaction
	if _, err := tx.Exec(ctx); err != nil {
		zap.L().Error("failed to execute redis transaction for storing set",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return set, false, err
	}

	zap.L().Debug("Successfully created new set in cache",
		zap.String("set_id", set.Id.String()),
		zap.Int("bricklink_id", set.BricklinkID),
	)

	return set, true, nil
}

func SetRedisSet(ctx context.Context, set Set, ttl time.Duration) error {
	// Marshal set to JSON
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
		)
		return err
	}

	key := BuildKeySet(set.Id)
	err = database.DB().Redis().Client.Set(ctx, key, setJSON, ttl).Err()
	if err != nil {
		return err
	}
	return nil
}

// SetRedisBrick stores a Brick in Redis by its BrickID and currency
func SetRedisBrick(ctx context.Context, brick Brick, ttl time.Duration) error {
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

	key := BuildKeyBrick(id, brick.DesignID)
	err = database.DB().Redis().Client.Set(ctx, key, brickJSON, ttl).Err()
	if err != nil {
		return err
	}
	return nil
}
