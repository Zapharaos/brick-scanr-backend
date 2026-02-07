package set

import "sync"

type BrickMap struct {
	BricksByDesign map[DesignID][]Brick
	BricksByID     map[BrickID]Brick
	mutex          sync.RWMutex
	_set           *Set
}

// NewBrickMap creates a new brick map from the set's bricks slice
func NewBrickMap(bricks []Brick) BrickMap {
	bbd := make(map[DesignID][]Brick)
	bbi := make(map[BrickID]Brick)

	// Populate with pointers to bricks in the slice
	for i := range bricks {
		brick := bricks[i] // Get pointer to the actual brick in the slice
		// Design: create slices if it doesn't exist
		if _, exists := bbd[brick.DesignID]; !exists {
			bbd[brick.DesignID] = make([]Brick, 0)
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
	}
}

// GetBricksByDesign safely retrieves bricks by designID from the map
// Returns the brick pointer and a boolean indicating if it was found
func (bm *BrickMap) GetBricksByDesign(id DesignID) ([]Brick, bool) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	bricks, ok := bm.BricksByDesign[id]
	return bricks, ok
}

// GetBrickByID safely retrieves a brick by ID from the map
// Returns the brick pointer and a boolean indicating if it was found
func (bm *BrickMap) GetBrickByID(id BrickID) (Brick, bool) {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	brick, ok := bm.BricksByID[id]
	return brick, ok
}
