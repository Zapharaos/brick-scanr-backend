package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TODO : v2 - rate limiting

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

	// Search through BrickLink
	client := bricklink.NewClient()
	bricklinkSets, err := client.SearchSets(query)
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
		// todo : concurrent write/get ? if the same search happens at the same time
		// todo : v3 - skip cache if not same locale?
		bricklinkSet, err := set.GetRedisBricklinkSet(r.Context(), fmt.Sprintf("%d", item.BricklinkID))
		if errors.Is(err, set.ErrKeyNotFound) {
			// Not found in cache, store it
			err = set.SetRedisBricklinkSet(r.Context(), item, 0)
			if err != nil {
				// Failed to cache set, ignore it
				continue
			}
		} else if err != nil {
			zap.L().Error("Failed to check set in Redis cache",
				zap.Error(err),
				zap.String("set_id", item.Id.String()),
			)
			continue
		}

		// Use the cached UUID if available
		if bricklinkSet.Id != uuid.Nil {
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

	zap.L().Info("StartSetDetailsJob endpoint called",
		zap.String("id", setId.String()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// todo : v3 - skip cache if not same locale? cache brick separately from set?
	// Search for cached set data
	cachedSet, err := set.GetRedisSet(r.Context(), setId)
	if err != nil {
		// Failed to retrieve cached data, start a new fetch
	} else if cachedSet.FetchStatus == set.FetchStatusFailed {
		// Previous fetch failed, log and continue to start a new fetch
		zap.L().Warn("Previous set details fetch failed, starting a new fetch",
			zap.String("id", setId.String()),
		)
	} else if cachedSet.FetchStatus == set.FetchStatusCompleted {
		// Data is cached and complete, return it with completed flag
		zap.L().Info("Set details found in cache, returning cached data",
			zap.String("id", setId.String()),
		)
		render.Accepted(w, r, set.DetailsResponse{
			Completed: true,
			Set:       cachedSet,
		})
		return
	} else if cachedSet.FetchStatus == set.FetchStatusFetching {
		// Data is being fetched
		zap.L().Info("Set details still being fetched, returning websocket info",
			zap.String("id", setId.String()),
		)
		// Find the existing runtime set
		if rs, ok := h.srh.FindRuntimeSetBySetId(setId); ok {
			render.Accepted(w, r, set.DetailsResponse{
				Completed:   false,
				WebsocketID: rs.ID.String(),
			})
			return
		}
		// If we reach here, something is inconsistent. Log and continue to start a new fetch
		zap.L().Warn("Inconsistent state: set marked as fetching but no runtime set found",
			zap.String("id", setId.String()),
		)
	}

	// Retrieve BrickLink set info from cache
	bricklinkSet, err := set.GetRedisBricklinkSetFromSetID(r.Context(), setId)
	if err != nil {
		render.Error(w, r, err, "Failed to retrieve BrickLink set from cache")
		return
	}

	// Create websocket
	rs := h.srh.RunSet(bricklinkSet)

	// Extract locale + currency from context
	locale := GetLocaleFromContext(r)
	currency := GetCurrencyFromContext(r)

	// Helper to handle fatal errors
	handleFatalError := func(dataType setruntime.DataType, err error, msg string, fields ...zap.Field) {
		zap.L().Error(msg, append(fields, zap.Error(err))...)
		h.srh.PushChange(rs.ID, setId, dataType, setruntime.DataTypeFailed)
	}

	// Start processing in background
	go func() {
		// Ensure runtime set is stopped at the end
		// TODO : keep? users still need the final result
		// defer h.srh.StopRuntimeSet(rs.ID)

		// Use background context since request context will be canceled after response is sent
		ctx := context.Background()

		// Initialize copy of set inside redis
		cpRedisSet := bricklinkSet

		// Update in cache
		cpRedisSet.FetchStatus = set.FetchStatusStarting
		err = set.SetRedisSet(ctx, cpRedisSet, 0)
		if err != nil {
			handleFatalError(setruntime.DataTypeSet, err, "Failed to init Redis set",
				zap.String("set_id", setId.String()))
			return
		}

		// Push to websocket
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSet, setruntime.DataTypeCreated)

		// TODO : v2 - fetch set

		// Fetch inventory
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSetInventory, setruntime.DataTypeCreated)
		inventory, err := bricklink.C().FetchInventory(bricklinkSet.BricklinkID, bricklinkSet.BricklinkNumber)
		if err != nil {
			handleFatalError(setruntime.DataTypeSetInventory, err, "Failed to fetch inventory",
				zap.Int("id", bricklinkSet.BricklinkID),
				zap.String("set_number", bricklinkSet.BricklinkNumber))
			return
		}

		// Map Bricklink inventory to internal set bricks
		cpRedisSet.Bricks = set.MapBricksFromBricklinkInventory(inventory)

		// Update in cache
		cpRedisSet.FetchStatus = set.FetchStatusFetchingInventoryPrices
		err = set.SetRedisSet(ctx, cpRedisSet, 0)
		if err != nil {
			handleFatalError(setruntime.DataTypeSet, err, "Failed to update Redis set inventory",
				zap.String("set_id", setId.String()))
			return
		}

		// Push to websocket
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSetInventory, setruntime.DataTypeCompleted)
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSetInventoryPrices, setruntime.DataTypeCreated)

		// Fetch prices
		bmap := cpRedisSet.NewBrickMap()
		currentProgress := 0
		total := len(bmap.BricksByDesign)
		// todo : v3 - optimize
		for designID := range bmap.BricksByDesign {
			// Fetch bricks by designID
			results, err := pickabrick.C().FetchBricksByDesignID(string(designID), locale, currency)
			if err != nil {
				handleFatalError(setruntime.DataTypeSetInventory, err, "Failed to fetch bricks by designID",
					zap.String("designID", string(designID)),
					zap.String("set_id", setId.String()))
				return
			}

			// Process results
			for _, res := range results {
				brickID := set.BrickID(res.ID)
				// Try to match the result ID's with the set bricks ID's
				brick, ok := bmap.GetBrickByID(brickID)
				if !ok || brick == nil {
					continue
				}
				if brick.Price.CentAmount == 0 || res.Price.CentAmount < brick.Price.CentAmount {
					brick.MainID = &brickID
					brick.Price = set.MapPriceFromPickabrick(res.Price)
				}
			}

			currentProgress++

			// Update progress every 10 items or on last item
			if currentProgress%10 == 0 || currentProgress == total {
				// TODO : v2 - use percentages? keep?
				_ = 25 + ((currentProgress * 75) / total) // 25-100%

				// Determine current fetch status
				var fetchStatus set.FetchStatus
				var changeReason setruntime.DataChangeReason
				if currentProgress == total {
					fetchStatus = set.FetchStatusCompleted
					changeReason = setruntime.DataTypeCompleted
				} else {
					fetchStatus = set.FetchStatusFetchingInventoryPrices
					changeReason = setruntime.DataTypeUpdated
				}

				// Update in cache
				cpRedisSet.FetchStatus = fetchStatus
				err = set.SetRedisSet(ctx, cpRedisSet, 0)
				if err != nil {
					handleFatalError(setruntime.DataTypeSet, err, "Failed to update Redis set inventory prices",
						zap.String("set_id", setId.String()))
					return
				}

				// Push to websocket
				h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSetInventoryPrices, changeReason)
			}
		}

		// Push to websocket
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSet, setruntime.DataTypeCompleted)

		zap.L().Info("Successfully completed set details job",
			zap.String("id", setId.String()),
			zap.Int("unique_items", total),
		)
	}()

	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// SetDetailsJobWebSocket godoc
//
//	@Id				SetDetailsJobWebSocket
//
//	@Summary		Websocket for a set details job
//	@Description    Websocket for updates on a set details job.
//	@Tags			Set
//	@Security		Bearer
//	@Param			id		path		string			true			"Job ID"
//	@Success		200		{string}	string							"ok"
//	@Failure		401		{object}	setruntime.packetSpec			"Packet Specification"
//
// TODO:	Client //  @Failure		409		{object}	setruntime.checkoutTypeItem	 	"checkoutTypeItem"
//
//	@Failure		500		{string}	string							"Internal Server Error"
//	@Router			/api/v1/details/ws/{id} [get]
func (h Handler) SetDetailsJobWebSocket(w http.ResponseWriter, r *http.Request) {
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

	// TODO : Client - handle user ID?
	userId, err := uuid.NewUUID()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Check if client already connected
	if rs.HasClient(userId) {
		w.WriteHeader(http.StatusConflict)
		return
	}

	// Upgrade to websocket
	conn, err := h.srh.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("SetDetailsJobWebSocket.Upgrade:", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Create and register client
	setruntime.NewClient(rs, conn, userId)
}
