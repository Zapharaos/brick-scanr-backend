package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// todo : v3 - rate limiting
// todo : v3 - determine TTLs? in config file?
// todo : v3 - implement retry mechanism for setRedis?

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
		// todo : v3 - concurrent write/get ? if the same search happens at the same time
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

	// Extract locale + currency from context
	locale := GetLocaleFromContext(r)
	currency := GetCurrencyFromContext(r)

	zap.L().Info("StartSetDetailsJob endpoint called",
		zap.String("id", setId.String()),
		zap.String("locale", locale.String()),
		zap.String("currency", currency.String()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Search for cached set data
	cachedSet, err := set.GetRedisSet(r.Context(), setId)
	if err != nil {
		// Failed to retrieve cached data, start a new fetch
	} else {
		switch cachedSet.FetchStatus {
		case set.FetchStatusFailed:
			// Previous fetch failed, log and continue to start a new fetch
			zap.L().Warn("Previous set details fetch failed, starting a new fetch",
				zap.String("id", setId.String()),
			)
			break
		case set.FetchStatusCompleted:
			// Data is cached and complete, meaning the bricks and prices are available as well
			// If a brick is missing, it means it was not available at fetch time => check TTL before re-fetching

			zap.L().Info("Set details found in cache, returning cached data",
				zap.String("id", setId.String()),
			)

			bfull := make([]set.Brick, 0)
			bmissing := make([]set.Brick, 0)
			pmissing := make([]set.Brick, 0)

			// For each brick, retrieve full data from cache and apply currency
			for _, bmin := range cachedSet.Bricks {

				// Retrieve brickID - needed to get the brick from cache - it should not fail
				brickID, err := bmin.GetBrickIDForRedis()
				if err != nil {
					zap.L().Error("failed to get brick ID for redis",
						zap.Error(err),
					)
					continue
				}

				// Try to find in cache first
				brick, err := set.GetRedisBrick(r.Context(), brickID, bmin.DesignID)
				if err != nil {
					// TODO : v2 - shouldn't happen unless TTL expired => re-fetch?
					bmissing = append(bmissing, bmin)
					continue
				}

				// Search for the currency price
				if brick.Prices == nil || len(brick.Prices) == 0 {
					// No prices at all => the product is probably not available for sale anymore
				} else if price := brick.GetPriceForLocale(currency); price == nil || price.CentAmount == 0 {
					// TODO : v2 - price missing for currency => re-fetch?
					pmissing = append(pmissing, bmin)
					continue
				}

				// Apply currency only locally, without caching it
				brick.ApplyCurrency(currency)
				bfull = append(bfull, brick)
			}

			cachedSet.Bricks = bfull

			if len(bmissing) > 0 || len(pmissing) > 0 {
				zap.L().Warn("Set details incomplete in cache: missing bricks or prices",
					zap.String("id", setId.String()),
					zap.Int("bricks_missing_count", len(bmissing)),
					zap.Int("prices_missing_count", len(pmissing)),
				)
			}

			// For now, we return whatever is cached
			render.Accepted(w, r, set.DetailsResponse{
				Completed: true,
				Set:       cachedSet,
			})
			return
		default:
			// Data is being fetched
			zap.L().Info("Set details still being fetched, returning websocket info",
				zap.String("id", setId.String()),
			)

			// todo : v2 - for each bricks, retrieve the cached brick (or API brick) and apply currency
			// todo : v2 - how to sync? at the time we get here, the goroutine might have reached the next step

			// TODO : handle differently depending on the rs.FetchStatus?

			// Find the existing runtime set
			if rs, ok := h.srh.FindRuntimeSetBySetId(setId); ok {
				render.Accepted(w, r, set.DetailsResponse{
					Completed:   false,
					Set:         cachedSet,
					WebsocketID: rs.ID.String(),
				})
				return
			}
			// If we reach here, something is inconsistent. Log and continue to start a new fetch
			zap.L().Warn("Inconsistent state: set marked as fetching but no runtime set found",
				zap.String("id", setId.String()),
			)
		}
	}

	// Retrieve BrickLink set info from cache
	// This assumes the set was cached during search - should always be the case
	bricklinkSet, err := set.GetRedisBricklinkSetFromSetID(r.Context(), setId)
	if err != nil {
		render.Error(w, r, err, "Failed to retrieve BrickLink set from cache")
		return
	}

	// Create websocket
	rs := h.srh.RunSet(bricklinkSet)

	// Helper for fatal errors
	handleFatalError := func(step set.FetchErrorStep, data set.Set, dataType setruntime.DataType, err error, msg string, fields ...zap.Field) {
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
		h.srh.PushChange(rs.ID, setId, dataType, setruntime.DataTypeFailed)
	}

	// Helper for non-critical errors
	handleNonFatalError := func(err error, msg string, fields ...zap.Field) {
		zap.L().Warn(msg, append(fields, zap.Error(err))...)
	}

	// Start processing in background
	go func() {

		// Ensure runtime set is stopped at the end
		defer func() {
			// Give clients time to receive the message before cleanup
			time.Sleep(3 * time.Second)
			h.srh.StopRuntimeSet(rs.ID)
		}()

		// Use background context since request context will be canceled after response is sent
		ctx := context.Background()

		// Initialize copy of set inside redis
		cpRedisSet := bricklinkSet

		// Cache the status => for concurrent access to the websocket
		cpRedisSet.FetchStatus = set.FetchStatusFetching
		err = set.SetRedisSet(ctx, cpRedisSet, 0)
		if err != nil {
			// Failed to update the set in cache => FATAL, concurrent requests won't see it's being processed
			handleFatalError(set.FetchErrorInitCache, cpRedisSet, setruntime.DataTypeSet, err, "Failed to update Redis set inventory",
				zap.String("set_id", setId.String()))
			return
		}
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSet, setruntime.DataTypeCreated)

		// TODO : v2 - fetch set
		/* err = set.SetRedisSet(ctx, cpRedisSet, 0)
		if err != nil {
			handleFatalError(setruntime.DataTypeSet, err, "Failed to update Redis set inventory",
				zap.String("set_id", setId.String()))
			return
		}
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSet, setruntime.DataTypeUpdated)*/

		// Fetch inventory
		inventory, err := bricklink.C().FetchInventory(bricklinkSet.BricklinkID, bricklinkSet.BricklinkNumber)
		if err != nil {
			// Failed to fetch inventory => FATAL, the set is useless without it
			handleFatalError(set.FetchErrorFetchInventory, cpRedisSet, setruntime.DataTypeBricklinkBricks, err, "Failed to fetch inventory",
				zap.Int("id", bricklinkSet.BricklinkID),
				zap.String("set_number", bricklinkSet.BricklinkNumber))
			return
		}

		// Map Bricklink inventory to internal set bricks
		bprogress := setruntime.NewProgress(len(inventory.Items))
		for _, item := range inventory.Items {

			// Try to find in cache first
			brick, err := set.GetRedisBrick(ctx, set.BrickID(item.ItemIDs[0]), set.DesignID(item.ItemNo))
			if err != nil {
				// Not found in cache, map from Bricklink item
				brick = set.MapBrickFromBricklinkInventoryItem(item)

				// Cache the brick
				if err = set.SetRedisBrick(ctx, brick, 0); err != nil {
					// Failed to cache brick, log and pursue processing
					zap.L().Warn("Failed to cache brick", zap.Error(err),
						zap.String("brick_design_id", item.ItemNo),
					)
				}
			}

			// TODO : v2 - brick already cached? update it's TTL to match set's TTL
			// But then there is a risk that the price might become outdated at some point
			// Use price TTL ? Limit the amount of TTL postponements? Limit TTL regarding a brick.created_at field?

			// Append new brickIDs to cached bricksIDs ?

			// Update set copy with minimal brick info - just enough to identify the brick
			// Note: this helps linking a set brick to brick instance; full brick data can be gotten from cache or API
			cpRedisSet.Bricks = append(cpRedisSet.Bricks, brick)

			// Update progress
			bprogress.AddItem(brick)

			// Check batch progress
			if bprogress.HasReachedBatchLimit() {

				// No items in batch, continue
				if bprogress.EmptyItems() {
					bprogress.CompleteBatch()
					continue
				}

				// Send batch update via websocket with current batch data
				h.srh.PushBatchProgress(rs.ID, setruntime.DataTypeBricklinkBricks, *bprogress)
				bprogress.CompleteBatch()

				// Update set in cache
				err = set.SetRedisSet(ctx, cpRedisSet, 0)
				if err != nil {
					// Fatal error - inventory is essential
					handleFatalError(set.FetchErrorBatchCache, cpRedisSet, setruntime.DataTypeSet, err, "Failed to update Redis set inventory",
						zap.String("set_id", setId.String()))
					return
				}
			}
		}

		// Fetch prices
		bmap := set.NewBrickMap(cpRedisSet.Bricks)
		bprogress = setruntime.NewProgress(len(bmap.BricksByDesign))

		// todo : v3 - optimize
		for designID := range bmap.BricksByDesign {

			// Not using cache here because prices need to be fresh. However, we could put a short TTL cache?

			// Note : the API allows fetching all brickID's linked to a designID, but also allows fetching single brickID
			// DesignID : fewer API calls, less rate limiting risk, less network latency. More processing complexity.
			// DesignID should be more performant as the set inventory unique brick count grows.

			// Fetch bricks by designID
			matchingBricks, err := pickabrick.C().FetchBricksByDesignID(string(designID), locale, currency)
			if err != nil {
				handleNonFatalError(err, "Failed to fetch bricks by designID",
					zap.String("designID", string(designID)),
					zap.String("set_id", setId.String()))
				return
			}

			// Process matching bricks
			for _, mb := range matchingBricks {
				// Try to match the result ID's with the set bricks ID's
				brickID := set.BrickID(mb.ID)
				brick, ok := bmap.GetBrickByID(brickID)
				if !ok {
					// No matching brick ID, skip
					continue
				}

				// Check if currency price is already set => skip if price better than fetched price
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
					// Failed to update brick in cache, log and continue
					zap.L().Warn("Failed to update brick price in cache", zap.Error(err),
						zap.String("brick_design_id", string(designID)),
						zap.String("brick_id", string(brickID)),
					)
				}

				// Apply currency only locally, without caching it
				brick.ApplyCurrency(currency)

				// Update progress - add brick to batch
				bprogress.AddItem(brick)
			}

			// Update progress counter (even if no matching bricks found)
			bprogress.Increment()

			// Check batch progress
			if bprogress.HasReachedBatchLimit() {
				// Items in batch, update via websocket with current batch data
				if !bprogress.EmptyItems() {
					h.srh.PushBatchProgress(rs.ID, setruntime.DataTypePickabrickBricks, *bprogress)
				}
				bprogress.CompleteBatch()
			}
		}

		// Send final batch if there are remaining items
		if bprogress.BatchCurr > 0 {
			h.srh.PushBatchProgress(rs.ID, setruntime.DataTypePickabrickBricks, *bprogress)
			bprogress.CompleteBatch()
		}

		// Mark set fetch completed => send final update
		cpRedisSet.FetchStatus = set.FetchStatusCompleted
		err = set.SetRedisSet(ctx, cpRedisSet, 0)
		if err != nil {
			// Failed to cache the final version => FATAL, the set is useless without it
			handleFatalError(set.FetchErrorFinalCache, cpRedisSet, setruntime.DataTypeSet, err, "Failed to update Redis set inventory",
				zap.String("set_id", setId.String()))
			return
		}
		h.srh.PushChange(rs.ID, setId, setruntime.DataTypeSet, setruntime.DataTypeCompleted)

		zap.L().Info("Successfully completed set details job", zap.String("id", setId.String()))
	}()

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
