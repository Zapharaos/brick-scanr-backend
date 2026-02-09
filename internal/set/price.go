package set

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"golang.org/x/text/language"
)

type PricePerTag map[language.Tag]*Price

func (ppc PricePerTag) HasTag(tag language.Tag) bool {
	_, exists := ppc[tag]
	return exists
}

func (ppc PricePerTag) GetPrice(tag language.Tag) (*Price, bool) {
	price, exists := ppc[tag]
	return price, exists
}

type Price struct {
	CentAmount   int    `json:"cent_amount"`
	CurrencyCode string `json:"currency_code"`
	ItemID       string `json:"item_id"`
	FetchedAt    int64  `json:"fetched_at"`
	NotFound     bool   `json:"not_found"`
}

// IsValid checks if the price is valid
func (p *Price) IsValid() bool {
	return p != nil &&
		!p.NotFound &&
		p.CentAmount > 0 &&
		p.CurrencyCode != ""
}

// IsOutdated checks if the price is outdated based on the provided TTL duration
func (p *Price) IsOutdated(ttl time.Duration) bool {
	return time.Since(time.UnixMilli(p.FetchedAt)) > ttl
}

// IsLower checks if the price is lower than the provided centAmount
func (p *Price) IsLower(centAmount int) bool {
	return p.CentAmount < centAmount
}

// HasValidPrice checks if the Brick has a valid and up-to-date price for the given locale tag
// Returns false if price is not found, outdated, or invalid
func HasValidPrice(prices PricePerTag, tag language.Tag, ttl time.Duration) bool {
	price, ok := prices.GetPrice(tag)
	return ok && price.IsValid() && !price.IsOutdated(ttl)
}

// HasCachedNotFound checks if the item has a cached "not found" entry for the given tag
// This helps avoid repeated API calls for items that don't exist
func HasCachedNotFound(prices PricePerTag, tag language.Tag, ttl time.Duration) bool {
	price, ok := prices.GetPrice(tag)
	return ok && price != nil && price.NotFound && !price.IsOutdated(ttl)
}

// MapPriceFromPickabrick maps a pickabrick.Price to internal Price representation
func MapPriceFromPickabrick(price pickabrick.Price) Price {
	return Price{
		CentAmount:   price.CentAmount,
		CurrencyCode: price.CurrencyCode,
	}
}

// MapPriceFromLego maps a lego.Price to internal Price representation
func MapPriceFromLego(price lego.Price) Price {
	return Price{
		CentAmount:   price.CentAmount,
		CurrencyCode: price.CurrencyCode,
	}
}
