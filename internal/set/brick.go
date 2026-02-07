package set

import (
	"errors"
	"strings"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"golang.org/x/text/language"
)

type BrickID string
type DesignID string

type Brick struct {
	// Local data
	MainID *BrickID `json:"main_id"`

	// General data
	IDs      []BrickID `json:"ids"`
	DesignID DesignID  `json:"design_id"`
	IsCustom bool      `json:"is_custom"`
	Prices   PricePerCurrencies

	// Could be made locale specific, but not for now
	Status        Status `json:"status"`
	Name          string `json:"name"`
	ImageURL      string `json:"image_url"`
	PickabrickURL string `json:"pickabrick_url"`
	Color         Color  `json:"color"`
}

// GetBrickIDForRedis returns the appropriate BrickID to use as a Redis key
func (b *Brick) GetBrickIDForRedis() (BrickID, error) {
	// Determine the ID to use for Redis key
	var keyID BrickID
	if b.MainID != nil {
		keyID = *b.MainID
	} else if len(b.IDs) > 0 {
		// No main ID: return the first non-empty (after trimming) ID in the list
		for _, id := range b.IDs {
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

// BuildPickabrickURL constructs the Pick-a-Brick URL for the Brick based on its ID and the given locale
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

// SetPrice sets the price for the given currency in the Brick's Prices map
func (b *Brick) SetPrice(price Price, currency language.Tag) {
	if b.Prices == nil {
		b.Prices = make(map[language.Tag]*Price)
	}
	b.Prices[currency] = &price
}

// HasValidPrice checks if the Brick has a valid and up-to-date price for the given locale tag
func (b *Brick) HasValidPrice(currency language.Tag) bool {
	return HasValidPrice(b.Prices, currency, database.DB().Redis().TTLS.BrickPrice)
}

// HasLowerPrice compares the price of the current Brick with another Brick for the given currency
// returning true if current has a valid price that is lower than the other, else false
func (b *Brick) HasLowerPrice(b2 Brick, currency language.Tag) bool {
	// First check if the current brick has a valid price for the currency
	if b.HasValidPrice(currency) {
		p1, _ := b.Prices.GetPrice(currency)
		if !b2.HasValidPrice(currency) {
			// If b2 doesn't have a valid price, we consider b1 as having a lower price
			return true
		}
		p2, _ := b2.Prices.GetPrice(currency)
		// Both have valid prices, compare them
		return p1.IsLower(p2.CentAmount)
	}
	return false
}

// MapBrickFromPickabrick maps a Pickabrick Brick to an internal Brick representation, updating price and other fields
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
