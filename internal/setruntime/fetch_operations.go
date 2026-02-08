package setruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/Zapharaos/brick-scanr-backend/internal/workerpool"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchSetComplete performs a complete fetch of set details including inventory and prices
// This is used when there's no cached data or when cached data is stale
func (h *Handler) FetchSetComplete(
	ctx context.Context,
	rs *RuntimeSet,
	setID uuid.UUID,
	cachedSet set.Set,
	locale language.Tag,
	currency language.Tag,
) {
	defer func() {
		// Handle panics gracefully
		if r := recover(); r != nil {
			h.logCriticalError(setID, "FetchSetComplete.Panic", fmt.Errorf("panic recovered: %v", r))
			zap.L().Error("Panic in FetchSetComplete", zap.Any("panic", r), zap.String("set_id", setID.String()))
		}
		// Give clients time to receive the message before cleanup
		time.Sleep(3 * time.Second)
		h.StopRuntimeSet(rs.Key())
	}()

	zap.L().Info("Starting complete set details fetch",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("key", rs.Key().String()),
		zap.String("set_id", setID.String()),
	)

	// Initialize copy of set for Redis
	cpRedisSet := cachedSet

	// Cache the status => for concurrent access to the websocket
	cpRedisSet.FetchStatus = set.FetchStatusFetching
	err := set.SetRedisSet(ctx, cpRedisSet, true)
	if err != nil {
		// Failed to update the set in cache => FATAL
		handleFatalError(h, rs.ID, setID, set.FetchErrorInitCache, cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set status")
		return
	}
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCreated)

	// Check if set price is already cached and valid
	if !set.HasValidPrice(cpRedisSet.Prices, currency, database.DB().Redis().TTLS.SetPrice) {
		ok, err := set.FetchLegoProductDetails(ctx, setID, &cpRedisSet, locale, currency, true)
		if err == nil && ok {
			// Successfully fetched details, send update to clients
			h.PushChange(rs.ID, setID, DataTypeSet, DataTypeUpdated)
		}
		// Error not fatal and already handled in FetchLegoProductDetails
	}

	// Fetch inventory from BrickLink (sequential - needed for prices)
	if err := h.fetchInventory(ctx, rs.ID, setID, &cpRedisSet); err != nil {
		return // Error already handled
	}

	// Fetch prices from Pick-a-Brick (sequential - depends on inventory)
	if err := h.fetchBricks(ctx, rs, setID, &cpRedisSet, locale, currency); err != nil {
		return // Error already handled
	}

	// Cleanup up
	cpRedisSet.CleanupForCache()

	// Mark set fetch completed
	cpRedisSet.FetchStatus = set.FetchStatusCompleted
	err = set.SetRedisSet(ctx, cpRedisSet, true)
	if err != nil {
		// Failed to cache the final version => FATAL
		handleFatalError(h, rs.ID, setID, set.FetchErrorFinalCache, cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set with final data")
		return
	}
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)

	zap.L().Info("Successfully completed set details fetch",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("set_id", setID.String()),
	)
}

// fetchInventory fetches the inventory for a set from BrickLink using a worker pool
func (h *Handler) fetchInventory(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set) error {
	inventory, err := bricklink.C().FetchInventory(cpRedisSet.BricklinkID, cpRedisSet.BricklinkNumber)
	if err != nil {
		// Failed to fetch inventory => FATAL
		handleFatalError(h, rsID, setID, set.FetchErrorFetchInventory, *cpRedisSet, DataTypeBricklinkBricks, err,
			"Failed to fetch inventory from BrickLink")
		return err
	}

	inventorySize := len(inventory.RegularItems)
	if inventorySize == 0 {
		return nil
	}

	// Create optimal config based on item count
	config := workerpool.NewConfigOptimal(inventorySize, len(h.sets))

	// Create shared progress tracker
	bprogress := NewProgress(inventorySize, config.BatchSize)

	zap.L().Debug("Starting worker pool for inventory processing",
		zap.Int("item_count", inventorySize),
		zap.Int("workers", config.Workers),
		zap.Int("batch_size", config.BatchSize),
	)

	// Worker function: process a single inventory item
	workerFunc := func(ctx context.Context, item bricklink.InventoryItem) (set.BrickSet, error) {
		skipRedis := len(item.ItemIDs) == 0
		var brick set.BrickSet
		var errW error

		// Make sure we have at least one ItemID to lookup in cache
		if !skipRedis {
			// Try to find in cache first
			brick.Brick, errW = set.GetRedisBrick(ctx, set.BrickID(item.ItemIDs[0]))
		}

		// If no redis yet, map from BrickLink item
		if skipRedis || errW != nil {
			brick = set.MapBrickFromBricklinkInventoryItem(item)
		} else if !skipRedis && errW == nil {
			// Different sources can update a brick redis instance, and bricklink is not the preferred source
			// Therefore, we must be careful when updating the brick instance in redis and only update specific fields
			brick = set.SafeMapBrickFromBricklinkInventoryItem(brick, item)
		}

		// Cache the brick: either for the first time, or to refresh the TTL
		if cacheErr := set.SetRedisBrick(ctx, brick.Brick, true); cacheErr != nil {
			h.logWarning(setID, "Redis.SetRedisBrick", cacheErr)
			zap.L().Warn("Failed to cache brick",
				zap.Error(cacheErr),
				zap.String("brick_design_id", item.ItemNo),
			)
		}

		return brick, nil
	}

	// Batch handler: send batch to frontend and update Redis
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.BrickSet) error {
		return h.batchHandlerBricksProgress(ctx, rsID, cpRedisSet, batch, bprogress, DataTypeBricklinkBricks)
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		h.logWarning(setID, "Worker.InventoryProcessing", err)
		zap.L().Warn("Worker encountered error processing brick",
			zap.Error(err),
		)
	})

	// Process all inventory items
	if err := pool.Process(inventory.RegularItems); err != nil {
		handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
			"Failed to process inventory with worker pool")
		return err
	}

	zap.L().Debug("Worker pool completed inventory processing",
		zap.Int("item_count", inventorySize),
		zap.Int("bricks_processed", len(cpRedisSet.Bricks)),
	)

	return nil
}

// fetchBricks fetches prices for all Bricks in a set from Pick-a-Brick using a worker pool
func (h *Handler) fetchBricks(ctx context.Context, rs *RuntimeSet, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag) error {
	// Calculate total bricks
	bricksSize := len(cpRedisSet.Bricks)

	// Create optimal config based on brick count
	config := workerpool.NewConfigOptimal(bricksSize, len(h.sets))

	// Create shared progress tracker
	bprogress := NewProgress(bricksSize, config.BatchSize)

	zap.L().Debug("Starting worker pool for price fetching",
		zap.Int("bricks_size", bricksSize),
		zap.Int("workers", config.Workers),
		zap.Int("batch_size", config.BatchSize),
	)

	// Worker function: fetch prices for a single design ID
	workerFunc := func(ctx context.Context, brick set.BrickSet) (set.BrickSet, error) {
		return h.workerHandlerBrickPrice(ctx, setID, brick, locale, currency)
	}

	// Batch handler: send batch to frontend
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.BrickSet) error {
		return h.batchHandlerBricksProgress(ctx, rs.ID, cpRedisSet, batch, bprogress, DataTypePickabrickBricks)
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		h.logWarning(setID, "Worker.PriceProcessing", err)
		zap.L().Warn("Worker encountered error processing price job",
			zap.Error(err),
		)
	})

	// Copy bricks into a new slice for batch processing
	cpBricks := make([]set.BrickSet, len(cpRedisSet.Bricks))
	copy(cpBricks, cpRedisSet.Bricks)

	// Clear bricks to avoid duplication in batch handler
	// Initial slice was filled with inventory data, along with specified brickID's
	// However batch may apply a different brickID's which would cause duplicates if not cleared here
	cpRedisSet.Bricks = []set.BrickSet{}

	// Enable staging mode to build new brick map without clearing the active one
	// This prevents empty brick map window - clients continue seeing inventory data
	// while prices are being fetched and built in stagingBricks
	rs.EnableStagingMode()

	// Process all price jobs
	if err := pool.Process(cpBricks); err != nil {
		handleFatalError(h, rs.ID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
			"Failed to process prices with worker pool")
		return err
	}

	// Atomically promote staging bricks to active bricks
	// This swaps the maps instantly, avoiding any empty window
	rs.PromoteStaging()

	zap.L().Debug("Worker pool completed price fetching",
		zap.Int("bricks_size", bricksSize),
	)

	return nil
}

// FetchFetchSetIncomplete fetches Bricks that are missing for the requested currency
func (h *Handler) FetchFetchSetIncomplete(
	ctx context.Context,
	rs *RuntimeSet,
	setID uuid.UUID,
	cacheResult *CacheCheckResult,
	locale language.Tag,
	currency language.Tag,
) {
	defer func() {
		// Give clients time to receive the message before cleanup
		time.Sleep(3 * time.Second)
		h.StopRuntimeSet(rs.Key())
	}()

	zap.L().Info("Starting bricks fetch",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("key", rs.Key().String()),
		zap.Int("bricks_count", len(cacheResult.BricksWoPrices)),
	)

	// Push set manually (to avoid pulling from cache which does not have preprocessed set data)
	h.PushChangeSet(rs.ID, setID, DataTypeSet, DataTypeCreated, cacheResult.Set)

	// Check if set price is outdated
	if cacheResult.SetWoPrice {
		ok, err := set.FetchLegoProductDetails(ctx, setID, &cacheResult.Set.Set, locale, currency, true)
		if err != nil {
			handleFatalError(h, rs.ID, setID, set.FetchErrorDetailsCache, cacheResult.Set.Set, DataTypeSet, err,
				"Failed to update Redis set inventory")
		} else if ok {
			// Push set manually
			// Otherwise default behavior pulls from cache which only holds a set.Set and not set.SetExternal
			// Especially since the current runtime is for an incomplete set which could already have partial final data calculated
			h.PushChangeSet(rs.ID, setID, DataTypeSet, DataTypeUpdated, cacheResult.Set)
		}
	}

	bricksCount := len(cacheResult.BricksWoPrices)
	if bricksCount == 0 {
		h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)
		return
	}

	// Create optimal config based on brick count
	config := workerpool.NewConfigOptimal(bricksCount, len(h.sets))

	// Create shared progress tracker
	bprogress := NewProgress(bricksCount, config.BatchSize)

	zap.L().Debug("Starting worker pool for bricks fetching",
		zap.Int("bricks_count", bricksCount),
		zap.Int("workers", config.Workers),
		zap.Int("batch_size", config.BatchSize),
	)

	// Worker function: fetch for a single brick
	workerFunc := func(ctx context.Context, brick set.BrickSet) (set.BrickSet, error) {
		return h.workerHandlerBrickPrice(ctx, setID, brick, locale, currency)
	}

	// Batch handler: send batch to frontend
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.BrickSet) error {
		return h.batchHandlerBricksProgress(ctx, rs.ID, &cacheResult.Set.Set, batch, bprogress, DataTypePickabrickBricks)
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		h.logWarning(setID, "Worker.BrickPriceProcessing", err)
		zap.L().Warn("Worker encountered error processing brick price",
			zap.Error(err),
		)
	})

	// Clear bricks to avoid duplication in batch handler
	// Initial slice was filled with inventory data, along with specified brickID's
	// However batch may apply a different brickID's which would cause duplicates if not cleared here
	cacheResult.Set.Bricks = []set.BrickSet{}

	// Process all brick price jobs
	if err := pool.Process(cacheResult.BricksWoPrices); err != nil {
		// Use handleFatalError for consistency - it already handles logging and client notification
		handleFatalError(h, rs.ID, setID, set.FetchErrorFetchPrices, cacheResult.Set.Set, DataTypePickabrickBricks, err,
			"Failed to process brick prices with worker pool",
			zap.String("runtime_id", rs.ID.String()),
		)
		return
	}

	// Use runtime bricks and update the set's bricks before broadcasting
	cacheResult.Set.Bricks = utils.MapValues(rs.bricks)

	// Clean up
	cacheResult.Set.CleanupForCache()

	// Mark set fetch completed
	cacheResult.Set.FetchStatus = set.FetchStatusCompleted
	err := set.SetRedisSet(ctx, cacheResult.Set.Set, true)
	if err != nil {
		// Failed to cache the final version => FATAL
		handleFatalError(h, rs.ID, setID, set.FetchErrorFinalCache, cacheResult.Set.Set, DataTypeSet, err,
			"Failed to update Redis set with final data")
		return
	}
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)

	zap.L().Info("Price fetch completed",
		zap.String("runtime_id", rs.ID.String()),
	)
}

// workerHandlerBrickPrice is the worker function for fetching and updating a single brick's price, with cache checking and fallback to API
func (h *Handler) workerHandlerBrickPrice(
	ctx context.Context,
	setID uuid.UUID,
	brick set.BrickSet,
	locale language.Tag,
	currency language.Tag,
) (set.BrickSet, error) {

	// Try to find the best brick ID
	// Track which IDs we've tried and their results for final not-found caching
	var firstNotFoundID *set.BrickID
	foundValidPrice := false

	for _, elementID := range brick.IDs {

		// Check cache first for this specific brick ID
		if cacheBrick, err := set.GetRedisBrick(ctx, elementID); err == nil {
			// Check if this brick ID has a valid price for the currency
			if set.HasValidPrice(cacheBrick.Prices, currency, database.DB().Redis().TTLS.BrickPrice) {
				// Check if current brick already has a valid lower price
				if brick.Brick.HasLowerPrice(cacheBrick, currency) {
					continue
				}

				// Found valid cached price for this brick ID
				brick.Brick = cacheBrick
				brick.MustApplyCurrency(currency)
				brick.CalculateTotalPrice()
				foundValidPrice = true
				continue
			}

			// Check if this brick ID has a valid cached not-found entry
			if set.HasCachedNotFound(cacheBrick.Prices, currency, database.DB().Redis().TTLS.BrickPrice) {
				zap.L().Debug("Brick ID has cached not-found price, trying next ID",
					zap.String("elementID", string(elementID)),
					zap.String("currency", currency.String()),
					zap.String("design_id", string(brick.DesignID)),
				)
				// Remember this ID as not found, but keep trying other IDs
				if firstNotFoundID == nil {
					idCopy := elementID
					firstNotFoundID = &idCopy
				}
				continue
			}
		}

		// No valid cache entry for this brick ID - fetch from API
		results, err := pickabrick.C().FetchBricksByBrickID(string(elementID), locale, currency)
		if err != nil {
			// Check if it's a not-found error
			if errors.Is(err, pickabrick.ErrBrickNotFound) {
				zap.L().Debug("Brick ID not found in pick-a-brick API",
					zap.String("elementID", string(elementID)),
					zap.String("currency", currency.String()),
					zap.String("design_id", string(brick.DesignID)),
				)

				// Create a completely independent brick for caching not-found status
				// We must not modify the original brick that will be used in the set
				notFoundBrick := set.Brick{
					MainID:   &elementID,
					IDs:      brick.IDs,
					DesignID: brick.DesignID,
					IsCustom: brick.IsCustom,
					Status:   brick.Status,
					Name:     brick.Name,
					ImageURL: brick.ImageURL,
					Color:    brick.Color,
					Prices:   make(map[language.Tag]*set.Price),
				}

				// Set only the not-found price for this currency
				notFoundPrice := set.Price{
					NotFound:  true,
					Currency:  currency.String(),
					ItemID:    string(elementID),
					FetchedAt: time.Now().UnixMilli(),
				}
				notFoundBrick.Prices[currency] = &notFoundPrice

				// Cache this brick ID as not-found (independent entry, won't affect the set's brick)
				if cacheErr := set.SetRedisBrick(ctx, notFoundBrick, true); cacheErr != nil {
					h.logWarning(setID, "Redis.SetRedisBrick.NotFound", cacheErr)
					zap.L().Warn("Failed to cache brick ID with not-found price",
						zap.Error(cacheErr),
						zap.String("elementID", string(elementID)),
						zap.String("design_id", string(brick.DesignID)),
					)
				}

				// Remember this ID as not found, but keep trying other IDs
				if firstNotFoundID == nil {
					idCopy := elementID
					firstNotFoundID = &idCopy
				}
				continue // Try next ID
			}

			// Other error - log and try next ID
			h.logWarning(setID, "Pickabrick.FetchBricksByBrickID", err)
			zap.L().Warn("Failed to fetch brick by elementID",
				zap.Error(err),
				zap.String("elementID", string(elementID)),
				zap.String("set_id", setID.String()))
			continue // Try next ID
		}

		// There should be only one matching brick per ID
		// API client returns a slice so to be safe we will loop through the results just in case
		for _, b := range results {
			// Just in case, check if the returned brick ID matches the requested one
			if set.BrickID(b.ID) == elementID {

				// Map result to local representation
				mappedB := set.MapBrickFromPickabrick(brick.Brick, elementID, b, locale, currency)

				// Check if current price currency is already set, valid and lower
				if brick.Brick.HasLowerPrice(mappedB, currency) {
					continue
				}

				// Found a new valid and lower price, update brick with fetched data from pickabrick
				brick.Brick = mappedB

				// Cleanup brick data for caching
				copyPrice, copyTotalPrice := brick.CleanupForCache()

				// Cache the updated brick with new price data
				if cacheErr := set.SetRedisBrick(ctx, brick.Brick, true); cacheErr != nil {
					h.logWarning(setID, "Redis.SetRedisBrick", cacheErr)
					zap.L().Warn("Failed to cache brick with new price",
						zap.Error(cacheErr),
						zap.String("brick_design_id", string(brick.DesignID)),
						zap.String("set_id", setID.String()),
					)
					// Not fatal, continue without caching
				}

				// Restore the original price data after caching
				brick.RestoreAfterCache(copyPrice, copyTotalPrice)

				// Update brick with final data for sending to client
				brick.MustApplyCurrency(currency)
				brick.CalculateTotalPrice()
				foundValidPrice = true
			}
		}
	}

	// If we didn't find a valid price for any brick ID, but we did encounter not-found entries,
	// set the MainID to one of the not-found IDs to avoid unnecessary cache lookups next time
	if !foundValidPrice && firstNotFoundID != nil {
		brick.Brick.MainID = firstNotFoundID
		zap.L().Debug("No valid price found for any brick ID, set MainID to first not-found ID",
			zap.String("main_id", string(*firstNotFoundID)),
			zap.String("design_id", string(brick.DesignID)),
			zap.String("currency", currency.String()),
		)
	}

	return brick, nil
}

// batchHandlerBricksProgress handles batch progress updates for bricks, including updating the set, caching, and sending updates to clients
func (h *Handler) batchHandlerBricksProgress(
	ctx context.Context,
	rsID uuid.UUID,
	s *set.Set,
	batch []set.BrickSet,
	progress *Progress,
	dataType DataType,
) error {
	// Add Bricks to set
	s.Bricks = append(s.Bricks, batch...)

	// Update shared progress with current batch
	for _, brick := range batch {
		progress.AddItem(brick)
	}

	// Prepare for sending (updates done to include current batch)
	progress.PrepareForSend()

	// Send batch to frontend
	h.PushBatchProgress(rsID, dataType, *progress)

	// Complete batch (resets items array for next batch)
	progress.CompleteBatch()

	// Update set in cache
	if err := set.SetRedisSet(ctx, *s, true); err != nil {
		handleFatalError(h, rsID, s.Id, set.FetchErrorBatchCache, *s, DataTypeSet, err,
			"Failed to update Redis set with batch data")
		return err
	}

	return nil
}

// handleFatalError handles fatal errors during set fetching
// It logs the error asynchronously, updates cache, and notifies clients
func handleFatalError(h *Handler, rsID uuid.UUID, setID uuid.UUID, step set.FetchErrorStep, data set.Set, dataType DataType, err error, msg string, fields ...zap.Field) {
	// Log to async error logger for persistence/tracking
	h.logCriticalError(setID, string(dataType), err)

	// Log immediately to zap for operational visibility
	zap.L().Error(msg, append(fields, zap.Error(err))...)

	// Mark set as failed in cache with error details
	data.FetchStatus = set.FetchStatusFailed
	data.FetchError = &set.FetchError{
		Message: msg,
		Step:    step,
	}

	// Try to update cache - best effort, don't fail if this fails
	if cacheErr := set.SetRedisSet(context.Background(), data, false); cacheErr != nil {
		// Log cache update failure but don't propagate
		h.logWarning(setID, "Redis.UpdateFailedStatus", cacheErr)
		zap.L().Warn("Failed to update failed status in cache",
			zap.Error(cacheErr),
			zap.String("set_id", setID.String()),
		)
	}

	// Notify all connected clients
	h.PushChange(rsID, setID, dataType, DataTypeFailed)
}
