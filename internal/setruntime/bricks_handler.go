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
	// Lock both mutexes for reading to safely access the final and missing slices
	bh.fMutex.RLock()
	bh.mMutex.RLock()

	// Combine final and missing bricks into a single slice
	bricks := append(bh.final, bh.getMissingAsSlice()...)

	// Unlock the mutexes after copying the bricks
	bh.mMutex.RUnlock()
	bh.fMutex.RUnlock()

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
