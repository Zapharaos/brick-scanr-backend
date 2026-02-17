package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchBrickDetailsByElement godoc
//
//	@Id				FetchBrickDetailsByElement
//	@Summary		Start a job to fetch brick details by element ID
//	@Description	Analyzes a brick to retrieve all related data.
//	@Tags			Brick
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"Element ID"
//	@Security		Bearer
//	@Success		200	{object}	brick.Locale				"Data available"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		404	{object}	render.ErrorResponse		"Not found"
//	@Router			/api/v1/brick/details/element/{id} [get]
func (h Handler) FetchBrickDetailsByElement(w http.ResponseWriter, r *http.Request) {
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
	bLocale.ID.ElementID = elementID

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
	err = brick.RedisSetLocale(r.Context(), bLocale, xlocale, true)
	if err != nil {
		zap.L().Error("Failed to cache brick in Redis",
			zap.Error(err),
			zap.String("element_id", string(elementID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	render.JSON(w, r, bLocale)
}

// FetchBrickDetailsByDesign godoc
//
//	@Id				FetchBrickDetailsByDesign
//	@Summary		Start a job to fetch brick details by design ID
//	@Description	Analyzes a brick to retrieve all related data.
//	@Tags			Brick
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"Design ID"
//	@Security		Bearer
//	@Success		200	{object}	brick.DesignIndex			"Data available"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		404	{object}	render.ErrorResponse		"Not found"
//	@Router			/api/v1/brick/details/design/{id} [get]
func (h Handler) FetchBrickDetailsByDesign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		render.BadRequest(w, r, fmt.Errorf("id parameter is required"))
		return
	}

	// Extract data
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)

	// Fetch the design details
	mainDesign, err := fetchDesign(r.Context(), brick.DesignID(id), lang, xlocale)
	if err != nil {
		render.Error(w, r, err, "Failed to fetch design details")
		return
	}

	// Create a design index to hold the main design and its alternates
	designIndex := make(brick.DesignIndex)
	designIndex[mainDesign.Design.ID.DesignID] = &mainDesign

	// Iterate over each design ID
	for _, alternateDesignID := range mainDesign.Alternates {

		var alternateDesign brick.DesignWithBricks

		// Fetch the design details
		alternateDesign, err = fetchDesign(r.Context(), alternateDesignID, lang, xlocale)
		if err != nil {
			render.Error(w, r, err, "Failed to fetch design details")
			return
		}

		// Add the alternate design to the index
		designIndex[alternateDesign.Design.ID.DesignID] = &alternateDesign
	}

	// TODO : provide a price range ? no status ? special label ? a dropdown to see every element IDs !!!
	render.JSON(w, r, designIndex)
}

// fetchDesign is a helper function to fetch the design details by design ID, it will check the cache first and if not found, it will fetch the data from BrickLink and pick-a-brick API, then cache the data for future lookups. It will also handle the different design status (minimal or complete) to optimize the fetching process.
func fetchDesign(
	ctx context.Context,
	designID brick.DesignID,
	lang language.Tag,
	xlocale language.Tag,
) (brick.DesignWithBricks, error) {
	// Check cache by design ID
	design, err := brick.RedisGetDesign(ctx, designID, xlocale)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		// An error has occurred, it's not a cache miss (not found) => log and skip caching for this item
		zap.L().Error("Failed to check design in Redis cache",
			zap.Error(err),
			zap.String("design_id", string(designID)),
		)
		return brick.DesignWithBricks{}, err
	}

	// Note: in order to fetch the data related to a design ID, we have the following options:
	// option 1 : fetch by design ID, update element ID's cache entries if applicable

	// (+) single API call everytime, (-) N cache calls, N comparisons
	// option 2 : check all element ID's cache entries, fetch by element ID for the missing ones
	// (+) multiple API calls, but only when missing, (-) N cache calls
	// Decision : since we have to make the cache calls for every element ID anyway, it is much easier for now
	// to fetch by design ID

	var bricks []brick.Locale
	design = brick.Design{}
	design.ID.DesignID = designID

	switch design.DesignStatus {

	case brick.DesignStatusMinimal:
		// Design data is minimal (bricks for element IDs are not provided) - retrieve the bricks

		bricks, err = design.FetchBricks(ctx, lang, xlocale)
		if err != nil {
			zap.L().Error("Failed to fetch complete design details",
				zap.Error(err),
				zap.String("design_id", string(designID)),
			)
			return brick.DesignWithBricks{}, err
		}

	// Design data is complete - make sure we have the latest data for each element ID and related brick locale
	//case brick.DesignStatusComplete:

	// For each element ID, we could check its value in the cache.
	// If present, we can use it instantly
	// If missing, we can fetch :
	//		1 : fetch it by element ID and update the cache
	//		2 : append to missing, and depending on missing length, use option 1 or fetch by design ID if large enough

	// Fallback to default case for now, it will be easier to implement

	default:
		// Not found in cache - retrieve all design ID data

		bricks, err = design.Fetch(ctx, lang, xlocale)
		if err != nil {
			zap.L().Error("Failed to fetch design details from BrickLink",
				zap.Error(err),
				zap.String("design_id", string(designID)),
			)
			return brick.DesignWithBricks{}, err
		}
	}

	// Return the processed bricks for design ID
	return brick.DesignWithBricks{
		Design: design,
		Bricks: bricks,
	}, err
}
