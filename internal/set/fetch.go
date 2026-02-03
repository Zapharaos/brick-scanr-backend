package set

import (
	"context"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchDetails fetches set details from Bricklink and LEGO, updating the set
func FetchDetails(ctx context.Context, setID uuid.UUID, set *Set, locale language.Tag, currency language.Tag) (bool, error) {
	// Fetch Bricklink details
	err := FetchBricklinkDetais(ctx, set)
	if err != nil {
		return false, err
	}

	// Fetch LEGO product details
	priceFetched, err := FetchLegoProductDetails(ctx, setID, set, locale, currency, false)
	if err != nil {
		return false, err
	}

	return priceFetched, nil
}

// FetchBricklinkDetais fetches set details from Bricklink and updates the set
func FetchBricklinkDetais(ctx context.Context, set *Set) error {
	bricklinkSet, err := bricklink.C().FetchSetDetails(set.BricklinkID)
	if err != nil {
		return err
	}

	// Update set with fetched details
	set.Number = bricklinkSet.StrItemNo
	set.YearReleased = bricklinkSet.NYearReleased
	set.Parts = bricklinkSet.NInvPartCnt
	set.ImageURL = bricklinkSet.ImageList.GetMainImageURL()
	set.GenerateSlug()

	// Update in cache
	err = SetRedisSet(ctx, *set, true)
	if err != nil {
		return err
	}

	return nil
}

// FetchLegoProductDetails fetches product details from LEGO and updates the set
func FetchLegoProductDetails(ctx context.Context, setID uuid.UUID, set *Set, locale language.Tag, currency language.Tag, priceOnly bool) (bool, error) {
	// Fetch product details from LEGO
	legoProduct, err := lego.C().FetchProductDetails(set.Number, currency)
	if err != nil {
		// Non-fatal: LEGO data is supplementary, log warning and continue
		// This can happen for older or discontinued sets not present in LEGO's API, or for specific locales etc.
		zap.L().Warn("Failed to fetch product details from LEGO",
			zap.Error(err),
			zap.String("set_number", set.Number),
			zap.String("set_id", setID.String()),
		)
	} else {

		if !priceOnly {
			set.Slug = legoProduct.Slug
			set.BuildLegoURL(locale)
			set.BuildInstructionsURL(locale)
			set.Status = MapLegoProductStatus(*legoProduct)
		}

		// Update set with fetched price
		lp := MapPriceFromLego(legoProduct.Variant.Price)
		lp.FetchedAt = time.Now().UnixMilli()
		if set.Prices == nil {
			set.Prices = make(map[language.Tag]*Price)
		}
		set.Prices[currency] = &lp
		set.MustApplyCurrency(currency)

		// Update in cache
		err = SetRedisSet(ctx, *set, true)
		if err != nil {
			return false, err
		}

		return true, nil
	}
	return false, nil
}
