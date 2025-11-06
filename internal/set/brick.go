package set

import (
	"strconv"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
)

type BrickID string

type Brick struct {
	MainID   *BrickID  `json:"main_id"`
	IDs      []BrickID `json:"ids"`
	DesignID string    `json:"design_id"`
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
		DesignID: bi.ItemNo,
		Name:     bi.Description,
		ImageURL: bi.ImageURL,
		Color:    bi.Color,
		Quantity: qty,
	}
}

type BrickMap struct {
	bricks map[string]*Brick
	mutex  sync.RWMutex
	_set   *Set
}

// TODO : search by designID returns for all elementIDS => map[designID][]elementID
// 		internal processing: get matching elementIDs in the set bricks
//		if brick already have price, if price lower then update price + main property

/*// NewBrickMap creates a new brick map from the set's bricks slice
func (s *Set) NewBrickMap() BrickMap {
	brickMap := make(map[string]*Brick, len(s.Bricks))

	// Populate map with pointers to bricks in the slice
	for i := range s.Bricks {
		brickMap[s.Bricks[i].ID] = &s.Bricks[i]
	}

	return BrickMap{
		bricks: brickMap,
		_set:   s,
	}
}

// GetBrick safely retrieves a brick by ID from the map
// Returns the brick pointer and a boolean indicating if it was found
func (bm *BrickMap) GetBrick(brickID string) (*Brick, bool) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	brick, ok := bm.bricks[brickID]
	return brick, ok
}

// UpdateBrick safely updates a brick in both the slice and map
// Returns true if the brick was found and updated, false otherwise
func (bm *BrickMap) UpdateBrick(brickID string, updateFn func(*Brick)) bool {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	brick, ok := bm.bricks[brickID]
	if !ok {
		return false
	}

	// Apply the update function
	updateFn(brick)
	return true
}

// SetBrick safely sets a brick in both the slice and map
// Returns true if the brick was found and updated, false otherwise
func (bm *BrickMap) SetBrick(brickID string, brick Brick) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	_, ok := bm.bricks[brickID]
	if !ok {
		bm.AddBrick(brick)
		return
	}

	// Apply the update function
	bm.bricks[brickID] = &brick
	return
}

// AddBrick adds a new brick to both the slice and map
func (bm *BrickMap) AddBrick(brick Brick) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	// Add to slice
	bm._set.Bricks = append(bm._set.Bricks, brick)

	// Add pointer to map (pointing to the last element in the slice)
	bm.bricks[brick.ID] = &bm._set.Bricks[len(bm._set.Bricks)-1]
}

// RemoveBrick removes a brick from both the slice and map
// Returns true if the brick was found and removed, false otherwise
func (bm *BrickMap) RemoveBrick(brickID string) bool {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	// Check if brick exists in map
	_, ok := bm.bricks[brickID]
	if !ok {
		return false
	}

	// Remove from slice
	for i, brick := range bm._set.Bricks {
		if brick.ID == brickID {
			// Remove element from slice
			bm._set.Bricks = append(bm._set.Bricks[:i], bm._set.Bricks[i+1:]...)
			break
		}
	}

	// Remove from map
	delete(bm.bricks, brickID)

	// Rebuild map to ensure all pointers are still valid after slice modification
	bm.rebuildBrickMapUnsafe()

	return true
}

// rebuildBrickMapUnsafe rebuilds the brick map without acquiring the lock
// Should only be called when the lock is already held
func (bm *BrickMap) rebuildBrickMapUnsafe() {
	bm.bricks = make(map[string]*Brick, len(bm._set.Bricks))
	for i := range bm._set.Bricks {
		bm.bricks[bm._set.Bricks[i].ID] = &bm._set.Bricks[i]
	}
}
*/
