package jobs

import (
	"fmt"
	"sync"
	"time"
)

// Manager handles job storage and retrieval (generic job management)
// Uses sync.Map for lock-free reads and efficient concurrent access
type Manager struct {
	jobs sync.Map // map[string]Job - lock-free for concurrent reads
}

// NewManager creates a new job manager
func NewManager() *Manager {
	return &Manager{}
}

var _globalManager *Manager

// M is used to access the global Manager singleton
func M() *Manager {
	if _globalManager == nil {
		_globalManager = NewManager()
	}
	return _globalManager
}

// ReplaceGlobalManager affects a new manager to the global manager singleton
func ReplaceGlobalManager(manager *Manager) func() {
	prev := _globalManager
	_globalManager = manager
	return func() { ReplaceGlobalManager(prev) }
}

// Store stores a job
// sync.Map is optimized for cases where:
// - Entry for a given key is only ever written once but read many times
// - Multiple goroutines read, write, and overwrite entries for disjoint sets of keys
func (m *Manager) Store(job Job) {
	m.jobs.Store(job.GetID(), job)
}

// Get retrieves a job by ID
// Lock-free read operation - highly efficient for concurrent access
func (m *Manager) Get(id string) (Job, error) {
	value, exists := m.jobs.Load(id)
	if !exists {
		return nil, fmt.Errorf("job not found")
	}

	job, ok := value.(Job)
	if !ok {
		return nil, fmt.Errorf("invalid job type in storage")
	}

	return job, nil
}

// Delete removes a job
func (m *Manager) Delete(id string) {
	m.jobs.Delete(id)
}

// CleanupOld removes jobs older than the specified duration
func (m *Manager) CleanupOld(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	// Range is safe for concurrent use
	m.jobs.Range(func(key, value interface{}) bool {
		job, ok := value.(Job)
		if ok && job.GetUpdatedAt().Before(cutoff) {
			m.jobs.Delete(key)
		}
		return true // continue iteration
	})
}

// GetAll returns all jobs (for debugging/admin)
func (m *Manager) GetAll() []Job {
	result := make([]Job, 0)

	m.jobs.Range(func(key, value interface{}) bool {
		if job, ok := value.(Job); ok {
			result = append(result, job)
		}
		return true // continue iteration
	})

	return result
}

// Count returns the number of jobs
func (m *Manager) Count() int {
	count := 0
	m.jobs.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// UpdateStatus updates the job status
func (m *Manager) UpdateStatus(jobID string, status Status) error {
	job, err := m.Get(jobID)
	if err != nil {
		return err
	}

	// Type assertion to access BaseJob setter methods
	if updatable, ok := job.(interface {
		SetStatus(Status)
		SetUpdatedAt(time.Time)
	}); ok {
		updatable.SetStatus(status)
		updatable.SetUpdatedAt(time.Now())
		m.Store(job)
		return nil
	}

	return fmt.Errorf("job does not support status updates")
}

// UpdateProgress updates the job progress
func (m *Manager) UpdateProgress(jobID, stage, message string, current, total int) error {
	job, err := m.Get(jobID)
	if err != nil {
		return err
	}

	percentDone := 0
	if total > 0 {
		percentDone = (current * 100) / total
	}

	progress := &Progress{
		Stage:       stage,
		Message:     message,
		Current:     current,
		Total:       total,
		PercentDone: percentDone,
	}

	// Type assertion to access BaseJob setter methods
	if updatable, ok := job.(interface {
		SetProgress(*Progress)
		SetUpdatedAt(time.Time)
	}); ok {
		updatable.SetProgress(progress)
		updatable.SetUpdatedAt(time.Now())
		m.Store(job)
		return nil
	}

	return fmt.Errorf("job does not support progress updates")
}

// FailJob marks the job as failed
func (m *Manager) FailJob(jobID string, errorMsg string) error {
	job, err := m.Get(jobID)
	if err != nil {
		return err
	}

	// Type assertion to access BaseJob setter methods
	if updatable, ok := job.(interface {
		SetStatus(Status)
		SetError(string)
		SetUpdatedAt(time.Time)
	}); ok {
		updatable.SetStatus(StatusFailed)
		updatable.SetError(errorMsg)
		updatable.SetUpdatedAt(time.Now())
		m.Store(job)
		return nil
	}

	return fmt.Errorf("job does not support failure updates")
}

// CompleteJob marks the job as complete
func (m *Manager) CompleteJob(jobID string) error {
	job, err := m.Get(jobID)
	if err != nil {
		return err
	}

	// Type assertion to access BaseJob setter methods
	if updatable, ok := job.(interface {
		SetStatus(Status)
		SetUpdatedAt(time.Time)
	}); ok {
		updatable.SetStatus(StatusComplete)
		updatable.SetUpdatedAt(time.Now())
		m.Store(job)
		return nil
	}

	return fmt.Errorf("job does not support completion updates")
}
