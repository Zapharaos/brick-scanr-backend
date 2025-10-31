package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/jobs"
)

// Service handles set details job operations
type Service struct {
	jobManager      *jobs.Manager
	bricklinkClient *bricklink.Client
}

func NewService(jobManager *jobs.Manager) Service {
	return Service{
		jobManager:      jobManager,
		bricklinkClient: bricklink.NewClient(),
	}
}

var _globalService Service

// S is used to access the global Service singleton
func S() Service {
	return _globalService
}

// ReplaceGlobals affect a new service to the global service singleton
func ReplaceGlobals(service Service) func() {
	prev := _globalService
	_globalService = service
	return func() { ReplaceGlobals(prev) }
}
