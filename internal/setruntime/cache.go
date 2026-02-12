package setruntime

import (
	"context"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
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

// CacheSet represents the result of checking cached set data
type CacheSet struct {
	Status        CacheStatus  // Status indicates what action should be taken
	RuntimeSetID  uuid.UUID    // RuntimeSetID ID of the runtime set (if applicable)
	Set           set.External // Set contains the cached set data (if available)
	MissingPrice  bool         // MissingPrice indicates if the set price needs a price update for the requested xlocale
	MissingBricks []set.Brick  // MissingBricks contains Bricks that need price updates for the requested xlocale
	FinalBricks   []set.Brick  // FinalBricks contains Bricks that have valid prices for the requested xlocale and can be used as is
}

// GetCacheSet checks Redis cache for set data and determines what action is needed
func (h *Handler) GetCacheSet(ctx context.Context, setID uuid.UUID, xlocale language.Tag) (*CacheSet, error) {
	// Search for the active runtime sets
	rss := h.FindRuntimeSetBySetId(setID)

	// No active runtime sets found, check redis cache to decide next steps
	if len(rss) == 0 {
		return checkSetInRedis(ctx, setID, xlocale)
	}

	// There are active runtime sets for this set ID, process them to decide next steps
	missesCachedData := false
	for _, rs := range rss {

		// Check if the runtime set is fetching with the requested xlocale
		if rs.Key().XLocale == xlocale {
			switch rs.Read().FetchStatus {
			case set.FetchStatusPending, set.FetchStatusFetching:
				// Runtime is fetching or pending, let the caller join it
				zap.L().Info("Set details currently being fetched in runtime set",
					zap.String("set_id", setID.String()),
					zap.String("xlocale", xlocale.String()),
				)
				return &CacheSet{
					Status: CacheStatusFetching,
					Set:    *rs.Read(),
				}, nil
			case set.FetchStatusCompleted:
				// Data already fetched and cached by this runtime set
				// No need to check data validity as it should be up-to-date since the RS is still active, just return it
				// Very low probability to reach this case here since the RS should be cleared right after completing
				zap.L().Info("Set details already fetched and cached in runtime set",
					zap.String("set_id", setID.String()),
					zap.String("xlocale", xlocale.String()),
				)
				return &CacheSet{
					Status:       CacheStatusComplete,
					Set:          *rs.Read(),
					RuntimeSetID: rs.ID,
				}, nil
			default:
				// considering any other status as failed, the RS instance should be cleared soon so allow to ignore here
				// handling it will now depend on the operation type, see below
				// it will most likely end up retrying with a new runtime
			}
		}

		if rs.Key().OpType == OpTypeFull {
			// There is an active runtime set fetching the full set details but not with the requested xlocale unless it has failed
			// It means that crucial data is most likely missing
			missesCachedData = true
		} else {
			// There is an active runtime set fetching prices only, therefore crucial data is already cached
		}
	}

	if missesCachedData {
		// There is an active runtime set fetching the full set details but not with the requested xlocale, unless it has failed

		// Option 1 - Start a new RS to fetch all with the requested xlocale
		// check for valid cached data, if existing then care for missing prices / missing currencies / outdated xlocale prices

		// Option 2 - Wait for ongoing RS to finish inventory, then filter missing/outdated prices with xlocale, then fetch them

		// For simplicity, we will go with Option 1 for now
		zap.L().Info("Set details missing cached data for requested xlocale, needs refetch",
			zap.String("set_id", setID.String()),
			zap.String("xlocale", xlocale.String()),
		)
		return &CacheSet{Status: CacheStatusNeedsRefetch}, nil
	}

	// There are active runtime sets, but none match the requested xlocale
	// Check the set data validity to decide upon the next step
	return checkSetInRedis(ctx, setID, xlocale)
}

// checkSetInRedis checks Redis cache for the set data and analyzes it to determine what action is needed
func checkSetInRedis(ctx context.Context, setID uuid.UUID, xlocale language.Tag) (*CacheSet, error) {
	// Try to get cached set
	sCache, err := set.RedisGetLocale(ctx, setID, xlocale, true)
	if err != nil {
		return &CacheSet{Status: CacheStatusMissing}, nil
	}

	// Check cached fetch status
	switch sCache.FetchStatus {
	case set.FetchStatusCompleted:
		return checkSetDataValidity(ctx, sCache, setID, xlocale)
	default:
		// A previous fetch might have failed, or the redis instance became an orphan without a runtime set link
		// Therefore the redis set instance will be considered incomplete and as needing refetch
		zap.L().Warn("Previous set details fetch failed or incomplete",
			zap.String("set_id", setID.String()),
		)
		return &CacheSet{Status: CacheStatusFailed}, nil
	}
}

// checkSetDataValidity validates completed existing data and checks for missing elements
func checkSetDataValidity(ctx context.Context, s set.Locale, setID uuid.UUID, xlocale language.Tag) (*CacheSet, error) {
	zap.L().Info("Set details found in cache, checking Bricks and prices",
		zap.String("set_id", setID.String()),
		zap.String("xlocale", xlocale.String()),
	)

	// If there are no bricks in the cached set, it means the data is stale or incomplete, needs refetch
	if len(s.Bricks) == 0 {
		return &CacheSet{Status: CacheStatusNeedsRefetch}, nil
	}

	// Set up the cache response
	sExternal := set.External{
		Locale: s,
	}
	sExternal.MissingParts = len(s.Bricks)

	// Check if set price is missing
	setPriceMissing := !s.HasValidPrice()

	// Prepare slices to hold full Brick data and those missing prices
	bricksFinal := make([]set.Brick, 0)
	bricksMissing := make([]set.Brick, 0)

	// For each brick in the set, retrieve full data from cache and check for missing prices
	// This supposes that the set cache instance always has its bricks complete and valid, which is the case in theory
	for _, bSet := range s.Bricks {
		b, final := checkBrickCache(ctx, bSet, xlocale)
		if final {
			bricksFinal = append(bricksFinal, b)
			sExternal.AddFinalBrickData(b)
		} else {
			bricksMissing = append(bricksMissing, bSet)
		}
	}

	// Build final set data
	sFinal := sExternal

	// If all elements are present and up-to-date, return complete
	if !setPriceMissing && len(bricksMissing) == 0 {
		zap.L().Info("All Bricks have prices for requested xlocale",
			zap.String("set_id", setID.String()),
			zap.String("xlocale", xlocale.String()),
		)

		// Force final bricks in here since already sorted by index
		// Also we don't it to be restricted as Inventory/Core data, this will not update the set core redis instance
		sFinal.Bricks = bricksFinal

		return &CacheSet{
			Status: CacheStatusComplete,
			Set:    sFinal,
		}, nil
	}

	// Apply the processed bricks into the runtime set instance
	sFinal.SetBricks(append(bricksFinal, bricksMissing...), true)

	zap.L().Info("Set cached data is incomplete",
		zap.String("set_id", setID.String()),
		zap.String("xlocale", xlocale.String()),
		zap.Bool("missing_set_price", setPriceMissing),
		zap.Int("missing_bricks", len(bricksMissing)),
	)

	return &CacheSet{
		Status:        CacheStatusIncomplete,
		Set:           sFinal,
		MissingPrice:  setPriceMissing,
		MissingBricks: bricksMissing,
		FinalBricks:   bricksFinal,
	}, nil
}

// checkBrickCache checks the cache for a given Brick and returns the updated Brick and whether it has valid cached data
func checkBrickCache(ctx context.Context, bSet set.Brick, tag language.Tag) (set.Brick, bool) {
	// When applying locales from cache, we might overwrite the elementIDs slice
	// To avoid loosing data, we reset it to the original slice
	originalElementIDs := bSet.Locale.ElementIDs

	// If the brick was correctly cached in the inventory, it should have a valid or not-found cached brick
	bRedis, valid, notFound := bSet.Locale.LoadFromRedis(ctx, *bSet.ElementID, tag)
	if valid || notFound {
		// Brick has valid price in cache, apply its locale data and add to final
		// Note: if notFound, we could check all ElementID's to be 100% sure, but it should be useless if cached correctly
		bSet = set.NewBrickWithID(bSet.ID, bSet.Inventory, bRedis)
		bSet.SetElementIDs(originalElementIDs) // Reset to original slice to avoid losing data
		return bSet, true
	}

	var firstNotFoundLocale *brick.Locale
	var validLocale bool

	// The main ElementID isn't 100% valid, process over the available ElementIDs and decide afterward
	for _, elementID := range bSet.ElementIDs {
		// Try to find in cache first
		bRedis, valid, notFound = bSet.Locale.LoadFromRedis(ctx, elementID, tag)
		if notFound && firstNotFoundLocale == nil {
			// Brick Locale cached with not-found price
			// We can consider this brick as up-to-date with a not-found price
			// Save temporarily until we process them all, we might find a matching ID with a valid price
			firstNotFoundLocale = &bRedis
			continue // Try next ID
		} else if valid {
			// Brick Locale cached with valid price and up-to-date
			// Save temporarily until we process them all, we might find a matching ID with a better price
			bSet = set.NewBrickWithID(bSet.ID, bSet.Inventory, bRedis)
			validLocale = true
			continue // Try next ID
		}
	}

	// bSet has already been updated with a valid locale from cache, append it and continue
	if validLocale {
		bSet.SetElementIDs(originalElementIDs) // Reset to original slice to avoid losing data
		return bSet, true
	}

	// No 100% valid locale was found, but at least one not-found brick locale was found
	// We can consider this brick as up-to-date with a not-found price, append it and continue
	if firstNotFoundLocale != nil {
		bSet = set.NewBrickWithID(bSet.ID, bSet.Inventory, *firstNotFoundLocale)
		bSet.SetElementIDs(originalElementIDs) // Reset to original slice to avoid losing data
		return bSet, true
	}

	// No cached data was found for any of the brick's ElementIDs
	return bSet, false
}

// cacheSet stores the set data in Redis cache
func (rs *RuntimeSet) cacheSet(ctx context.Context) {
	// Retrieve the runtime bricks before caching to ensure we cache the most up-to-date data
	// They are already sorted by index
	bricks := rs.bricks.get()

	// Update the bricks in the runtime set with the cleaned versions that are ready for caching
	rs.set.SetBricks(bricks)

	// Cache the set inventory in Redis
	err := set.RedisSetCore(ctx, rs.Read().Core, true)
	if err != nil {
		zap.L().Error("Failed to cache set locale in Redis",
			zap.String("set_id", rs.Key().SetID.String()),
			zap.String("xlocale", rs.Key().XLocale.String()),
			zap.Error(err),
		)
	}
}
