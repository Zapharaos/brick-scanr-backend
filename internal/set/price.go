package set

import "github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"

type Price struct {
	CentAmount int    `json:"cent_amount"`
	Currency   string `json:"currency"`
	ItemID     string `json:"item_id"`
}

// MapPriceFromPickabrick maps a pickabrick.Price to internal Price representation
func MapPriceFromPickabrick(price pickabrick.Price) Price {
	return Price{
		CentAmount: price.CentAmount,
		Currency:   price.CurrencyCode,
	}
}
