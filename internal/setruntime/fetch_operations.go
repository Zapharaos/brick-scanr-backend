package setruntime

import (
	"context"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
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

	// Initialize progress tracker
	rs := h.GetRuntimeSet(rsID)
	bprogress := NewProgress(len(bricks), rs.opt.ProgressBatchSize)

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
				if brick.Prices == nil {
					brick.Prices = make(map[language.Tag]*set.Price)
				}
				brick.Prices[currency] = &pbp

				// Cache the updated brick
				if err = set.SetRedisBrick(ctx, brick, 0); err != nil {
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
	err := set.SetRedisSet(ctx, cpRedisSet, 0)
	if err != nil {
		// Failed to update the set in cache => FATAL
		handleFatalError(h, rsID, setID, set.FetchErrorInitCache, cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set status")
		return
	}
	h.PushChange(rsID, setID, DataTypeSet, DataTypeCreated)

	// TODO : async ?
	// Fetch set details
	if err := h.fetchDetails(ctx, rsID, setID, &cpRedisSet, locale, currency); err != nil {
		return // Error already handled
	}

	// Fetch inventory from BrickLink
	if err := h.fetchInventory(ctx, rsID, setID, &cpRedisSet); err != nil {
		return // Error already handled
	}

	// Fetch prices from Pick-a-Brick
	if err := h.fetchPrices(ctx, rsID, setID, &cpRedisSet, locale, currency); err != nil {
		return // Error already handled
	}

	// Mark set fetch completed
	cpRedisSet.FetchStatus = set.FetchStatusCompleted
	err = set.SetRedisSet(ctx, cpRedisSet, 0)
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

	err = set.SetRedisSet(ctx, *cpRedisSet, 0)
	if err != nil {
		handleFatalError(h, rsID, setID, set.FetchErrorDetailsCache, *cpRedisSet, DataTypeSet, err,
			"Failed to update Redis set inventory")
		return err
	}
	h.PushChange(rsID, setID, DataTypeSet, DataTypeUpdated)

	// TODO : ISSUE #3 - Lego

	// TODO : ISSUE #3 - Instructions : https://www.lego.com/fr-fr/service/building-instructions/21068

	return nil
}

// fetchInventory fetches the inventory for a set from BrickLink
func (h *Handler) fetchInventory(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set) error {
	inventory, err := bricklink.C().FetchInventory(cpRedisSet.BricklinkID, cpRedisSet.BricklinkNumber)
	if err != nil {
		// Failed to fetch inventory => FATAL
		handleFatalError(h, rsID, setID, set.FetchErrorFetchInventory, *cpRedisSet, DataTypeBricklinkBricks, err,
			"Failed to fetch inventory from BrickLink")
		return err
	}

	// Map BrickLink inventory to internal set bricks
	rs := h.GetRuntimeSet(rsID)
	bprogress := NewProgress(len(inventory.Items), rs.opt.ProgressBatchSize)
	for idx, item := range inventory.Items {
		// Try to find in cache first
		brick, err := set.GetRedisBrick(ctx, set.BrickID(item.ItemIDs[0]), set.DesignID(item.ItemNo))
		if err != nil {
			// Not found in cache, map from BrickLink item
			brick = set.MapBrickFromBricklinkInventoryItem(item)

			// Set the index to maintain order
			brick.Index = idx

			// Cache the brick
			if err = set.SetRedisBrick(ctx, brick, 0); err != nil {
				zap.L().Warn("Failed to cache brick",
					zap.Error(err),
					zap.String("brick_design_id", item.ItemNo),
				)
			}
		}

		// todo : ISSUE #8 - Async : brick already cached? update it's TTL to match set's TTL
		// But then there is a risk that the price might become outdated at some point
		// Use price TTL ? Limit the amount of TTL postponements? Limit TTL regarding a brick.created_at field?

		// Update set copy with brick
		cpRedisSet.Bricks = append(cpRedisSet.Bricks, brick)

		// Update progress
		bprogress.AddItem(brick)

		// Check batch progress
		if bprogress.HasReachedBatchLimit() {
			if bprogress.EmptyItems() {
				bprogress.CompleteBatch()
				continue
			}

			// Send batch update via websocket
			h.PushBatchProgress(rsID, DataTypeBricklinkBricks, *bprogress)
			bprogress.CompleteBatch()

			// Update set in cache
			err = set.SetRedisSet(ctx, *cpRedisSet, 0)
			if err != nil {
				// Fatal error - inventory is essential
				handleFatalError(h, rsID, setID, set.FetchErrorBatchCache, *cpRedisSet, DataTypeSet, err,
					"Failed to update Redis set with inventory batch")
				return err
			}
		}
	}

	return nil
}

// fetchPrices fetches prices for all bricks in a set from Pick-a-Brick
func (h *Handler) fetchPrices(ctx context.Context, rsID uuid.UUID, setID uuid.UUID, cpRedisSet *set.Set, locale language.Tag, currency language.Tag) error {
	bmap := set.NewBrickMap(cpRedisSet.Bricks)
	rs := h.GetRuntimeSet(rsID)
	bprogress := NewProgress(len(bmap.BricksByDesign), rs.opt.ProgressBatchSize)

	// TODO : ISSUE #1 - Search Alternate Items
	// todo : optimize
	// todo : process seems slow and inefficient
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

			// Check if currency price is already set
			price := brick.GetPriceForLocale(currency)
			if price != nil && brick.Price.CentAmount != 0 && price.CentAmount < mb.Price.CentAmount {
				continue
			}

			// Update brick with fetched price
			pbp := set.MapPriceFromPickabrick(mb.Price)
			pbp.ItemID = string(brickID)
			if brick.Prices == nil {
				brick.Prices = make(map[language.Tag]*set.Price)
			}
			brick.Prices[currency] = &pbp

			// Update brick in cache
			if err = set.SetRedisBrick(ctx, brick, 0); err != nil {
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
	_ = set.SetRedisSet(context.Background(), data, time.Hour) // Short TTL for failed states

	// Notify all connected clients
	h.PushChange(rsID, setID, dataType, DataTypeFailed)
}

// handleNonFatalError handles non-critical errors during set fetching
func handleNonFatalError(err error, msg string, fields ...zap.Field) {
	zap.L().Warn(msg, append(fields, zap.Error(err))...)
}
