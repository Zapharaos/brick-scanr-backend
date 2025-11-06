package set

import (
	"strconv"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
)

type BrickID string
type DesignID string

type Brick struct {
	MainID   *BrickID  `json:"main_id"`
	IDs      []BrickID `json:"ids"`
	DesignID DesignID  `json:"design_id"`
	Name     string    `json:"name"`
	ImageURL string    `json:"image_url"`
	Color    string    `json:"color"`
	ColorHex string    `json:"color_hex"`
	Quantity int       `json:"quantity"`
	Price    Price     `json:"price"`
}

func MapBrickFromBricklinkInventoryItem(bi bricklink.InventoryItem) Brick {
	// Map to internal brick representation
	qty := 0
	if bi.Quantity != "" {
		if q, err := strconv.Atoi(bi.Quantity); err == nil {
			qty = q
		}
	}

	// Map ItemIDs to BrickIDs
	var ids []BrickID
	ids = make([]BrickID, len(bi.ItemIDs))
	for i, id := range bi.ItemIDs {
		ids[i] = BrickID(id)
	}

	// If there's a unique ItemID, mark it as the main ID
	var mainID *BrickID
	if bi.HasUniqueItemID() {
		mainID = &ids[0]
	}

	return Brick{
		MainID:   mainID,
		IDs:      ids,
		DesignID: DesignID(bi.ItemNo),
		Name:     bi.Description,
		ImageURL: bi.ImageURL,
		Color:    bi.Color,
		Quantity: qty,
	}
}

type BrickMap struct {
	BricksByDesign map[DesignID][]*Brick
	BricksByID     map[BrickID]*Brick
	mutex          sync.RWMutex
	_set           *Set
}

// NewBrickMap creates a new brick map from the set's bricks slice
func (s *Set) NewBrickMap() BrickMap {
	bbd := make(map[DesignID][]*Brick)
	bbi := make(map[BrickID]*Brick)

	// Populate with pointers to bricks in the slice
	for i := range s.Bricks {
		brick := &s.Bricks[i] // Get pointer to the actual brick in the slice
		// Design: create slices if it doesn't exist
		if _, exists := bbd[brick.DesignID]; !exists {
			bbd[brick.DesignID] = make([]*Brick, 0)
		}
		// Design: append brick to designID slice
		bbd[brick.DesignID] = append(bbd[brick.DesignID], brick)

		// ID: map brickID to brick
		for _, brickID := range brick.IDs {
			if _, exists := bbi[brickID]; !exists {
				bbi[brickID] = brick
			}
		}
	}

	return BrickMap{
		BricksByDesign: bbd,
		BricksByID:     bbi,
		_set:           s,
	}
}

// GetBricksByDesign safely retrieves bricks by designID from the map
// Returns the brick pointer and a boolean indicating if it was found
func (bm *BrickMap) GetBricksByDesign(id DesignID) ([]*Brick, bool) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	bricks, ok := bm.BricksByDesign[id]
	return bricks, ok
}

// GetBrickByID safely retrieves a brick by ID from the map
// Returns the brick pointer and a boolean indicating if it was found
func (bm *BrickMap) GetBrickByID(id BrickID) (*Brick, bool) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	brick, ok := bm.BricksByID[id]
	return brick, ok
}
