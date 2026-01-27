package setruntime

import (
	"context"
	"fmt"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// CacheCheckResult represents the result of checking cached set data
type CacheCheckResult struct {
	// Status indicates what action should be taken
	Status CacheStatus
	// Set contains the cached set data (if available)
	Set set.Set
	// BricksNeedingPrices contains bricks that need price updates for the requested currency
	BricksNeedingPrices []set.Brick
	// FullBricks contains all bricks with their full data
	FullBricks []set.Brick
}

type CacheStatus int

const (
	// CacheStatusMissing indicates no cached data was found
	CacheStatusMissing CacheStatus = iota
	// CacheStatusFailed indicates previous fetch failed
	CacheStatusFailed
	// CacheStatusComplete indicates data is fully cached and ready
	CacheStatusComplete
	// CacheStatusNeedsPrices indicates data is cached but needs price updates
	CacheStatusNeedsPrices
	// CacheStatusNeedsRefetch indicates cached data is stale and needs complete refetch
	CacheStatusNeedsRefetch
	// CacheStatusFetching indicates data is currently being fetched
	CacheStatusFetching
)

// CheckCachedSetData checks Redis cache for set data and determines what action is needed
func CheckCachedSetData(ctx context.Context, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	// Try to get cached set
	cachedSet, err := set.GetRedisSet(ctx, setID)
	if err != nil {
		return &CacheCheckResult{Status: CacheStatusMissing}, nil
	}

	// Check fetch status
	switch cachedSet.FetchStatus {
	case set.FetchStatusFailed:
		zap.L().Warn("Previous set details fetch failed",
			zap.String("set_id", setID.String()),
		)
		return &CacheCheckResult{
			Status: CacheStatusFailed,
			Set:    cachedSet,
		}, nil

	case set.FetchStatusCompleted:
		return checkCompletedSetCache(ctx, cachedSet, setID, currency)

	default:
		// TODO : make sure that new user joining ongoing fetch doesnt miss out on data
		// FetchStatusPending or FetchStatusFetching
		zap.L().Info("Set details currently being fetched",
			zap.String("set_id", setID.String()),
		)
		return &CacheCheckResult{
			Status: CacheStatusFetching,
			Set:    cachedSet,
		}, nil
	}
}

// checkCompletedSetCache validates completed cached data and checks for missing prices
func checkCompletedSetCache(ctx context.Context, cachedSet set.Set, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	zap.L().Info("Set details found in cache, checking bricks and prices",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
	)

	fullBricks := make([]set.Brick, 0, len(cachedSet.Bricks))
	bricksNeedingPrices := make([]set.Brick, 0)

	// For each brick in the set, retrieve full data from cache and check for missing prices
	for idx, brickMin := range cachedSet.Bricks {
		// Get brick ID for Redis lookup
		brickID, err := brickMin.GetBrickIDForRedis()
		if err != nil {
			zap.L().Error("Failed to get brick ID for Redis",
				zap.Error(err),
			)
			continue
		}

		// Try to find brick in cache
		brick, err := set.GetRedisBrick(ctx, brickID, brickMin.DesignID)
		if err != nil {
			// Brick cache expired - set data is stale
			zap.L().Warn("Brick cache expired, set data is stale",
				zap.String("brick_id", string(brickID)),
				zap.String("design_id", string(brickMin.DesignID)),
				zap.String("set_id", setID.String()),
			)
			return &CacheCheckResult{
				Status: CacheStatusNeedsRefetch,
				Set:    cachedSet,
			}, nil
		}

		// Set the index to maintain order
		if brickMin.Index >= 0 {
			brick.Index = brickMin.Index
		} else {
			brick.Index = idx
		}

		// Check if we have prices
		if brick.Prices == nil || len(brick.Prices) == 0 {
			// No prices at all - product probably not available for sale
			zap.L().Debug("Brick has no prices available",
				zap.String("brick_id", string(brickID)),
				zap.String("design_id", string(brickMin.DesignID)),
			)
			fullBricks = append(fullBricks, brick)
			continue
		}

		// Try to apply the requested currency
		if !brick.ApplyCurrency(currency) {
			// Price for this currency not cached - need to fetch it
			zap.L().Debug("Brick missing price for currency",
				zap.String("brick_id", string(brickID)),
				zap.String("design_id", string(brickMin.DesignID)),
				zap.String("currency", currency.String()),
			)
			bricksNeedingPrices = append(bricksNeedingPrices, brick)
		}

		fullBricks = append(fullBricks, brick)
	}

	// Determine the result based on what we found
	if len(bricksNeedingPrices) == 0 {
		zap.L().Info("All bricks have prices for requested currency",
			zap.String("set_id", setID.String()),
			zap.String("currency", currency.String()),
		)
		cachedSet.Bricks = fullBricks
		return &CacheCheckResult{
			Status:     CacheStatusComplete,
			Set:        cachedSet,
			FullBricks: fullBricks,
		}, nil
	}

	zap.L().Info("Some bricks need price updates",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
		zap.Int("bricks_needing_prices", len(bricksNeedingPrices)),
	)

	return &CacheCheckResult{
		Status:              CacheStatusNeedsPrices,
		Set:                 cachedSet,
		FullBricks:          fullBricks,
		BricksNeedingPrices: bricksNeedingPrices,
	}, nil
}

// GetBricklinkSetFromCache retrieves BrickLink set info from cache by set ID
func GetBricklinkSetFromCache(ctx context.Context, setID uuid.UUID) (set.Set, error) {
	bricklinkSet, err := set.GetRedisBricklinkSetFromSetID(ctx, setID)
	if err != nil {
		return set.Set{}, fmt.Errorf("failed to retrieve BrickLink set from cache: %w", err)
	}
	return bricklinkSet, nil
}
