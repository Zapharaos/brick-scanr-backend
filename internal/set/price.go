package set

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"golang.org/x/text/language"
)

type PricePerCurrencies map[language.Tag]*Price

func (ppc PricePerCurrencies) HasCurrency(tag language.Tag) bool {
	_, exists := ppc[tag]
	return exists
}

func (ppc PricePerCurrencies) GetPrice(tag language.Tag) (*Price, bool) {
	price, exists := ppc[tag]
	return price, exists
}

type Price struct {
	CentAmount int    `json:"cent_amount"`
	Currency   string `json:"currency"`
	ItemID     string `json:"item_id"`
	FetchedAt  int64  `json:"fetched_at"`
}

// IsValid checks if the price is valid
func (p *Price) IsValid() bool {
	return p != nil &&
		p.CentAmount > 0 &&
		p.Currency != ""
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
func HasValidPrice(prices PricePerCurrencies, tag language.Tag, ttl time.Duration) bool {
	price, ok := prices.GetPrice(tag)
	return ok && price.IsValid() && !price.IsOutdated(ttl)
}

// MapPriceFromPickabrick maps a pickabrick.Price to internal Price representation
func MapPriceFromPickabrick(price pickabrick.Price) Price {
	return Price{
		CentAmount: price.CentAmount,
		Currency:   price.CurrencyCode,
	}
}

// MapPriceFromLego maps a lego.Price to internal Price representation
func MapPriceFromLego(price lego.Price) Price {
	return Price{
		CentAmount: price.CentAmount,
		Currency:   price.CurrencyCode,
	}
}
