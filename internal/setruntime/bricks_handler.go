package setruntime

import (
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/google/uuid"
)

type BricksHandler struct {
	// Represents the final bricks that have been fully fetched and processed, ready to be sent to clients
	// Using a slice because we don't need to access them
	final  []set.Brick
	fMutex sync.RWMutex

	// Represents the missing bricks that are still being fetched, or were not found
	// Stored in a map for efficient access and updates by ID
	missing map[uuid.UUID]set.Brick
	mMutex  sync.RWMutex
}

// newBricksHandler creates a new BricksHandler with initialized slices and mutexes
func newBricksHandler() BricksHandler {
	return BricksHandler{
		final:   []set.Brick{},
		fMutex:  sync.RWMutex{},
		missing: make(map[uuid.UUID]set.Brick),
		mMutex:  sync.RWMutex{},
	}
}

// get returns all Bricks currently stored in the runtime, sorted by index
func (bh *BricksHandler) get() []set.Brick {
	// Making a copy of the final slice to avoid potential issues with the response usages
	// Encountered those issues with missing bricks appearing twice in the response
	bh.fMutex.RLock()
	bricks := make([]set.Brick, len(bh.final))
	copy(bricks, bh.final)
	bh.fMutex.RUnlock()

	// Combine final and missing bricks into a single slice
	bricks = append(bricks, bh.getMissingAsSlice()...)

	// Sort Bricks by index to maintain original order from the set
	set.SortBricksByIndex(bricks)
	return bricks
}

// getMissingAsSlice safely retrieves the missing bricks as a slice
func (bh *BricksHandler) getMissingAsSlice() []set.Brick {
	bh.mMutex.RLock()
	defer bh.mMutex.RUnlock()

	return utils.MapValues(bh.missing)
}

// appendFinal adds a Brick to the final slice
func (bh *BricksHandler) appendFinal(brick set.Brick) {
	bh.fMutex.Lock()
	defer bh.fMutex.Unlock()
	bh.final = append(bh.final, brick)
}

// hasMissing checks if a Brick with the given ID exists in the missing map
func (bh *BricksHandler) hasMissing(id uuid.UUID) bool {
	bh.mMutex.Lock()
	defer bh.mMutex.Unlock()

	_, exists := bh.missing[id]
	return exists
}

// appendMissing adds a Brick to the missing slice
func (bh *BricksHandler) appendMissing(brick set.Brick) {
	bh.mMutex.Lock()
	defer bh.mMutex.Unlock()

	// Initialize the missing map if it's nil
	if bh.missing == nil {
		bh.missing = make(map[uuid.UUID]set.Brick)
	}

	// Add the brick to the missing map using its ID as the key
	bh.missing[brick.UUID] = brick
}

// removeMissing removes a Brick from the missing slice by its ID
func (bh *BricksHandler) removeMissing(id uuid.UUID) {
	bh.mMutex.Lock()
	defer bh.mMutex.Unlock()

	// Remove the brick from the missing map using its ID as the key
	delete(bh.missing, id)
}
