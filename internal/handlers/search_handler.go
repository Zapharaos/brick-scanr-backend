package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/Zapharaos/brick-scanr-backend/internal/searchruntime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type SearchResponseType int

const (
	SearchResponseTypeSet SearchResponseType = iota
	SearchResponseTypeBrickElement
	SearchResponseTypeBrickDesign
)

type SearchResponseItem struct {
	Type SearchResponseType `json:"type"`
	Item interface{}        `json:"item"`
}

// SearchResponse wraps the top-level search response.
// When websocket_id is non-empty the client should open a WebSocket connection instead of reading items.
type SearchResponse struct {
	WebsocketID string               `json:"websocket_id,omitempty"`
	Items       []SearchResponseItem `json:"items,omitempty"`
}

// Search godoc
//
//	@Id				Search
//	@Summary		Search for LEGO elements
//	@Description	Search for LEGO elements on BrickLink.
//	@Description	When the number of results exceeds the configured threshold, a websocket_id is returned
//	@Description	and the client should connect to /api/v1/search/ws/{websocket_id} to receive results progressively.
//	@Tags			Set, Brick
//	@Produce		json
//	@Param			query	path		string				true	"Search query"
//	@Security		Bearer
//	@Success		200	{object}	SearchResponse			"Search results (or websocket_id for async mode)"
//	@Failure		400	{object}	render.ErrorResponse	"Bad Request"
//	@Failure		500	{object}	render.ErrorResponse	"Internal Server Error"
//	@Router			/api/v1/search/{query} [get]
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := chi.URLParam(r, "query")

	zap.L().Info("Search endpoint called",
		zap.String("query", query),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if query == "" {
		zap.L().Warn("Search called with empty query")
		render.BadRequest(w, r, fmt.Errorf("query parameter is required"))
		return
	}

	locale := GetLanguageFromContext(r)

	// Execute the BrickLink search – always required to know the result count
	bricklinkSets, bricklinkBricks, err := bricklink.C().Search(query, locale)
	if err != nil {
		zap.L().Error("Failed to search BrickLink", zap.Error(err), zap.String("query", query))
		render.Error(w, r, err, "Failed to search BrickLink")
		return
	}

	totalResults := len(bricklinkSets) + len(bricklinkBricks)

	// ---- WebSocket path (above threshold) ----
	if h.srch.NeedsWebSocket(totalResults) {
		rt := h.srch.RunSearch(
			r.Context(),
			query,
			locale,
			totalResults,
			func(rt *searchruntime.Runtime) {
				searchruntime.ProcessSearchAsync(
					context.Background(),
					rt,
					bricklinkSets,
					bricklinkBricks,
					locale,
				)
			},
		)
		render.JSON(w, r, SearchResponse{WebsocketID: rt.ID.String()})
		return
	}

	// ---- Synchronous path (at or below threshold) ----
	responseItems := make([]SearchResponseItem, 0, totalResults)

	for _, s := range bricklinkSets {
		result, err := searchruntime.ProcessSetItem(r.Context(), s, locale)
		if err != nil {
			zap.L().Warn("Failed to process set search result", zap.Error(err))
			continue
		}
		responseItems = append(responseItems, SearchResponseItem{
			Type: SearchResponseTypeSet,
			Item: result.Item,
		})
	}

	for _, b := range bricklinkBricks {
		result, err := searchruntime.ProcessBrickItem(r.Context(), b, locale)
		if err != nil {
			zap.L().Warn("Failed to process brick search result", zap.Error(err))
			continue
		}
		var itemType SearchResponseType
		switch result.Type {
		case "brickDesign":
			itemType = SearchResponseTypeBrickDesign
		default:
			itemType = SearchResponseTypeBrickElement
		}
		responseItems = append(responseItems, SearchResponseItem{
			Type: itemType,
			Item: result.Item,
		})
	}

	zap.L().Info("Search completed synchronously",
		zap.String("query", query),
		zap.Int("count", len(responseItems)),
	)

	render.JSON(w, r, SearchResponse{Items: responseItems})
}

// SearchWebSocket godoc
//
//	@Id				SearchWebSocket
//	@Summary		WebSocket for progressive search results
//	@Description	Connect with the websocket_id returned by the Search endpoint to receive results progressively.
//	@Tags			Set, Brick
//	@Security		Bearer
//	@Param			id	path		string	true						"WebSocket ID from the Search response"
//	@Success		101	{string}	string								"Switching Protocols"
//	@Failure		401	{object}	searchruntime.packetSpec			"Packet Specification"
//	@Failure		404	{string}	string								"Runtime not found"
//	@Failure		500	{string}	string								"Internal Server Error"
//	@Router			/api/v1/search/ws/{id} [get]
func (h *Handler) SearchWebSocket(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseParamUUID(w, r, "id")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	rt := h.srch.GetRuntime(id)
	if rt == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	conn, err := h.srch.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("SearchWebSocket upgrade failed", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	clientID := uuid.New()
	zap.L().Info("Search WebSocket client connected",
		zap.String("runtime_id", rt.ID.String()),
		zap.String("client_id", clientID.String()),
	)

	searchruntime.NewClient(rt, conn, clientID)
}

// searchBrickByDesignID performs a BrickLink search by design ID and maps it to the internal representation.
// Used by brick_handler.go when cache miss occurs for a design-based lookup.
func searchBrickByDesignID(w http.ResponseWriter, r *http.Request, designID brick.DesignID, inputDesign brick.DesignWithBricks) (brick.DesignWithBricks, bool) {
	// Extract data
	locale := GetLanguageFromContext(r)

	_, bricklinkBricks, err := bricklink.C().Search(string(designID), locale)
	if err != nil {
		zap.L().Error("Failed to search BrickLink", zap.Error(err), zap.String("query", string(designID)))
		render.Error(w, r, err, "Failed to search BrickLink")
		return brick.DesignWithBricks{}, false
	}

	// No results found, or multiple results found, we cannot be sure which one is the correct one, return not found
	if len(bricklinkBricks) == 0 || len(bricklinkBricks) > 1 {
		render.NotFound(w, r, fmt.Errorf("design not found on BrickLink"))
		return brick.DesignWithBricks{}, false
	}

	// Get the design ID from the BrickLink search item
	resDesignID := brick.GetDesignIDFromBricklinkSearchItem(bricklinkBricks[0])

	// Fetch the design details for the main design ID
	design, ok := searchruntime.GetBrickDesign(r.Context(), resDesignID, locale)
	if !ok || design.DesignStatus < brick.DesignStatusMinimal {
		render.NotFound(w, r, fmt.Errorf("design not found on BrickLink"))
		return brick.DesignWithBricks{}, false
	}

	// Cache the main design
	if cacheErr := brick.RedisSetDesign(r.Context(), design, locale, true); cacheErr != nil {
		zap.L().Error("Failed to cache design in Redis", zap.Error(cacheErr))
		// Not a critical error, we can still return the data without caching
	}

	// Use the fetched design as the main one, but replace with the requested design ID data
	for i, alternateID := range design.Alternates {
		if alternateID == designID {
			design.Alternates[i] = design.ID.DesignID
			break
		}
	}
	inputDesign.Alternates = design.Alternates
	inputDesign.ID.DesignID = designID

	// Cache the requested design
	if cacheErr := brick.RedisSetDesign(r.Context(), design, locale, true); cacheErr != nil {
		zap.L().Error("Failed to cache design in Redis", zap.Error(cacheErr))
		// Not a critical error, we can still return the data without caching
	}

	return inputDesign, true
}

// searchBrickByElementID performs a BrickLink search by element ID and maps it to the internal representation.
// Used by brick_handler.go when cache miss occurs for an element-based lookup.
func searchBrickByElementID(w http.ResponseWriter, r *http.Request, elementID brick.ElementID) (brick.Locale, brick.DesignIndex, bool) {
	// Extract locale from context
	locale := GetLanguageFromContext(r)

	_, bricklinkBricks, err := bricklink.C().Search(string(elementID), locale)
	if err != nil {
		zap.L().Error("Failed to search BrickLink", zap.Error(err), zap.String("query", string(elementID)))
		render.Error(w, r, err, "Failed to search BrickLink")
		return brick.Locale{}, nil, false
	}

	// No results found, or multiple results found, we cannot be sure which one is the correct one, return not found
	if len(bricklinkBricks) == 0 || len(bricklinkBricks) > 1 {
		render.NotFound(w, r, fmt.Errorf("element not found on BrickLink"))
		return brick.Locale{}, nil, false
	}

	// Get the element ID and design ID from the BrickLink search item
	bsi := bricklinkBricks[0]
	bsiElementID, bsiDesignID := brick.GetIDsFromBricklinkSearchItem(bsi)

	// If both element ID and design ID are empty, log an error and skip this item
	// This should not happen, as we should have at least one of them, but we want to be safe and avoid processing invalid data
	if (bsiElementID == "" && bsiDesignID == "") || bsiElementID != elementID {
		zap.L().Error("Mismatched element ID from BrickLink search item", zap.String("strItemNo", bsi.StrItemNo))
		render.NotFound(w, r, fmt.Errorf("element not found on BrickLink"))
		return brick.Locale{}, nil, false
	}

	// We have a matching brick, we can try to find the design by design ID
	designIndex, ok := fetchDesignIndexByDesignID(w, r, bsiDesignID)
	if !ok {
		// The helper function already rendered the error response, we can just return here
		return brick.Locale{}, nil, false
	}

	// Build a minimal brick locale version
	design, ok := designIndex[bsiDesignID]
	if !ok {
		render.NotFound(w, r, fmt.Errorf("design not found"))
		return brick.Locale{}, nil, false
	}

	bLocale := brick.Locale{}
	bLocale.Core = design.Core
	bLocale.ID.ElementID = elementID

	// Search for the brick locale in cache
	var valid, notfound bool
	bLocale, valid, notfound = bLocale.LoadFromRedis(r.Context(), elementID, locale, false, false)
	if !valid && !notfound {
		render.NotFound(w, r, fmt.Errorf("design not found"))
		return brick.Locale{}, nil, false
	}

	// Brick locale already cached, return it
	return bLocale, designIndex, true
}
