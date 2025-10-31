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
