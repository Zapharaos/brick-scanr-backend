package supervisor

import (
	"context"
	"sync"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"go.uber.org/zap"
)

// AsyncErrorLogger handles asynchronous error logging to the database.
type AsyncErrorLogger struct {
	errChan chan set.Error
	wg      sync.WaitGroup
}

// NewAsyncErrorLogger creates a new AsyncErrorLogger with the given buffer size and context.
func NewAsyncErrorLogger(ctx context.Context, bufferSize int) *AsyncErrorLogger {
	l := &AsyncErrorLogger{
		errChan: make(chan set.Error, bufferSize),
	}
	l.wg.Add(1)
	go l.run(ctx)
	return l
}

// LogError sends an error to be logged asynchronously. Non-blocking if buffer is not full.
func (l *AsyncErrorLogger) LogError(err set.Error) {
	select {
	case l.errChan <- err:
		// sent successfully
	default:
		zap.L().Warn("AsyncErrorLogger: Error channel full, dropping error", zap.Any("error", err))
	}
}

// todo : ISSUE #8 - Async : keep? replace with log file?

// run is the background goroutine that writes errors to the database.
func (l *AsyncErrorLogger) run(ctx context.Context) {
	defer l.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-l.errChan:
			if !ok {
				zap.L().Info("AsyncErrorLogger: Channel closed, stopping logger")
				return
			}
			if valid, _ := err.IsValid(); valid {
				zap.L().Error("AsyncErrorLogger: Write error", zap.Any("error", err))
			} else {
				zap.L().Warn("AsyncErrorLogger: Invalid error", zap.Any("error", err))
			}
		}
	}
}

// Close gracefully shuts down the logger and flushes remaining errors.
func (l *AsyncErrorLogger) Close() {
	close(l.errChan)
	for err := range l.errChan {
		if valid, _ := err.IsValid(); valid {
			zap.L().Error("AsyncErrorLogger: Write error during close", zap.Any("error", err))
		}
	}
	l.wg.Wait()
}
