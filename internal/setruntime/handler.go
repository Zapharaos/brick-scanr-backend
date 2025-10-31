package setruntime

import (
	"context"
	"net/http"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/supervisor"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Handler struct {
	sets        map[uuid.UUID]*RuntimeSet
	Upgrader    *websocket.Upgrader
	ErrorLogger *supervisor.AsyncErrorLogger

	clientChanCap  int
	receiveChanCap int

	wg    *sync.WaitGroup
	mutex sync.RWMutex
}

// NewHandler creates a new handler
func NewHandler(ctx context.Context) *Handler {
	return &Handler{
		sets:           make(map[uuid.UUID]*RuntimeSet),
		mutex:          sync.RWMutex{},
		wg:             &sync.WaitGroup{},
		clientChanCap:  100, // todo make configurable
		receiveChanCap: 100, // todo make configurable
		Upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		ErrorLogger: supervisor.NewAsyncErrorLogger(ctx, 1000),
	}
}

// RunSet runs a set, if it is not already running
func (h *Handler) RunSet(set set.Set) *RuntimeSet {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// First check if the set is already running
	if rs, ok := h.sets[set.Id]; ok {
		return rs
	}

	// Create a new runtime set
	rs := NewRuntimeSet(set, RuntimeOptionsFromConfig(), h.wg, h.ErrorLogger)

	// Set the set end callback
	rs.onEnd = h.onSetEnd

	// Start the set
	rs.Start()

	// Add the set to the handler
	h.sets[set.Id] = rs

	return rs
}

// GetSet gets a runtime set from the handler
func (h *Handler) GetSet(id uuid.UUID) *RuntimeSet {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.sets[id]
}

// RemoveSet removes a runtime set from the handler
func (h *Handler) RemoveSet(id uuid.UUID) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	delete(h.sets, id)
}

// PushChange pushes a change to the set, if it exists
func (h *Handler) PushChange(setId, changedId, changedParentId uuid.UUID, dType DataType, reason DataChangeReason) {
	if rs := h.GetSet(setId); rs != nil {
		rs.PushChange(dataChange{
			Id:       changedId,
			ParentId: changedParentId,
			Type:     dType,
			Reason:   reason,
		})
	}
}

// StopSet stops a specific running set cleanly
func (h *Handler) StopSet(id uuid.UUID) bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	s, exists := h.sets[id]

	if !exists {
		return false
	}

	select {
	case s.done <- struct{}{}:
		return true
	default:
		// Channel is full or closed, set might already be stopping
		return false
	}
}

// Shutdown shuts down the handler
func (h *Handler) Shutdown() {
	h.mutex.Lock()

	for _, s := range h.sets {
		select {
		case s.done <- struct{}{}:
		default:
		}
	}

	h.mutex.Unlock()

	h.wg.Wait()
}

// IsSetRunning checks if a set is running
func (h *Handler) IsSetRunning(id uuid.UUID) bool {
	return h.GetSet(id) != nil
}

// onSetEnd is called when a set ends
func (h *Handler) onSetEnd(id uuid.UUID) {
	h.RemoveSet(id)
}
