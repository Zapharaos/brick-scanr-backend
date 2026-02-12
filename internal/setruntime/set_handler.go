package setruntime

import (
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
)

// SetHandler provides thread-safe access to the set data
type SetHandler struct {
	set   set.External
	mutex sync.RWMutex
}

// newSetHandler creates a new SetHandler with the provided set and initializes the mutex
func newSetHandler(s set.External) SetHandler {
	return SetHandler{
		set:   s,
		mutex: sync.RWMutex{},
	}
}

// Read returns a shallow copy of the set for safe reading
func (rs *RuntimeSet) Read() *set.External {
	rs.set.mutex.RLock()
	defer rs.set.mutex.RUnlock()
	cpSet := rs.set.set
	return &cpSet
}

// SetBricks safely updates the bricks of the set
func (sh *SetHandler) SetBricks(bricks []set.Brick) {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()
	sh.set.SetBricks(bricks, false)
}

// AddFinalBrickData safely calling set.AddFinalBrickData
func (sh *SetHandler) AddFinalBrickData(brick set.Brick) {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()
	sh.set.AddFinalBrickData(brick)
}

// SetFetchStatus safely updates the fetch status of the set
func (sh *SetHandler) SetFetchStatus(status set.FetchStatus) {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()
	sh.set.FetchStatus = status
}

// SetFetchError apply an error to fetch status and fetch error
func (sh *SetHandler) SetFetchError(step set.FetchErrorStep, msg string) {
	sh.mutex.Lock()
	defer sh.mutex.Unlock()
	sh.set.FetchStatus = set.FetchStatusFailed
	sh.set.FetchError = &set.FetchError{
		Message: msg,
		Step:    step,
	}
}
