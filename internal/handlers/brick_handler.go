package handlers

import (
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// FetchBrickDetails godoc
//
//	@Id				FetchBrickDetails
//	@Summary		Start a job to fetch brick details
//	@Description	Analyzes a brick to retrieve all related data.
//	@Tags			Brick
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"Element ID"
//	@Security		Bearer
//	@Success		200	{object}	brick.Locale				"Job created or data available"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		404	{object}	render.ErrorResponse		"Not found"
//	@Router			/api/v1/brick/details/{id} [post]
func (h Handler) FetchBrickDetails(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		render.BadRequest(w, r, fmt.Errorf("id parameter is required"))
		return
	}

	// Extract language + xlocale from context
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)

	// Build a minimal brick locale version
	elementID := brick.ElementID(id)
	bLocale := brick.Locale{}
	bLocale.ElementID = &elementID

	// Search for the brick locale in cache
	var valid, notfound bool
	bLocale, valid, notfound = bLocale.LoadFromRedis(r.Context(), elementID, xlocale, false, false)

	// Brick locale already cached, return it
	if valid || notfound {
		render.JSON(w, r, bLocale)
		return
	}

	// Not found in cache

	// Query BrickLink for brick details
	bricklinkBrick, err := bricklink.C().FetchBrickDetails(string(elementID), lang)
	if err != nil {
		render.Error(w, r, err, "Failed to fetch brick details from BrickLink")
		return
	}

	// Map BrickLink brick details to internal representation
	bCore := brick.NewCoreFromBricklinkBrick(bricklinkBrick)

	// Create brick locale with the core data
	bLocale = brick.Locale{
		Core: bCore,
	}

	// Fetch data from pick-a-brick API
	ok, _, _ := bLocale.Fetch(r.Context(), elementID, lang, xlocale)
	if !ok {
		render.NotFound(w, r, fmt.Errorf("brick details not found on pick-a-brick API"))
		return
	}

	// Cache the brick details in Redis for future searches and lookups
	err = brick.RedisSet(r.Context(), bLocale, xlocale, true)
	if err != nil {
		zap.L().Error("Failed to cache brick in Redis",
			zap.Error(err),
			zap.String("element_id", string(elementID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	render.JSON(w, r, bLocale)
}
