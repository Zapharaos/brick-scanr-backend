package brick

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"golang.org/x/text/language"
)

// MapLocaleFromPickabrick maps a pickabrick.Brick to an internal Locale representation
func MapLocaleFromPickabrick(brick Locale, pab pickabrick.Brick, tag language.Tag) Locale {
	// Update Core
	elementID := ElementID(pab.ID)
	brick.ElementID = &elementID
	brick.DesignID = DesignID(pab.DesignID)
	brick.Name = pab.Name
	brick.ImageURL = pab.ImageUrl

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
