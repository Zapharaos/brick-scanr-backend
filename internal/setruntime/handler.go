package setruntime

import (
	"context"
	"net/http"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/supervisor"
	"github.com/Zapharaos/brick-scanr-backend/internal/wsruntime"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type Handler struct {
	sets        map[uuid.UUID]*RuntimeSet // Direct lookup by RS ID
	setsByKey   map[string]*RuntimeSet    // Lookup by operation key
	Upgrader    *websocket.Upgrader
	ErrorLogger *supervisor.AsyncErrorLogger

	wg    *sync.WaitGroup
	mutex sync.RWMutex
}

// NewHandler creates a new handler
func NewHandler(ctx context.Context) *Handler {
	return &Handler{
		sets:      make(map[uuid.UUID]*RuntimeSet),
		setsByKey: make(map[string]*RuntimeSet),
		mutex:     sync.RWMutex{},
		wg:        &sync.WaitGroup{},
		Upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		ErrorLogger: supervisor.NewAsyncErrorLogger(ctx, 1000),
	}
}

// RunSet runs a runtime set, if it is not already running
func (h *Handler) RunSet(s set.External, locale language.Tag, opType OperationType, ihAccess InventoryAccess) *RuntimeSet {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	key := NewRuntimeSetKey(s.ID, locale, opType)
	keyStr := key.String()

	// Check if a runtime set with this exact key already exists
	if rs, ok := h.setsByKey[keyStr]; ok {
		// Check if the runtime set is healthy (not failed)
		if rs.Read().FetchStatus == set.FetchStatusFailed {
			// Runtime set has failed, force stop it and create a new one
			zap.L().Info("Stopping failed runtime set to create new one",
				zap.String("key", keyStr),
			)
			h.forceStopRuntimeSetLocked(rs)
		} else {
			// Runtime set is healthy and matches our key, return it
			zap.L().Info("Reusing existing healthy runtime set",
				zap.String("key", keyStr),
			)
			return rs
		}
	}

	// Create a new runtime set
	rs := NewRuntimeSet(key, RuntimeOptionsFromConfig(), s, ihAccess, h.wg, h.ErrorLogger)

	// Set the runtime set end callback
	rs.onEnd = h.onRuntimeSetEnd

	// Start the set
	rs.Start()

	// Add the runtime set to the handler
	h.sets[rs.ID] = rs
	h.setsByKey[keyStr] = rs

	zap.L().Info("Started new runtime set",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("key", keyStr),
	)

	return rs
}

// GetRuntimeSet gets a runtime set from the handler
func (h *Handler) GetRuntimeSet(id uuid.UUID) *RuntimeSet {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.sets[id]
}

// FindRuntimeSetByKey finds a runtime set by its key
func (h *Handler) FindRuntimeSetByKey(key RuntimeSetKey) (*RuntimeSet, bool) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	rs, ok := h.setsByKey[key.String()]
	return rs, ok
}

// FindRuntimeSetBySetId finds all runtime sets for a given set ID
// Multiple runtime sets can exist for the same set ID with different currencies/operations
func (h *Handler) FindRuntimeSetBySetId(setId uuid.UUID) []*RuntimeSet {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	sets := make([]*RuntimeSet, 0)
	for _, rs := range h.sets {
		if rs.Read().ID == setId {
			sets = append(sets, rs)
		}
	}
	return sets
}

// RemoveRuntimeSet removes a runtime set from the handler
func (h *Handler) RemoveRuntimeSet(key RuntimeSetKey) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	keyStr := key.String()
	if rs, ok := h.setsByKey[keyStr]; ok {
		setID := rs.Read().ID
		delete(h.sets, rs.ID)
		delete(h.setsByKey, keyStr)
		zap.L().Info("Removed runtime set",
			zap.String("runtime_id", rs.ID.String()),
			zap.String("key", keyStr),
		)

		// Check if there are any other runtime sets for the same setID
		hasOtherRuntimeSets := false
		for _, otherRs := range h.setsByKey {
			if otherRs.Read().ID == setID {
				hasOtherRuntimeSets = true
				break
			}
		}

		// If no other runtime sets exist for this setID, cleanup the inventory
		if !hasOtherRuntimeSets {
			IH().CleanupInventory(setID)
		}
	}
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
func (h *Handler) PushBatchProgress(rsId uuid.UUID, dType DataType, progress wsruntime.Progress) {
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
func (h *Handler) StopRuntimeSet(key RuntimeSetKey) bool {
	h.mutex.RLock()
	rs, exists := h.setsByKey[key.String()]
	h.mutex.RUnlock()

	if !exists {
		return false
	}

	select {
	case rs.done <- struct{}{}:
		return true
	default:
		// Channel is full or closed, runtime set might already be stopping
		return false
	}
}

// forceStopRuntimeSetLocked forcefully stops a runtime set (must be called with mutex locked)
func (h *Handler) forceStopRuntimeSetLocked(rs *RuntimeSet) {
	// Remove from maps immediately
	delete(h.sets, rs.ID)
	delete(h.setsByKey, rs.Key().String())

	// Try to signal the runtime to stop
	select {
	case rs.done <- struct{}{}:
	default:
		// Channel is full or closed, runtime set might already be stopping
	}

	zap.L().Info("Forcefully stopped runtime set",
		zap.String("runtime_id", rs.ID.String()),
		zap.String("key", rs.Key().String()),
	)
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

// onRuntimeSetEnd is called when a runtime set ends
func (h *Handler) onRuntimeSetEnd(key RuntimeSetKey) {
	h.RemoveRuntimeSet(key)
}

// logWarning logs a warning-level error to the async error logger
// Used for non-critical errors that don't stop execution
func (h *Handler) logWarning(setID uuid.UUID, scope string, err error) {
	if h.ErrorLogger == nil || err == nil {
		return
	}

	h.ErrorLogger.LogError(set.Error{
		SetId:    setID,
		Scope:    scope,
		Message:  err.Error(),
		Severity: "warning",
	})
}

// logError logs an error-level error to the async error logger
// Used for errors that may impact functionality but allow continued operation
func (h *Handler) logError(setID uuid.UUID, scope string, err error) {
	if h.ErrorLogger == nil || err == nil {
		return
	}

	h.ErrorLogger.LogError(set.Error{
		SetId:    setID,
		Scope:    scope,
		Message:  err.Error(),
		Severity: "error",
	})
}

// logCriticalError logs a critical-level error to the async error logger
// Used for fatal errors that stop execution or cause operation failure
func (h *Handler) logCriticalError(setID uuid.UUID, scope string, err error) {
	if h.ErrorLogger == nil || err == nil {
		return
	}

	h.ErrorLogger.LogError(set.Error{
		SetId:    setID,
		Scope:    scope,
		Message:  err.Error(),
		Severity: "critical",
	})
}
