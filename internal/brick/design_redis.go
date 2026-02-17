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

// RedisBuildKeyDesign constructs a Redis key for a given DesignID and tag
func RedisBuildKeyDesign(designID DesignID, tag language.Tag) string {
	return fmt.Sprintf("brick:design:%s:%s", designID, tag.String())
}

// RedisGetDesign retrieves a Brick from Redis by its ElementID and tag
func RedisGetDesign(ctx context.Context, designID DesignID, tag language.Tag) (Design, error) {
	key := RedisBuildKeyDesign(designID, tag)
	data, err := redis.Get(ctx, key, true)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		zap.L().Error(
			"failed to fetch design data from redis",
			zap.String("designID", string(designID)),
			zap.Error(err),
		)
		return Design{}, err
	} else if data == "" || errors.Is(err, redis.ErrKeyNotFound) {
		return Design{}, redis.ErrKeyNotFound
	}

	// Found cached data, unmarshal it
	var design Design
	if err = json.Unmarshal([]byte(data), &design); err != nil {
		zap.L().Error(
			"failed to unmarshal cached design data",
			zap.String("designID", string(designID)),
			zap.Error(err),
		)
		return Design{}, err
	}

	return design, nil
}

// RedisSetDesign stores a Brick in Redis by its ElementID and tag
func RedisSetDesign(ctx context.Context, design Design, tag language.Tag, updateTTL bool) error {
	// Marshal brick to JSON
	brickJSON, err := json.Marshal(design)
	if err != nil {
		zap.L().Error("failed to marshal design to JSON",
			zap.Error(err),
			zap.String("designID", string(design.ID.DesignID)),
		)
		return err
	}

	key := RedisBuildKeyDesign(design.ID.DesignID, tag)

	// Determine GetTTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Brick
	} else {
		ttl = redis.KeepTTL
	}

	return redis.Set(ctx, key, brickJSON, ttl, true)
}
