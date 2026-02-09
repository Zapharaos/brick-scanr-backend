package setruntime

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/supervisor"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type OperationType string

const (
	OpTypeFull       OperationType = "full"
	OpTypeIncomplete OperationType = "incomplete"
)

// RuntimeSetKey uniquely identifies a runtime set operation
type RuntimeSetKey struct {
	SetID   uuid.UUID
	XLocale language.Tag
	OpType  OperationType
}

func NewRuntimeSetKey(setID uuid.UUID, xlocale language.Tag, opType OperationType) RuntimeSetKey {
	return RuntimeSetKey{
		SetID:   setID,
		XLocale: xlocale,
		OpType:  opType,
	}
}

func (k RuntimeSetKey) String() string {
	return fmt.Sprintf("%s_%s_%s", k.SetID, k.XLocale, k.OpType)
}

type RuntimeSet struct {
	ID  uuid.UUID     // Unique ID for this runtime set (used for websocket connections)
	key RuntimeSetKey // Key identifying the operation (SetID + XLocale + OpType)
	opt RuntimeOptions

	// setMutex protects concurrent access to the set field
	setMutex sync.RWMutex
	set      set.SetExternal

	// bricks stores all bricks processed during this runtime (by BrickID+DesignID)
	// This allows new clients to receive all bricks when joining an ongoing fetch
	bricksMutex sync.RWMutex
	bricks      map[string]set.BrickSet

	// stagingBricks is used during price fetching to build the new brick map without clearing the active one
	// Once price fetching completes, it atomically replaces bricks to avoid empty windows
	stagingBricks map[string]set.BrickSet
	useStaging    bool // When true, AddBricks writes to stagingBricks instead of bricks

	*clientHolder
	register   chan Client
	unregister chan uuid.UUID
	changeChan chan dataChange

	done  chan struct{}
	onEnd func(key RuntimeSetKey)

	wg *sync.WaitGroup

	errorLogger *supervisor.AsyncErrorLogger
}

// NewRuntimeSet creates a new runtime set
func NewRuntimeSet(s set.SetExternal, key RuntimeSetKey, opt RuntimeOptions, wg *sync.WaitGroup, errorLogger *supervisor.AsyncErrorLogger) *RuntimeSet {
	rs := &RuntimeSet{
		ID:            uuid.New(),
		key:           key,
		set:           s,
		errorLogger:   errorLogger,
		opt:           opt,
		clientHolder:  newClientHolder(true),
		register:      make(chan Client, opt.ClientChanCap),
		unregister:    make(chan uuid.UUID, opt.ClientChanCap),
		changeChan:    make(chan dataChange, opt.ChangeChanCap),
		bricks:        make(map[string]set.BrickSet),
		stagingBricks: make(map[string]set.BrickSet),
		useStaging:    false,
		bricksMutex:   sync.RWMutex{},
		done:          make(chan struct{}),
		wg:            wg,
	}

	return rs
}

func (rs *RuntimeSet) Key() RuntimeSetKey {
	return rs.key
}

// Start starts the runtime set
func (rs *RuntimeSet) Start() {
	go rs.run()
}

// PushChange pushes a data change to the runtime set
func (rs *RuntimeSet) PushChange(change dataChange) {
	rs.changeChan <- change
}

// Unregister returns the unregister channel
func (rs *RuntimeSet) Unregister() chan uuid.UUID {
	return rs.unregister
}

// Register returns the register channel
func (rs *RuntimeSet) Register() chan Client {
	return rs.register
}

// HasClient checks if a client is connected
func (rs *RuntimeSet) HasClient(id uuid.UUID) bool {
	defer rs.clientMutex.RUnlock()
	rs.clientMutex.RLock()
	_, ok := rs.clients[id]
	return ok
}

// GetSet returns a shallow copy of the set for safe reading
func (rs *RuntimeSet) GetSet() set.SetExternal {
	rs.setMutex.RLock()
	defer rs.setMutex.RUnlock()
	return rs.set
}

// GetSetID returns the set ID (immutable, no lock needed)
func (rs *RuntimeSet) GetSetID() uuid.UUID {
	return rs.set.Id
}

// GetFetchStatus returns the current fetch status
func (rs *RuntimeSet) GetFetchStatus() set.FetchStatus {
	rs.setMutex.RLock()
	defer rs.setMutex.RUnlock()
	return rs.set.FetchStatus
}

// GetFetchError returns the current fetch error
func (rs *RuntimeSet) GetFetchError() *set.FetchError {
	rs.setMutex.RLock()
	defer rs.setMutex.RUnlock()
	return rs.set.FetchError
}

// UpdateSetBricks updates the Bricks slice in rs.set.Bricks
func (rs *RuntimeSet) UpdateSetBricks(bricks []set.BrickSet) {
	rs.setMutex.Lock()
	defer rs.setMutex.Unlock()
	rs.set.Bricks = bricks
}

// getBrickMapItemKey generates a unique key for a brick based on its BrickID and DesignID
func (rs *RuntimeSet) getBrickMapItemKey(brick set.Brick) string {
	// Create unique key from BrickID and DesignID
	brickID, err := brick.GetBrickIDForRedis()
	if err != nil {
		rs.logWarning("RuntimeSet.AddBrick", err)
		zap.L().Warn("Failed to get brick ID for runtime storage",
			zap.Error(err),
			zap.String("design_id", string(brick.DesignID)),
		)
		return ""
	}
	return string(brickID) + ":" + string(brick.DesignID)
}

// AddBricks adds or updates multiple Bricks in the runtime set
func (rs *RuntimeSet) AddBricks(bricks []set.BrickSet) {
	rs.bricksMutex.Lock()
	defer rs.bricksMutex.Unlock()

	// Determine which map to write to
	targetMap := rs.bricks
	if rs.useStaging {
		targetMap = rs.stagingBricks
	}

	for _, brick := range bricks {
		// Create unique key for brick
		key := rs.getBrickMapItemKey(brick.Brick)
		if key == "" {
			return
		}

		_, exists := targetMap[key]
		if exists {
			// Update existing brick if needed (e.g., quantity, price)
			targetMap[key] = brick
		} else {
			// Add new brick
			targetMap[key] = brick
		}
	}
}

// GetAllBricks returns all Bricks currently stored in the runtime, sorted by index
func (rs *RuntimeSet) GetAllBricks() []set.BrickSet {
	rs.bricksMutex.RLock()
	defer rs.bricksMutex.RUnlock()

	bricks := make([]set.BrickSet, 0, len(rs.bricks))
	for _, brick := range rs.bricks {
		bricks = append(bricks, brick)
	}

	// Sort Bricks by index to maintain original order from the set
	sort.Slice(bricks, func(i, j int) bool {
		return bricks[i].Index < bricks[j].Index
	})

	return bricks
}

// EnableStagingMode enables staging mode where AddBricks writes to stagingBricks instead of bricks
// This allows building a new brick map without affecting clients reading from the active map
func (rs *RuntimeSet) EnableStagingMode() {
	rs.bricksMutex.Lock()
	defer rs.bricksMutex.Unlock()
	rs.useStaging = true
	rs.stagingBricks = make(map[string]set.BrickSet)
}

// PromoteStaging atomically replaces the active bricks map with stagingBricks
// This avoids having an empty brick map window when transitioning between phases
func (rs *RuntimeSet) PromoteStaging() {
	rs.bricksMutex.Lock()
	defer rs.bricksMutex.Unlock()
	rs.bricks = rs.stagingBricks
	rs.stagingBricks = make(map[string]set.BrickSet)
	rs.useStaging = false
}

// ClearBricks clear the Bricks slice
func (rs *RuntimeSet) ClearBricks() {
	rs.bricksMutex.Lock()
	defer rs.bricksMutex.Unlock()
	rs.bricks = make(map[string]set.BrickSet)
}

// logWarning logs a warning-level error to the async error logger
func (rs *RuntimeSet) logWarning(scope string, err error) {
	if rs.errorLogger == nil || err == nil {
		return
	}

	rs.errorLogger.LogError(set.Error{
		Scope:    scope,
		Message:  err.Error(),
		Severity: "warning",
		SetId:    rs.GetSetID(),
	})
}

// logError logs an error-level error to the async error logger
func (rs *RuntimeSet) logError(scope string, err error) {
	if rs.errorLogger == nil || err == nil {
		return
	}

	rs.errorLogger.LogError(set.Error{
		Scope:    scope,
		Message:  err.Error(),
		Severity: "error",
		SetId:    rs.GetSetID(),
	})
}

// logCritical logs a critical-level error to the async error logger
func (rs *RuntimeSet) logCritical(scope string, err error) {
	if rs.errorLogger == nil || err == nil {
		return
	}

	rs.errorLogger.LogError(set.Error{
		Scope:    scope,
		Message:  err.Error(),
		Severity: "critical",
		SetId:    rs.GetSetID(),
	})
}

// stop stops the runtime set
func (rs *RuntimeSet) stop() {
	zap.L().Info("Set ended", zap.String("set", rs.GetSetID().String()))

	rs.clientMutex.RLock()
	// unregister all clients
	for _, c := range rs.clients {
		c.close()
	}
	rs.clientMutex.RUnlock()

	rs.onEnd(rs.Key())
}

// run starts the runtime set
func (rs *RuntimeSet) run() {
	rs.wg.Add(1)

	setActivityTimer := time.NewTimer(rs.opt.Timeout)
	clientExpire := time.NewTicker(rs.opt.ClientTimeoutCheckFreq)
	defer func() {

		// handle possible panics
		if r := recover(); r != nil {
			rs.logCritical("RuntimeSet.Panic", fmt.Errorf("panic recovered: %v", r))
			zap.L().Error("Panic in set runtime, recovering and stopping set",
				zap.Any("panic", r),
				zap.String("runtime_id", rs.ID.String()),
				zap.String("set_id", rs.GetSetID().String()),
			)
		}

		setActivityTimer.Stop()
		clientExpire.Stop()
		rs.stop()
		rs.wg.Done()
	}()

	for {
		select {
		case cli := <-rs.register:
			setActivityTimer.Reset(rs.opt.Timeout)
			rs.handleClientConnect(cli)

		case id := <-rs.unregister:
			rs.handleClientDisconnect(id)

		case change := <-rs.changeChan:
			setActivityTimer.Reset(rs.opt.Timeout)
			rs.handleDataChange(change)

		case <-clientExpire.C:
			rs.runClientExpireChecker()

		case <-setActivityTimer.C:
			rs.stop()
			return

		case <-rs.done:
			rs.stop()
			return
		}
	}
}

// handleDataChange handles a data change
func (rs *RuntimeSet) handleDataChange(change dataChange) {
	switch change.Reason {
	case DataTypeCreated:
		rs.handleDataChangeCreated(change)
	case DataTypeUpdated:
		rs.handleDataChangeUpdated(change)
	case DataTypeCompleted:
		rs.handleDataChangeCompleted(change)
	case DataTypeFailed:
		rs.handleDataChangeFailed(change)
	case DataTypeProgress:
		rs.handleDataChangeProgress(change)
	}
}

// Client functions

// handleClientConnect handles a client connect
func (rs *RuntimeSet) handleClientConnect(client Client) {
	rs.registerClient(client)

	// Update rs.set.Bricks with all Bricks stored in the runtime
	// This ensures PacketInit contains all Bricks fetched so far
	s := rs.GetSet()
	s.Bricks = rs.GetAllBricks()

	// Send initial packet with set info and all Bricks
	client.SendPacket(NewPacketInit(s))
}

// handleClientDisconnect handles a client disconnect
func (rs *RuntimeSet) handleClientDisconnect(id uuid.UUID) {
	defer func() {
		rs.unregisterClient(id)
	}()

	rs.clientMutex.RLock()
	_, ok := rs.clients[id]
	rs.clientMutex.RUnlock()
	if !ok {
		return
	}
}

// runClientExpireChecker handles client expiration
func (rs *RuntimeSet) runClientExpireChecker() {
	defer rs.clientMutex.Unlock()
	rs.clientMutex.Lock()

	now := time.Now()
	nb := 0

	for _, c := range rs.clients {
		if now.Sub(c.LastActivity()) > rs.opt.ClientTimeout {
			nb++
			c.close()
		}
	}

	if nb > 0 {
		zap.L().Info("Closed expired clients", zap.Int("nb", nb), zap.Duration("timeout", rs.opt.ClientTimeout))
	}
}
