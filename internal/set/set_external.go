package set

import "time"

// DetailsResponse represents the response for a set details request
type DetailsResponse struct {
	// WebsocketID is the WebSocket UUID to connect to for updates
	WebsocketID string `json:"websocket_id"`
	// Completed indicates if the job is already done
	Completed bool `json:"completed"`
	// Set contains the data if already completed, otherwise nil
	Set SetExternal `json:"set,omitempty"`
}

// SetExternal represents the external view of a Set, with only the fields relevant for the client response
type SetExternal struct {
	Set

	XLocale      string `json:"xlocale"`
	TotalPrice   Price  `json:"total_price"`
	MissingParts int    `json:"missing_parts"`
}

// ApplyTotalPrice sets the total price with the specified cent amount and currency symbol, and updates the fetched timestamp to now
// It does not calculate the total price from bricks, use CalculateBricksTotalPrices for that.
func (s *SetExternal) ApplyTotalPrice(centAmount int, currencySymbol string) {
	s.TotalPrice = Price{
		CentAmount:   centAmount,
		CurrencyCode: currencySymbol,
		FetchedAt:    time.Now().UnixMilli(),
	}
}
