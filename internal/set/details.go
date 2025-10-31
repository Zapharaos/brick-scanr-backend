package set

import (
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/google/uuid"
	"go.uber.org/zap"
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
func (s Service) CreateDetailsJob(itemID, setNumber, wsConnID string) *DetailsJob {
	job := &DetailsJob{
		BaseJob: jobs.BaseJob{
			ID:          uuid.New().String(),
			Status:      jobs.StatusPending,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			WebSocketID: wsConnID,
		},
		SetNumber: setNumber,
		ItemID:    itemID,
	}

	s.jobManager.Store(job)
	return job
}

// GetDetailsJob retrieves a set details job by ID
func (s Service) GetDetailsJob(jobID string) (*DetailsJob, error) {
	job, err := s.jobManager.Get(jobID)
	if err != nil {
		return nil, err
	}

	detailsJob, ok := job.(*DetailsJob)
	if !ok {
		return nil, fmt.Errorf("job is not a set details job")
	}

	return detailsJob, nil
}

// UpdateStatus updates the job status
func (s Service) UpdateStatus(jobID string, status jobs.Status) error {
	job, err := s.GetDetailsJob(jobID)
	if err != nil {
		return err
	}

	job.Status = status
	job.UpdatedAt = time.Now()
	s.jobManager.Store(job)

	return nil
}

// UpdateProgress updates the job progress
func (s Service) UpdateProgress(jobID, stage, message string, current, total int) error {
	job, err := s.GetDetailsJob(jobID)
	if err != nil {
		return err
	}

	percentDone := 0
	if total > 0 {
		percentDone = (current * 100) / total
	}

	job.Progress = &jobs.Progress{
		Stage:       stage,
		Message:     message,
		Current:     current,
		Total:       total,
		PercentDone: percentDone,
	}
	job.UpdatedAt = time.Now()
	s.jobManager.Store(job)

	return nil
}

// CompleteJob marks the job as complete with results
func (s Service) CompleteJob(jobID string, result *DetailsResult) error {
	job, err := s.GetDetailsJob(jobID)
	if err != nil {
		return err
	}

	job.Status = jobs.StatusComplete
	job.Result = result
	job.UpdatedAt = time.Now()
	s.jobManager.Store(job)

	return nil
}

// FailJob marks the job as failed
func (s Service) FailJob(jobID string, errorMsg string) error {
	job, err := s.GetDetailsJob(jobID)
	if err != nil {
		return err
	}

	job.Status = jobs.StatusFailed
	job.Error = errorMsg
	job.UpdatedAt = time.Now()
	s.jobManager.Store(job)

	return nil
}

// ProcessDetailsJob processes a set details job in the background
func (s Service) ProcessDetailsJob(job *DetailsJob, wsHandler jobs.WebSocketHandler) {
	// Update status to processing
	_ = s.UpdateStatus(job.ID, jobs.StatusProcessing)
	if job.WebSocketID != "" {
		wsHandler.SendStatus(job.WebSocketID, job.ID, "processing", "Starting to fetch set details...")
	}

	// TODO : fetch set

	// Fetch inventory
	_ = s.UpdateProgress(job.ID, "fetching_inventory", "Fetching set inventory...", 0, 100)

	inventory, err := s.bricklinkClient.FetchInventory(job.ItemID, job.SetNumber)
	if err != nil {
		zap.L().Error("Failed to fetch inventory",
			zap.Error(err),
			zap.String("job_id", job.ID),
			zap.String("id", job.ItemID),
			zap.String("set_number", job.SetNumber),
		)
		_ = s.FailJob(job.ID, "Failed to fetch inventory from BrickLink")
		if job.WebSocketID != "" {
			wsHandler.SendError(job.WebSocketID, job.ID, "Failed to fetch inventory from BrickLink")
		}
		return
	}

	_ = s.UpdateProgress(job.ID, "inventory_loaded", "Inventory loaded", 25, 100)

	// Get unique items
	uniqueItems := make(map[string]bricklink.InventoryItem)
	for _, item := range inventory.Items {
		if _, exists := uniqueItems[item.ItemNo]; !exists {
			uniqueItems[item.ItemNo] = item
		}
	}

	totalUnique := len(uniqueItems)
	_ = s.UpdateProgress(job.ID, "fetching_prices", fmt.Sprintf("Fetching prices for %d items...", totalUnique), 25, 100)

	// Fetch prices
	prices := make([]pickabrick.Brick, 0, totalUnique)
	currentProgress := 0

	for itemNo, item := range uniqueItems {
		currentProgress++

		// TODO: Replace with actual price fetching
		// price, err := s.bricklinkClient.FetchPickABrickPrice(item.ItemNo)

		priceInfo := pickabrick.Brick{
			ItemID: item.ItemID,
			ItemNo: itemNo,
			// Price:  price,
		}

		prices = append(prices, priceInfo)

		// Update progress every 10 items or on last item
		if currentProgress%10 == 0 || currentProgress == totalUnique {
			percentDone := 25 + ((currentProgress * 75) / totalUnique) // 25-100%
			_ = s.UpdateProgress(
				job.ID,
				"fetching_prices",
				fmt.Sprintf("Loaded %d/%d prices", currentProgress, totalUnique),
				percentDone,
				100,
			)

			if job.WebSocketID != "" {
				wsHandler.SendProgress(job.WebSocketID, job.ID, currentProgress, totalUnique, fmt.Sprintf("Loaded %d/%d prices", currentProgress, totalUnique))
			}
		}
	}

	// Complete job
	result := &DetailsResult{
		Inventory: inventory,
		Prices:    prices,
	}

	_ = s.CompleteJob(job.ID, result)

	if job.WebSocketID != "" {
		wsHandler.SendComplete(job.WebSocketID, job.ID, result)
	}

	zap.L().Info("Successfully completed set details job",
		zap.String("job_id", job.ID),
		zap.String("id", job.ItemID),
		zap.String("set_number", job.SetNumber),
		zap.Int("unique_items", totalUnique),
	)
}
