package brick

import (
	"context"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"golang.org/x/text/language"
)

// Locale represents the locale-specific information of a brick Core.
type Locale struct {
	Core

	Price         utils.Price  `json:"price"`
	Status        utils.Status `json:"status"`
	PickabrickURL string       `json:"pickabrick_url"`
	Color         Color        `json:"color"`
}

// Copy creates a copy of the Locale struct, including the embedded Core struct.
func (l *Locale) Copy() Locale {
	return Locale{
		Core:          l.Core.Copy(),
		Price:         l.Price,
		Status:        l.Status,
		PickabrickURL: l.PickabrickURL,
		Color:         l.Color,
	}
}

// ResetDownToCore resets the Locale fields to their zero values, keeping only the Core information intact
func (l *Locale) ResetDownToCore() {
	l.Price = utils.Price{}
	l.Status = utils.StatusUnknown
	l.PickabrickURL = ""
	l.Color = Color{}
}

// BuildPickabrickURL constructs the Pick-a-Brick URL for the Brick based on its ID and the given tag
func (l *Locale) BuildPickabrickURL(tag language.Tag) {
	var id string
	if l.ID != nil {
		id = string(l.ID.ElementID)
	} else if len(l.IDs) > 0 {
		id = string(l.IDs[0].ElementID)
	}
	l.PickabrickURL = "https://www.lego.com/" + tag.String() + "/pick-and-build/pick-a-brick?selectedElement=" + id
}

// HasValidPrice checks if the brick price is within the freshness window
func (l *Locale) HasValidPrice() bool {
	return l.Price.IsValid(database.DB().Redis().TTLS.BrickPriceFreshness)
}

// HasOutdatedPrice checks if the brick price has exceeded the freshness window
func (l *Locale) HasOutdatedPrice() bool {
	return l.Price.IsOutdated(database.DB().Redis().TTLS.BrickPriceFreshness)
}

// HasLowerPrice compares the price of the current Brick with another Brick for the given tag
// returning true if current has a valid price that is lower than the other, else false
func (l *Locale) HasLowerPrice(l2 Locale) bool {
	// First check if the current brick has a valid price
	if l.HasValidPrice() {
		if !l2.HasValidPrice() {
			// If l2 doesn't have a valid price, we consider l as having a lower price
			return true
		}
		// Both have valid prices, compare them
		return l.Price.IsLower(l2.Price.CentAmount)
	}
	return false
}

// LoadFromRedis attempts to update the Locale with data from the cache for the given ElementID and language tag.
func (l *Locale) LoadFromRedis(ctx context.Context, id ElementID, tag language.Tag, allowOutdated, mustLower bool) (Locale, bool, bool) {
	// Check cache first for this specific brick ID
	if bCache, err := RedisGetLocale(ctx, id, tag); err == nil {

		// Check if it has a valid cached not-found entry
		if bCache.Price.IsNotFound() {

			// Price is not outdated, mark as not-found valid entry
			if !bCache.HasOutdatedPrice() {
				return bCache, false, true
			}

			// Price is outdated, but we allow outdated entries, mark as not-found valid entry
			if allowOutdated {
				return bCache, false, true
			}

			// Price is outdated and we don't allow outdated entries
		}

		// We allow outdated entries, the price is valid (i.e. not zero)
		// OR
		// We don't allow outdated entries, the price is valid (i.e. not zero and not outdated)
		if (allowOutdated && !bCache.Price.IsZero()) ||
			(!allowOutdated && bCache.HasValidPrice()) {
			// We don't require the cached price to be lower, mark as valid entry
			if !mustLower {
				return bCache, true, false
			}

			// The cached price is lower than the current price, mark as valid entry
			if bCache.HasLowerPrice(*l) {
				return bCache, true, false
			}
		}
	}
	return *l, false, false
}
