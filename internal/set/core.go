package set

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Core represents the core information of a Lego set.
type Core struct {
	ID          uuid.UUID `json:"id"`
	Number      string    `json:"number"`
	NameDefault string    `json:"name_default"`
	SlugDefault string    `json:"slug_default"`

	// Details from Bricklink
	Parts           int     `json:"parts"`
	ImageURL        string  `json:"image_url"`
	YearReleased    int     `json:"year_released"`
	BricklinkID     int     `json:"bricklink_id"`
	BricklinkNumber string  `json:"bricklink_number"`
	Bricks          []Brick `json:"bricks"`
}

// SetBricks sets the Bricks for the Core, optionally sorting them by their Index field.
func (c *Core) SetBricks(bricks []Brick, sort bool) {
	workingBricks := make([]Brick, len(bricks))
	copy(workingBricks, bricks)

	// Sort bricks by their Index field if requested before setting them to the set
	if sort {
		SortBricksByIndex(workingBricks)
	}

	// Reset each brick's locale information down to core before setting it to the set
	for i, b := range workingBricks {
		b.ResetDownToInventoryCore()
		workingBricks[i] = b
	}

	c.Bricks = workingBricks
}

// RedisBuildKeyCore creates a Redis key for looking up Core by UUID
func RedisBuildKeyCore(identifier uuid.UUID) string {
	return fmt.Sprintf("set:%s", identifier)
}

// RedisGetCore retrieves a Set from Redis by its UUID
func RedisGetCore(ctx context.Context, setID uuid.UUID) (Core, error) {
	key := RedisBuildKeyCore(setID)
	data, err := redis.Get(ctx, key, true)
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Core{}, err
	}

	// Found cached data, unmarshal it
	var cache Core
	if err = json.Unmarshal([]byte(data), &cache); err != nil {
		zap.L().Error(
			"failed to unmarshal cached data",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Core{}, err
	}

	return cache, nil
}

// RedisSetCore stores a Set in Redis by its UUID
func RedisSetCore(ctx context.Context, set Core, updateTTL bool) error {
	key := RedisBuildKeyCore(set.ID)

	// Marshal set to JSON
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
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

	return redis.Set(ctx, key, setJSON, ttl, true)
}

// RedisSetCoreForBricklinkID stores both the set UUID mapping and the set Core data in Redis by its Bricklink ID
func RedisSetCoreForBricklinkID(ctx context.Context, set Core, updateTTL bool) error {
	coreKey := RedisBuildKeyCore(set.ID)
	lockKey := redis.BuildLockKey(coreKey)

	// Determine TTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Set
	} else {
		ttl = redis.KeepTTL
	}

	// Prepare BricklinkID mapping data
	bricklinkKey := RedisBuildKeyBricklinkIDToSetID(fmt.Sprintf("%d", set.BricklinkID))
	setIDJSON, err := json.Marshal(set.ID)
	if err != nil {
		zap.L().Error("failed to marshal setID to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
		)
		return err
	}

	// Prepare Core data
	coreJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
		)
		return err
	}

	// Execute transaction to set both keys atomically
	return redis.Transaction(ctx, lockKey, true, func(pipe goredis.Pipeliner) error {
		pipe.Set(ctx, bricklinkKey, setIDJSON, ttl)
		pipe.Set(ctx, coreKey, coreJSON, ttl)
		return nil
	})
}
