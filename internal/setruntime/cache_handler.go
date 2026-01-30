package setruntime

import (
	"context"
	"fmt"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// CacheCheckResult represents the result of checking cached set data
type CacheCheckResult struct {
	Status           CacheStatus // Status indicates what action should be taken
	Set              set.Set     // Set contains the cached set data (if available)
	SetPriceOutdated bool        // SetPriceOutdated indicates if the set price is outdated
	BricksWoPrices   []set.Brick // BricksWoPrices contains Bricks that need price updates for the requested currency
	Bricks           []set.Brick // Bricks contains all Bricks with their full data
	RsID             uuid.UUID   // RsID ID of the runtime set if applicable
}

type CacheStatus int

const (
	// CacheStatusMissing indicates no cached data was found
	CacheStatusMissing CacheStatus = iota
	// CacheStatusFailed indicates previous fetch failed
	CacheStatusFailed
	// CacheStatusComplete indicates data is fully cached and ready
	CacheStatusComplete
	// CacheStatusMissesPrices indicates data is cached but needs price updates
	CacheStatusMissesPrices
	// CacheStatusNeedsRefetch indicates cached data is stale and needs complete refetch
	CacheStatusNeedsRefetch
	// CacheStatusFetching indicates data is currently being fetched
	CacheStatusFetching
)

// CheckCachedSet checks Redis cache for set data and determines what action is needed
func (h *Handler) CheckCachedSet(ctx context.Context, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	// Search for the active runtime sets
	rss := h.FindRuntimeSetBySetId(setID)

	// No active runtime sets found, check redis cache to decide next steps
	if len(rss) == 0 {
		return checkSetInRedis(ctx, setID, currency)
	}

	// There are active runtime sets for this set ID, process them to decide next steps
	missesCachedData := false
	for _, rs := range rss {
		// Check if the runtime set is fetching with the requested currency
		if rs.Key().Currency == currency {
			switch rs.GetFetchStatus() {
			case set.FetchStatusPending, set.FetchStatusFetching:
				// Runtime is fetching or pending, let the caller join it
				zap.L().Info("Set details currently being fetched in runtime set",
					zap.String("set_id", setID.String()),
					zap.String("currency", currency.String()),
				)
				return &CacheCheckResult{
					Status: CacheStatusFetching,
					Set:    rs.GetSet(),
				}, nil
			case set.FetchStatusCompleted:
				// Data already fetched and cached by this runtime set
				zap.L().Info("Set details already fetched and cached in runtime set",
					zap.String("set_id", setID.String()),
					zap.String("currency", currency.String()),
				)
				return &CacheCheckResult{
					Status: CacheStatusComplete,
					Set:    rs.GetSet(),
					RsID:   rs.ID,
				}, nil
			default:
				// considering any other status as failed, the RS instance should be cleared soon so allow to ignore here
				// handling it will now depend on the operation type, see below (will most likely end up retrying)
			}
		}

		if rs.Key().OpType == OpTypeFull {
			// There is an active runtime set fetching the full set details but not with the requested currency unless it has failed
			missesCachedData = true
		} else {
			// There is an active runtime set fetching prices only, therefore crucial data is already cached
		}
	}

	if missesCachedData {
		// There is an active runtime set fetching the full set details but not with the requested currency unless it has failed

		// Option 1 - Start a new RS to fetch all with the requested currency
		// check for valid cached data, if existing then care for missing prices / missing currencies / outdated currency prices

		// Option 2 - Wait for ongoing RS to finish inventory, then filter missing/outdated prices with currency, then fetch them

		// For simplicity, we will go with Option 1 for now
		zap.L().Info("Set details missing cached data for requested currency, needs refetch",
			zap.String("set_id", setID.String()),
			zap.String("requested_currency", currency.String()),
		)
		return &CacheCheckResult{Status: CacheStatusNeedsRefetch}, nil
	}

	// There are active runtime sets, but none match the requested currency
	zap.L().Info("Active runtime sets found, but none match requested currency",
		zap.String("set_id", setID.String()),
		zap.String("requested_currency", currency.String()),
	)
	return &CacheCheckResult{Status: CacheStatusMissesPrices}, nil
}

func checkSetInRedis(ctx context.Context, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	// Try to get cached set
	cachedSet, err := set.GetRedisSet(ctx, setID)
	if err != nil {
		return &CacheCheckResult{Status: CacheStatusMissing}, nil
	}

	// Check cached fetch status
	switch cachedSet.FetchStatus {
	case set.FetchStatusCompleted:
		return checkSetDataValidity(ctx, cachedSet, setID, currency)
	default:
		/* TODO : NOW - rs create before redis create ?
			if so then case set.FetchStatusPending, set.FetchStatusFetching: are considered incomplete data
		    else use FetchStatusFetchingInventory FetchStatusFetchingPrices to help decide ?
		*/
		// Previous fetch failed or is incomplete, needs refetch
		zap.L().Warn("Previous set details fetch failed or incomplete",
			zap.String("set_id", setID.String()),
		)
		return &CacheCheckResult{Status: CacheStatusFailed}, nil
	}
}

// checkSetDataValidity validates completed cached data and checks for missing prices
func checkSetDataValidity(ctx context.Context, cachedSet set.Set, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	zap.L().Info("Set details found in cache, checking Bricks and prices",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
	)

	// Check if set price for requested currency is available
	setPriceOutdated := false
	if price, ok := cachedSet.Prices.GetPrice(currency); !ok || price.IsOutdated(database.DB().Redis().TTLS.SetPrice) {
		setPriceOutdated = true
	} else {
		// Apply the price to the set
		if !cachedSet.ApplyCurrency(currency) {
			setPriceOutdated = true
		}
	}

	// Prepare slices to hold full Brick data and those missing prices
	bricks := make([]set.Brick, 0, len(cachedSet.Bricks))
	bricksWoPrices := make([]set.Brick, 0)

	// For each brick in the set, retrieve full data from cache and check for missing prices
	for idx, brickMin := range cachedSet.Bricks {

		// TODO: ISSUE #1 - Alternate items
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

		// TODO : handle quantity and index properly
		// Set the index to maintain order
		if brickMin.Index >= 0 {
			brick.Index = brickMin.Index
		} else {
			brick.Index = idx
		}

		// Brick price for requested currency is outdated
		if p, ok := brick.Prices.GetPrice(currency); !ok || p.IsOutdated(database.DB().Redis().TTLS.BrickPrice) {
			bricksWoPrices = append(bricksWoPrices, brick)
		}

		// Price is valid and up-to-date, apply it
		if !brick.ApplyCurrency(currency) {
			// Price for this currency not cached - should not happen since we checked above
			bricksWoPrices = append(bricksWoPrices, brick)
		}

		bricks = append(bricks, brick)
	}

	// If analyze show that all prices are present and up-to-date, return complete
	if !setPriceOutdated && len(bricksWoPrices) == 0 {
		zap.L().Info("All Bricks have prices for requested currency",
			zap.String("set_id", setID.String()),
			zap.String("currency", currency.String()),
		)
		cachedSet.Bricks = bricks
		return &CacheCheckResult{
			Status: CacheStatusComplete,
			Set:    cachedSet,
			Bricks: bricks,
		}, nil
	}

	zap.L().Info("Some Bricks need price updates",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
		zap.Bool("set_price_outdated", setPriceOutdated),
		zap.Int("bricks_wo_prices", len(bricksWoPrices)),
	)

	return &CacheCheckResult{
		Status:           CacheStatusMissesPrices,
		Set:              cachedSet,
		SetPriceOutdated: setPriceOutdated,
		Bricks:           bricks,
		BricksWoPrices:   bricksWoPrices,
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
