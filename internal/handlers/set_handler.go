package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/database"
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

	// TODO : ISSUE #5 - Search Brick

	// TODO: upgrade the search query to also fetch details from lego.com to get the slug for frontend URL + more detailed data when multiple results
	// upon receiving the search results from bricklink API, for each set: fetchDetails() at this stage instead of later
	// 1. fetch details from lego.com and get the slug which will be used in the frontend URL (still use uuid under the hood)
	// 2. lego.com fails, we need to generate the slug ourselves (use strItemNo and strItemName probably)

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
		bricklinkID := fmt.Sprintf("%d", item.BricklinkID)
		cachedSet, ttl, err := set.GetRedisSetByBricklinkID(r.Context(), bricklinkID)
		if errors.Is(err, set.ErrKeyNotFound) {
			// Not found in cache, store it atomically
			// This will return the canonical set (with consistent UUID) even if another goroutine wins the race
			err := set.SetRedisSetForBricklinkID(r.Context(), item, true)
			if err != nil {
				// Failed to cache set, log but continue with the item we have
				zap.L().Warn("Failed to cache set in Redis",
					zap.Error(err),
					zap.String("set_id", item.Id.String()),
					zap.Int("bricklink_id", item.BricklinkID),
				)
			}
		} else if err != nil {
			zap.L().Error("Failed to check set in Redis cache",
				zap.Error(err),
				zap.String("set_id", item.Id.String()),
			)
			continue
		} else if set.IsTTLBelowThreshold(ttl, database.DB().Redis().TTLS.SetBricklinkMinThreshold) {
			// Check if TTL is too low (about to expire soon)
			// TTL is too low, delete the old cached data and refresh
			zap.L().Info("Cached set TTL is below threshold, refreshing",
				zap.String("set_id", cachedSet.Id.String()),
				zap.Int("bricklink_id", item.BricklinkID),
				zap.Duration("remaining_ttl", ttl),
			)

			// Delete the expired/about-to-expire data
			set.MustDeleteRedisKey(r.Context(), set.BuildKeyBricklinkIDToSetID(bricklinkID))

			// Create a new cache entry, with a new setID to not conflict with existing one which may still be in use
			// This will return the canonical set (with consistent UUID) even if another goroutine wins the race
			err := set.SetRedisSetForBricklinkID(r.Context(), item, true)
			if err != nil {
				// Failed to cache set, log but continue with the item we have
				zap.L().Warn("Failed to cache set in Redis",
					zap.Error(err),
					zap.String("set_id", item.Id.String()),
					zap.Int("bricklink_id", item.BricklinkID),
				)
			}
		} else {
			// No errors, data was found and TTL is fine, use the cached UUID
			item.Id = cachedSet.Id
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
	cacheResult, err := h.srh.CheckCachedSet(r.Context(), setId, currency)
	if err != nil || cacheResult == nil {
		render.Error(w, r, err, "Failed to check cached set data")
		return
	}

	// Handle based on cache status
	switch cacheResult.Status {
	case setruntime.CacheStatusComplete:
		// Data is fully cached and ready to return
		render.Accepted(w, r, set.DetailsResponse{
			Completed: true,
			Set:       cacheResult.Set,
		})
		return

	case setruntime.CacheStatusFetching:
		// Data is currently being fetched, return existing websocket
		render.Accepted(w, r, set.DetailsResponse{
			Completed:   false,
			WebsocketID: cacheResult.RsID.String(),
		})
		return

	case setruntime.CacheStatusMissesBricks:
		// Data is cached but needs price updates for requested currency
		h.handleMissingBricks(w, r, setId, cacheResult, locale, currency)
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

// handleMissingBricks handles the case where we need to fetch missing bricks
func (h Handler) handleMissingBricks(w http.ResponseWriter, r *http.Request, setId uuid.UUID, cacheResult *setruntime.CacheCheckResult, locale, currency language.Tag) {
	// Create the key for this operation
	key := setruntime.NewRuntimeSetKey(setId, currency, setruntime.OpTypePrices)

	// Check if there's already a runtime for this exact operation
	if rs, ok := h.srh.FindRuntimeSetByKey(key); ok {
		// Check if the runtime set is healthy (not failed)
		if rs.GetFetchStatus() == set.FetchStatusFailed {
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

	// Create websocket runtime for fetching
	rs := h.srh.RunSet(cacheResult.Set, currency, setruntime.OpTypePrices)

	// Add all bricks to the runtime in their original order
	rs.AddBricks(cacheResult.Bricks)

	// Start goroutine to fetch missing bricks
	go h.srh.FetchMissingBricks(
		context.Background(),
		rs,
		setId,
		cacheResult,
		locale,
		currency,
	)

	// Return websocket ID for client to connect
	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// handleCompleteFetch handles the case where we need to perform a complete fetch
func (h Handler) handleCompleteFetch(w http.ResponseWriter, r *http.Request, setId uuid.UUID, locale, currency language.Tag) {
	// Retrieve BrickLink set info from cache
	cachedSet, err := set.GetRedisSet(r.Context(), setId)
	if err != nil {
		// TTL most likely expired - cannot proceed
		render.NotFound(w, r, err)
		return
	}

	// Create websocket
	rs := h.srh.RunSet(cachedSet, currency, setruntime.OpTypeFull)

	// Start processing in background
	go h.srh.FetchCompleteSetDetails(
		context.Background(),
		rs,
		setId,
		cachedSet,
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
