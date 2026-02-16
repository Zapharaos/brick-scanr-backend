package brick

import (
	"strconv"
	"strings"

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

// NewCoreFromBricklinkBrick maps a Bricklink Brick to an internal Core representation
func NewCoreFromBricklinkBrick(b *bricklink.Brick) Core {
	var core Core

	// Set element IDs
	elementID := ElementID(b.ItemNo)
	core.ElementID = &elementID
	core.ElementIDs = []ElementID{elementID}

	// Add alternate item numbers if available
	// Alternate item numbers can be a comma-separated list like "6141, 15570, 30057"
	if b.AlternateItemNo != "" {
		// Split by comma and process each alternate item number
		altItems := strings.Split(b.AlternateItemNo, ",")
		for _, altItem := range altItems {
			// Trim whitespace from each item
			trimmed := strings.TrimSpace(altItem)
			if trimmed != "" {
				altElementID := ElementID(trimmed)
				core.ElementIDs = append(core.ElementIDs, altElementID)
			}
		}
	}

	// Set name and image
	core.Name = b.Name
	core.ImageURL = b.ImageURL

	return core
}
