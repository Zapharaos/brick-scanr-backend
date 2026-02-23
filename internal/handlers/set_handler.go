package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
	"github.com/Zapharaos/go-spit"
	"github.com/Zapharaos/lingo"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

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
//	@Failure		404	{object}	render.ErrorResponse		"Not found
//	@Router			/api/v1/set/details/{id} [post]
func (h *Handler) FetchSetDetails(w http.ResponseWriter, r *http.Request) {
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

	// Extract locale from context
	locale := GetLanguageFromContext(r)

	zap.L().Info("FetchSetDetails endpoint called",
		zap.String("id", setId.String()),
		zap.String("locale", locale.String()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Look for any form of cached data, either through runtime or cache
	cacheSet, err := h.srh.GetCacheSet(r.Context(), setId, locale)
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
		h.handleSetFetchIncomplete(w, r, setId, cacheSet, locale)
		return

	case setruntime.CacheStatusFailed, setruntime.CacheStatusNeedsRefetch, setruntime.CacheStatusMissing:
		// Need to start a new complete fetch
		h.handleFetchSetComplete(w, r, setId, cacheSet.InventoryAccess, locale)
		return

	default:
		render.Error(w, r, fmt.Errorf("unknown cache status"), "Internal error")
		return
	}
}

// handleSetFetchIncomplete handles the case where we need to fetch missing elements
func (h *Handler) handleSetFetchIncomplete(w http.ResponseWriter, r *http.Request, setId uuid.UUID, cache *setruntime.CacheSet, locale language.Tag) {
	// Create the key for this operation
	key := setruntime.NewRuntimeSetKey(setId, locale, setruntime.OpTypeIncomplete)

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
		rs = h.srh.RunSet(cache.Set, locale, setruntime.OpTypeIncomplete, cache.InventoryAccess)
	} else {
		// There isn't an inventory access, we can skip the inventory stuff and proceed normally

		// Create websocket runtime for fetching
		rs = h.srh.RunSet(cache.Set, locale, setruntime.OpTypeIncomplete, setruntime.InventoryAccess{})

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
		locale,
	)

	// Return websocket ID for client to connect
	// Return the cached set data as well so the client can display it while waiting for the missing data to be fetched
	render.Accepted(w, r, set.DetailsResponse{
		Completed:   false,
		WebsocketID: rs.ID.String(),
	})
}

// handleFetchSetComplete handles the case where we need to perform a complete fetch
func (h *Handler) handleFetchSetComplete(w http.ResponseWriter, r *http.Request, setId uuid.UUID, ihAccess setruntime.InventoryAccess, locale language.Tag) {
	// Retrieve BrickLink set info from cache
	// If there was any set locale data, we would have handled it in the incomplete flow, not here
	cacheLocaleWithCore, _, err := set.RedisGetLocale(r.Context(), setId, locale, true)
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
	rs := h.srh.RunSet(se, locale, setruntime.OpTypeFull, ihAccess)

	// Start processing in background
	go h.srh.FetchSetComplete(
		context.Background(),
		rs,
		setId,
		locale,
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
func (h *Handler) SetDetailsWebSocket(w http.ResponseWriter, r *http.Request) {
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
func ExportSet(w http.ResponseWriter, r *http.Request) {
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

	// Extract locale from context
	locale := GetLanguageFromContext(r)

	// Retrieve set locale from cache
	sLocale, ok, err := set.RedisGetLocale(r.Context(), setId, locale, true)
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
		b, _ := setruntime.CheckBrickCache(r.Context(), bSet, locale, true)
		sLocale.Bricks[i] = b
	}

	// Get locale translations localizer
	localizer, _, err := lingo.GetLocalizer(locale)
	if err != nil {
		zap.L().Error("Failed to get localizer", zap.Error(err))
		render.Error(w, r, err, "Get localizer")
		return
	}

	// Generate export table
	startTime := time.Now()
	table := set.ExportBuildTable(sLocale, localizer, locale)

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
