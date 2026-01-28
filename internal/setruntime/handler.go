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

	wg    *sync.WaitGroup
	mutex sync.RWMutex
}

// NewHandler creates a new handler
func NewHandler(ctx context.Context) *Handler {
	return &Handler{
		sets:  make(map[uuid.UUID]*RuntimeSet),
		mutex: sync.RWMutex{},
		wg:    &sync.WaitGroup{},
		Upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		ErrorLogger: supervisor.NewAsyncErrorLogger(ctx, 1000),
	}
}

// RunSet runs a runtime set, if it is not already running
func (h *Handler) RunSet(set set.Set) *RuntimeSet {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// First check if the runtime set is already running
	if rs, ok := h.FindRuntimeSetBySetId(set.Id); ok {
		// TODO : ISSUE #8 - Async : check rs.set.FetchStatus and FetchError ?
		return rs
	}

	// Create a new runtime set
	rs := NewRuntimeSet(set, RuntimeOptionsFromConfig(), h.wg, h.ErrorLogger)

	// Set the runtime set end callback
	rs.onEnd = h.onRuntimeSetEnd

	// Start the set
	rs.Start()

	// Add the runtime set to the handler
	h.sets[rs.ID] = rs

	return rs
}

// GetRuntimeSet gets a runtime set from the handler
func (h *Handler) GetRuntimeSet(id uuid.UUID) *RuntimeSet {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.sets[id]
}

// FindRuntimeSetBySetId finds a runtime set by its set ID
func (h *Handler) FindRuntimeSetBySetId(setId uuid.UUID) (*RuntimeSet, bool) {
	for _, rs := range h.sets {
		if rs.set.Id == setId {
			return rs, true
		}
	}
	return nil, false
}

// RemoveRuntimeSet removes a runtime set from the handler
func (h *Handler) RemoveRuntimeSet(id uuid.UUID) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	delete(h.sets, id)
}

// PushChange pushes a change to the runtime set, if it exists
func (h *Handler) PushChange(rsId, changedId uuid.UUID, dType DataType, reason DataChangeReason) {
	if rs := h.GetRuntimeSet(rsId); rs != nil {
		rs.PushChange(dataChange{
			Id:     changedId,
			Type:   dType,
			Reason: reason,
		})
	}
}

// PushBatchProgress pushes batch progress updates to the runtime set
// This is specifically for incremental processing updates during inventory/price fetching
func (h *Handler) PushBatchProgress(rsId uuid.UUID, dType DataType, progress Progress) {
	if rs := h.GetRuntimeSet(rsId); rs != nil {
		rs.PushChange(dataChange{
			Id:       uuid.Nil, // No specific entity ID for batch progress
			Type:     dType,
			Reason:   DataTypeProgress,
			Progress: progress,
		})
	}
}

// StopRuntimeSet stops a specific running runtime set cleanly
func (h *Handler) StopRuntimeSet(id uuid.UUID) bool {
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
		// Channel is full or closed, runtime set might already be stopping
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

// IsSetRunning checks if a runtime set is running
func (h *Handler) IsSetRunning(id uuid.UUID) bool {
	return h.GetRuntimeSet(id) != nil
}

// onRuntimeSetEnd is called when a runtime set ends
func (h *Handler) onRuntimeSetEnd(id uuid.UUID) {
	h.RemoveRuntimeSet(id)
}
