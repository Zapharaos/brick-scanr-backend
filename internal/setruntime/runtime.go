package setruntime

import (
	"fmt"
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
	SetID  uuid.UUID
	Locale language.Tag
	OpType OperationType
}

func NewRuntimeSetKey(setID uuid.UUID, locale language.Tag, opType OperationType) RuntimeSetKey {
	return RuntimeSetKey{
		SetID:  setID,
		Locale: locale,
		OpType: opType,
	}
}

func (k RuntimeSetKey) String() string {
	return fmt.Sprintf("%s_%s_%s", k.SetID, k.Locale, k.OpType)
}

type RuntimeSet struct {
	ID  uuid.UUID     // Unique ID for this runtime set (used for websocket connections)
	key RuntimeSetKey // Key identifying the operation (SetID + Locale + OpType)
	opt RuntimeOptions

	// Core runtime data
	set      SetHandler
	bricks   BricksHandler
	ihAccess InventoryAccess

	// Client management
	*clientHolder
	register   chan Client
	unregister chan uuid.UUID
	changeChan chan dataChange

	// Synchronization
	done  chan struct{}
	onEnd func(key RuntimeSetKey)
	wg    *sync.WaitGroup

	errorLogger *supervisor.AsyncErrorLogger
}

// NewRuntimeSet creates a new runtime set
func NewRuntimeSet(key RuntimeSetKey, opt RuntimeOptions, s set.External, ihAccess InventoryAccess, wg *sync.WaitGroup, errorLogger *supervisor.AsyncErrorLogger) *RuntimeSet {
	rs := &RuntimeSet{
		ID:           uuid.New(),
		key:          key,
		opt:          opt,
		set:          newSetHandler(s),
		bricks:       newBricksHandler(),
		ihAccess:     ihAccess,
		clientHolder: newClientHolder(true),
		register:     make(chan Client, opt.ClientChanCap),
		unregister:   make(chan uuid.UUID, opt.ClientChanCap),
		changeChan:   make(chan dataChange, opt.ChangeChanCap),
		done:         make(chan struct{}),
		wg:           wg,
		errorLogger:  errorLogger,
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

// unregisterUUID returns the unregister channel as a send-only channel for use by wsruntime.BaseClient.
func (rs *RuntimeSet) unregisterUUID() chan<- uuid.UUID {
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

// NewBricksHandler sets the final and missing bricks in the runtime set, replacing any existing bricks
func (rs *RuntimeSet) NewBricksHandler(final, missing []set.Brick) {
	// No need to update the runtime set instance with those bricks

	// Replace the final bricks slice
	rs.bricks.final = final

	// Reset the missing bricks map and populate it with the new missing bricks
	rs.bricks.missing = make(map[uuid.UUID]set.Brick)
	for _, b := range missing {
		rs.bricks.appendMissing(b)
	}
}

// stop stops the runtime set
func (rs *RuntimeSet) stop() {
	zap.L().Info("Set ended", zap.String("set", rs.Read().ID.String()))

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
				zap.String("set_id", rs.Read().ID.String()),
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
			return

		case <-rs.done:
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

	// Copy the set for safe concurrent access while building data for new client
	cpSetExternal := *rs.Read()

	// Update set.Bricks with all Bricks stored in the runtime
	// This ensures PacketInit contains all Bricks fetched so far
	cpSetExternal.Bricks = rs.bricks.get()

	// Send initial packet with set info and all Bricks
	client.SendPacket(NewPacketInit(cpSetExternal))
}

// handleClientDisconnect handles a client disconnect
func (rs *RuntimeSet) handleClientDisconnect(id uuid.UUID) {
	rs.unregisterClient(id)
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

// Log functions

// logWarning logs a warning-level error to the async error logger
func (rs *RuntimeSet) logWarning(scope string, err error) {
	if rs.errorLogger == nil || err == nil {
		return
	}

	rs.errorLogger.LogError(set.Error{
		Scope:    scope,
		Message:  err.Error(),
		Severity: "warning",
		SetId:    rs.Read().ID,
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
		SetId:    rs.Read().ID,
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
		SetId:    rs.Read().ID,
	})
}
