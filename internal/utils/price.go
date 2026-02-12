package utils

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
)

type Price struct {
	CentAmount   int    `json:"cent_amount"`
	CurrencyCode string `json:"currency_code"`
	FetchedAt    int64
	NotFound     bool
	ItemID       string
}

// IsZero checks if the price is zero or not found
func (p *Price) IsZero() bool {
	return p == nil || (p.CentAmount == 0 && p.CurrencyCode == "" && p.FetchedAt == 0 && !p.NotFound)
}

// IsNotFound checks if the price is found
func (p *Price) IsNotFound() bool {
	return p != nil && p.NotFound
}

// IsOutdated checks if the price is outdated based on the provided TTL duration
func (p *Price) IsOutdated(ttl time.Duration) bool {
	return time.Since(time.UnixMilli(p.FetchedAt)) > ttl
}

// IsValid checks if the price is valid and up-to-date based on the provided TTL duration
func (p *Price) IsValid(ttl time.Duration) bool {
	return !p.IsZero() && !p.IsOutdated(ttl)
}

// IsLower checks if the price is lower than the provided centAmount
func (p *Price) IsLower(centAmount int) bool {
	return p.CentAmount < centAmount
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
