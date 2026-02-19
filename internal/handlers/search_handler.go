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
	SearchResponseTypeBrickElement
	SearchResponseTypeBrickDesign
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
		itemType, item, ok := handleSearchResultBricklinkBrick(r.Context(), b, lang, xlocale)
		if !ok {
			// Error already logged, skip this item
			continue
		}

		responseItems = append(responseItems, SearchResponseItem{
			Type: itemType,
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
func handleSearchResultBricklinkBrick(ctx context.Context, bsi bricklink.SearchItem, lang language.Tag, xlocale language.Tag) (SearchResponseType, interface{}, bool) {

	// Get the element ID and design ID from the BrickLink search item
	elementID, designID := brick.GetIDsFromBricklinkSearchItem(bsi)

	// If both element ID and design ID are empty, log an error and skip this item
	// This should not happen, as we should have at least one of them, but we want to be safe and avoid processing invalid data
	if elementID == "" && designID == "" {
		zap.L().Error("Failed to extract element ID and design ID from BrickLink search item",
			zap.String("strItemNo", bsi.StrItemNo),
			zap.String("strPCC", func() string {
				if bsi.StrPCC != nil {
					return *bsi.StrPCC
				}
				return "null"
			}()),
		)
		return SearchResponseTypeBrickElement, brick.Locale{}, false
	}

	// First try to find the design by design ID
	design, ok := handleBricklinkBrickByDesignID(ctx, designID, lang, xlocale)

	// This can occur if the search input is not an element ID but a design ID
	// Check must be run before checkin ok boolean, we need to be sure of the search response type
	if elementID == "" {
		return SearchResponseTypeBrickDesign, design, ok
	}

	// Search input is an element ID, but we failed to find the design
	if !ok {
		return SearchResponseTypeBrickElement, design, ok
	}

	// We have a design, we can try to find the brick locale by element ID
	design.ID.ElementID = elementID
	data, ok := handleBricklinkBrickByElementID(ctx, design.Core, lang, xlocale)
	return SearchResponseTypeBrickElement, data, ok
}

// handleBricklinkBrickByDesignID fetches brick details from BrickLink using the design ID, maps it to internal representation, caches it, and returns the brick locale
func handleBricklinkBrickByDesignID(ctx context.Context, designID brick.DesignID, lang language.Tag, xlocale language.Tag) (brick.Design, bool) {
	// Check cache by design ID
	design, err := brick.RedisGetDesign(ctx, designID, xlocale)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		// An error has occurred, it's not a cache miss (not found) => log and skip caching for this item
		zap.L().Error("Failed to check design in Redis cache",
			zap.Error(err),
			zap.String("design_id", string(designID)),
		)
		return brick.Design{}, false
	}

	// Found in cache and data complete, return it
	if err == nil &&
		design.DesignStatus >= brick.DesignStatusMinimal &&
		design.DesignStatus != brick.DesignStatusBricks {
		return design, true
	}

	id := brick.ID{
		DesignID: designID,
	}
	design.ID = &id

	// Not found in cache, fetch it and cache it
	err = design.FetchMinimal(ctx, lang, xlocale)
	if err != nil {
		return brick.Design{}, false
	}

	return design, true
}

// handleBricklinkBrickByElementID fetches brick details from BrickLink using the element ID, maps it to internal representation, caches it, and returns the brick locale
func handleBricklinkBrickByElementID(ctx context.Context, designCore brick.Core, lang language.Tag, xlocale language.Tag) (brick.Locale, bool) {
	// Build a minimal brick locale version
	bLocale := brick.Locale{}
	bLocale.Core = designCore

	// Search for the brick locale in cache
	var valid, notfound bool
	bLocale, valid, notfound = bLocale.LoadFromRedis(ctx, bLocale.ID.ElementID, xlocale, false, false)

	// Brick locale already cached, return it
	if valid || notfound {
		// TODO : should merge IDs and Design if applicable, but not if cached correctly upon set load
		return bLocale, true
	}

	// Not found in cache

	ok, _, _ := bLocale.Fetch(ctx, bLocale.ID.ElementID, lang, xlocale)
	if !ok {
		return brick.Locale{}, false
	}

	// Cache the brick details in Redis for future searches and lookups
	err := brick.RedisSetLocale(ctx, bLocale, xlocale, true)
	if err != nil {
		zap.L().Error("Failed to cache brick in Redis",
			zap.Error(err),
			zap.String("element_id", string(bLocale.ID.ElementID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	// TODO : the problem is that this updates only the matching brick with element ID
	// does not handle same design ID but different element ID, nor set bricks

	return bLocale, true
}
