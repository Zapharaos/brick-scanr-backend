package set

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
)

// DetailsResponse represents the response for a set details request
type DetailsResponse struct {
	// WebsocketID is the WebSocket UUID to connect to for updates
	WebsocketID string `json:"websocket_id"`
	// Completed indicates if the job is already done
	Completed bool `json:"completed"`
	// Set contains the data if already completed, otherwise nil
	Set External `json:"set,omitempty"`
}

// External represents the external view of a Set, with fields relevant for the client response
type External struct {
	Locale

	MissingParts int         `json:"missing_parts"`
	TotalPrice   utils.Price `json:"total_price"`
}

// IncrementMissingParts increments the MissingParts count while staying within the bounds
func (e *External) IncrementMissingParts() {
	e.MissingParts++
}

// AddFinalBrickData adds the price of a final brick to the total price of the set and updates the missing parts count
func (e *External) AddFinalBrickData(b Brick) {
	// Only add brick data if it has a valid price
	if !b.HasValidPrice() || b.Price.IsNotFound() {
		return
	}

	// Only update if all bricks haven't been found yet
	if e.MissingParts == 0 {
		return
	}

	// If total price is zero, initialize it with the currency code from the brick price
	if e.TotalPrice.IsZero() {
		e.TotalPrice = utils.Price{
			CurrencyCode: b.Price.CurrencyCode,
		}
	}

	// Add the brick's total price to the set's total price
	e.TotalPrice.CentAmount += b.TotalPrice.CentAmount
	e.TotalPrice.FetchedAt = time.Now().UnixMilli()

	// Decrement missing parts count
	e.MissingParts--
}
