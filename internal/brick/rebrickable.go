package brick

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/rebrickable"
)

// MapNewFromRebrickableInventoryItem maps a Rebrickable InventoryItem to a new
// (Core, Inventory) pair.
func MapNewFromRebrickableInventoryItem(ri rebrickable.InventoryItem) (Core, Inventory) {
	return MapFromRebrickableInventoryItem(Core{}, ri)
}

// MapFromRebrickableInventoryItem maps a Rebrickable InventoryItem into an existing
// Core, updating the identifying fields. Rebrickable already provides a single
// unambiguous LEGO element ID per line (or none), so the IDs slice holds at most one
// entry — no "X or Y or Z" resolution is required.
func MapFromRebrickableInventoryItem(core Core, ri rebrickable.InventoryItem) (Core, Inventory) {
	id := ID{
		DesignID:  DesignID(ri.DesignID),
		ElementID: ElementID(ri.ElementID),
	}

	core.IsCustom = ri.IsCustom()
	core.ID = &id
	if ri.ElementID != "" {
		core.IDs = []ID{id}
	} else {
		core.IDs = []ID{}
	}
	core.Name = ri.Description
	core.ImageURL = ri.ImageURL

	var inventory Inventory
	inventory.Index = ri.Index
	inventory.Quantity = ri.Quantity
	inventory.ColorID = ri.ColorID

	return core, inventory
}
