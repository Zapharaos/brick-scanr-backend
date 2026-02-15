package utils

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
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
	return p == nil || (p.CentAmount == 0 && !p.NotFound)
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

// Formatted returns a formatted string representation of the price
func (p *Price) Formatted(tag language.Tag) string {
	// If the price is zero or not found, return a placeholder (e.g., "-")
	if p.IsZero() || p.IsNotFound() {
		return "-"
	}

	// Default if no tag provided
	if tag == (language.Tag{}) {
		tag = GetLocale()
	}

	// Parse the currency unit from the currency code (e.g., "EUR", "USD")
	unit, err := currency.ParseISO(p.CurrencyCode)
	if err != nil {
		// If currency code is invalid, return a simple formatted string
		return p.CurrencyCode + " " + formatCentAmount(p.CentAmount, tag)
	}

	// Create a printer for the specified language tag
	printer := message.NewPrinter(tag)

	// Convert cent amount to the actual currency amount (divide by 100)
	amount := float64(p.CentAmount) / 100.0

	// Format the currency using locale-aware formatting
	// This automatically handles symbol position (e.g., "$50.00" for en-US, "50,00 €" for fr-FR)
	return printer.Sprint(currency.Symbol(unit.Amount(amount)))
}

// formatCentAmount is a helper function to format cent amounts as decimal strings
func formatCentAmount(centAmount int, tag language.Tag) string {
	// Default if no tag provided
	if tag == (language.Tag{}) {
		tag = GetLocale()
	}

	dollars := centAmount / 100
	cents := centAmount % 100
	if cents < 10 {
		return message.NewPrinter(tag).Sprintf("%d.0%d", dollars, cents)
	}
	return message.NewPrinter(tag).Sprintf("%d.%d", dollars, cents)
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
