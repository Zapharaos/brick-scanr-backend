package brick

import (
	"context"
	"errors"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// MapLocaleFromPickabrick maps a pickabrick.Brick to an internal Locale representation
func MapLocaleFromPickabrick(brick Locale, pab pickabrick.Brick, tag language.Tag) Locale {
	// Update Core
	id := ID{
		ElementID: ElementID(pab.ID),
		DesignID:  DesignID(pab.DesignID),
	}
	brick.ID = &id
	brick.Name = pab.Name
	// Use GetValidatedImageURL which automatically validates CDN PNG and falls back to API URL if needed
	brick.ImageURL = pab.GetValidatedImageURL(pickabrick.C())

	// Prepare fetched price
	pbp := utils.MapPriceFromPickabrick(pab.Price)
	pbp.ItemID = pab.ID
	pbp.FetchedAt = time.Now().UnixMilli()

	// Update Locale
	brick.Price = pbp
	brick.Status = utils.MapPickabrickStatus(pab.Availability)
	brick.BuildPickabrickURL(tag)
	brick.Color = MapColorFromPickabrick(pab)

	return brick
}

// Fetch attempts to fetch price and availability data for the given elementID and locale from pick-a-brick API.
// It returns three values:
// - A boolean indicating whether the fetch operation was successful
// - A boolean indicating whether a valid locale with price data was found
// - A pointer to a Locale struct containing a potential locale if not found on pick-a-brick, or nil if not applicable
func (l *Locale) Fetch(ctx context.Context, elementID ElementID, locale language.Tag) (bool, bool, *Locale) {
	results, err := pickabrick.C().FetchBricksByBrickID(string(elementID), locale)
	if err != nil {
		// Check if it's a not-found error
		if !errors.Is(err, pickabrick.ErrBrickNotFound) {
			// Other error - log and try next ID
			zap.L().Warn("Failed to fetch brick by elementID",
				zap.Error(err),
				zap.String("elementID", string(elementID)),
				zap.String("locale", locale.String()))

			// Return as unsuccessful fetch, with empty data
			return false, false, nil
		}

		zap.L().Debug("ElementID not found in pick-a-brick API",
			zap.String("elementID", string(elementID)),
			zap.String("locale", locale.String()),
		)

		// Create a completely independent brick for caching not-found status
		// We must not modify the original brick that will be used in the set
		bLocaleNotFound := l.Copy()
		bLocaleNotFound.Price = utils.Price{
			CurrencyCode: locale.String(),
			FetchedAt:    time.Now().UnixMilli(),
			NotFound:     true,
			ItemID:       string(elementID),
		}
		bLocaleNotFound.ID.ElementID = elementID

		// Cache this brick ID as not-found (independent entry, won't affect the set's brick)
		if cacheErr := RedisSetLocale(ctx, bLocaleNotFound, locale, true); cacheErr != nil {
			zap.L().Warn("Failed to cache brick ID with not-found price",
				zap.Error(cacheErr),
				zap.String("elementID", string(elementID)),
				zap.String("locale", locale.String()),
			)
		}

		// Return as successful fetch, data has not-found price status
		return true, false, &bLocaleNotFound
	}

	var validLocale bool

	// There should be only one matching brick per ID
	// API client returns a slice so to be safe we will loop through the results just in case
	for _, pab := range results {
		// Just in case, check if the returned brick ID matches the requested one
		if ElementID(pab.ID) == elementID {

			// Map result to local representation
			mappedB := MapLocaleFromPickabrick(*l, pab, locale)

			// Check if current price currency is already set, valid and lower
			if l.HasLowerPrice(mappedB) {
				continue
			}

			// Found a new valid and lower price, update brick with fetched data from pickabrick
			l.Core = mappedB.Core
			l.Price = mappedB.Price
			l.Status = mappedB.Status
			l.PickabrickURL = mappedB.PickabrickURL
			l.Color = mappedB.Color

			// Cache the updated brick with new price data
			if cacheErr := RedisSetLocale(ctx, *l, locale, true); cacheErr != nil {
				zap.L().Warn("Failed to cache brick with new price",
					zap.Error(cacheErr),
					zap.String("element_id", string(elementID)),
					zap.String("locale", locale.String()),
				)
				// Not fatal, continue without caching
			}

			// Mark that we found a locale brick with valid price
			validLocale = true
		}
	}

	return true, validLocale, nil
}
