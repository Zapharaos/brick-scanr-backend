package setruntime

// Progress tracks batch processing progress with generic items
type Progress struct {
	Total     int   `json:"total"` // Total items to process
	Done      int   `json:"done"`  // Items completed (sent in previous batches)
	Items     []any `json:"items"` // Current batch items (can be any type)
	BatchCurr int   `json:"-"`     // Current batch counter (not serialized)
	BatchSize int   `json:"-"`     // Batch size limit (not serialized)
}

// NewProgress creates a new progress tracker
func NewProgress(total int, batchSize int) *Progress {
	return &Progress{
		Total:     total,
		Done:      0,
		Items:     []any{},
		BatchCurr: 0,
		BatchSize: batchSize,
	}
}

// HasReachedBatchLimit checks if the current batch has reached the batch size limit
func (p *Progress) HasReachedBatchLimit() bool {
	totalCurr := p.Done + p.BatchCurr
	return p.BatchCurr >= p.BatchSize || totalCurr >= p.Total
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

// PrepareForSend updates done to include the current batch before sending to frontend
// This ensures the frontend sees accurate progress including the current batch
func (p *Progress) PrepareForSend() {
	p.Done += p.BatchCurr
	p.BatchCurr = 0
}

// CompleteBatch resets the items array after batch has been sent
// Should be called after PrepareForSend and sending to frontend
func (p *Progress) CompleteBatch() {
	p.Items = []any{}
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
