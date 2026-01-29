package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// SearchSets godoc
//
//	@Id				SearchSets
//	@Summary		Search for LEGO sets
//	@Description	Search for LEGO sets on BrickLink by set number or name.
//	@Tags			Set
//	@Produce		json
//	@Param			query	path		string						true	"Search query (set number or name)"
//	@Security		Bearer
//	@Success		200	{array}		set.Set						"List of matching sets"
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

	// TODO : ISSUE #1 - Search Alternate Items

	// Search through BrickLink
	bricklinkSets, err := bricklink.C().SearchSets(query)
	if err != nil {
		zap.L().Error("Failed to search sets",
			zap.Error(err),
			zap.String("query", query),
		)
		render.Error(w, r, err, "Failed to search BrickLink")
		return
	}

	// Process to local representation and cache
	sets := make([]set.Set, 0, len(bricklinkSets))
	for _, s := range bricklinkSets {

		// Map to internal representation
		item, err := set.MapSetFromBricklinkSearch(s)
		if err != nil {
			zap.L().Error("Failed to map BrickLink set to internal representation",
				zap.Error(err),
				zap.String("set_number", s.StrItemNo),
			)
			continue
		}

		// Try to find the set in Redis cache by BrickLink ID
		bricklinkSet, err := set.GetRedisBricklinkSet(r.Context(), fmt.Sprintf("%d", item.BricklinkID))
		if errors.Is(err, set.ErrKeyNotFound) {
			// Not found in cache, store it atomically
			// This will return the canonical set (with consistent UUID) even if another goroutine wins the race
			canonicalSet, _, err := set.SetRedisBricklinkSet(r.Context(), item)
			if err != nil {
				// Failed to cache set, log but continue with the item we have
				zap.L().Warn("Failed to cache set in Redis",
					zap.Error(err),
					zap.String("set_id", item.Id.String()),
					zap.Int("bricklink_id", item.BricklinkID),
				)
			} else {
				// Use the canonical set (which has the definitive UUID for this BrickLink ID)
				item = canonicalSet
			}
		} else if err != nil {
			zap.L().Error("Failed to check set in Redis cache",
				zap.Error(err),
				zap.String("set_id", item.Id.String()),
			)
			continue
		} else {
			// TODO : what if the redis key expires by the time we finish executing this function? extend TTL on get ?
			// Use the cached UUID
			item.Id = bricklinkSet.Id
		}

		sets = append(sets, item)
	}

	zap.L().Info("Successfully retrieved sets",
		zap.String("query", query),
		zap.Int("count", len(sets)),
	)

	render.JSON(w, r, sets)
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
//	@Param			id			path		string						true	"Item ID (from search results)"
//	@Security		Bearer
//	@Success		202	{object}	set.DetailsResponse				"Job created or data available"
//	@Failure		400	{object}	render.ErrorResponse			"Bad Request"
//	@Router			/api/v1/set/details/{id} [post]
func (h Handler) FetchSetDetails(w http.ResponseWriter, r *http.Request) {
	setId, ok := ParseParamUUID(w, r, "id")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Extract locale + currency from context
	locale := GetLocaleFromContext(r)
	currency := GetCurrencyFromContext(r)

	zap.L().Info("FetchSetDetails endpoint called",
		zap.String("id", setId.String()),
		zap.String("locale", locale.String()),
		zap.String("currency", currency.String()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Check cached set data
	cacheResult, err := setruntime.CheckCachedSetData(r.Context(), setId, currency)
	if err != nil {
		render.Error(w, r, err, "Failed to check cached set data")
		return
	}

	// Handle based on cache status
	switch cacheResult.Status {
	case setruntime.CacheStatusComplete:
		// TODO : update the lego set price if outdated
		// TODO : update the pickabrick brick prices if outdated
		// Data is fully cached and ready to return
		render.Accepted(w, r, set.DetailsResponse{
			Completed: true,
			Set:       cacheResult.Set,
		})
		return

	case setruntime.CacheStatusNeedsPrices:
		// TODO : update the lego set price if outdated
		// Data is cached but needs price updates for requested currency
		h.handlePriceFetch(w, r, setId, cacheResult, locale, currency)
		return

	case setruntime.CacheStatusFetching:
		// Data is currently being fetched, return existing websocket
		h.handleFetchingStatus(w, r, setId, cacheResult.Set)
		return

	case setruntime.CacheStatusFailed, setruntime.CacheStatusNeedsRefetch, setruntime.CacheStatusMissing:
		// Need to start a complete fetch
		h.handleCompleteFetch(w, r, setId, locale, currency)
		return

	default:
		render.Error(w, r, fmt.Errorf("unknown cache status"), "Internal error")
		return
	}
}

// handlePriceFetch handles the case where we need to fetch prices for a different currency
func (h Handler) handlePriceFetch(w http.ResponseWriter, r *http.Request, setId uuid.UUID, cacheResult *setruntime.CacheCheckResult, locale, currency language.Tag) {
	// Check if there's already a runtime for this set
	if rs, ok := h.srh.FindRuntimeSetBySetId(setId); ok {

		// todo: ISSUE #9 - Currency : check if the requested currency matches? [FIRST]

		// Check if the runtime set is healthy (not failed)
		if rs.GetFetchStatus() == set.FetchStatusFailed {
			// Runtime set has failed - it should be cleaning itself up via its error handler
			// Don't interfere with its cleanup, just fall through to start a new fetch
			// The failed RS will call onRuntimeSetEnd and remove itself from the map
		} else {
			// Runtime set is healthy, return existing websocket
			render.Accepted(w, r, set.DetailsResponse{
				Completed:   false,
				WebsocketID: rs.ID.String(),
			})
			return
		}
	}

	// Create websocket runtime for price fetching
	rs := h.srh.RunSet(cacheResult.Set)

	// Add all bricks to the runtime in their original order
	// todo : ISSUE #9 - Currency : fix because bfull has 193 items but rs ends up with only 162
	rs.AddBricks(cacheResult.FullBricks)

	// Start goroutine to fetch missing prices
	go h.srh.FetchPricesForBricks(
		context.Background(),
		rs.ID,
		setId,
		cacheResult.BricksNeedingPrices,
		locale,
		currency,
	)

	// Return websocket ID for client to connect
	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// handleFetchingStatus handles the case where data is currently being fetched
func (h Handler) handleFetchingStatus(w http.ResponseWriter, r *http.Request, setId uuid.UUID, cachedSet set.Set) {
	// Find the existing runtime set
	if rs, ok := h.srh.FindRuntimeSetBySetId(setId); ok {

		// todo: ISSUE #9 - Currency : check if the requested currency matches? [FIRST] -> runset for user2 ?

		// Check if the runtime set is healthy (not failed)
		if rs.GetFetchStatus() == set.FetchStatusFailed {
			// Runtime set has failed - it should be cleaning itself up via its error handler
			// Don't interfere with its cleanup, just fall through to start a new fetch
			// The failed RS will call onRuntimeSetEnd and remove itself from the map
		} else {
			// Runtime set is healthy, return existing websocket
			render.Accepted(w, r, set.DetailsResponse{
				Completed:   false,
				WebsocketID: rs.ID.String(),
			})
			return
		}
	}

	// todo: ISSUE #9 - Currency : user1 fetches for currency1, but user2 might request a different currency -> runset for user2 ?

	// Inconsistent state: set marked as fetching but no runtime set found
	zap.L().Warn("Inconsistent state: set marked as fetching but no runtime set found",
		zap.String("set_id", setId.String()),
	)

	// Fall back to starting a new fetch
	h.handleCompleteFetch(w, r, setId, GetLocaleFromContext(r), GetCurrencyFromContext(r))
}

// handleCompleteFetch handles the case where we need to perform a complete fetch
func (h Handler) handleCompleteFetch(w http.ResponseWriter, r *http.Request, setId uuid.UUID, locale, currency language.Tag) {
	// Retrieve BrickLink set info from cache
	bricklinkSet, err := setruntime.GetBricklinkSetFromCache(r.Context(), setId)
	if err != nil {
		render.NotFound(w, r, err)
		return
	}

	// Create websocket
	rs := h.srh.RunSet(bricklinkSet)

	// Start processing in background
	go h.srh.FetchCompleteSetDetails(
		context.Background(),
		rs.ID,
		setId,
		bricklinkSet,
		locale,
		currency,
	)

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
		// Check if set failed in cache
		cachedSet, err := set.GetRedisSet(r.Context(), rsId)
		if err == nil && cachedSet.FetchStatus == set.FetchStatusFailed {
			render.Error(w, r, fmt.Errorf("FetchStatusFailed"), fmt.Sprintf("Set %s FetchStatusFailed", rsId.String()))
			return
		}

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
