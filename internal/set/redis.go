package set

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
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

// SetRedisBricklinkSet stores a Set in Redis by its Bricklink ID and also maps the Set ID to the Bricklink ID
func SetRedisBricklinkSet(ctx context.Context, set Set, ttl time.Duration) error {
	// Marshal set to JSON
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return err
	}

	// Use a transactional pipeline (MULTI/EXEC)
	tx := database.DB().Redis().Client.TxPipeline()

	// Bind set ID to BrickLink ID
	// Useful to act as a step while retrieving bricklink set by our internal UUID
	keySetIDToBricklinkID := BuildKeySetIDToBricklinkID(set.Id)
	tx.Set(ctx, keySetIDToBricklinkID, set.BricklinkID, ttl)

	// Store bricklink set as JSON
	keyBricklinkIDToSet := BuildKeyBricklinkIDToSet(fmt.Sprintf("%d", set.BricklinkID))
	tx.Set(ctx, keyBricklinkIDToSet, setJSON, ttl)

	// execute transaction
	if _, err := tx.Exec(ctx); err != nil {
		zap.L().Error("failed to execute redis transaction for storing set",
			zap.Error(err),
			zap.String("set_id", set.Id.String()),
			zap.Int("bricklink_id", set.BricklinkID),
		)
		return err
	}

	return nil
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
