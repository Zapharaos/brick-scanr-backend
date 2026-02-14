package set

import (
	"fmt"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/google/uuid"
)

// NewCoreFromBricklinkSearchItem maps a Bricklink search item to an internal Core representation
func NewCoreFromBricklinkSearchItem(bs bricklink.SearchItem) (Core, error) {
	// Assign a local UUID to each set
	setId, err := uuid.NewUUID()
	if err != nil {
		return Core{}, err
	}

	// Map to internal set representation
	return Core{
		ID:              setId,
		BricklinkName:   bs.StrItemName,
		BricklinkID:     bs.IDItem,
		BricklinkNumber: bs.StrItemNo,
	}, nil
}

// RedisBuildKeyBricklinkIDToSetID creates a Redis key for looking up Set by Bricklink ID
func RedisBuildKeyBricklinkIDToSetID(bricklinkID string) string {
	return fmt.Sprintf("set:bricklink:%s", bricklinkID)
}
