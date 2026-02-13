package set

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// NewCoreFromBricklinkSearchItem maps a Bricklink search item to an internal Core representation
func NewCoreFromBricklinkSearchItem(bs bricklink.SearchItem) (Core, error) {
	// Assign a local UUID to each set
	setId, err := uuid.NewUUID()
	if err != nil {
		return Core{}, err
	}

	// Map to internal set representation
	return Core{
		ID:              setId,
		BricklinkName:   bs.StrItemName,
		BricklinkID:     bs.IDItem,
		BricklinkNumber: bs.StrItemNo,
	}, nil
}

// RedisBuildKeyBricklinkIDToSetID creates a Redis key for looking up Set by Bricklink ID
func RedisBuildKeyBricklinkIDToSetID(bricklinkID string) string {
	return fmt.Sprintf("set:bricklink:%s", bricklinkID)
}

// RedisGetSetIDByBricklinkID retrieves the set uuid.UUID from Redis by its Bricklink ID
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func RedisGetSetIDByBricklinkID(ctx context.Context, bricklinkID string) (uuid.UUID, error) {
	key := RedisBuildKeyBricklinkIDToSetID(bricklinkID)
	data, err := redis.Get(ctx, key, true) // Use lock on cache miss
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

// RedisSetSetIDForBricklinkID stores the set uuid.UUID in Redis by its Bricklink ID
// Uses distributed locking to ensure only one UUID is generated per BrickLink ID across concurrent requests
func RedisSetSetIDForBricklinkID(ctx context.Context, set Core, updateTTL bool) error {
	key := RedisBuildKeyBricklinkIDToSetID(strconv.Itoa(set.BricklinkID))

	// Marshal set to JSON
	setIdJSON, err := json.Marshal(set.ID)
	if err != nil {
		zap.L().Error("failed to marshal setID to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
		)
		return err
	}

	// Determine GetTTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Set
	} else {
		ttl = redis.KeepTTL
	}

	return redis.Set(ctx, key, setIdJSON, ttl, true)
}
