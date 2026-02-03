package set

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
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

type Color struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	Hex         string `json:"hex"`
	ContrastHex string `json:"contrast_hex"`
	FamilyName  string `json:"family_name"`
	FamilyKey   string `json:"family_key"`
}

func MapColorFromPickabrick(pab pickabrick.Brick) Color {
	color := Color{
		Hex:         pab.ColorHex,
		ContrastHex: pab.ContrastColorHex,
	}

	if pab.Facets != nil {
		if pab.Facets.Color != nil {
			color.Name = pab.Facets.Color.Name
			color.Key = pab.Facets.Color.Key
		}
		if pab.Facets.ColorFamily != nil {
			color.FamilyName = pab.Facets.ColorFamily.Name
			color.FamilyKey = pab.Facets.ColorFamily.Key
		}
	}

	return color
}

type Brick struct {
	BrickMinimal
	Status        Status `json:"status"`
	Name          string `json:"name"`
	ImageURL      string `json:"image_url"`
	PickabrickURL string `json:"pickabrick_url"`
	Color         Color  `json:"color"`
	Quantity      int    `json:"quantity"`
	Price         Price  `json:"price"`
	Prices        PricePerCurrencies
	TotalPrice    Price `json:"total_price"`
}

func (b *Brick) BuildPickabrickURL(locale language.Tag) {
	var id string
	if b.MainID != nil {
		id = string(*b.MainID)
	} else if len(b.IDs) > 0 {
		id = string(b.IDs[0])
	} else {
		id = string(b.DesignID)
	}
	b.PickabrickURL = "https://www.lego.com/" + locale.String() + "/pick-and-build/pick-a-brick?selectedElement=" + id
}

// CleanupForRedis prepares the Brick for Redis storage by removing quantity and index, returning them for later use
func (b *Brick) CleanupForRedis() (quantity, index int) {
	quantity = b.Quantity
	index = b.BrickMinimal.Index
	b.Quantity = 0
	b.BrickMinimal.Index = 0
	return
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

// CalculateTotalPrice calculates the total price based on unit price and quantity
func (b *Brick) CalculateTotalPrice() {
	b.TotalPrice = Price{
		CentAmount: b.Price.CentAmount * b.Quantity,
		Currency:   b.Price.Currency,
		ItemID:     b.Price.ItemID,
		FetchedAt:  b.Price.FetchedAt,
	}
}

// SafeMapBrickFromBricklinkInventoryItem safely maps a Bricklink InventoryItem to an existing Brick, updating only certain fields
func SafeMapBrickFromBricklinkInventoryItem(brick Brick, bi bricklink.InventoryItem) Brick {
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
func MapBrickFromBricklinkInventoryItem(bi bricklink.InventoryItem) Brick {
	return SafeMapBrickFromBricklinkInventoryItem(Brick{}, bi)
}

func MapBrickFromPickabrick(brick Brick, brickID BrickID, pab pickabrick.Brick, locale, currency language.Tag) Brick {
	// Prepare fetched price
	pbp := MapPriceFromPickabrick(pab.Price)
	pbp.ItemID = string(brickID)
	pbp.FetchedAt = time.Now().UnixMilli()

	// Update brick with fetched price
	if brick.Prices == nil {
		brick.Prices = make(map[language.Tag]*Price)
	}
	brick.Prices[currency] = &pbp

	// Update additional fields from Pick-a-Brick
	brick.MainID = &brickID
	brick.DesignID = DesignID(pab.DesignID)
	brick.BuildPickabrickURL(locale)
	brick.Status = MapPickabrickStatus(pab.Availability)
	brick.Color = MapColorFromPickabrick(pab)
	brick.Name = pab.Name

	return brick
}
