package searchruntime

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/workerpool"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

const (
	CategorySets   = "sets"
	CategoryBricks = "bricks"
)

// SearchResult holds a single processed search result ready for the client
type SearchResult struct {
	Type string      `json:"type"` // "set", "brickElement", "brickDesign"
	Item interface{} `json:"item"`
}

// ProcessSearchAsync is the function passed to Handler.RunSearch.
// It runs both sets and bricks goroutines concurrently, waits for both, then signals complete.
func ProcessSearchAsync(
	ctx context.Context,
	rt *Runtime,
	bricklinkSets []bricklink.SearchItem,
	bricklinkBricks []bricklink.SearchItem,
	locale language.Tag,
) {
	defer func() {
		if r := recover(); r != nil {
			rt.SignalError(fmt.Sprintf("internal panic: %v", r))
			zap.L().Error("Panic in search processing", zap.Any("panic", r))
		}
	}()

	setsTotal := len(bricklinkSets)
	bricksTotal := len(bricklinkBricks)

	processSets(ctx, rt, bricklinkSets, setsTotal, locale)
	processBricks(ctx, rt, bricklinkBricks, bricksTotal, locale)

	rt.SignalComplete()
}

// processSets runs a worker pool over bricklinkSets, sending batches via the runtime
func processSets(
	ctx context.Context,
	rt *Runtime,
	items []bricklink.SearchItem,
	total int,
	locale language.Tag,
) {
	config := workerpool.NewConfigOptimal(total, 0)

	workerFunc := func(ctx context.Context, bsi bricklink.SearchItem) (SearchResult, error) {
		return ProcessSetItem(ctx, bsi, locale)
	}

	progress := &batchProgress{done: 0, total: total}

	batchHandler := func(batch []SearchResult) error {
		return pushBatch(rt, batch, progress, CategorySets)
	}

	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)
	pool.SetErrorHandler(func(err error) {
		zap.L().Warn("Search set worker error", zap.Error(err))
	})

	if err := pool.Process(items); err != nil {
		zap.L().Error("Search set pool failed", zap.Error(err))
		// Non-fatal: bricks goroutine may still succeed
	}
}

// processBricks runs a worker pool over bricklinkBricks, sending batches via the runtime
func processBricks(
	ctx context.Context,
	rt *Runtime,
	items []bricklink.SearchItem,
	total int,
	locale language.Tag,
) {
	config := workerpool.NewConfigOptimal(total, 0)

	workerFunc := func(ctx context.Context, bsi bricklink.SearchItem) (SearchResult, error) {
		return ProcessBrickItem(ctx, bsi, locale)
	}

	progress := &batchProgress{done: 0, total: total}

	batchHandler := func(batch []SearchResult) error {
		return pushBatch(rt, batch, progress, CategoryBricks)
	}

	pool := workerpool.NewPool(ctx, config, workerFunc, batchHandler)
	pool.SetErrorHandler(func(err error) {
		zap.L().Warn("Search brick worker error", zap.Error(err))
	})

	if err := pool.Process(items); err != nil {
		zap.L().Error("Search brick pool failed", zap.Error(err))
	}
}

// batchProgress tracks how many items have been sent so far for a category
type batchProgress struct {
	done  int
	total int
}

// pushBatch converts a slice of SearchResult into a batchChange and sends it to the runtime.
// Zero-value results (from failed workers) are filtered out before sending.
func pushBatch(rt *Runtime, batch []SearchResult, prog *batchProgress, category string) error {
	items := make([]batchItem, 0, len(batch))
	for _, r := range batch {
		// Skip zero-value results produced by workers that returned an error
		if r.Type == "" {
			continue
		}
		items = append(items, batchItem{responseItem: r})
	}
	prog.done += len(batch)

	// Only push if there are valid items to send
	if len(items) == 0 {
		return nil
	}

	rt.pushBatchChange(batchChange{
		items:    items,
		done:     prog.done,
		total:    prog.total,
		category: category,
	})
	return nil
}

// ProcessSetItem processes a single BrickLink set search result. Exported for reuse in the sync path.
func ProcessSetItem(ctx context.Context, bsi bricklink.SearchItem, locale language.Tag) (SearchResult, error) {
	core, err := set.NewCoreFromBricklinkSearchItem(bsi)
	if err != nil {
		return SearchResult{}, fmt.Errorf("map set: %w", err)
	}

	bricklinkID := strconv.Itoa(core.BricklinkID)
	sCache, ttl, err := set.RedisGetLocaleByBricklinkID(ctx, bricklinkID, locale)

	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		return SearchResult{}, fmt.Errorf("redis get set: %w", err)
	}

	created := false
	var sLocale set.Locale

	if err != nil {
		// Not in cache – store core
		if cacheErr := set.RedisSetCoreForBricklinkID(ctx, core, true); cacheErr != nil {
			return SearchResult{}, fmt.Errorf("redis set core: %w", cacheErr)
		}
		sLocale = set.Locale{Core: core}
		created = true
	} else if redis.IsTTLBelowThreshold(ttl, database.DB().Redis().TTLS.SetBricklinkMinThreshold) {
		// TTL too low – refresh
		redis.MustDelete(ctx, set.RedisBuildKeyBricklinkIDToSetID(bricklinkID))
		if cacheErr := set.RedisSetCoreForBricklinkID(ctx, core, true); cacheErr != nil {
			return SearchResult{}, fmt.Errorf("redis refresh set core: %w", cacheErr)
		}
		sLocale = set.Locale{Core: core}
		created = true
	} else {
		sLocale = sCache
	}

	// Fetch details when newly created or price missing
	if created || !sLocale.HasValidPrice() {
		if _, fetchErr := set.FetchDetails(ctx, sLocale.ID, &sLocale, locale); fetchErr != nil {
			return SearchResult{}, fmt.Errorf("fetch set details: %w", fetchErr)
		}
		if slugErr := set.RedisSetSetIDForSlug(ctx, sLocale, true); slugErr != nil {
			zap.L().Warn("Failed to cache slug to set ID mapping",
				zap.Error(slugErr),
				zap.String("set_id", sLocale.ID.String()),
			)
		}
	}

	return SearchResult{
		Type: "set",
		Item: set.External{Locale: sLocale},
	}, nil
}

// ProcessBrickItem processes a single BrickLink brick search result. Exported for reuse in the sync path.
func ProcessBrickItem(ctx context.Context, bsi bricklink.SearchItem, locale language.Tag) (SearchResult, error) {
	elementID, designID := brick.GetIDsFromBricklinkSearchItem(bsi)

	if elementID == "" && designID == "" {
		return SearchResult{}, fmt.Errorf("empty element and design ID for item %s", bsi.StrItemNo)
	}

	// Fetch design
	design, ok := GetBrickDesign(ctx, designID, locale)
	if !ok {
		return SearchResult{}, fmt.Errorf("design not found: %s", designID)
	}

	// Design-only result
	if elementID == "" {
		return SearchResult{Type: "brickDesign", Item: design}, nil
	}

	// Element result
	design.ID.ElementID = elementID
	bLocale, ok := GetBrickLocale(ctx, design.Core, locale)
	if !ok {
		return SearchResult{}, fmt.Errorf("brick locale not found: %s", elementID)
	}

	return SearchResult{Type: "brickElement", Item: bLocale}, nil
}

// GetBrickDesign fetches or caches a brick design by design ID. Exported for reuse in the sync path.
func GetBrickDesign(ctx context.Context, designID brick.DesignID, locale language.Tag) (brick.Design, bool) {
	design, err := brick.RedisGetDesign(ctx, designID, locale)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		zap.L().Error("Redis get design", zap.Error(err), zap.String("design_id", string(designID)))
		return brick.Design{}, false
	}

	if err == nil &&
		design.DesignStatus >= brick.DesignStatusMinimal &&
		design.DesignStatus != brick.DesignStatusBricks {
		return design, true
	}

	id := brick.ID{DesignID: designID}
	design.ID = &id

	if fetchErr := design.FetchMinimal(ctx, locale); fetchErr != nil {
		return brick.Design{}, false
	}

	return design, true
}

// GetBrickLocale fetches a brick locale by element ID, cache-first. Exported for reuse in the sync path.
func GetBrickLocale(ctx context.Context, designCore brick.Core, locale language.Tag) (brick.Locale, bool) {
	bLocale := brick.Locale{}
	bLocale.Core = designCore

	bLocale, valid, notfound := bLocale.LoadFromRedis(ctx, bLocale.ID.ElementID, locale, false, false)
	if valid || notfound {
		return bLocale, true
	}

	ok, _, _ := bLocale.Fetch(ctx, bLocale.ID.ElementID, locale)
	if !ok {
		return brick.Locale{}, false
	}

	if err := brick.RedisSetLocale(ctx, bLocale, locale, true); err != nil {
		zap.L().Warn("Failed to cache brick locale", zap.Error(err))
	}

	return bLocale, true
}
