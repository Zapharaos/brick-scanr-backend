package set

import (
	"context"
	"errors"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchDetails fetches set details from Bricklink and LEGO, updating the set
func FetchDetails(ctx context.Context, setID uuid.UUID, set *Locale, lang language.Tag, xlocale language.Tag) (bool, error) {
	// Fetch Bricklink details
	err := FetchBricklinkDetails(ctx, &set.Core, lang)
	if err != nil {
		return false, err
	}

	// Fetch LEGO product details
	priceFetched, err := FetchLegoProductDetails(ctx, setID, set, lang, xlocale, false)
	if err != nil {
		return false, err
	}

	return priceFetched, nil
}

// FetchBricklinkDetails fetches set details from Bricklink and updates the set
func FetchBricklinkDetails(ctx context.Context, set *Core, lang language.Tag) error {
	bricklinkSet, err := bricklink.C().FetchSetDetails(set.BricklinkID, lang)
	if err != nil {
		return err
	}

	// Update set with fetched details
	set.Number = bricklinkSet.StrItemNo
	set.YearReleased = bricklinkSet.NYearReleased
	set.Parts = bricklinkSet.NInvPartCnt
	set.ImageURL = bricklinkSet.ImageList.GetMainImageURL()
	set.GenerateSlugDefault()

	// Update in cache
	err = RedisSetCore(ctx, *set, true)
	if err != nil {
		return err
	}

	return nil
}

// FetchLegoProductDetails fetches product details from LEGO and updates the set
func FetchLegoProductDetails(ctx context.Context, setID uuid.UUID, set *Locale, lang language.Tag, xlocale language.Tag, priceOnly bool) (bool, error) {
	// Fetch product details from LEGO
	legoProduct, err := lego.C().FetchProductDetails(set.Number, lang, xlocale)
	if err != nil {
		// Check if it's a not-found error
		if errors.Is(err, lego.ErrProductNotFound) {
			// Cache the not-found status to avoid repeated API calls
			set.Price = utils.Price{
				NotFound:     true,
				CurrencyCode: xlocale.String(),
				FetchedAt:    time.Now().UnixMilli(),
			}

			// Update in cache
			err = RedisSetLocale(ctx, *set, xlocale, false, true)
			if err != nil {
				return false, err
			}

			zap.L().Info("Cached not-found price for LEGO set",
				zap.String("set_number", set.Number),
				zap.String("set_id", setID.String()),
				zap.String("currency", xlocale.String()),
			)
			return false, nil
		}

		// Non-fatal: LEGO data is supplementary, log warning and continue
		// This can happen for older or discontinued sets not present in LEGO's API, or for specific locales etc.
		zap.L().Warn("Failed to fetch product details from LEGO",
			zap.Error(err),
			zap.String("set_number", set.Number),
			zap.String("set_id", setID.String()),
		)
		return false, nil
	}

	if !priceOnly {
		set.ImageURL = legoProduct.BaseImgUrl
		set.Status = utils.MapLegoProductStatus(*legoProduct)
		set.Name = legoProduct.Name
		set.Slug = legoProduct.Slug
		set.BuildLegoURL(lang)
		set.BuildInstructionsURL(lang)
	}

	// Update set with fetched price
	lp := utils.MapPriceFromLego(legoProduct.Variant.Price)
	lp.FetchedAt = time.Now().UnixMilli()
	set.Price = lp
	zap.L().Info("LEGO product price fetched",
		zap.String("set_number", set.Number),
		zap.String("set_id", setID.String()),
		zap.String("xlocale", xlocale.String()),
		zap.Int("price", lp.CentAmount))

	// Update in cache, along with core if priceOnly is false because we might update the imageURL
	err = RedisSetLocale(ctx, *set, xlocale, !priceOnly, true)
	if err != nil {
		return false, err
	}

	return true, nil
}
