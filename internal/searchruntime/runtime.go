package searchruntime

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// RuntimeStatus tracks the lifecycle of a search runtime
type RuntimeStatus int

const (
	RuntimeStatusRunning  RuntimeStatus = iota
	RuntimeStatusComplete               // processing finished normally
	RuntimeStatusFailed                 // processing ended with a fatal error
)

// Runtime manages a single search WebSocket session
type Runtime struct {
	ID     uuid.UUID    // unique ID used as the websocket session identifier
	Query  string       // original search query
	Locale language.Tag // requested locale
	Status RuntimeStatus

	*clientHolder
	register   chan Client
	unregister chan uuid.UUID
	changeChan chan batchChange // carries batch updates from the processing goroutines

	done  chan struct{}
	onEnd func(id uuid.UUID)
	wg    *sync.WaitGroup
	opt   RuntimeOptions

	// Progress counters (updated atomically by the fetch goroutines)
	totalResults int // total items expected (sets + bricks from BL search)
}

// batchChange carries a batch of processed items to the runtime loop for broadcasting
type batchChange struct {
	items    []batchItem
	done     int
	total    int
	category string // "sets" or "bricks"
	fatal    string // non-empty means fatal error
	complete bool   // true when all processing is done
}

type batchItem struct {
	responseItem interface{}
}

// NewRuntime creates and returns a new search runtime (not yet started)
func NewRuntime(query string, locale language.Tag, totalResults int, wg *sync.WaitGroup, onEnd func(id uuid.UUID)) *Runtime {
	opt := RuntimeOptionsFromConfig()
	rt := &Runtime{
		ID:           uuid.New(),
		Query:        query,
		Locale:       locale,
		Status:       RuntimeStatusRunning,
		totalResults: totalResults,
		clientHolder: newClientHolder(),
		register:     make(chan Client, opt.ClientChanCap),
		unregister:   make(chan uuid.UUID, opt.ClientChanCap),
		changeChan:   make(chan batchChange, opt.ChangeChanCap),
		done:         make(chan struct{}),
		onEnd:        onEnd,
		wg:           wg,
		opt:          opt,
	}
	return rt
}

// Start begins the runtime event loop
func (rt *Runtime) Start() {
	go rt.run()
}

// Register returns the register channel for new clients
func (rt *Runtime) Register() chan Client {
	return rt.register
}

// Unregister returns the unregister channel for disconnected clients
func (rt *Runtime) Unregister() chan uuid.UUID {
	return rt.unregister
}

// unregisterUUID returns the unregister channel as send-only, for use by wsruntime.BaseClient.
func (rt *Runtime) unregisterUUID() chan<- uuid.UUID {
	return rt.unregister
}

// pushBatchChange is used by fetch_operations to carry typed items to the runtime event loop
func (rt *Runtime) pushBatchChange(bc batchChange) {
	rt.changeChan <- bc
}

// SignalComplete sends a completion signal to the runtime loop
func (rt *Runtime) SignalComplete() {
	rt.changeChan <- batchChange{complete: true}
}

// SignalError sends a fatal error signal to the runtime loop
func (rt *Runtime) SignalError(msg string) {
	rt.changeChan <- batchChange{fatal: msg}
}

// run is the main event loop for the runtime
func (rt *Runtime) run() {
	rt.wg.Add(1)

	activityTimer := time.NewTimer(rt.opt.Timeout)
	clientExpire := time.NewTicker(rt.opt.ClientTimeoutCheckFreq)
	defer func() {
		if r := recover(); r != nil {
			zap.L().Error("Panic in search runtime, recovering",
				zap.Any("panic", r),
				zap.String("runtime_id", rt.ID.String()),
				zap.String("query", rt.Query),
			)
		}
		activityTimer.Stop()
		clientExpire.Stop()
		rt.stop()
		rt.wg.Done()
	}()

	for {
		select {
		case cli := <-rt.register:
			activityTimer.Reset(rt.opt.Timeout)
			rt.handleClientConnect(cli)

		case id := <-rt.unregister:
			rt.handleClientDisconnect(id)

		case change := <-rt.changeChan:
			activityTimer.Reset(rt.opt.Timeout)
			rt.handleChange(change)
			if change.complete || change.fatal != "" {
				return
			}

		case <-clientExpire.C:
			rt.runClientExpireChecker()

		case <-activityTimer.C:
			zap.L().Info("Search runtime timed out",
				zap.String("runtime_id", rt.ID.String()),
				zap.String("query", rt.Query),
			)
			rt.stop()
			return

		case <-rt.done:
			rt.stop()
			return
		}
	}
}

func (rt *Runtime) handleClientConnect(c Client) {
	rt.registerClient(c)
	// Send init packet with total expected results
	c.SendPacket(NewPacketInit(rt.totalResults))
}

func (rt *Runtime) handleClientDisconnect(id uuid.UUID) {
	rt.unregisterClient(id)
}

func (rt *Runtime) handleChange(change batchChange) {
	if change.fatal != "" {
		rt.Status = RuntimeStatusFailed
		rt.broadcastPacket(NewPacketError(change.fatal))
		return
	}
	if change.complete {
		rt.Status = RuntimeStatusComplete
		rt.broadcastPacket(NewPacketComplete())
		return
	}
	// It's a batch of items
	rt.broadcastPacket(change.toPacket())
}

func (rt *Runtime) stop() {
	zap.L().Info("Search runtime stopping",
		zap.String("runtime_id", rt.ID.String()),
		zap.String("query", rt.Query),
	)

	rt.clientMutex.RLock()
	for _, c := range rt.clients {
		c.close()
	}
	rt.clientMutex.RUnlock()

	if rt.onEnd != nil {
		rt.onEnd(rt.ID)
	}
}

func (rt *Runtime) runClientExpireChecker() {
	rt.clientMutex.Lock()
	defer rt.clientMutex.Unlock()

	now := time.Now()
	nb := 0
	for _, c := range rt.clients {
		if now.Sub(c.LastActivity()) > rt.opt.ClientTimeout {
			nb++
			c.close()
		}
	}
	if nb > 0 {
		zap.L().Info("Closed expired search clients",
			zap.Int("nb", nb),
			zap.String("runtime_id", rt.ID.String()),
		)
	}
}

// toPacket converts a batchChange into a broadcast packet
func (bc *batchChange) toPacket() *PacketBatch {
	items := make([]interface{}, 0, len(bc.items))
	for _, it := range bc.items {
		items = append(items, it.responseItem)
	}
	return NewPacketBatch(items, bc.done, bc.total, bc.category)
}

// IsAlive reports whether the runtime is still processing
func (rt *Runtime) IsAlive() bool {
	return rt.Status == RuntimeStatusRunning
}

// String returns a human-readable key for the runtime
func (rt *Runtime) String() string {
	return fmt.Sprintf("%s_%s", rt.Query, rt.Locale)
}
