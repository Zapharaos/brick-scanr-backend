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

// FetchPricesForBricks fetches prices for bricks that are missing them for the requested currency
// This is used when we have cached brick data but need prices for a different currency
func (h *Handler) FetchPricesForBricks(
	ctx context.Context,
	rsID uuid.UUID,
	setID uuid.UUID,
	bricks []set.Brick,
	locale language.Tag,
	currency language.Tag,
) {
	defer h.StopRuntimeSet(rsID)

	zap.L().Info("Starting price fetch",
		zap.String("runtime_id", rsID.String()),
		zap.Int("bricks_count", len(bricks)),
	)

	// Calculate optimal batch size
	batchSize := workerpool.CalculateOptimalBatchSize(len(bricks))
	bprogress := NewProgress(len(bricks), batchSize)

	// Fetch prices for each brick needing updates
	for _, brick := range bricks {
		brickID, err := brick.GetBrickIDForRedis()
		if err != nil {
			zap.L().Warn("Failed to get brick ID for redis",
				zap.Error(err),
			)
			continue
		}

		// Fetch brick with price for the requested currency
		matchingBricks, err := pickabrick.C().FetchBricksByBrickID(string(brickID), locale, currency)
		if err != nil {
			// Failed to fetch price - log and continue
			zap.L().Warn("Failed to fetch brick price for currency",
				zap.Error(err),
				zap.String("brick_id", string(brickID)),
				zap.String("design_id", string(brick.DesignID)),
				zap.String("currency", currency.String()),
			)
			// Add brick without new price to progress
			bprogress.AddItem(brick)

			// Send batch update if reached limit
			if bprogress.HasReachedBatchLimit() && !bprogress.EmptyItems() {
				h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)
				bprogress.CompleteBatch()
			}
			continue
		}

		// Find matching brick and update price
		priceUpdated := false
		for _, mb := range matchingBricks {
			if set.BrickID(mb.ID) == brickID {
				// Update brick with fetched price
				pbp := set.MapPriceFromPickabrick(mb.Price)
				pbp.ItemID = string(brickID)
				pbp.FetchedAt = time.Now().UnixMilli()
				if brick.Prices == nil {
					brick.Prices = make(map[language.Tag]*set.Price)
				}
				brick.Prices[currency] = &pbp

				// Cache the updated brick
				if err = set.SetRedisBrick(ctx, brick, false); err != nil {
					zap.L().Warn("Failed to cache updated brick price",
						zap.Error(err),
						zap.String("brick_id", string(brickID)),
					)
				}

				// Apply currency
				brick.ApplyCurrency(currency)
				priceUpdated = true
				break
			}
		}

		if !priceUpdated {
			// Brick not found in API response - might be discontinued
			brick.ApplyCurrency(currency)
		}

		// Update progress
		bprogress.AddItem(brick)

		// Send batch update if reached limit
		if bprogress.HasReachedBatchLimit() && !bprogress.EmptyItems() {
			h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)
			bprogress.CompleteBatch()
		}
	}

	// Send final batch if there are remaining items
	if !bprogress.EmptyItems() {
		h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)
		bprogress.CompleteBatch()
	}

	// Mark as completed
	h.PushChange(rsID, setID, DataTypeSet, DataTypeCompleted)

	zap.L().Info("Price fetch completed",
		zap.String("runtime_id", rsID.String()),
	)
}

// FetchCompleteSetDetails performs a complete fetch of set details including inventory and prices
// This is used when there's no cached data or when cached data is stale
func (h *Handler) FetchCompleteSetDetails(
	ctx context.Context,
	rsID uuid.UUID,
	setID uuid.UUID,
	bricklinkSet set.Set,
	locale language.Tag,
	currency language.Tag,
) {
	defer func() {
		// Give clients time to receive the message before cleanup
		time.Sleep(3 * time.Second)
		h.StopRuntimeSet(rsID)
	}()

	zap.L().Info("Starting complete set details fetch",
		zap.String("runtime_id", rsID.String()),
		zap.String("set_id", setID.String()),
	)

	// Initialize copy of set for Redis
	cpRedisSet := bricklinkSet

	// Cache the status => for concurrent access to the websocket
	cpRedisSet.FetchStatus = set.FetchStatusFetching
	err := set.SetRedisSet(ctx, cpRedisSet, true)
	if err != nil {
		// Failed to update the set in cache => FATAL
		handleFatalError(h, rsID, setID, set.FetchErrorInitCache, cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set status")
		return
	}
	h.PushChange(rsID, setID, DataTypeSet, DataTypeCreated)

	// Fetch set details asynchronously (independent from inventory/prices)
	// Redis locks provide thread-safety across all operations
	detailsErrChan := make(chan error, 1)
	go func() {
		detailsErrChan <- h.fetchDetails(ctx, rsID, setID, &cpRedisSet, locale, currency)
	}()

	// Fetch inventory from BrickLink (sequential - needed for prices)
	if err := h.fetchInventory(ctx, rsID, setID, &cpRedisSet); err != nil {
		<-detailsErrChan // Wait for goroutine to complete before returning
		return           // Error already handled
	}

	// Fetch prices from Pick-a-Brick (sequential - depends on inventory)
	if err := h.fetchPrices(ctx, rsID, setID, &cpRedisSet, locale, currency); err != nil {
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
		handleFatalError(h, rsID, setID, set.FetchErrorFinalCache, cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set with final data")
		return
	}
	h.PushChange(rsID, setID, DataTypeSet, DataTypeCompleted)

	zap.L().Info("Successfully completed set details fetch",
		zap.String("runtime_id", rsID.String()),
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

	// Fetch product details from LEGO
	legoProduct, err := lego.C().FetchProductDetails(cpRedisSet.Number, locale, currency)
	if err != nil {
		// Non-fatal: LEGO data is supplementary, log warning and continue
		// This can happen for older or discontinued sets not present in LEGO's API, or for specific locales etc.
		zap.L().Warn("Failed to fetch product details from LEGO",
			zap.Error(err),
			zap.String("set_number", cpRedisSet.Number),
			zap.String("set_id", setID.String()),
		)
	} else {
		cpRedisSet.Slug = legoProduct.Slug
		cpRedisSet.BuildLegoURL(locale)
		cpRedisSet.BuildInstructionsURL(locale)
		cpRedisSet.Status = set.MapLegoProductStatus(*legoProduct)

		// Update set with fetched price
		lp := set.MapPriceFromLego(legoProduct.Variant.Price)
		lp.FetchedAt = time.Now().UnixMilli()
		if cpRedisSet.Prices == nil {
			cpRedisSet.Prices = make(map[language.Tag]*set.Price)
		}
		cpRedisSet.Prices[currency] = &lp
		cpRedisSet.ApplyCurrency(currency)

		err = set.SetRedisSet(ctx, *cpRedisSet, true)
		if err != nil {
			handleFatalError(h, rsID, setID, set.FetchErrorDetailsCache, *cpRedisSet, DataTypeSet, err,
				"Failed to update Redis set inventory")
			return err
		}
		h.PushChange(rsID, setID, DataTypeSet, DataTypeUpdated)
	}

	return nil
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

	itemCount := len(inventory.Items)
	if itemCount == 0 {
		return nil
	}

	// Create optimal config based on item count
	config := workerpool.NewConfigWithDefaults(itemCount)

	zap.L().Debug("Starting worker pool for inventory processing",
		zap.Int("item_count", itemCount),
		zap.Int("workers", config.Workers),
		zap.Int("batch_size", config.BatchSize),
	)

	// Worker function: process a single inventory item
	workerFunc := func(ctx context.Context, item bricklink.InventoryItem) (set.Brick, error) {
		// Try to find in cache first
		brick, err := set.GetRedisBrick(ctx, set.BrickID(item.ItemIDs[0]), set.DesignID(item.ItemNo))
		if err != nil {
			// Not found in cache, map from BrickLink item
			brick = set.MapBrickFromBricklinkInventoryItem(item)
		}

		// Cache the brick: either for the first time, or to refresh the TTL
		if cacheErr := set.SetRedisBrick(ctx, brick, true); cacheErr != nil {
			zap.L().Warn("Failed to cache brick",
				zap.Error(cacheErr),
				zap.String("brick_design_id", item.ItemNo),
			)
		}

		return brick, nil
	}

	// Batch handler: send batch to frontend and update Redis
	batchHandler := func(batch []set.Brick) error {
		// Add bricks to set
		cpRedisSet.Bricks = append(cpRedisSet.Bricks, batch...)

		// Create progress update
		bprogress := NewProgress(itemCount, config.BatchSize)
		for _, brick := range batch {
			bprogress.AddItem(brick)
		}

		// Send batch to frontend
		h.PushBatchProgress(rsID, DataTypeBricklinkBricks, *bprogress)

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
	if err := pool.Process(inventory.Items); err != nil {
		handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
			"Failed to process inventory with worker pool")
		return err
	}

	zap.L().Debug("Worker pool completed inventory processing",
		zap.Int("item_count", itemCount),
		zap.Int("bricks_processed", len(cpRedisSet.Bricks)),
	)

	return nil
}

// fetchPrices fetches prices for all bricks in a set from Pick-a-Brick
func (h *Handler) fetchPrices(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag) error {
	bmap := set.NewBrickMap(cpRedisSet.Bricks)

	// Calculate optimal batch size
	batchSize := workerpool.CalculateOptimalBatchSize(len(bmap.BricksByDesign))
	bprogress := NewProgress(len(bmap.BricksByDesign), batchSize)

	// TODO : ISSUE #1 - Search Alternate Items
	// TODO : ISSUE #8 - Async : make inventory fetching async if possible
	for designID := range bmap.BricksByDesign {
		// Fetch bricks by designID
		matchingBricks, err := pickabrick.C().FetchBricksByDesignID(string(designID), locale, currency)
		if err != nil {
			handleNonFatalError(err, "Failed to fetch bricks by designID",
				zap.String("designID", string(designID)),
				zap.String("set_id", setID.String()))
			// Continue to next design ID
			bprogress.Increment()
			continue
		}

		// Process matching bricks
		for _, mb := range matchingBricks {
			brickID := set.BrickID(mb.ID)
			brick, ok := bmap.GetBrickByID(brickID)
			if !ok {
				// No matching brick ID, skip
				continue
			}

			// Check if currency price is already set and valid
			price := brick.GetPriceForLocale(currency)
			if price.IsValid() && price.IsLower(mb.Price.CentAmount) &&
				!price.IsOutdated(database.DB().Redis().TTLS.BrickPrice) {
				continue
			}

			// Update brick with fetched price
			pbp := set.MapPriceFromPickabrick(mb.Price)
			pbp.ItemID = string(brickID)
			pbp.FetchedAt = time.Now().UnixMilli()
			if brick.Prices == nil {
				brick.Prices = make(map[language.Tag]*set.Price)
			}
			brick.Prices[currency] = &pbp

			// Update brick in cache
			if err = set.SetRedisBrick(ctx, brick, false); err != nil {
				zap.L().Warn("Failed to update brick price in cache",
					zap.Error(err),
					zap.String("brick_design_id", string(designID)),
					zap.String("brick_id", string(brickID)),
				)
			}

			// Apply currency only locally
			brick.ApplyCurrency(currency)

			// Update progress - add brick to batch
			bprogress.AddItem(brick)
		}

		// Update progress counter (even if no matching bricks found)
		bprogress.Increment()

		// Check batch progress
		if bprogress.HasReachedBatchLimit() && !bprogress.EmptyItems() {
			h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)
			bprogress.CompleteBatch()
		}
	}

	// TODO : fix - total is way smaller than actual processed items, making progress percentage incorrect

	// Send final batch if there are remaining items
	if bprogress.BatchCurr > 0 {
		h.PushBatchProgress(rsID, DataTypePickabrickBricks, *bprogress)
		bprogress.CompleteBatch()
	}

	return nil
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
