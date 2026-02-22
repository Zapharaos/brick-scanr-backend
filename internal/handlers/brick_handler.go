package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// BrickByElementIDResponse represents the response structure for fetching brick details by element ID
type BrickByElementIDResponse struct {
	Brick       brick.Locale      `json:"brick,omitempty"`
	DesignIndex brick.DesignIndex `json:"design_index"`
}

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
//	@Success		200	{object}	BrickByElementIDResponse	"Data available"
//	@Failure		400	{object}	render.ErrorResponse		"Bad Request"
//	@Failure		404	{object}	render.ErrorResponse		"Not found"
//	@Router			/api/v1/brick/details/element/{id} [get]
func FetchBrickDetailsByElement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		render.BadRequest(w, r, fmt.Errorf("id parameter is required"))
		return
	}

	// Build a minimal brick locale version
	elementID := brick.ElementID(id)

	// Search for the brick locale in cache
	if bLocale, err := brick.RedisGetLocale(r.Context(), elementID, GetLanguageFromContext(r)); err == nil {

		// Fetch the design details for the brick design ID
		designIndex, ok := fetchDesignIndexByDesignID(w, r, bLocale.ID.DesignID)
		if !ok {
			// The helper function already rendered the error response, we can just return here
			return
		}

		render.JSON(w, r, BrickByElementIDResponse{
			Brick:       bLocale,
			DesignIndex: designIndex,
		})
		return
	}

	// Not found in cache : must run a search query first
	bLocale, designIndex, ok := searchBrickByElementID(w, r, elementID)
	if !ok {
		// The helper function already rendered the error response, we can just return here
		return
	}

	render.JSON(w, r, BrickByElementIDResponse{
		Brick:       bLocale,
		DesignIndex: designIndex,
	})
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
func FetchBrickDetailsByDesign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		render.BadRequest(w, r, fmt.Errorf("id parameter is required"))
		return
	}

	designIndex, ok := fetchDesignIndexByDesignID(w, r, brick.DesignID(id))
	if !ok {
		// The helper function already rendered the error response, we can just return here
		return
	}

	render.JSON(w, r, designIndex)
}

// fetchDesignIndexByDesignID is a helper function to fetch the design details for a given design ID
func fetchDesignIndexByDesignID(w http.ResponseWriter, r *http.Request, designID brick.DesignID) (brick.DesignIndex, bool) {
	// Extract locale from context
	locale := GetLanguageFromContext(r)

	// Fetch the design details
	mainDesign, err := fetchDesign(r.Context(), designID, locale)
	if err != nil {
		render.Error(w, r, err, "Failed to fetch design details")
		return nil, false
	}

	if mainDesign.DesignStatus != brick.DesignStatusComplete && mainDesign.DesignStatus != brick.DesignStatusBricksNotFound {
		// Status not being complete here means it was not found on Bricklink
		// This implies that the requested design ID is not the main design ID for this brick
		// But we could be able to retrieve it the main one by performing a search query with the design ID

		design, ok := searchBrickByDesignID(w, r, designID, mainDesign)
		if !ok {
			return nil, false
		}

		mainDesign = design
	}

	// Create a design index to hold the main design and its alternates
	designIndex := make(brick.DesignIndex)
	designIndex[mainDesign.Design.ID.DesignID] = &mainDesign

	var defaultDesign brick.Design
	if mainDesign.Core.ID != nil {
		defaultDesign = mainDesign.Design
	}

	// Iterate over each design ID
	for _, alternateDesignID := range mainDesign.Alternates {

		var alternateDesign brick.DesignWithBricks

		// Fetch the design details
		alternateDesign, err = fetchDesignWithDefault(r.Context(), alternateDesignID, locale, defaultDesign)
		if err != nil {
			render.Error(w, r, err, "Failed to fetch design details")
			return nil, false
		}

		if defaultDesign.ID == nil && alternateDesign.Core.ID != nil {
			defaultDesign = alternateDesign.Design
		}

		// Add the alternate design to the index
		designIndex[alternateDesign.Design.ID.DesignID] = &alternateDesign
	}

	return designIndex, true
}

// fetchDesignWithDefaultCore is a helper function to fetch the design details by design ID, it will check the cache first and if not found, it will fetch the data from BrickLink and pick-a-brick API, then cache the data for future lookups. It will also handle the different design status (minimal or complete) to optimize the fetching process.
func fetchDesign(
	ctx context.Context,
	designID brick.DesignID,
	locale language.Tag,
) (brick.DesignWithBricks, error) {
	return fetchDesignWithDefault(ctx, designID, locale, brick.Design{})
}

// fetchDesignWithDefaultCore is a helper function to fetch the design details by design ID, it will check the cache first and if not found, it will fetch the data from BrickLink and pick-a-brick API, then cache the data for future lookups. It will also handle the different design status (minimal or complete) to optimize the fetching process.
func fetchDesignWithDefault(
	ctx context.Context,
	designID brick.DesignID,
	locale language.Tag,
	defaultDesign brick.Design,
) (brick.DesignWithBricks, error) {
	// Check cache by design ID
	design, err := brick.RedisGetDesign(ctx, designID, locale)
	if err != nil && !errors.Is(err, redis.ErrKeyNotFound) {
		// An error has occurred, it's not a cache miss (not found) => log and skip caching for this item
		zap.L().Error("Failed to check design in Redis cache",
			zap.Error(err),
			zap.String("design_id", string(designID)),
		)
		return brick.DesignWithBricks{}, err
	}

	id := brick.ID{
		DesignID: designID,
	}

	// Design was not found, apply the default core data
	if err != nil && defaultDesign.DesignStatus != brick.DesignStatusUnknown {
		design = defaultDesign

		// Reset those fields
		design.ElementIDs = nil
		design.DesignStatus = brick.DesignStatusUnknown

		// Rebuild the alternates slice around the design ID
		design.Alternates = []brick.DesignID{}
		/*if defaultDesign.ID != nil {
			design.Alternates = append(design.Alternates, defaultDesign.ID.DesignID)
		}*/
		for _, alternateID := range defaultDesign.Alternates {
			if alternateID != designID {
				design.Alternates = append(design.Alternates, alternateID)
			}
		}
	}

	design.ID = &id
	design.IDs = defaultDesign.IDs

	// Note: in order to fetch the data related to a design ID, we have the following options:
	// option 1 : fetch by design ID, update element ID's cache entries if applicable

	// (+) single API call everytime, (-) N cache calls, N comparisons
	// option 2 : check all element ID's cache entries, fetch by element ID for the missing ones
	// (+) multiple API calls, but only when missing, (-) N cache calls
	// Decision : since we have to make the cache calls for every element ID anyway, it is much easier for now
	// to fetch by design ID

	var bricks []brick.Locale

	switch design.DesignStatus {

	case brick.DesignStatusMinimal:
		// Design data is minimal (bricks for element IDs are not provided) - retrieve the bricks

		bricks, err = design.FetchBricks(ctx, locale)
		if err != nil {
			zap.L().Error("Failed to fetch complete design details",
				zap.Error(err),
				zap.String("design_id", string(designID)),
			)
			return brick.DesignWithBricks{}, err
		}

	case brick.DesignStatusBricksNotFound:
		// Design is not found, we can return early with an empty bricks list

		return brick.DesignWithBricks{
			Design: design,
			Bricks: []brick.Locale{},
		}, nil

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

		bricks, err = design.Fetch(ctx, locale)
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
