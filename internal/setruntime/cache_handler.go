package setruntime

import (
	"context"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type CacheStatus int

const (
	CacheStatusMissing      CacheStatus = iota // no cached data was found
	CacheStatusFailed                          // previous fetch failed
	CacheStatusComplete                        // data is fully cached and ready
	CacheStatusIncomplete                      // data is cached but incomplete (missing prices, bricks, outdated etc.)
	CacheStatusNeedsRefetch                    // cached data is stale and needs complete refetch
	CacheStatusFetching                        // data is currently being fetched
)

// CacheCheckResult represents the result of checking cached set data
type CacheCheckResult struct {
	Status         CacheStatus     // Status indicates what action should be taken
	Set            set.SetExternal // Set contains the cached set data (if available)
	SetWoPrice     bool            // SetWoPrice indicates if the set price needs a price update for the requested currency
	BricksWoPrices []set.BrickSet  // BricksWoPrices contains Bricks that need price updates for the requested currency
	RsID           uuid.UUID       // RsID ID of the runtime set if applicable
}

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
				// No need to check data validity as it should be up-to-date since the RS is still active, just return it
				// Very low probability to reach this case here since the RS should be cleared right after completing
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
				// handling it will now depend on the operation type, see below
				// it will most likely end up retrying with a new runtime
			}
		}

		if rs.Key().OpType == OpTypeFull {
			// There is an active runtime set fetching the full set details but not with the requested currency unless it has failed
			// It means that crucial data is most likely missing
			missesCachedData = true
		} else {
			// There is an active runtime set fetching prices only, therefore crucial data is already cached
		}
	}

	if missesCachedData {
		// There is an active runtime set fetching the full set details but not with the requested currency, unless it has failed

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
	// Check the set data validity to decide upon the next step
	return checkSetInRedis(ctx, setID, currency)
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
		// A previous fetch might have failed, or the redis instance became an orphan without a runtime set link
		// Therefore the redis set instance will be considered incomplete and as needing refetch
		zap.L().Warn("Previous set details fetch failed or incomplete",
			zap.String("set_id", setID.String()),
		)
		return &CacheCheckResult{Status: CacheStatusFailed}, nil
	}
}

// checkSetDataValidity validates completed existing data and checks for missing elements
func checkSetDataValidity(ctx context.Context, cachedSet set.Set, setID uuid.UUID, currency language.Tag) (*CacheCheckResult, error) {
	zap.L().Info("Set details found in cache, checking Bricks and prices",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
	)

	// If there are no bricks in the cached set, it means the data is stale or incomplete, needs refetch
	if len(cachedSet.Bricks) == 0 {
		return &CacheCheckResult{Status: CacheStatusNeedsRefetch}, nil
	}

	// Since we are only readying, we need to find the currency symbol from any valid price found in the set
	// This is needed to apply the total price with the correct currency symbol
	var currencySymbol string

	// Check if set price for requested currency is available
	setPriceMissing := false
	if price, ok := cachedSet.Prices.GetPrice(currency); !ok || price.IsOutdated(database.DB().Redis().TTLS.SetPrice) {
		setPriceMissing = true
	} else {
		// Set price is valid and up-to-date, use its currency symbol
		currencySymbol = price.Currency
	}

	// Prepare slices to hold full Brick data and those missing prices
	bricks := make([]set.BrickSet, 0, len(cachedSet.Bricks))
	bricksWoPrices := make([]set.BrickSet, 0)

	// Total cent amount accumulator
	totalCentAmount := 0

	// For each brick in the set, retrieve full data from cache and check for missing prices
	// This supposes that the set cache instance always has its bricks complete and valid, which is the case in theory
	for _, brickSet := range cachedSet.Bricks {

		// Get brick ID for Redis lookup
		brickID, err := brickSet.GetBrickIDForRedis()
		if err != nil {
			// This shouldn't happen as the set should have been validated before caching, but handle gracefully just in case
			zap.L().Error("Failed to get brick ID for Redis",
				zap.Error(err),
			)
			return &CacheCheckResult{Status: CacheStatusNeedsRefetch}, nil
		}

		// Try to find brick in cache
		brick, err := set.GetRedisBrick(ctx, brickID)
		if err != nil {
			// Brick price for requested currency is missing or outdated
			bricksWoPrices = append(bricksWoPrices, brickSet)
			continue
		}

		// Update BrickSet with most recent Brick data from cache
		brickSet.Brick = brick

		if !set.HasValidPrice(brick.Prices, currency, database.DB().Redis().TTLS.BrickPrice) {
			// Brick price for requested currency is missing or outdated
			bricksWoPrices = append(bricksWoPrices, brickSet)
			continue
		}

		// Price is valid and up-to-date, apply it
		brickSet.MustApplyCurrency(currency)
		brickSet.CalculateTotalPrice()
		totalCentAmount += brickSet.TotalPrice.CentAmount

		// Use the currency symbol from the first valid price found for consistency across the set
		if currencySymbol == "" {
			currencySymbol = brickSet.Price.Currency
		}

		bricks = append(bricks, brickSet)
	}

	// Build the final SetExternal with the updated bricks and price information
	cachedSet.Bricks = bricks
	setFull := set.SetExternal{
		Set: cachedSet,
	}

	// If all elements are present and up-to-date, return complete
	if !setPriceMissing && len(bricksWoPrices) == 0 {
		zap.L().Info("All Bricks have prices for requested currency",
			zap.String("set_id", setID.String()),
			zap.String("currency", currency.String()),
		)

		// Update cached set with final data
		setFull.MissingParts = 0
		setFull.Currency = currency.String()
		setFull.ApplyTotalPrice(totalCentAmount, currencySymbol)

		return &CacheCheckResult{
			Status: CacheStatusComplete,
			Set:    setFull,
		}, nil
	}

	zap.L().Info("Some Bricks need price updates",
		zap.String("set_id", setID.String()),
		zap.String("currency", currency.String()),
		zap.Bool("set_price_outdated", setPriceMissing),
		zap.Int("bricks_wo_prices", len(bricksWoPrices)),
	)

	// Update cached set with final data
	setFull.Currency = currency.String()
	setFull.MissingParts = len(bricksWoPrices)
	if totalCentAmount > 0 && currencySymbol != "" {
		setFull.ApplyTotalPrice(totalCentAmount, currencySymbol)
	}

	return &CacheCheckResult{
		Status:         CacheStatusIncomplete,
		Set:            setFull,
		SetWoPrice:     setPriceMissing,
		BricksWoPrices: bricksWoPrices,
	}, nil
}
