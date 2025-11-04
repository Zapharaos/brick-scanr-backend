package handlers

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/setruntime"
)

type Handler struct {
	srh *setruntime.Handler
}

// NewHandler creates a new handler
// Wraps the set runtime handler
func NewHandler(setHandler *setruntime.Handler) *Handler {
	return &Handler{
		srh: setHandler,
	}
}
