package jobs

import (
	"time"

	"github.com/google/uuid"
)

// Status represents the status of a job
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusComplete   Status = "complete"
	StatusFailed     Status = "failed"
)

// Job is the base interface for all jobs
type Job interface {
	GetID() string
	GetStatus() Status
	GetCreatedAt() time.Time
	GetUpdatedAt() time.Time
}

// BaseJob contains common fields for all jobs
type BaseJob struct {
	ID          string    `json:"id"`
	Status      Status    `json:"status"`
	Progress    *Progress `json:"progress,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	WebSocketID uuid.UUID `json:"-"` // Internal: WS connection ID
}

// Progress represents the progress of a set details job
type Progress struct {
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Current     int    `json:"current"`
	Total       int    `json:"total"`
	PercentDone int    `json:"percent_done"`
}

func NewBaseJob() *BaseJob {
	return &BaseJob{
		ID:        uuid.New().String(),
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func (j *BaseJob) GetID() string             { return j.ID }
func (j *BaseJob) GetStatus() Status         { return j.Status }
func (j *BaseJob) GetProgress() *Progress    { return j.Progress }
func (j *BaseJob) GetCreatedAt() time.Time   { return j.CreatedAt }
func (j *BaseJob) GetUpdatedAt() time.Time   { return j.UpdatedAt }
func (j *BaseJob) GetWebSocketID() uuid.UUID { return j.WebSocketID }

func (j *BaseJob) SetWebSocketID(id uuid.UUID)      { j.WebSocketID = id }
func (j *BaseJob) SetStatus(status Status)          { j.Status = status }
func (j *BaseJob) SetProgress(progress *Progress)   { j.Progress = progress }
func (j *BaseJob) SetError(error string)            { j.Error = error }
func (j *BaseJob) SetUpdatedAt(updatedAt time.Time) { j.UpdatedAt = updatedAt }
