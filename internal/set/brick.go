package set

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"golang.org/x/text/language"
)

type BrickID string
type DesignID string

type BrickMinimal struct {
	MainID   *BrickID  `json:"main_id"`
	IDs      []BrickID `json:"ids"`
	DesignID DesignID  `json:"design_id"`
	Index    int       `json:"index"`
	IsCustom bool      `json:"is_custom"`
}

// GetBrickIDForRedis returns the appropriate BrickID to use as a Redis key
func (bm *BrickMinimal) GetBrickIDForRedis() (BrickID, error) {
	// Determine the ID to use for Redis key
	var keyID BrickID
	if bm.MainID != nil {
		keyID = *bm.MainID
	} else if len(bm.IDs) > 0 {
		// No main ID: return the first non-empty (after trimming) ID in the list
		for _, id := range bm.IDs {
			if strings.TrimSpace(string(id)) != "" {
				keyID = id
				break
			}
		}
		// If no valid ID found in the slice, fall through to the error
	}
	if keyID == "" {
		// No IDs at all - this shouldn't happen, but handle gracefully
		return "", errors.New("brick has no valid ID")
	}
	return keyID, nil
}

// TODO : ISSUE #1 : Alternate items - cannot have index + quantity for a brick because this is related to a set

type Brick struct {
	BrickMinimal
	Name       string `json:"name"`
	ImageURL   string `json:"image_url"`
	Color      string `json:"color"`
	ColorHex   string `json:"color_hex"`
	Quantity   int    `json:"quantity"`
	Price      Price  `json:"price"`
	Prices     PricePerCurrencies
	TotalPrice Price `json:"total_price"`
}

// MustApplyCurrency sets the Brick's Price and MainID based on the given locale tag if possible, otherwise does nothing
func (b *Brick) MustApplyCurrency(tag language.Tag) {
	price, ok := b.Prices.GetPrice(tag)
	if !ok {
		return
	}
	b.Price = *price
	brickID := BrickID(price.ItemID)
	b.MainID = &brickID
}

// MapBrickFromBricklinkInventoryItem maps a Bricklink InventoryItem to an internal Brick representation
func MapBrickFromBricklinkInventoryItem(bi bricklink.InventoryItem) Brick {
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

	return Brick{
		BrickMinimal: BrickMinimal{
			MainID:   mainID,
			IDs:      ids,
			DesignID: DesignID(bi.ItemNo),
			Index:    bi.Index,
			IsCustom: bi.IsCustom(),
		},
		Name:     bi.Description,
		ImageURL: bi.ImageURL,
		Color:    bi.Color,
		Quantity: qty,
	}
}
