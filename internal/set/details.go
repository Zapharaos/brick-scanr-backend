package set

import (
	"fmt"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/google/uuid"
)

// DetailsJob represents a job to fetch set details (inventory + prices)
// @Description Job for fetching set details with inventory and prices
type DetailsJob struct {
	jobs.BaseJob
	SetNumber string         `json:"set_number" example:"21043-1"`
	ItemID    string         `json:"item_id" example:"171510"`
	Result    *DetailsResult `json:"result,omitempty"`
}

// DetailsResult represents the result of a completed set details job
// @Description Complete result of set details fetch
type DetailsResult struct {
	Inventory *bricklink.Inventory `json:"inventory"`
	Prices    []pickabrick.Brick   `json:"prices"`
}

// CreateDetailsJob creates a new set details job
func CreateDetailsJob(itemID, setNumber string, wsID uuid.UUID) *DetailsJob {

	j := jobs.NewBaseJob()
	j.SetWebSocketID(wsID)

	job := &DetailsJob{
		BaseJob:   *j,
		SetNumber: setNumber,
		ItemID:    itemID,
	}

	jobs.M().Store(job)
	return job
}

// GetDetailsJob retrieves a set details job by ID
func GetDetailsJob(jobID string) (*DetailsJob, error) {
	job, err := jobs.M().Get(jobID)
	if err != nil {
		return nil, err
	}

	detailsJob, ok := job.(*DetailsJob)
	if !ok {
		return nil, fmt.Errorf("job is not a set details job")
	}

	return detailsJob, nil
}

// CompleteJob marks the job as complete with results
func CompleteJob(jobID string, result *DetailsResult) error {
	job, err := GetDetailsJob(jobID)
	if err != nil {
		return err
	}

	// Set the result specific to DetailsJob
	job.Result = result
	jobs.M().Store(job)

	// Mark as complete
	return jobs.M().CompleteJob(jobID)
}
