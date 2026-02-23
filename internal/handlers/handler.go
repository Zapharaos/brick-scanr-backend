package handlers

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/searchruntime"
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
)

type Handler struct {
	srh  *setruntime.Handler
	srch *searchruntime.Handler
}

// NewHandler creates a new handler wrapping both the set and search runtime handlers
func NewHandler(setHandler *setruntime.Handler, searchHandler *searchruntime.Handler) *Handler {
	return &Handler{
		srh:  setHandler,
		srch: searchHandler,
	}
}
