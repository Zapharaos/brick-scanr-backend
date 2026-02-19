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

	// Search for the brick locale in cache
	if bLocale, err := brick.RedisGetLocale(r.Context(), elementID, xlocale); err == nil {

		// Fetch the design details for the brick design ID
		designIndex, ok := h.fetchDesignIndexByDesignID(w, r, bLocale.ID.DesignID)
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

	// TODO : Not found in cache : must run a search query

	// Query BrickLink for brick details
	bricklinkBrick, err := bricklink.C().FetchBrickDetails(string(elementID), lang)
	if err != nil {
		render.Error(w, r, err, "Failed to fetch brick details from BrickLink")
		return
	}

	// Map BrickLink brick details to internal representation
	bCore := brick.NewCoreFromBricklinkBrick(bricklinkBrick)

	// Create brick locale with the core data
	bLocale := brick.Locale{
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

	designIndex, ok := h.fetchDesignIndexByDesignID(w, r, brick.DesignID(id))
	if !ok {
		// The helper function already rendered the error response, we can just return here
		return
	}

	render.JSON(w, r, designIndex)
}

// fetchDesignIndexByDesignID is a helper function to fetch the design details for a given design ID
func (h Handler) fetchDesignIndexByDesignID(w http.ResponseWriter, r *http.Request, designID brick.DesignID) (brick.DesignIndex, bool) {
	// Extract data
	lang := GetLanguageFromContext(r)
	xlocale := GetXLocaleFromContext(r)

	// Fetch the design details
	mainDesign, err := fetchDesign(r.Context(), designID, lang, xlocale)
	if err != nil {
		render.Error(w, r, err, "Failed to fetch design details")
		return nil, false
	}

	if mainDesign.DesignStatus != brick.DesignStatusComplete && mainDesign.DesignStatus != brick.DesignStatusBricksNotFound {
		// Status not being complete here means it was not found on Bricklink
		// This implies that the requested design ID is not the main design ID for this brick
		// But we could be able to retrieve it the main one by performing a search query with the design ID

		_, bricklinkBricks, err := bricklink.C().Search(string(designID), lang)
		if err != nil {
			zap.L().Error("Failed to search responseItems",
				zap.Error(err),
				zap.String("query", string(designID)),
			)
			render.Error(w, r, err, "Failed to search BrickLink")
			return nil, false
		}

		// No results found, or multiple results found, we cannot be sure which one is the correct one, return not found
		if len(bricklinkBricks) == 0 || len(bricklinkBricks) > 1 {
			render.NotFound(w, r, fmt.Errorf("design not found on BrickLink"))
			return nil, false
		}

		// Get the design ID from the BrickLink search item
		resDesignID := brick.GetDesignIDFromBricklinkSearchItem(bricklinkBricks[0])

		// Fetch the design details for the main design ID
		design, ok := handleBricklinkBrickByDesignID(r.Context(), resDesignID, lang, xlocale)
		if !ok || design.DesignStatus < brick.DesignStatusMinimal {
			render.NotFound(w, r, fmt.Errorf("design not found on BrickLink"))
			return nil, false
		}

		// Cache the main design
		err = brick.RedisSetDesign(r.Context(), design, xlocale, true)
		if err != nil {
			zap.L().Error("Failed to cache design in Redis",
				zap.Error(err),
				zap.String("design_id", string(design.ID.DesignID)),
			)
			// Not a critical error, we can still return the data without caching
		}

		// Use the fetched design as the main one, but replace with the requested design ID data
		for i, alternateID := range design.Alternates {
			if alternateID == designID {
				design.Alternates[i] = design.ID.DesignID
				break
			}
		}
		mainDesign.Alternates = design.Alternates
		mainDesign.ID.DesignID = designID

		// Cache the requested design
		err = brick.RedisSetDesign(r.Context(), design, xlocale, true)
		if err != nil {
			zap.L().Error("Failed to cache design in Redis",
				zap.Error(err),
				zap.String("design_id", string(design.ID.DesignID)),
			)
			// Not a critical error, we can still return the data without caching
		}
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
		alternateDesign, err = fetchDesignWithDefault(r.Context(), alternateDesignID, lang, xlocale, defaultDesign)
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
	lang language.Tag,
	xlocale language.Tag,
) (brick.DesignWithBricks, error) {
	return fetchDesignWithDefault(ctx, designID, lang, xlocale, brick.Design{})
}

// fetchDesignWithDefaultCore is a helper function to fetch the design details by design ID, it will check the cache first and if not found, it will fetch the data from BrickLink and pick-a-brick API, then cache the data for future lookups. It will also handle the different design status (minimal or complete) to optimize the fetching process.
func fetchDesignWithDefault(
	ctx context.Context,
	designID brick.DesignID,
	lang language.Tag,
	xlocale language.Tag,
	defaultDesign brick.Design,
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

		bricks, err = design.FetchBricks(ctx, lang, xlocale)
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
