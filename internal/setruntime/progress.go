package setruntime

// todo : v3 - modulable batch size? use config value?

const ProgressBatchSize = 10

// Progress tracks batch processing progress with generic items
type Progress struct {
	Total     int   `json:"total"` // Total items to process
	Done      int   `json:"done"`  // Items completed (sent in previous batches)
	Items     []any `json:"items"` // Current batch items (can be any type)
	BatchCurr int   `json:"-"`     // Current batch counter (not serialized)
}

// NewProgress creates a new progress tracker
func NewProgress(total int) *Progress {
	return &Progress{
		Total:     total,
		Done:      0,
		Items:     []any{},
		BatchCurr: 0,
	}
}

// HasReachedBatchLimit checks if the current batch has reached the batch size limit
func (p *Progress) HasReachedBatchLimit() bool {
	totalCurr := p.Done + p.BatchCurr
	return p.BatchCurr >= ProgressBatchSize || totalCurr >= p.Total
}

// Increment increments the current batch counter and total processed counter
func (p *Progress) Increment() {
	p.BatchCurr++
}

// AddItem adds an item to the current batch and increments the counter
func (p *Progress) AddItem(item any) {
	p.Items = append(p.Items, item)
	p.Increment()
}

// CompleteBatch marks the current batch as done and resets the batch counter
func (p *Progress) CompleteBatch() {
	p.Done += p.BatchCurr
	p.Items = []any{}
	p.BatchCurr = 0
}

// GetPercentage returns the completion percentage
func (p *Progress) GetPercentage() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Done+p.BatchCurr) / float64(p.Total) * 100
}

// EmptyItems checks if there are no items in the current batch
func (p *Progress) EmptyItems() bool {
	return len(p.Items) == 0
}
