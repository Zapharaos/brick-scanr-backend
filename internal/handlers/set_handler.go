package handlers

import (
	"fmt"
	"net/http"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/handlers/render"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// TODO : cache
// TODO : rate limiting

// SearchSets godoc
//
//	@Id				SearchSets
//
//	@Summary		Search for LEGO sets
//	@Description	Search for LEGO sets on BrickLink by set number or name.
//	@Tags			Set
//	@Produce		json
//	@Param			query	path	string	true	"Search query (set number or name)"
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
		render.BadRequest(w, r, fmt.Errorf("search query is required"))
		return
	}

	client := bricklink.NewClient()
	sets, err := client.SearchSets(query)
	if err != nil {
		zap.L().Error("Failed to search sets",
			zap.Error(err),
			zap.String("query", query),
		)
		render.Error(w, r, err, "Failed to search sets on BrickLink")
		return
	}

	zap.L().Info("Successfully retrieved search results",
		zap.String("query", query),
		zap.Int("result_count", len(sets)),
	)

	render.JSON(w, r, sets)
}

// GetSetInventory godoc
//
//	@Id				GetSetInventory
//
//	@Summary		Get inventory for a specific LEGO set
//	@Description	Fetches the complete parts inventory for a LEGO set using its BrickLink ID and set number.
//	@Tags			Set
//	@Produce		json
//	@Param			id			path	string	true	"BrickLink item ID (from search results)"
//	@Param			setNumber	path	string	true	"LEGO set number (e.g., 21043-1)"
//	@Security		Bearer
//	@Success		200	{object}	bricklink.Inventory		"Complete inventory with parts list"
//	@Failure		400	{object}	render.ErrorResponse			"Bad Request"
//	@Failure		500	{object}	render.ErrorResponse			"Internal Server Error"
//	@Router			/api/v1/set/inventory/{id}/{setNumber} [get]
func GetSetInventory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	setNumber := chi.URLParam(r, "setNumber")

	zap.L().Info("GetSetInventory endpoint called",
		zap.String("id", idStr),
		zap.String("set_number", setNumber),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if idStr == "" || setNumber == "" {
		zap.L().Warn("GetSetInventory called with missing parameters",
			zap.String("id", idStr),
			zap.String("set_number", setNumber),
		)
		render.BadRequest(w, r, fmt.Errorf("both id and setNumber are required"))
		return
	}

	client := bricklink.NewClient()
	inventory, err := client.FetchInventory(idStr, setNumber)
	if err != nil {
		zap.L().Error("Failed to fetch inventory",
			zap.Error(err),
			zap.String("id", idStr),
			zap.String("set_number", setNumber),
		)
		render.Error(w, r, err, "Failed to fetch inventory from BrickLink")
		return
	}

	zap.L().Info("Successfully retrieved inventory",
		zap.String("id", idStr),
		zap.String("set_number", setNumber),
		zap.Int("parts_count", len(inventory.Items)),
	)

	render.JSON(w, r, inventory)
}
