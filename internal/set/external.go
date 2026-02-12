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

// CopyWithoutBricks creates a copy of the External struct without the Bricks slice, useful for sending data without brick details to the client
func (e *External) CopyWithoutBricks() External {
	cp := *e
	cp.Bricks = nil
	return cp
}

// IncrementMissingParts increments the MissingParts count while staying within the bounds
func (e *External) IncrementMissingParts() {
	e.MissingParts++
}

// AddFinalBrickData adds the price of a final brick to the total price of the set and updates the missing parts count
func (e *External) AddFinalBrickData(b Brick) {
	// Only add brick data if it has a valid price
	if !b.HasValidPrice() {
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

// ApplyTotalPrice sets the total price with the specified cent amount and currency symbol, and updates the fetched timestamp to now
// It does not calculate the total price from bricks, use CalculateBricksTotalPrices for that.
func (e *External) ApplyTotalPrice(centAmount int, currencySymbol string) {
	e.TotalPrice = utils.Price{
		CentAmount:   centAmount,
		CurrencyCode: currencySymbol,
		FetchedAt:    time.Now().UnixMilli(),
	}
}

// CalculateBricksTotalPrices calculates and applies the total price for each brick
// Returns the total sum and how many missing brick prices
func (e *External) CalculateBricksTotalPrices() (int, int) {
	countMissingBrickPrices := 0
	sumTotalPriceCentAmount := 0

	// Process each brick
	for _, brick := range e.Bricks {

		// Brick reference is missing price
		if brick.Price.CentAmount == 0 {
			countMissingBrickPrices++
			continue
		}

		// Calculate total price for the brick
		brick.TotalPrice = brick.Price
		brick.TotalPrice.CentAmount = brick.Price.CentAmount * brick.Quantity
		sumTotalPriceCentAmount += brick.TotalPrice.CentAmount
	}

	return sumTotalPriceCentAmount, countMissingBrickPrices
}
