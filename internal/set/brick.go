package set

import (
	"sort"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/google/uuid"
)

// Brick represents a Lego brick with all its core, inventory, and locale-specific information.
type Brick struct {
	brick.Inventory
	brick.Locale

	// ID is used for tracking and referencing purposes during the fetching process and while communicating with clients
	ID         uuid.UUID   `json:"id"`
	TotalPrice utils.Price `json:"total_price"`
}

// NewBrick creates a new Brick instance by combining the provided Inventory and Locale information,
// and calculates the total price based on the unit price and quantity.
func NewBrick(inventory brick.Inventory, locale brick.Locale) Brick {
	return NewBrickWithID(uuid.New(), inventory, locale)
}

// NewBrickWithID creates a new Brick instance with a specified ID by combining the provided Inventory and Locale information,
// and calculates the total price based on the unit price and quantity.
func NewBrickWithID(id uuid.UUID, inventory brick.Inventory, locale brick.Locale) Brick {
	b := Brick{
		ID:        id,
		Inventory: inventory,
		Locale:    locale,
	}
	b.CalculateTotalPrice()
	return b
}

// CalculateTotalPrice calculates the total price based on unit price and quantity
func (b *Brick) CalculateTotalPrice() {
	b.TotalPrice = utils.Price{
		CentAmount:   b.Price.CentAmount * b.Quantity,
		CurrencyCode: b.Price.CurrencyCode,
		FetchedAt:    b.Price.FetchedAt,
		ItemID:       b.Price.ItemID,
	}
}

// ResetDownToInventoryCore takes a slice of Bricks and returns a new slice where each Brick's Locale information is reset down to its Core information, effectively downgrading the Bricks to InventoryCores while keeping the inventory data intact.
func (b *Brick) ResetDownToInventoryCore() {
	// Maintain inventory data + core data + ID

	// Downgrade the total price since it's irrelevant when cached, it will be recalculated anyway upon load
	b.TotalPrice = utils.Price{}

	// Locale down to core
	locale := &b.Locale
	locale.ResetDownToCore()
}

// SortBricksByIndex sorts a slice of Bricks in-place based on their Index field in ascending order.
func SortBricksByIndex(bricks []Brick) {
	sort.Slice(bricks, func(i, j int) bool {
		return bricks[i].Index < bricks[j].Index
	})
}
