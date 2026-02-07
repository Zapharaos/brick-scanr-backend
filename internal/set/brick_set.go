package set

import (
	"strconv"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"golang.org/x/text/language"
)

// BrickSet represents a Brick in the context of a Set, including quantity and price information.
// Do not use when caching a brick, except in the context of a Set.
type BrickSet struct {
	Brick

	Index      int   `json:"index"`
	Quantity   int   `json:"quantity"`
	Price      Price `json:"price"`
	TotalPrice Price `json:"total_price"`
}

// CleanupForCache clears fields that are not needed for caching and returns cleaned up data for later restoration
func (bs *BrickSet) CleanupForCache() (Price, Price) {
	copyPrice := bs.Price
	copyTotalPrice := bs.TotalPrice

	// Clear fields that are not needed for caching
	bs.Price = Price{}
	bs.TotalPrice = Price{}

	return copyPrice, copyTotalPrice
}

// RestoreAfterCache restores the original data after caching
func (bs *BrickSet) RestoreAfterCache(price Price, totalPrice Price) {
	bs.Price = price
	bs.TotalPrice = totalPrice
}

// MustApplyCurrency sets the Brick's Price and MainID based on the given locale tag if possible, otherwise does nothing
func (bs *BrickSet) MustApplyCurrency(tag language.Tag) {
	price, ok := bs.Prices.GetPrice(tag)
	if !ok {
		return
	}
	bs.Price = *price
	brickID := BrickID(price.ItemID)
	bs.MainID = &brickID
}

// CalculateTotalPrice calculates the total price based on unit price and quantity
func (bs *BrickSet) CalculateTotalPrice() {
	bs.TotalPrice = Price{
		CentAmount: bs.Price.CentAmount * bs.Quantity,
		Currency:   bs.Price.Currency,
	}
}

// SafeMapBrickFromBricklinkInventoryItem safely maps a Bricklink InventoryItem to an existing Brick, updating only certain fields
func SafeMapBrickFromBricklinkInventoryItem(brick BrickSet, bi bricklink.InventoryItem) BrickSet {
	qty := 0
	if bi.Quantity != "" {
		if q, err := strconv.Atoi(bi.Quantity); err == nil {
			qty = q
		}
	}

	// Map ItemIDs to BrickIDs
	var ids []BrickID
	ids = make([]BrickID, len(bi.ItemIDs))
	for i, id := range bi.ItemIDs {
		ids[i] = BrickID(id)
	}

	// If there's a unique ItemID, mark it as the main ID
	var mainID *BrickID
	if bi.HasUniqueItemID() {
		mainID = &ids[0]
	}

	// Update minimal fields
	brick.MainID = mainID
	brick.IDs = ids
	brick.DesignID = DesignID(bi.ItemNo)
	brick.Index = bi.Index
	brick.IsCustom = bi.IsCustom()

	// Update other fields
	if brick.Name == "" {
		// Only set name if not already set, pick-a-brick name has priority
		brick.Name = bi.Description
	}
	brick.ImageURL = bi.ImageURL
	brick.Quantity = qty

	return brick
}

// MapBrickFromBricklinkInventoryItem maps a Bricklink InventoryItem to an internal Brick representation
func MapBrickFromBricklinkInventoryItem(bi bricklink.InventoryItem) BrickSet {
	return SafeMapBrickFromBricklinkInventoryItem(BrickSet{}, bi)
}
