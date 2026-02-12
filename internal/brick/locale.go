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

// IsEmptyExceptCore checks if all fields of the Locale are empty or zero values except for the Core information
func (l *Locale) IsEmptyExceptCore() bool {
	return !l.HasValidPrice() &&
		l.Status == utils.StatusUnknown &&
		l.PickabrickURL == "" &&
		l.Color.IsEmpty()
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
	if l.ElementID != nil {
		id = string(*l.ElementID)
	} else if len(l.ElementIDs) > 0 {
		id = string(l.ElementIDs[0])
	} else {
		id = string(l.DesignID)
	}
	l.PickabrickURL = "https://www.lego.com/" + tag.String() + "/pick-and-build/pick-a-brick?selectedElement=" + id
}

// HasValidPrice checks if the Brick has a valid and up-to-date
func (l *Locale) HasValidPrice() bool {
	return l.Price.IsValid(database.DB().Redis().TTLS.BrickPrice)
}

// HasOutdatedPrice checks if the Brick has a price that is outdated based on the configured TTL for Brick prices
func (l *Locale) HasOutdatedPrice() bool {
	return l.Price.IsOutdated(database.DB().Redis().TTLS.BrickPrice)
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
func (l *Locale) LoadFromRedis(ctx context.Context, id ElementID, tag language.Tag) (locale Locale, valid, notFound bool) {
	// Check cache first for this specific brick ID
	if bCache, err := RedisGet(ctx, id, tag); err == nil {

		// Check if it has a valid cached not-found entry
		if bCache.Price.IsNotFound() && !bCache.HasOutdatedPrice() {
			notFound = true
		}

		// Check if this brick ID has a valid price that is lower than the current price (if any)
		if bCache.HasValidPrice() && bCache.HasLowerPrice(*l) {
			valid = true
		}

		// If applicable, update fields with data from cache
		if valid || notFound {
			locale = bCache
			return
		}
	}
	locale = *l
	return
}
