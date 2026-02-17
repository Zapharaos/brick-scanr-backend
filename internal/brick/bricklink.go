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
	var ids []ID
	ids = make([]ID, len(bi.ItemIDs))
	for i, id := range bi.ItemIDs {
		ids[i] = ID{
			ElementID: ElementID(id),
		}
	}

	id := ID{
		DesignID: DesignID(bi.ItemNo),
	}
	if bi.HasUniqueItemID() {
		id.ElementID = ids[0].ElementID
	}

	// Update Core
	core.IsCustom = bi.IsCustom()
	core.ID = &id
	core.IDs = ids
	core.Name = bi.Description
	core.ImageURL = bi.ImageURL

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
	id := ID{
		DesignID: DesignID(b.ItemNo),
	}
	core.ID = &id
	core.IDs = []ID{id}

	// Add alternate item numbers if available
	// Alternate item numbers can be a comma-separated list like "6141, 15570, 30057"
	if b.AlternateItemNo != "" {
		// Split by comma and process each alternate item number
		altItems := strings.Split(b.AlternateItemNo, ",")
		for _, altItem := range altItems {
			// Trim whitespace from each item
			trimmed := strings.TrimSpace(altItem)
			if trimmed != "" {
				core.IDs = append(core.IDs, ID{
					DesignID: DesignID(trimmed),
				})
			}
		}
	}

	// Set name and image
	core.Name = b.Name
	core.ImageURL = b.ImageURL

	return core
}

// GetIDsFromBricklinkSearchItem extracts the ElementID and DesignID from a Bricklink SearchItem
func GetIDsFromBricklinkSearchItem(bsi bricklink.SearchItem) (ElementID, DesignID) {
	var elementID ElementID
	if bsi.StrPCC != nil {
		// Extract the numeric part before the parentheses

		// B : "strItemNo": "2780", "strPCC": "278026(11)"
		// strItemNo is the Design ID, parse strPCC to get the element ID
		// we could get the color code as well from the parentheses, but we don't need it for now

		pccParts := strings.Split(*bsi.StrPCC, "(")
		if len(pccParts) > 0 {
			elementID = ElementID(pccParts[0])
		}
	}

	// A : "strItemNo": "4073", "strPCC": null
	// strItemNo is the Design ID, we have no element ID
	designID := DesignID(bsi.StrItemNo)

	return elementID, designID
}
