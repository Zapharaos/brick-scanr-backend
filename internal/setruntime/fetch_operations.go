package setruntime

import (
	"context"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/workerpool"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchCompleteSetDetails performs a complete fetch of set details including inventory and prices
// This is used when there's no cached data or when cached data is stale
func (h *Handler) FetchCompleteSetDetails(
	ctx context.Context,
	rs *RuntimeSet,
	setID uuid.UUID,
	bricklinkSet set.Set,
	locale language.Tag,
	currency language.Tag,
) {
	defer func() {
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
	cpRedisSet := bricklinkSet

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

	// Fetch set details asynchronously (independent from inventory/prices)
	// Redis locks provide thread-safety across all operations
	detailsErrChan := make(chan error, 1)
	go func() {
		detailsErrChan <- h.fetchDetails(ctx, rs.ID, setID, &cpRedisSet, locale, currency)
	}()

	// Fetch inventory from BrickLink (sequential - needed for prices)
	if err := h.fetchInventory(ctx, rs.ID, setID, &cpRedisSet); err != nil {
		<-detailsErrChan // Wait for goroutine to complete before returning
		return           // Error already handled
	}

	// Clear bricks between batch progress to avoid duplicates
	rs.ClearBricks()

	// Fetch prices from Pick-a-Brick (sequential - depends on inventory)
	if err := h.fetchBricks(ctx, rs.ID, setID, &cpRedisSet, locale, currency); err != nil {
		<-detailsErrChan // Wait for goroutine to complete before returning
		return           // Error already handled
	}

	// Wait for async details fetch to complete
	if detailsErr := <-detailsErrChan; detailsErr != nil {
		// Details fetch failed, but inventory and prices succeeded
		// This is already logged and handled by fetchDetails, just return
		return
	}

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

// fetchDetails fetches the details for a set
func (h *Handler) fetchDetails(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag) error {
	// Fetch set details from BrickLink
	bricklinkSet, err := bricklink.C().FetchSetDetails(cpRedisSet.BricklinkID)
	if err != nil {
		// Failed to fetch details => FATAL
		handleFatalError(h, rsID, setID, set.FetchErrorFetchDetails, *cpRedisSet, DataTypeBricklinkDetails, err,
			"Failed to fetch details from BrickLink")
		return err
	}

	// Update cpRedisSet with fetched details
	cpRedisSet.Number = bricklinkSet.StrItemNo
	cpRedisSet.YearReleased = bricklinkSet.NYearReleased
	cpRedisSet.Parts = bricklinkSet.NInvPartCnt
	cpRedisSet.ImageURL = bricklinkSet.ImageList.GetMainImageURL()

	err = set.SetRedisSet(ctx, *cpRedisSet, true)
	if err != nil {
		handleFatalError(h, rsID, setID, set.FetchErrorDetailsCache, *cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set inventory")
		return err
	}
	h.PushChange(rsID, setID, DataTypeSet, DataTypeUpdated)

	ok, err := h.fetchLegoProductDetails(ctx, rsID, setID, cpRedisSet, locale, currency, false)
	if err == nil && ok {
		h.PushChange(rsID, setID, DataTypeSet, DataTypeUpdated)
	}

	return nil
}

// fetchLegoProductDetails fetches product details from LEGO and updates the set
func (h *Handler) fetchLegoProductDetails(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag, priceOnly bool) (bool, error) {
	// Fetch product details from LEGO
	legoProduct, err := lego.C().FetchProductDetails(cpRedisSet.Number, currency)
	if err != nil {
		// Non-fatal: LEGO data is supplementary, log warning and continue
		// This can happen for older or discontinued sets not present in LEGO's API, or for specific locales etc.
		zap.L().Warn("Failed to fetch product details from LEGO",
			zap.Error(err),
			zap.String("set_number", cpRedisSet.Number),
			zap.String("set_id", setID.String()),
		)
	} else {

		if !priceOnly {
			cpRedisSet.Slug = legoProduct.Slug
			cpRedisSet.BuildLegoURL(locale)
			cpRedisSet.BuildInstructionsURL(locale)
			cpRedisSet.Status = set.MapLegoProductStatus(*legoProduct)
		}

		// Update set with fetched price
		lp := set.MapPriceFromLego(legoProduct.Variant.Price)
		lp.FetchedAt = time.Now().UnixMilli()
		if cpRedisSet.Prices == nil {
			cpRedisSet.Prices = make(map[language.Tag]*set.Price)
		}
		cpRedisSet.Prices[currency] = &lp
		cpRedisSet.MustApplyCurrency(currency)

		err = set.SetRedisSet(ctx, *cpRedisSet, true)
		if err != nil {
			handleFatalError(h, rsID, setID, set.FetchErrorDetailsCache, *cpRedisSet, DataTypeSet, err,
				"Failed to update Redis set inventory")
			return false, err
		}

		return true, nil
	}
	return false, nil
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
	workerFunc := func(ctx context.Context, item bricklink.InventoryItem) (set.Brick, error) {
		skipRedis := len(item.ItemIDs) == 0
		var brick set.Brick
		var errW error

		// Make sure we have at least one ItemID to lookup in cache
		if !skipRedis {
			// Try to find in cache first
			brick, errW = set.GetRedisBrick(ctx, set.BrickID(item.ItemIDs[0]), set.DesignID(item.ItemNo))
		}

		// If no redis yet, map from BrickLink item
		if skipRedis || errW != nil {
			brick = set.MapBrickFromBricklinkInventoryItem(item)
		} else if !skipRedis && errW == nil {
			// Different sources can update the brick redis instance, and bricklink is not the preferred source
			// Therefore, we must be careful when updating the brick instance in redis and only update
			brick = set.SafeMapBrickFromBricklinkInventoryItem(brick, item)
		}

		// Temporarily hide index and quantity, redis brick instance can be shared across sets
		quantity, index := brick.CleanupForRedis()

		// Cache the brick: either for the first time, or to refresh the TTL
		if cacheErr := set.SetRedisBrick(ctx, brick, true); cacheErr != nil {
			zap.L().Warn("Failed to cache brick",
				zap.Error(cacheErr),
				zap.String("brick_design_id", item.ItemNo),
			)
		}

		// Restore index and quantity
		brick.Quantity = quantity
		brick.BrickMinimal.Index = index

		return brick, nil
	}

	// Batch handler: send batch to frontend and update Redis
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.Brick) error {
		// Add Bricks to set
		cpRedisSet.Bricks = append(cpRedisSet.Bricks, batch...)

		// Update shared progress with current batch
		for _, brick := range batch {
			bprogress.AddItem(brick)
		}

		// Prepare for sending (updates done to include current batch)
		bprogress.PrepareForSend()

		// Send batch to frontend
		h.PushBatchProgress(rsID, DataTypeBricklinkBricks, *bprogress)

		// Complete batch (resets items array for next batch)
		bprogress.CompleteBatch()

		// Update set in cache
		if err := set.SetRedisSet(ctx, *cpRedisSet, true); err != nil {
			handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
				"Failed to update Redis set with inventory batch")
			return err
		}

		return nil
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
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
func (h *Handler) fetchBricks(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag) error {
	// Calculate total bricks
	bricksSize := len(cpRedisSet.Bricks)

	// Create optimal config based on brick count
	config := workerpool.NewConfigOptimal(bricksSize, len(h.sets))

	// TODO : inform frontend that 429 rate limit was reached?

	// Create shared progress tracker
	bprogress := NewProgress(bricksSize, config.BatchSize)

	zap.L().Debug("Starting worker pool for price fetching",
		zap.Int("bricks_size", bricksSize),
		zap.Int("workers", config.Workers),
		zap.Int("batch_size", config.BatchSize),
	)

	// Worker function: fetch prices for a single design ID
	workerFunc := func(ctx context.Context, brick set.Brick) (set.Brick, error) {

		// Try to find the best brick ID
		for _, elementID := range brick.IDs {

			// Fetch for elementID
			results, err := pickabrick.C().FetchBricksByBrickID(string(elementID), locale, currency)
			if err != nil {
				handleNonFatalError(err, "Failed to fetch brick by elementID",
					zap.String("elementID", string(elementID)),
					zap.String("set_id", setID.String()))
				continue // Try next ID
			}

			if len(results) == 0 {
				continue
			}

			pabb := results[0] // There should be only one matching brick per ID

			// Check if currency price is already set and valid
			price, ok := brick.Prices.GetPrice(currency)
			if ok && price.IsValid() && price.IsLower(pabb.Price.CentAmount) &&
				!price.IsOutdated(database.DB().Redis().TTLS.BrickPrice) {
				continue
			}

			// Update brick with fetched data from pickabrick
			brick = set.MapBrickFromPickabrick(brick, elementID, pabb, locale, currency)

			// Temporarily hide index and quantity, redis brick instance can be shared across sets
			quantity, index := brick.CleanupForRedis()

			// Update brick in cache
			if err = set.SetRedisBrick(ctx, brick, true); err != nil {
				zap.L().Warn("Failed to update brick price in cache",
					zap.Error(err),
					zap.String("brick_design_id", string(brick.DesignID)),
					zap.String("brick_id", string(elementID)),
				)
			}

			// Apply currency only locally
			brick.MustApplyCurrency(currency)

			// Restore index and quantity
			brick.Quantity = quantity
			brick.BrickMinimal.Index = index

			// Calculate brick total price
			brick.CalculateTotalPrice()
		}

		return brick, nil
	}

	// Batch handler: send batch to frontend
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.Brick) error {
		// Add Bricks to set
		cpRedisSet.Bricks = append(cpRedisSet.Bricks, batch...)

		// Update shared progress with current batch
		for _, brick := range batch {
			bprogress.AddItem(brick)
		}

		// Prepare for sending (updates done to include current batch)
		bprogress.PrepareForSend()

		// Send batch to frontend
		h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)

		// Complete batch (resets items array for next batch)
		bprogress.CompleteBatch()

		// Update set in cache
		if err := set.SetRedisSet(ctx, *cpRedisSet, true); err != nil {
			handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
				"Failed to update Redis set with inventory batch")
			return err
		}

		return nil
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		zap.L().Warn("Worker encountered error processing price job",
			zap.Error(err),
		)
	})

	// Copy bricks and clear from cpRedisSet for batch processing
	cpBricks := make([]set.Brick, len(cpRedisSet.Bricks))
	copy(cpBricks, cpRedisSet.Bricks)
	cpRedisSet.Bricks = []set.Brick{} // Clear bricks to avoid duplication in batch handler

	// Process all price jobs
	if err := pool.Process(cpBricks); err != nil {
		handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
			"Failed to process prices with worker pool")
		return err
	}

	zap.L().Debug("Worker pool completed price fetching",
		zap.Int("bricks_size", bricksSize),
	)

	return nil
}

// MissingBrickJob represents a job for fetching a specific brick
type MissingBrickJob struct {
	Brick set.Brick
	Index int // Original index for ordering
}

// MissingBrickJobResult represents the result of processing a brick job
type MissingBrickJobResult struct {
	Brick set.Brick
	Index int // Original index for ordering
}

// FetchMissingBricks fetches Bricks that are missing for the requested currency
func (h *Handler) FetchMissingBricks(
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

	// Check if set price is outdated
	if cacheResult.SetPriceOutdated {
		ok, err := h.fetchLegoProductDetails(ctx, rs.ID, setID, &cacheResult.Set, locale, currency, true)
		if err == nil && ok {
			h.PushChange(rs.ID, setID, DataTypeSet, DataTypeUpdated)
		}
	}

	bricksCount := len(cacheResult.BricksWoPrices)
	if bricksCount == 0 {
		h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)
		return
	}

	// Create jobs from bricks
	jobs := make([]MissingBrickJob, 0, bricksCount)
	for i, brick := range cacheResult.BricksWoPrices {
		jobs = append(jobs, MissingBrickJob{
			Brick: brick,
			Index: i,
		})
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
	workerFunc := func(ctx context.Context, job MissingBrickJob) (MissingBrickJobResult, error) {
		brick := job.Brick
		result := MissingBrickJobResult{
			Brick: brick,
			Index: job.Index,
		}

		brickID, err := brick.GetBrickIDForRedis()
		if err != nil {
			zap.L().Warn("Failed to get brick ID for redis",
				zap.Error(err),
			)
			// Apply currency and return brick without price update
			brick.MustApplyCurrency(currency)
			result.Brick = brick
			return result, nil
		}

		// Fetch brick with price for the requested currency
		matchingBricks, err := pickabrick.C().FetchBricksByBrickID(string(brickID), locale, currency)
		if err != nil {
			// Failed to fetch - log and continue
			zap.L().Warn("Failed to fetch brick for currency",
				zap.Error(err),
				zap.String("brick_id", string(brickID)),
				zap.String("design_id", string(brick.DesignID)),
				zap.String("currency", currency.String()),
			)
			// Apply currency and return brick without price update
			brick.MustApplyCurrency(currency)
			result.Brick = brick
			return result, nil
		}

		// Find matching brick and update price
		priceUpdated := false
		for _, mb := range matchingBricks {
			if set.BrickID(mb.ID) == brickID {

				// Update brick with fetched data from pickabrick
				brick = set.MapBrickFromPickabrick(brick, brickID, mb, locale, currency)

				// Temporarily hide index and quantity, redis brick instance can be shared across sets
				quantity, index := brick.CleanupForRedis()

				// Update brick in cache
				if err = set.SetRedisBrick(ctx, brick, true); err != nil {
					zap.L().Warn("Failed to update brick price in cache",
						zap.Error(err),
						zap.String("brick_id", string(brickID)),
					)
				}

				// Apply currency only locally
				brick.MustApplyCurrency(currency)

				// Restore index and quantity
				brick.Quantity = quantity
				brick.BrickMinimal.Index = index

				// Calculate brick total price
				brick.CalculateTotalPrice()

				priceUpdated = true
				break
			}
		}

		if !priceUpdated {
			// Brick not found in API response - might be discontinued
			brick.MustApplyCurrency(currency)
		}

		result.Brick = brick
		return result, nil
	}

	// Batch handler: send batch to frontend
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []MissingBrickJobResult) error {
		// Add all updated bricks to progress
		for _, result := range batch {
			bprogress.AddItem(result.Brick)
		}

		// Prepare for sending
		bprogress.PrepareForSend()

		// Send batch to frontend
		h.PushBatchProgress(rs.ID, DataTypePickabrickBricks, *bprogress)

		// Complete batch (resets items array for next batch)
		bprogress.CompleteBatch()

		return nil
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		zap.L().Warn("Worker encountered error processing brick price",
			zap.Error(err),
		)
	})

	// Process all brick price jobs
	if err := pool.Process(jobs); err != nil {
		zap.L().Error("Failed to process brick prices with worker pool",
			zap.Error(err),
			zap.String("runtime_id", rs.ID.String()),
		)
		h.PushChange(rs.ID, setID, DataTypeSet, DataTypeFailed)
		return
	}

	// Mark as completed
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)

	zap.L().Info("Price fetch completed",
		zap.String("runtime_id", rs.ID.String()),
	)
}

// handleFatalError handles fatal errors during set fetching
func handleFatalError(h *Handler, rsID uuid.UUID, setID uuid.UUID, step set.FetchErrorStep, data set.Set, dataType DataType, err error, msg string, fields ...zap.Field) {
	// Log the error
	zap.L().Error(msg, append(fields, zap.Error(err))...)

	// Mark set as failed in cache with error details
	data.FetchStatus = set.FetchStatusFailed
	data.FetchError = &set.FetchError{
		Message: msg,
		Step:    step,
	}

	// Try to update cache - best effort, don't fail if this fails
	_ = set.SetRedisSet(context.Background(), data, false)

	// Notify all connected clients
	h.PushChange(rsID, setID, dataType, DataTypeFailed)
}

// handleNonFatalError handles non-critical errors during set fetching
func handleNonFatalError(err error, msg string, fields ...zap.Field) {
	zap.L().Warn(msg, append(fields, zap.Error(err))...)
}
