package setruntime

import (
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/google/uuid"
)

// InventoryHolder manages the inventories for all sets
type InventoryHolder struct {
	inventories map[uuid.UUID]*Inventory
	mutex       sync.Mutex // Mutex to protect access to the inventories map
}

// Singleton instance of InventoryHolder
var _globalInventoryHandler *InventoryHolder

// IH returns the global InventoryHolder struct
func IH() *InventoryHolder {
	if _globalInventoryHandler == nil {
		_globalInventoryHandler = NewInventoryHolder()
	}
	return _globalInventoryHandler
}

// NewInventoryHolder creates a new InventoryHolder instance with initialized fields
func NewInventoryHolder() *InventoryHolder {
	return &InventoryHolder{
		inventories: make(map[uuid.UUID]*Inventory),
		mutex:       sync.Mutex{},
	}
}

// Access returns an inventory for the given setID, along with access type and initial data
func (i *InventoryHolder) Access(setID uuid.UUID) InventoryAccess {
	i.mutex.Lock()
	inv, ok := i.inventories[setID]

	if !ok {
		// Inventory does not exist, caller gets write access
		inv = newInventory(setID, i)

		// Acquire reference for the writer
		inv.acquire()

		// Store the new inventory before releasing the lock
		i.inventories[setID] = inv
		i.mutex.Unlock()

		// Return the inventory with write access (true) and an empty slice of bricks
		return InventoryAccess{
			true, true, inv,
		}
	}

	// Try to acquire a reference to the existing inventory
	if !inv.acquire() {
		// Inventory was already completed and done, cannot access
		i.mutex.Unlock()
		return InventoryAccess{
			false, false, nil,
		}
	}

	i.mutex.Unlock()

	// Inventory exists, we acquired a reference, caller gets read access
	return InventoryAccess{
		true, false, inv,
	}
}

// StopInventory marks the inventory for the given setID as complete and calls done() on it to signal completion
// The inventory remains accessible in the map so that listeners who haven't started yet can still access it
// It will be cleaned up later by CleanupInventory()
func (i *InventoryHolder) StopInventory(setID uuid.UUID) {
	i.mutex.Lock()
	inv, ok := i.inventories[setID]
	i.mutex.Unlock()

	// Call done() to signal completion to all listeners
	if ok {
		inv.done()
	}
}

// CleanupInventory removes a completed inventory from the map after all listeners have finished
// This should be called after all operations using the inventory are complete
func (i *InventoryHolder) CleanupInventory(setID uuid.UUID) {
	i.mutex.Lock()
	inv, ok := i.inventories[setID]
	if ok {
		// Mark as closed to prevent new accessors
		inv.refCountMutex.Lock()
		inv.closed = true
		inv.refCountMutex.Unlock()

		// Remove from map
		delete(i.inventories, setID)
	}
	i.mutex.Unlock()
}

// Shutdown gracefully shuts down the InventoryHolder by marking all inventories as complete and calling done() on them
func (i *InventoryHolder) Shutdown() {
	i.mutex.Lock()

	// Mark all inventories as complete and collect them
	invs := make([]*Inventory, 0, len(i.inventories))
	for _, inv := range i.inventories {
		inv.complete()
		invs = append(invs, inv)
	}

	// Clear the map while holding the lock
	i.inventories = make(map[uuid.UUID]*Inventory)
	i.mutex.Unlock()

	// Call done() on all inventories outside the lock
	for _, inv := range invs {
		inv.done()
	}
}

// --------------------------
// Access struct
// --------------------------

type InventoryAccess struct {
	Success   bool
	IsWriter  bool
	Inventory *Inventory
}

// Reset resets the InventoryAccess to its zero values
func (i InventoryAccess) Reset() {
	i.Success = false
	i.IsWriter = false
	i.Inventory = nil
}

// IsValid checks if the access was successful and the inventory is not nil
func (i InventoryAccess) IsValid() bool {
	return i.Success && i.Inventory != nil
}

// --------------------------
// Inventory struct
// --------------------------

// Inventory manages the state of a set's inventory fetching
type Inventory struct {
	setID     uuid.UUID
	inventory map[uuid.UUID]set.Brick
	total     int
	status    set.FetchStatus

	// Writer management
	writer sync.Mutex

	// Listener management
	listeners     []chan Progress
	listenerMutex sync.Mutex

	// Synchronization
	doneChan chan struct{}
	doneOnce sync.Once
	closed   bool // true when inventory is fully closed and should not accept new listeners

	// Reference counting for active accessors
	refCount      int
	refCountMutex sync.Mutex

	// Back-reference to holder for cleanup
	holder *InventoryHolder
}

// newInventory creates a new Inventory instance with initialized fields
func newInventory(setID uuid.UUID, holder *InventoryHolder) *Inventory {
	return &Inventory{
		setID:         setID,
		inventory:     make(map[uuid.UUID]set.Brick),
		status:        set.FetchStatusPending,
		writer:        sync.Mutex{},
		listeners:     make([]chan Progress, 0),
		listenerMutex: sync.Mutex{},
		doneChan:      make(chan struct{}),
		closed:        false,
		refCount:      0,
		refCountMutex: sync.Mutex{},
		holder:        holder,
	}
}

// acquire attempts to increment the reference count if the inventory is not closed
// Returns true if successfully acquired, false if inventory is closed
func (i *Inventory) acquire() bool {
	i.refCountMutex.Lock()
	defer i.refCountMutex.Unlock()

	if i.closed {
		return false
	}

	i.refCount++
	return true
}

// release decrements the reference count
func (i *Inventory) release() {
	i.refCountMutex.Lock()
	defer i.refCountMutex.Unlock()

	i.refCount--

	// Note: We don't automatically cleanup when refCount reaches 0
	// The inventory remains in the map so that late listeners can still join
	// Cleanup happens through explicit mechanisms (timeout or explicit removal)
}

// complete marks the inventory as complete and closed, preventing new accessors from acquiring it
func (i *Inventory) complete() bool {
	i.refCountMutex.Lock()
	defer i.refCountMutex.Unlock()

	if i.status == set.FetchStatusCompleted {
		return true
	}

	i.status = set.FetchStatusCompleted
	i.closed = true
	return false
}

// done signals that the inventory is complete and no more updates will be sent
func (i *Inventory) done() {
	// Mark as completed to prevent new accessors
	i.refCountMutex.Lock()
	i.status = set.FetchStatusCompleted
	i.refCountMutex.Unlock()

	// Close the done channel once to release all waiting listeners
	i.doneOnce.Do(func() {
		close(i.doneChan)

		// Close all listener channels to signal completion
		i.listenerMutex.Lock()
		for _, listenerChan := range i.listeners {
			close(listenerChan)
		}
		i.listenerMutex.Unlock()
	})

	// Release the writer's reference
	i.release()
}

// write updates the inventory with a new batch of bricks and notifies listeners of the change
func (i *Inventory) write(batch Progress) {
	// Mark as fetching on the first write
	if i.status == set.FetchStatusPending {
		i.refCountMutex.Lock()
		i.status = set.FetchStatusFetching
		i.refCountMutex.Unlock()
	}

	i.writer.Lock()
	// Update inventory with the new batch of bricks
	for _, brick := range batch.Items {
		if bp, ok := brick.(set.Brick); ok {
			i.inventory[bp.UUID] = bp
		}
		if bp, ok := brick.(*set.Brick); ok && bp != nil {
			i.inventory[bp.UUID] = *bp
		}
	}
	i.total = batch.Total
	i.writer.Unlock()

	// Broadcast to all listeners
	i.listenerMutex.Lock()
	for _, listenerChan := range i.listeners {
		listenerChan <- batch
	}
	i.listenerMutex.Unlock()
}

// listen registers a new listener for inventory updates and waits for updates until the inventory is done
func (i *Inventory) listen(handleBatch func(Progress)) []set.Brick {
	defer i.release() // Release reference when done listening

	// Create a channel for this listener
	listenerChan := make(chan Progress, 10)

	// Register this listener
	i.listenerMutex.Lock()
	i.listeners = append(i.listeners, listenerChan)
	i.listenerMutex.Unlock()

	// Stop the writer from editing the inventory while we handle the initial batch
	i.writer.Lock()

	// Create an initial progress object with the current inventory state and total
	var progress Progress
	for _, item := range i.inventory {
		progress.AddItem(item)
	}
	progress.Total = i.total
	progress.Done = len(progress.Items)
	handleBatch(progress)

	// Unlock the writer to allow updates while we listen for changes
	i.writer.Unlock()

	// Wait for updates from the writer
	for {
		select {
		case batch, ok := <-listenerChan:
			if !ok {
				// Channel closed, writer is done
				return i.returnInventory()
			}
			// Received a batch update, let the listener handle it
			handleBatch(batch)
		case <-i.doneChan:
			// Writer signaled completion
			return i.returnInventory()
		}
	}
}

// returnInventory returns the current inventory as a slice of bricks, sorted by index
func (i *Inventory) returnInventory() []set.Brick {
	i.writer.Lock()
	bricks := utils.MapValues(i.inventory)
	set.SortBricksByIndex(bricks)
	i.writer.Unlock()

	return bricks
}
