package handlers

import (
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/go-chi/chi/v5"
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

// StartSetDetailsJob godoc
//
//	@Id				StartSetDetailsJob
//	@Summary		Start a job to fetch set details
//	@Description	Creates a background job to fetch set inventory and prices. Returns immediately with a job ID.
//	@Description	Use the job ID to poll for status or connect via WebSocket for real-time updates.
//	@Tags			Set
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string						true	"BrickLink item ID (from search results)"
//	@Param			setNumber	path		string						true	"LEGO set number (e.g., 21043-1)"
//	@Param			ws_id		query		string						false	"WebSocket connection ID for real-time updates"
//	@Security		Bearer
//	@Success		202	{object}	set.DetailsJob				"Job created"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Router			/api/v1/set/details/{id}/{setNumber} [post]
func StartSetDetailsJob(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	setNumber := chi.URLParam(r, "setNumber")
	wsConnID := r.URL.Query().Get("ws_id")

	zap.L().Info("StartSetDetailsJob endpoint called",
		zap.String("id", idStr),
		zap.String("set_number", setNumber),
		zap.String("ws_id", wsConnID),
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

	// Create job
	job := set.S().CreateDetailsJob(idStr, setNumber, wsConnID)

	// Start processing in background
	go set.S().ProcessDetailsJob(job, jobs.WsHandlerAdapter{})

	// Return job info immediately
	w.WriteHeader(http.StatusAccepted)
	render.JSON(w, r, job)
}

// GetSetDetailsJob godoc
//
//	@Id				GetSetDetailsJob
//	@Summary		Get job status and results
//	@Description	Poll this endpoint to get the current status, progress, and results of a set details job.
//	@Tags			Set
//	@Produce		json
//	@Param			job_id	path		string						true	"Job ID from StartSetDetailsJob"
//	@Security		Bearer
//	@Success		200	{object}	set.DetailsJob				"Job status and results"
//	@Failure		404	{object}	render.ErrorResponse		"Job not found"
//	@Router			/api/v1/set/job/{job_id} [get]
func GetSetDetailsJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "job_id")

	job, err := set.S().GetDetailsJob(jobID)
	if err != nil {
		render.NotFound(w, r, err)
		return
	}

	render.JSON(w, r, job)
}
