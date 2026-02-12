package brick

import (
	"strconv"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
)

// MapNewFromBricklinkInventoryItem maps a Bricklink InventoryItem to a new Inventory instance
func MapNewFromBricklinkInventoryItem(bi bricklink.InventoryItem) (Core, Inventory) {
	return MapFromBricklinkInventoryItem(Core{}, bi)
}

// MapFromBricklinkInventoryItem maps a Bricklink InventoryItem into an existing Inventory instance, updating only certain fields
func MapFromBricklinkInventoryItem(core Core, bi bricklink.InventoryItem) (Core, Inventory) {
	// Map ItemIDs to ElementIDs
	var ids []ElementID
	ids = make([]ElementID, len(bi.ItemIDs))
	for i, id := range bi.ItemIDs {
		ids[i] = ElementID(id)
	}

	// Update Core
	core.IsCustom = bi.IsCustom()
	core.ElementIDs = ids
	core.DesignID = DesignID(bi.ItemNo)
	core.Name = bi.Description
	core.ImageURL = bi.ImageURL
	if bi.HasUniqueItemID() {
		core.ElementID = &ids[0]
	}

	// Update Inventory
	var inventory Inventory
	inventory.Index = bi.Index
	if bi.Quantity != "" {
		if q, err := strconv.Atoi(bi.Quantity); err == nil {
			inventory.Quantity = q
		}
	}

	return core, inventory
}
