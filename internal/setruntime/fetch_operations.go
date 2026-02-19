package setruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
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
	lang language.Tag,
	xlocale language.Tag,
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

	// Update runtime fetch status + push initial data to clients
	rs.set.SetFetchStatus(set.FetchStatusFetching)
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCreated)

	// Check if set price is already cached and valid
	if !rs.Read().HasValidPrice() {
		h.fetchSetDetails(ctx, rs, lang, xlocale, true)
	}

	// Fetch inventory from BrickLink (sequential)
	ok, err := h.fetchInventory(ctx, rs, lang)
	if err != nil {
		return // Error already handled
	}

	// Fetch bricks only if an inventory was found
	if ok {
		// Fetch prices from Pick-a-Brick (sequential - depends on inventory)
		if err = h.fetchBricks(ctx, rs, lang, xlocale, false); err != nil {
			return // Error already handled
		}
	}

	// Mark set fetch completed, send final data to make sure clients have everything
	rs.set.SetFetchStatus(set.FetchStatusCompleted)
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)

	// Update the set locale in redis
	err = set.RedisSetLocale(ctx, rs.Read().Locale, xlocale, false, true)
	if err != nil {
		h.logWarning(setID, "Redis.SetLocale", err)
		zap.L().Warn("Failed to update set locale in cache after finishing the complete set fetch",
			zap.Error(err),
			zap.String("set_id", setID.String()),
		)
	}

	zap.L().Info("Successfully completed set details fetch",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("set_id", setID.String()),
	)
}

// FetchFetchSetIncomplete fetches Bricks that are missing for the requested currency
func (h *Handler) FetchFetchSetIncomplete(
	ctx context.Context,
	rs *RuntimeSet,
	setID uuid.UUID,
	missingLocale bool,
	missingSetPrice bool,
	lang language.Tag,
	xlocale language.Tag,
) {
	defer func() {
		// Give clients time to receive the message before cleanup
		time.Sleep(3 * time.Second)
		h.StopRuntimeSet(rs.Key())
	}()

	zap.L().Info("Starting bricks fetch",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("key", rs.Key().String()),
		zap.Int("bricks_count", len(rs.bricks.missing)),
	)

	// Push set manually (to avoid pulling from cache which does not have preprocessed set data)
	rs.set.SetFetchStatus(set.FetchStatusFetching)
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCreated)

	// Check if set locale is missing or price is outdated
	if missingLocale || missingSetPrice {
		priceOnly := !missingLocale // Fetch more all product details if locale is missing
		h.fetchSetDetails(ctx, rs, lang, xlocale, priceOnly)
	}

	// Check if we have access to the inventory
	if rs.ihAccess.IsValid() {

		// Listen to inventory updates from the worker that is processing the inventory
		inventory := rs.ihAccess.Inventory.listen(func(batch Progress) {
			// Handle the batch
			h.batchHandlerListenProgress(rs, batch)
			return
		})

		// After listening, receive the final inventory data and clear runtime inventory handler

		// Update the set with the latest inventory data
		set.SortBricksByIndex(inventory)
		rs.set.SetBricks(inventory)

		var finalBatch Progress
		finalBatch.Total = len(inventory)
		finalBatch.Done = len(inventory)

		// Update the missing bricks in the runtime set based on the new inventory data
		for _, b := range inventory {
			if _, ok := rs.bricks.missing[b.UUID]; !ok {
				// This brick is not in the missing map, add it
				rs.bricks.appendMissing(b)
			}
			finalBatch.AddItem(b)
		}

		// Send final inventory batch to clients
		h.PushBatchProgress(rs.ID, DataTypeBricklinkBricks, finalBatch)

		// We don't need the inventory handler anymore, reset it to avoid any potential misuse later on
		rs.ihAccess.Reset()
	}

	// Fetch prices from Pick-a-Brick (sequential - depends on inventory)
	if err := h.fetchBricks(ctx, rs, lang, xlocale, true); err != nil {
		return // Error already handled
	}

	// Mark set fetch completed, send final data to make sure clients have everything
	rs.set.SetFetchStatus(set.FetchStatusCompleted)
	h.PushChange(rs.ID, setID, DataTypeSet, DataTypeCompleted)

	// Update the set locale in redis
	err := set.RedisSetLocale(ctx, rs.Read().Locale, xlocale, false, true)
	if err != nil {
		h.logWarning(setID, "Redis.SetLocale", err)
		zap.L().Warn("Failed to update set locale in cache after finishing the incomplete set fetch",
			zap.Error(err),
			zap.String("set_id", setID.String()),
		)
	}

	zap.L().Info("Successfully completed set details fetch for incomplete set",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("set_id", setID.String()),
	)
}

// fetchSetDetails fetches set details from LEGO and updates the set
func (h *Handler) fetchSetDetails(ctx context.Context, rs *RuntimeSet, lang language.Tag, xlocale language.Tag, priceOnly bool) {
	setID := rs.Read().ID
	// Get a copy of the current locale to pass to FetchLegoProductDetails
	locale := rs.Read().Locale
	ok, err := set.FetchLegoProductDetails(ctx, setID, &locale, lang, xlocale, priceOnly)
	if err == nil && ok {
		// Update the runtime set with the modified locale
		rs.set.UpdateLocale(locale)

		// Successfully fetched details, send update to clients
		h.PushChange(rs.ID, setID, DataTypeSet, DataTypeUpdated)

		// Detect a slug change that needs to be updated in Redis
		if rs.Read().BricklinkSlug == "" && locale.Slug != "" && locale.Slug != rs.Read().BricklinkSlug {
			err = set.RedisSetSetIDForSlug(ctx, locale, true)
			if err != nil {
				zap.L().Warn("Failed to cache slug to set ID mapping",
					zap.Error(err),
					zap.String("set_id", locale.ID.String()),
					zap.String("slug", locale.Slug),
				)
			}
		}

		// No need to update the set locale in redis, as FetchLegoProductDetails already handles it
	}
	// Error not fatal and already handled in FetchLegoProductDetails
}

// fetchInventory fetches the inventory for a set from BrickLink using a worker pool
func (h *Handler) fetchInventory(ctx context.Context, rs *RuntimeSet, tag language.Tag) (bool, error) {

	// Mark inventory as fetching to update clients and avoid any potential concurrent fetches
	rs.set.SetInventoryStatus(set.FetchStatusFetching)
	rs.cacheSet(ctx, false)

	// Fetch inventory from BrickLink
	inventory, err := bricklink.C().FetchInventory(rs.Read().BricklinkID, rs.Read().BricklinkNumber, tag)
	if err != nil {
		// Check if it's a not-found error
		if errors.Is(err, bricklink.ErrInventoryNotFound) {
			rs.set.SetInventoryStatus(set.FetchStatusCompleted)
			rs.cacheSet(ctx, false)
			return false, nil
		}

		// Failed to fetch inventory => FATAL
		handleFatalError(h, rs, set.FetchErrorFetchInventory, DataTypeBricklinkBricks, err, "Failed to fetch inventory from BrickLink")
		return false, err
	}

	// No items in inventory, we can mark inventory as complete and skip processing
	inventorySize := len(inventory.RegularItems)
	if inventorySize == 0 {
		rs.set.SetInventoryStatus(set.FetchStatusCompleted)
		rs.cacheSet(ctx, false)
		return false, nil
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
		// Map BrickLink inventory item to local Brick representation
		bCore, bInventory := brick.MapNewFromBricklinkInventoryItem(item)

		// Init brick
		bSet := set.NewBrick(bInventory, brick.Locale{
			Core: bCore,
		})

		// TODO : handle IDs ? fetch on bricklink?
		// TODO : if not then how do we let others related ones know about the IDs ?

		// When applying locales from cache, we might overwrite the IDs slice
		// To avoid loosing data, we reset it to the original slice
		originalIDs := bCore.IDs

		var firstNotFoundLocale *brick.Locale
		var validLocale bool

		// Process any existing itemID through cache first
		for _, id := range originalIDs {

			// Try to find in cache first
			bRedis, valid, notFound := bSet.Locale.LoadFromRedis(ctx, id.ElementID, tag, false, true)
			if notFound && firstNotFoundLocale == nil {
				// Brick Locale cached with not-found price
				// We can consider this brick as up-to-date with a not-found price
				firstNotFoundLocale = &bRedis
				continue // Try next ID
			}
			if valid {
				// Brick Locale cached with valid price and up-to-date, return safely
				bSet = set.NewBrickWithUUID(bSet.UUID, bSet.Inventory, bRedis)
				validLocale = true
				continue // Try next ID
			}
		}

		// bSet has already been updated with a valid locale from cache
		if validLocale {
			bSet.SetIDs(originalIDs) // Reset to original slice to avoid losing data
			return bSet, nil
		}

		// No 100% valid locale was found, but at least one not-found brick locale was found
		// We can consider this brick as up-to-date with a not-found price, return safely
		if firstNotFoundLocale != nil {
			bSet = set.NewBrickWithUUID(bSet.UUID, bSet.Inventory, *firstNotFoundLocale)
			bSet.SetIDs(originalIDs) // Reset to original slice to avoid losing data
			return bSet, nil
		}

		// Default - use fetched data
		return bSet, nil
	}

	// Batch handler: send batch to frontend and update Redis
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.Brick) error {
		return h.batchHandlerBricksProgress(rs, batch, bprogress, DataTypeBricklinkBricks, false)
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		h.logWarning(rs.Read().ID, "Worker.InventoryProcessing", err)
		zap.L().Warn("Worker encountered error processing brick",
			zap.Error(err),
		)
	})

	// Process all inventory items
	if err = pool.Process(inventory.RegularItems); err != nil {
		handleFatalError(h, rs, set.FetchErrorBatchCache, DataTypeSet, err, "Failed to process inventory with worker pool")
		return false, err
	}

	// Mark inventory fetching as complete to free the listeners and clear runtime inventory handler
	IH().StopInventory(rs.Read().ID)

	// We don't need the inventory handler anymore, reset it to avoid any potential misuse later on
	rs.ihAccess.Reset()

	// Cache the processed items
	rs.set.SetInventoryStatus(set.FetchStatusCompleted)
	rs.cacheSet(ctx, true)

	zap.L().Debug("Worker pool completed inventory processing",
		zap.Int("item_count", inventorySize),
		zap.Int("bricks_processed", len(rs.Read().Bricks)),
	)

	return true, nil
}

// fetchBricks fetches prices for all Bricks in a set from Pick-a-Brick using a worker pool
func (h *Handler) fetchBricks(ctx context.Context, rs *RuntimeSet, lang language.Tag, xlocale language.Tag, skipFinalCaching bool) error {
	// Calculate total bricks
	bricksSize := len(rs.bricks.missing)
	if bricksSize == 0 {
		return nil
	}

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
	workerFunc := func(ctx context.Context, brick set.Brick) (set.Brick, error) {
		return h.workerHandlerBrickPrice(ctx, brick, lang, xlocale)
	}

	// Batch handler: send batch to frontend
	// Note: This is called from a single collector goroutine, so no mutex needed
	batchHandler := func(batch []set.Brick) error {
		return h.batchHandlerBricksProgress(rs, batch, bprogress, DataTypePickabrickBricks, true)
	}

	// Create and run pool with batching
	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)

	// Set error handler
	pool.SetErrorHandler(func(err error) {
		h.logWarning(rs.Read().ID, "Worker.PriceProcessing", err)
		zap.L().Warn("Worker encountered error processing price job",
			zap.Error(err),
		)
	})

	// Process all price jobs
	if err := pool.Process(rs.bricks.getMissingAsSlice()); err != nil {
		handleFatalError(h, rs, set.FetchErrorBatchCache, DataTypeSet, err, "Failed to process prices with worker pool")
		return err
	}

	if !skipFinalCaching {
		// TODO : investigate why this is a fix
		// problem was that when sharing inventory fetching for same set but different locales,
		// when reloading the page it has missing bricks somehow, with some having centamount 0

		// Cache the processed items
		rs.cacheSet(ctx, true)
	}

	zap.L().Debug("Worker pool completed price fetching",
		zap.Int("bricks_size", bricksSize),
	)

	return nil
}

// workerHandlerBrickPrice is the worker function for fetching and updating a single brick's price, with cache checking and fallback to API
func (h *Handler) workerHandlerBrickPrice(
	ctx context.Context,
	bSet set.Brick,
	lang language.Tag,
	xlocale language.Tag,
) (set.Brick, error) {

	originalIDs := bSet.Locale.IDs
	var firstNotFoundLocale *brick.Locale
	var validLocale bool

	// Try to find a valid element ID. If multiple, compare to get the best one
	for _, id := range originalIDs {

		// Try to find in cache first
		bRedis, valid, notFound := bSet.Locale.LoadFromRedis(ctx, id.ElementID, xlocale, false, true)
		if notFound && firstNotFoundLocale == nil {
			// Brick Locale cached with not-found price
			// We can consider this brick as up-to-date with a not-found price
			// Save temporarily until we process them all, we might find a matching ID with a valid price
			firstNotFoundLocale = &bRedis
			continue // Try next ID
		} else if valid {
			// Brick Locale cached with valid price and up-to-date, return safely
			// Save temporarily until we process them all, we might find a matching ID with a better price
			bSet = set.NewBrickWithUUID(bSet.UUID, bSet.Inventory, bRedis)
			validLocale = true
			continue // Try next ID
		}

		// No valid cache entry for this brick ID - fetch from API
		ok, fetchValid, fetchNotFoundLocale := bSet.Locale.Fetch(ctx, id.ElementID, lang, xlocale)
		if !ok {
			continue // Try next ID
		}

		// As long as we haven't found a valid price from cache, we can consider the fetched data as our best option
		if !validLocale {
			validLocale = fetchValid
		} else if firstNotFoundLocale != nil {
			firstNotFoundLocale = fetchNotFoundLocale
		}
	}

	// If we didn't find a valid price for any brick ID, but we did encounter not-found entries,
	// set the MainID to one of the not-found IDs to avoid unnecessary cache lookups next time
	if !validLocale && firstNotFoundLocale != nil {
		bSet = set.NewBrickWithUUID(bSet.UUID, bSet.Inventory, *firstNotFoundLocale)
		zap.L().Debug("No valid price found for any brick ID, set ElementID to first not-found ID",
			zap.String("element_id", string(firstNotFoundLocale.ID.ElementID)),
			zap.String("xlocale", xlocale.String()),
		)
	}

	// When applying locales, we might overwrite the elementIDs slice
	// To avoid loosing data, we reset it to the original slice
	if validLocale || firstNotFoundLocale != nil {
		bSet.SetIDs(originalIDs) // Reset to original slice to avoid losing data
	}

	return bSet, nil
}

// batchHandlerBricksProgress handles batch progress updates for bricks, including updating the set, caching, and sending updates to clients
func (h *Handler) batchHandlerBricksProgress(
	rs *RuntimeSet,
	batch []set.Brick,
	progress *Progress,
	dataType DataType,
	reuseMissingPartsCount bool,
) error {

	// Update shared progress with current batch
	for _, b := range batch {

		// If the brick has is custom or has a valid price, it means it's final
		// We can update the runtime and set with its data
		if b.IsCustom || b.HasValidPrice() {
			// If applicable, remove the brick from the missing ones
			rs.bricks.removeMissing(b.UUID)

			// Still consider the brick as missing because price was not found
			// However it is not literally missing
			if !b.Price.IsNotFound() {
				// Calculate brick's total price
				b.CalculateTotalPrice()

				// Add to set final data
				rs.set.AddFinalBrickData(b)
			}

			// Add brick to final bricks in runtime set
			rs.bricks.appendFinal(b)
		} else {
			// Add brick to missing bricks in runtime set
			rs.bricks.appendMissing(b)

			if !reuseMissingPartsCount {
				// Update external set data
				rs.set.IncrementMissingParts()
			}
		}

		progress.AddItem(b)
	}

	// Prepare for sending (updates done to include current batch)
	progress.PrepareForSend()

	// Send batch to frontend
	h.PushBatchProgress(rs.ID, dataType, *progress)

	// Send external set update to clients, without bricks
	h.PushChange(rs.ID, rs.Read().ID, DataTypeSet, DataTypeUpdated)

	// If we have access to the inventory as a writer
	if dataType == DataTypeBricklinkBricks && rs.ihAccess.IsValid() && rs.ihAccess.IsWriter {
		// Send batch to inventory potential listeners
		rs.ihAccess.Inventory.write(*progress)
	}

	// Complete batch (resets items array for next batch)
	progress.CompleteBatch()

	return nil
}

// batchHandlerListenProgress is a simplified batch handler for inventory listening
// it only updates the runtime set and sends progress to clients without caching or external set updates
func (h *Handler) batchHandlerListenProgress(rs *RuntimeSet, progress Progress) {

	// Update runtime set with current batch
	for _, item := range progress.Items {
		// Add brick to missing bricks in runtime set
		if bp, ok := item.(set.Brick); ok {
			rs.bricks.appendMissing(bp)
		}
		if bp, ok := item.(*set.Brick); ok && bp != nil {
			rs.bricks.appendMissing(*bp)
		}

		// Update external set data
		rs.set.IncrementMissingParts()
	}

	// Send batch to frontend
	h.PushBatchProgress(rs.ID, DataTypeBricklinkBricks, progress)

	// Send external set update to clients, without bricks
	h.PushChange(rs.ID, rs.Read().ID, DataTypeSet, DataTypeUpdated)
}

// handleFatalError handles fatal errors during set fetching
// It logs the error asynchronously, updates cache, and notifies clients
func handleFatalError(h *Handler, rs *RuntimeSet, step set.FetchErrorStep, dataType DataType, err error, msg string, fields ...zap.Field) {
	setID := rs.Read().ID

	// Log to async error logger for persistence/tracking
	h.logCriticalError(setID, string(dataType), err)

	// Log immediately to zap for operational visibility
	zap.L().Error(msg, append(fields, zap.Error(err))...)

	// Mark set as failed in cache with error details
	rs.set.SetFetchError(step, msg)

	// Notify all connected clients
	h.PushChange(rs.ID, setID, dataType, DataTypeFailed)

	// Try to update cache - best effort, don't fail if this fails
	if cacheErr := set.RedisSetLocale(context.Background(), rs.Read().Locale, rs.Key().XLocale, false, false); cacheErr != nil {
		// Log cache update failure but don't propagate
		h.logWarning(setID, "Redis.UpdateFailedStatus", cacheErr)
		zap.L().Warn("Failed to update failed status in cache",
			zap.Error(cacheErr),
			zap.String("set_id", setID.String()),
		)
	}
}
