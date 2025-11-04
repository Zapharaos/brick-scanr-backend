package handlers

import (
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TODO : cache
// TODO : rate limiting

// SearchSets godoc
//
//	@Id				SearchSets
//	@Summary		Search for LEGO sets
//	@Description	Search for LEGO sets on BrickLink by set number or name.
//	@Tags			Set
//	@Produce		json
//	@Param			query	path		string						true	"Search query (set number or name)"
//	@Security		Bearer
//	@Success		200	{array}		bricklink.SearchItem		"List of matching sets"
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

	client := bricklink.NewClient()
	sets, err := client.SearchSets(query)
	if err != nil {
		zap.L().Error("Failed to search sets",
			zap.Error(err),
			zap.String("query", query),
		)
		render.Error(w, r, err, "Failed to search BrickLink")
		return
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
//	@Tags			Set
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string						true	"BrickLink item ID (from search results)"
//	@Param			setNumber	path		string						true	"LEGO set number (e.g., 21043-1)"
//	@Security		Bearer
//	@Success		202	{object}	set.DetailsJob				"Job created"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Router			/api/v1/set/details/{id}/{setNumber} [post]
func (h Handler) FetchSetDetails(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	setNumber := chi.URLParam(r, "setNumber")

	zap.L().Info("StartSetDetailsJob endpoint called",
		zap.String("id", idStr),
		zap.String("set_number", setNumber),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if idStr == "" || setNumber == "" {
		zap.L().Warn("StartSetDetailsJob called with missing parameters",
			zap.String("id", idStr),
			zap.String("set_number", setNumber),
		)
		render.BadRequest(w, r, fmt.Errorf("both id and setNumber are required"))
		return
	}

	// TODO : bind idStr and/or setNumber to a uuid.UUID
	setId, err := uuid.NewUUID()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO : if data is cached, return cached data instead of creating a new job

	// Create websocket
	_ = h.srh.RunSet(set.Set{
		Id: setId,
	})

	// Create job
	job := set.CreateDetailsJob(idStr, setNumber, setId)

	// TODO : keep jobs?

	// Start processing in background
	go func() {
		// Update status to processing
		_ = jobs.M().UpdateStatus(job.ID, jobs.StatusProcessing)
		h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSet, setruntime.DataTypeCreated)

		// TODO : fetch set

		// Fetch inventory
		_ = jobs.M().UpdateProgress(job.ID, "fetching_inventory", "Fetching set inventory...", 0, 100)
		h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSetInventory, setruntime.DataTypeCreated)

		inventory, err := bricklink.C().FetchInventory(job.ItemID, job.SetNumber)
		if err != nil {
			zap.L().Error("Failed to fetch inventory",
				zap.Error(err),
				zap.String("job_id", job.ID),
				zap.String("id", job.ItemID),
				zap.String("set_number", job.SetNumber),
			)

			// TODO : handle error
			_ = jobs.M().FailJob(job.ID, "Failed to fetch inventory from BrickLink")
			h.srh.PushChange()
			h.srh.StopRuntimeSet()
			h.srh.RemoveRuntimeSet()
			return
		}

		_ = jobs.M().UpdateProgress(job.ID, "inventory_loaded", "Inventory loaded", 25, 100)
		h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSetInventory, setruntime.DataTypeCompleted)

		// Get unique items
		uniqueItems := make(map[string]bricklink.InventoryItem)
		for _, item := range inventory.Items {
			if _, exists := uniqueItems[item.ItemNo]; !exists {
				uniqueItems[item.ItemNo] = item
			}
		}

		totalUnique := len(uniqueItems)
		_ = jobs.M().UpdateProgress(job.ID, "fetching_prices", fmt.Sprintf("Fetching prices for %d items...", totalUnique), 25, 100)
		h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSetInventoryPrices, setruntime.DataTypeCreated)

		// Fetch prices
		prices := make([]pickabrick.Brick, 0, totalUnique)
		currentProgress := 0

		for itemNo, item := range uniqueItems {
			currentProgress++

			// TODO: Replace with actual price fetching
			// price, err := s.bricklinkClient.FetchPickABrickPrice(item.ItemNo)

			priceInfo := pickabrick.Brick{
				ItemID: item.ItemID,
				ItemNo: itemNo,
				// Price:  price,
			}

			prices = append(prices, priceInfo)

			// Update progress every 10 items or on last item
			if currentProgress%10 == 0 || currentProgress == totalUnique {
				percentDone := 25 + ((currentProgress * 75) / totalUnique) // 25-100%
				err = jobs.M().UpdateProgress(
					job.ID,
					"fetching_prices",
					fmt.Sprintf("Loaded %d/%d prices", currentProgress, totalUnique),
					percentDone,
					100,
				)

				if currentProgress == totalUnique {
					h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSetInventoryPrices, setruntime.DataTypeCompleted)
				} else {
					h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSetInventoryPrices, setruntime.DataTypeUpdated)
				}
			}
		}

		// Complete job
		result := &set.DetailsResult{
			Inventory: inventory,
			Prices:    prices,
		}

		_ = set.CompleteJob(job.ID, result)
		h.srh.PushChange(setId, setId, setId, setruntime.DataTypeSet, setruntime.DataTypeCompleted)

		zap.L().Info("Successfully completed set details job",
			zap.String("job_id", job.ID),
			zap.String("id", job.ItemID),
			zap.String("set_number", job.SetNumber),
			zap.Int("unique_items", totalUnique),
		)
	}()

	// Return job info immediately
	w.WriteHeader(http.StatusAccepted)
	render.JSON(w, r, job)
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
// TODO	//  @Failure		409		{object}	setruntime.checkoutTypeItem	 	"checkoutTypeItem"
//
//	@Failure		500		{string}	string							"Internal Server Error"
//	@Router			/api/v1/details/job/{id}/ws [get]
func (h Handler) SetDetailsJobWebSocket(w http.ResponseWriter, r *http.Request) {
	jobId, ok := ParseParamUUID(w, r, "id")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Verify job exists
	rs := h.srh.GetRuntimeSet(jobId)
	if rs == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// TODO : handle user ID?
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
