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

func MapColorFromBricklink(colorName string) Color {
	return Color{
		Name: colorName,
	}
}

// TODO : ISSUE #1 : Alternate items - cannot have index + quantity for a brick because this is related to a set

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
		Quantity: qty,
	}
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
	brick.BuildPickabrickURL(locale)
	brick.Status = MapPickabrickStatus(pab.Availability)
	brick.Color = MapColorFromPickabrick(pab)
	brick.Name = pab.Name

	return brick
}
