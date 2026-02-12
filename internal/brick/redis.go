package brick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// RedisBuildKey constructs a Redis key for a given ElementID and tag
func RedisBuildKey(elementID ElementID, tag language.Tag) string {
	return fmt.Sprintf("brick:%s:%s", elementID, tag.String())
}

// RedisGet retrieves a Brick from Redis by its ElementID and tag
func RedisGet(ctx context.Context, elementID ElementID, tag language.Tag) (Locale, error) {
	key := RedisBuildKey(elementID, tag)
	data, err := redis.Get(ctx, key, true)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		zap.L().Error(
			"failed to fetch brick data from redis",
			zap.String("elementID", string(elementID)),
			zap.Error(err),
		)
		return Locale{}, err
	} else if data == "" || errors.Is(err, redis.ErrKeyNotFound) {
		return Locale{}, redis.ErrKeyNotFound
	}

	// Found cached data, unmarshal it
	var brick Locale
	if err = json.Unmarshal([]byte(data), &brick); err != nil {
		zap.L().Error(
			"failed to unmarshal cached brick data",
			zap.String("elementID", string(elementID)),
			zap.Error(err),
		)
		return Locale{}, err
	}

	return brick, nil
}

// RedisSet stores a Brick in Redis by its ElementID and tag
func RedisSet(ctx context.Context, brick Locale, tag language.Tag, updateTTL bool) error {
	id, err := brick.GetID()
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

	key := RedisBuildKey(id, tag)

	// Determine GetTTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Brick
	} else {
		ttl = redis.KeepTTL
	}

	return redis.Set(ctx, key, brickJSON, ttl, true)
}
