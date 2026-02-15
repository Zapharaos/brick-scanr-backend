package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/Zapharaos/go-spit"
	"github.com/Zapharaos/lingo"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// TODO : 21039 has duplicates for en-US
// Black Tile 1 x 8 with 'Shanghai' Pattern // Element: Part Color Code Missing // Design: 4162pb181
// TODO : duplicate not appearing for fr-FR on first load
// the cache reload considers one price missing and refetches it, and then it duplicate part appears
// TODO : on reload, initial missing parts count is wrong as well

// SearchSets godoc
//
//	@Id				SearchSets
//	@Summary		Search for LEGO sets
//	@Description	Search for LEGO sets on BrickLink by set number or name.
//	@Tags			Set
//	@Produce		json
//	@Param			query	path		string						true	"Search query (set number or name)"
//	@Security		Bearer
//	@Success		200	{array}		set.External				"List of matching sets"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		500	{object}	render.ErrorResponse		"Internal Server Error"
//	@Router			/api/v1/set/search/{query} [get]
func SearchSets(w http.ResponseWriter, r *http.Request) {
	query := chi.URLParam(r, "query")

	zap.L().Info("SearchSets endpoint called",
		zap.String("query", query),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if query == "" {
		zap.L().Warn("SearchSets called with empty query")
		render.BadRequest(w, r, fmt.Errorf("query parameter is required"))
		return
	}

	// Extract language + xlocale from context
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)
	// TODO : ISSUE #5 - Search Brick

	// Execute search on BrickLink
	bricklinkSets, err := bricklink.C().SearchSets(query, lang)
	if err != nil {
		zap.L().Error("Failed to search sets",
			zap.Error(err),
			zap.String("query", query),
		)
		render.Error(w, r, err, "Failed to search BrickLink")
		return
	}

	// Process each result : check cache, map to internal struct, fetch details if needed, prepare external representation
	sets := make([]set.Locale, 0, len(bricklinkSets))
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
		sets = append(sets, item)
	}

	zap.L().Info("Successfully retrieved sets",
		zap.String("query", query),
		zap.Int("count", len(sets)),
	)

	render.JSON(w, r, sets)
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

// FetchSetDetails godoc
//
//	@Id				FetchSetDetails
//	@Summary		Start a job to fetch set details
//	@Description	Creates a background job to fetch set inventory and prices. Returns immediately with a job ID.
//	@Description	Use the job ID to poll for status or connect via WebSocket for real-time updates.
//	@Description	If data is already cached, returns it immediately with completed=true.
//	@Tags			Set
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"SetID or slug"
//	@Security		Bearer
//	@Success		202	{object}	set.DetailsResponse				"Job created or data available"
//	@Failure		400	{object}	render.ErrorResponse			"Bad Request"
//	@Router			/api/v1/set/details/{id} [post]
func (h Handler) FetchSetDetails(w http.ResponseWriter, r *http.Request) {
	setId, ok := ParseParamUUIDSoft(r, "id")
	if !ok {
		// Param is not a UUID, might be a slug - try to resolve
		slug := chi.URLParam(r, "id")
		id, err := set.RedisGetSetIDBySlug(r.Context(), slug)
		if err != nil {
			// Slug not found either - return 404
			w.WriteHeader(http.StatusNotFound)
			return
		}
		setId = id // resolved UUID from slug
	}

	// Extract language + xlocale from context
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)

	zap.L().Info("FetchSetDetails endpoint called",
		zap.String("id", setId.String()),
		zap.String("language", lang.String()),
		zap.String("xlocale", xlocale.String()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Look for any form of cached data, either through runtime or cache
	cacheSet, err := h.srh.GetCacheSet(r.Context(), setId, xlocale)
	if err != nil || cacheSet == nil {
		render.Error(w, r, err, "Failed to check cached set data")
		return
	}

	// Handle based on cache status
	switch cacheSet.Status {
	case setruntime.CacheStatusComplete:
		// Data is fully cached with everything up-to-date, return it immediately without creating a job
		render.Accepted(w, r, set.DetailsResponse{
			Completed: true,
			Set:       cacheSet.Set,
		})
		return

	case setruntime.CacheStatusFetching:
		// Data is currently being fetched, return existing websocket ID for client to join
		render.Accepted(w, r, set.DetailsResponse{
			Completed:   false,
			WebsocketID: cacheSet.RuntimeSetID.String(),
		})
		return

	case setruntime.CacheStatusIncomplete:
		// Data is cached but incomplete : missing prices, bricks, currency, etc.
		h.handleSetFetchIncomplete(w, r, setId, cacheSet, lang, xlocale)
		return

	case setruntime.CacheStatusFailed, setruntime.CacheStatusNeedsRefetch, setruntime.CacheStatusMissing:
		// Need to start a new complete fetch
		h.handleFetchSetComplete(w, r, setId, cacheSet.InventoryAccess, lang, xlocale)
		return

	default:
		render.Error(w, r, fmt.Errorf("unknown cache status"), "Internal error")
		return
	}
}

// handleSetFetchIncomplete handles the case where we need to fetch missing elements
func (h Handler) handleSetFetchIncomplete(w http.ResponseWriter, r *http.Request, setId uuid.UUID, cache *setruntime.CacheSet, lang, xlocale language.Tag) {
	// Create the key for this operation
	key := setruntime.NewRuntimeSetKey(setId, xlocale, setruntime.OpTypeIncomplete)

	// Check if there's already a runtime for this exact operation
	if rs, ok := h.srh.FindRuntimeSetByKey(key); ok {
		// Check if the runtime set is healthy (not failed)
		if rs.Read().FetchStatus == set.FetchStatusFailed {
			// Runtime set has failed - it will be forcefully stopped and replaced
			// Fall through to create a new one
		} else {
			// Runtime set is healthy and matches our needs, return existing websocket
			render.Accepted(w, r, set.DetailsResponse{
				Completed:   false,
				WebsocketID: rs.ID.String(),
			})
			return
		}
	}

	var rs *setruntime.RuntimeSet
	missingLocale := cache.MissingLocale
	missingSetPrice := cache.MissingPrice

	// Check if we have a valid inventory access
	ihValid := cache.InventoryAccess.IsValid()
	if ihValid {
		// We need to listen to the inventory changes before proceeding normally
		rs = h.srh.RunSet(cache.Set, xlocale, setruntime.OpTypeIncomplete, cache.InventoryAccess)
	} else {
		// There isn't an inventory access, we can skip the inventory stuff and proceed normally

		// Create websocket runtime for fetching
		cache.Set.MissingParts = len(cache.MissingBricks)
		rs = h.srh.RunSet(cache.Set, xlocale, setruntime.OpTypeIncomplete, setruntime.InventoryAccess{})

		// Put cache analyze results into the runtime
		rs.NewBricksHandler(cache.FinalBricks, cache.MissingBricks)
	}

	// Start goroutine to fetch missing bricks
	go h.srh.FetchFetchSetIncomplete(
		context.Background(),
		rs,
		setId,
		missingLocale,
		missingSetPrice,
		lang,
		xlocale,
	)

	// Return websocket ID for client to connect
	// Return the cached set data as well so the client can display it while waiting for the missing data to be fetched
	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// handleFetchSetComplete handles the case where we need to perform a complete fetch
func (h Handler) handleFetchSetComplete(w http.ResponseWriter, r *http.Request, setId uuid.UUID, ihAccess setruntime.InventoryAccess, lang, xlocale language.Tag) {
	// Retrieve BrickLink set info from cache
	// If there was any set locale data, we would have handled it in the incomplete flow, not here
	cacheLocaleWithCore, _, err := set.RedisGetLocale(r.Context(), setId, xlocale, true)
	if err != nil {
		// TTL most likely expired - cannot proceed
		render.NotFound(w, r, err)
		return
	}

	// Prepare the external set representation for the runtime
	se := set.External{
		Locale: cacheLocaleWithCore,
	}

	// Check the inventory access
	if !ihAccess.IsValid() {
		// No access yet, try to get one
		ihAccess = setruntime.IH().Access(setId)
	}

	// If we still don't have a valid access or only as a reader, we cannot proceed with the complete fetch
	if !ihAccess.IsValid() || !ihAccess.IsWriter {
		render.Error(w, r, fmt.Errorf("unexpected internal error"), "Internal error")
		return
	}

	// Create websocket
	rs := h.srh.RunSet(se, xlocale, setruntime.OpTypeFull, ihAccess)

	// Start processing in background
	go h.srh.FetchSetComplete(
		context.Background(),
		rs,
		setId,
		lang,
		xlocale,
	)

	// Return websocket ID for client to connect
	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// SetDetailsWebSocket godoc
//
//	@Id				SetDetailsWebSocket
//
//	@Summary		Websocket for a set details
//	@Description    Websocket for real-time updates on set details fetching progress.
//	@Tags			Set
//	@Security		Bearer
//	@Param			id		path		string			true			"Runtime Set ID (WebSocket ID from FetchSetDetails response)"
//	@Success		101		{string}	string							"Switching Protocols"
//	@Failure		401		{object}	setruntime.packetSpec			"Packet Specification"
//	@Failure		404		{string}	string							"Runtime Set Not Found"
//	@Failure		500		{string}	string							"Internal Server Error"
//	@Router			/api/v1/set/details/ws/{id} [get]
func (h Handler) SetDetailsWebSocket(w http.ResponseWriter, r *http.Request) {
	rsId, ok := ParseParamUUID(w, r, "id")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Verify job exists
	rs := h.srh.GetRuntimeSet(rsId)
	if rs == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Upgrade to websocket
	conn, err := h.srh.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("SetDetailsJobWebSocket.Upgrade:", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Generate a unique client ID for this connection
	// Multiple clients can watch the same job progress simultaneously
	clientId := uuid.New()

	zap.L().Info("WebSocket client connected",
		zap.String("runtime_set_id", rsId.String()),
		zap.String("client_id", clientId.String()),
	)

	// Create and register client
	setruntime.NewClient(rs, conn, clientId)
}

// ExportSet godoc
//
//	@Id				ExportSet
//
//	@Summary		Export set
//	@Description	Exports set
//	@Tags			Set
//	@Accept			json
//	@Produce		octet-stream
//	@Param			id			path	string			true	"SetID or slug"
//	@Security		Bearer
//	@Success		200	{file}		result					file
//	@Failure		400	{object}	render.ErrorResponse	"Bad Request"
//	@Failure		401	{string}	string					"Permission denied"
//	@Failure		500	{object}	render.ErrorResponse	"Internal Server Error"
//	@Router			/api/v1/set/export/{id} [post]
func (h Handler) ExportSet(w http.ResponseWriter, r *http.Request) {
	setId, ok := ParseParamUUIDSoft(r, "id")
	if !ok {
		// Param is not a UUID, might be a slug - try to resolve
		slug := chi.URLParam(r, "id")
		id, err := set.RedisGetSetIDBySlug(r.Context(), slug)
		if err != nil {
			// Slug not found either - return 404
			w.WriteHeader(http.StatusNotFound)
			return
		}
		setId = id // resolved UUID from slug
	}

	// Extract xlocale from context
	xlocale := GetXLocaleFromContext(r)

	// Retrieve set locale from cache
	sLocale, ok, err := set.RedisGetLocale(r.Context(), setId, xlocale, true)
	if err != nil || !ok {
		render.NotFound(w, r, err)
		return
	}

	// Check if the set data is complete enough for export
	if sLocale.InventoryStatus != set.FetchStatusCompleted && sLocale.FetchStatus != set.FetchStatusCompleted {
		render.Error(w, r, fmt.Errorf("set data is not fully available yet"), "Set data is not fully available yet")
	}

	// For each brick in the set, retrieve full data from cache, even if outdated
	for i, bSet := range sLocale.Bricks {
		b, _ := setruntime.CheckBrickCache(r.Context(), bSet, xlocale, true)
		sLocale.Bricks[i] = b
	}

	// Get xlocale translations localizer
	localizer, _, err := lingo.GetLocalizer(xlocale)
	if err != nil {
		zap.L().Error("Failed to get localizer", zap.Error(err))
		render.Error(w, r, err, "Get localizer")
		return
	}

	// Generate export table
	startTime := time.Now()
	table := set.ExportBuildTable(sLocale, localizer, xlocale)

	zap.L().Info("Set export table generated",
		zap.String("setID", sLocale.ID.String()),
		zap.String("slug", sLocale.GetSlug()),
		zap.Duration("duration", time.Since(startTime)),
	)

	// File variables
	fileParams := spit.FileWriteParams{
		Filename:    sLocale.GetSlug(),
		UseTempFile: true,
	}
	var fileResult *spit.FileWriteResult

	// Export to XLSX
	table.WriteHeader = true
	sheetName := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.sheet"))
	spreadsheet := spit.NewSpreadsheetExcelize(sheetName, table)
	fileResult, err = spit.ExportXLSX(spreadsheet, fileParams)
	if err != nil {
		render.Error(w, r, err, "Generate XLSX export file")
		return
	}

	defer func(fileResult *spit.FileWriteResult) {
		_ = fileResult.RemoveFile()
	}(fileResult)

	// Stream the file to the client
	render.StreamFile(fileResult.Filepath, fileResult.Filename, w, r)
}
