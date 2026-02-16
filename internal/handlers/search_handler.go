package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type SearchResponseType int

const (
	SearchResponseTypeSet SearchResponseType = iota
	SearchResponseTypeBrick
)

type SearchResponseItem struct {
	Type SearchResponseType `json:"type"`
	Item interface{}        `json:"item"`
}

// Search godoc
//
//	@Id				Search
//	@Summary		Search for LEGO elements
//	@Description	Search for LEGO elements on BrickLink.
//	@Tags			Set, Brick
//	@Produce		json
//	@Param			query	path		string						true	"Search query"
//	@Security		Bearer
//	@Success		200	{array}		SearchResponseItem			"List of matching elements"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		500	{object}	render.ErrorResponse		"Internal Server Error"
//	@Router			/api/v1/search/{query} [get]
func Search(w http.ResponseWriter, r *http.Request) {
	query := chi.URLParam(r, "query")

	zap.L().Info("Search endpoint called",
		zap.String("query", query),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if query == "" {
		zap.L().Warn("Search called with empty query")
		render.BadRequest(w, r, fmt.Errorf("query parameter is required"))
		return
	}

	// Extract language + xlocale from context
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)

	// Execute search on BrickLink
	bricklinkSets, bricklinkBricks, err := bricklink.C().Search(query, lang)
	if err != nil {
		zap.L().Error("Failed to search responseItems",
			zap.Error(err),
			zap.String("query", query),
		)
		render.Error(w, r, err, "Failed to search BrickLink")
		return
	}

	responseItems := make([]SearchResponseItem, 0)

	// Process each result : check cache, map to internal struct, fetch details if needed, prepare external representation
	for _, s := range bricklinkSets {

		// Check cache and if found then return the cached data, otherwise map the search result to internal struct and cache it
		item, ok, created := handleSearchResultBricklinkSet(r.Context(), s, xlocale)
		if !ok {
			// Error already logged, skip this item
			continue
		}

		// If the set was newly created or misses price for xlocale, fetch full details
		if created || !item.HasValidPrice() {

			// Fetch details from LEGO and BrickLink to get details data
			_, err = set.FetchDetails(r.Context(), item.ID, &item, lang, xlocale)
			if err != nil {
				// Error already logged, skip this item
				continue
			}

			// Cache slug to set ID mapping
			err = set.RedisSetSetIDForSlug(r.Context(), item, true)
			if err != nil {
				zap.L().Warn("Failed to cache slug to set ID mapping",
					zap.Error(err),
					zap.String("set_id", item.ID.String()),
					zap.String("slug", item.Slug),
				)
				continue
			}
		}

		// Append to results with external representation
		responseItems = append(responseItems, SearchResponseItem{
			Type: SearchResponseTypeSet,
			Item: set.External{
				Locale: item,
			},
		})
	}

	// Process each result : check cache, map to internal struct, fetch details if needed, prepare external representation
	for _, b := range bricklinkBricks {

		// Check cache and if found then return the cached data, otherwise map the search result to internal struct and cache it
		item, ok := handleSearchResultBricklinkBrick(r.Context(), b, lang, xlocale)
		if !ok {
			// Error already logged, skip this item
			continue
		}

		responseItems = append(responseItems, SearchResponseItem{
			Type: SearchResponseTypeBrick,
			Item: item,
		})
	}

	zap.L().Info("Successfully retrieved responseItems",
		zap.String("query", query),
		zap.Int("count", len(responseItems)),
	)

	render.JSON(w, r, responseItems)
}

// handleSearchResultBricklinkSet processes a single BrickLink search item and returns the internal Set representation
func handleSearchResultBricklinkSet(ctx context.Context, bsi bricklink.SearchItem, xlocale language.Tag) (set.Locale, bool, bool) {
	// Map to internal representation
	core, err := set.NewCoreFromBricklinkSearchItem(bsi)
	if err != nil {
		zap.L().Error("Failed to map BrickLink set to internal representation",
			zap.Error(err),
			zap.String("set_number", bsi.StrItemNo),
		)
		return set.Locale{}, false, false
	}

	// Try to find the set in Redis cache by BrickLink ID
	bricklinkID := strconv.Itoa(core.BricklinkID)
	sCache, ttl, err := set.RedisGetLocaleByBricklinkID(ctx, bricklinkID, xlocale)

	// An error has occurred, it's not a cache miss (not found) => log and skip caching for this item
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		zap.L().Error("Failed to check set in Redis cache",
			zap.Error(err),
			zap.String("set_id", core.ID.String()),
		)
		return set.Locale{}, false, false
	}

	// Not found in cache, store it
	if err != nil {
		err = set.RedisSetCoreForBricklinkID(ctx, core, true)
		if err != nil {
			zap.L().Error("Failed to cache set in Redis",
				zap.Error(err),
				zap.String("set_id", core.ID.String()),
				zap.Int("bricklink_id", core.BricklinkID),
			)
			return set.Locale{}, false, false
		}

		// No cached data, return the new item we created from the search result
		return set.Locale{
			Core: core,
		}, true, true
	}

	// Check if TTL is too low :
	// The websocket will need to get the core data from redis, there is a risk it may expire before we get there
	if redis.IsTTLBelowThreshold(ttl, database.DB().Redis().TTLS.SetBricklinkMinThreshold) {
		// TTL is too low, delete the old cached data and refresh
		zap.L().Info("Cached set TTL is below threshold, refreshing",
			zap.String("set_id", sCache.ID.String()),
			zap.Int("bricklink_id", sCache.BricklinkID),
			zap.Duration("remaining_ttl", ttl),
		)

		// Delete the expired/about-to-expire data
		// This is what links the BrickLink ID to the set ID, and the websockets/runtimes relies on it
		redis.MustDelete(ctx, set.RedisBuildKeyBricklinkIDToSetID(bricklinkID))

		// Create a new cache entry, with a new setID to not conflict with existing one which may still be in use
		// This will return the canonical set (with consistent UUID) even if another goroutine wins the race
		err = set.RedisSetCoreForBricklinkID(ctx, core, true)
		if err != nil {
			zap.L().Error("Failed to cache set in Redis",
				zap.Error(err),
				zap.String("set_id", core.ID.String()),
				zap.Int("bricklink_id", core.BricklinkID),
			)
			return set.Locale{}, false, false
		}

		// No cached data, return the new item we created from the search result
		return set.Locale{
			Core: core,
		}, true, true
	}

	// Note : we might not have found a locale set, but there is at least a core
	// Warning : the core might not be reliable, it must be checked when fetching its details

	// No errors, data was found and TTL is fine, use the cached UUID
	return sCache, true, false
}

// handleSearchResultBricklinkBrick processes a single BrickLink search item and returns the internal Brick representation
func handleSearchResultBricklinkBrick(ctx context.Context, bsi bricklink.SearchItem, lang language.Tag, xlocale language.Tag) (brick.Locale, bool) {
	// Get the element ID from the BrickLink search result
	elementID := brick.ElementID(bsi.StrItemNo)

	// Build a minimal brick locale version
	bLocale := brick.Locale{}
	bLocale.ElementID = &elementID

	// Search for the brick locale in cache
	var valid, notfound bool
	bLocale, valid, notfound = bLocale.LoadFromRedis(ctx, elementID, xlocale, false, false)

	// Brick locale already cached, return it
	if valid || notfound {
		return bLocale, true
	}

	// Not found in cache

	// Query BrickLink for brick details
	bricklinkBrick, err := bricklink.C().FetchBrickDetails(string(elementID), lang)
	if err != nil {
		zap.L().Error("Failed to fetch brick details from BrickLink",
			zap.Error(err),
			zap.String("element_id", string(elementID)),
		)
		return brick.Locale{}, false
	}

	// Map BrickLink brick details to internal representation
	bCore := brick.NewCoreFromBricklinkBrick(bricklinkBrick)

	// Create brick locale with the core data
	bLocale = brick.Locale{
		Core: bCore,
	}

	// TODO : bricklink returns design ID's ?

	ok, _, _ := bLocale.Fetch(ctx, elementID, lang, xlocale)
	if !ok {
		return brick.Locale{}, false
	}

	// Cache the brick details in Redis for future searches and lookups
	err = brick.RedisSet(ctx, bLocale, xlocale, true)
	if err != nil {
		zap.L().Error("Failed to cache brick in Redis",
			zap.Error(err),
			zap.String("element_id", string(elementID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	// TODO : the problem is that this updates only the matching brick with element ID, and not the others elementID's

	return bLocale, true
}
