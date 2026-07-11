package setruntime

import (
	"math"
	"time"
)

// throughputWindow is the trailing window over which the processing rate is
// observed to estimate the remaining time.
const throughputWindow = 30 * time.Second

// throughputSample records how many items were done at a given instant.
type throughputSample struct {
	t    time.Time
	done int
}

// throughputTracker estimates an ETA for a fetch phase from the observed
// processing rate over the last throughputWindow.
//
// It is only ever accessed from the runtime set's run() goroutine (via
// handleDataChangeProgress), so it needs no synchronization. The tracker resets
// automatically when the phase (DataType) changes, e.g. from BrickLink inventory
// to Pick-a-Brick prices.
type throughputTracker struct {
	dataType DataType
	samples  []throughputSample
}

// record adds a progress observation and returns the estimated remaining time in
// seconds. It returns 0 when an ETA cannot be estimated yet (not enough history,
// no forward progress within the window, or nothing left to do) — callers should
// treat 0 as "unknown" and omit it.
func (t *throughputTracker) record(dataType DataType, done, total int) int {
	now := time.Now()

	// Reset when the phase changes so the rate reflects the current phase only.
	if dataType != t.dataType {
		t.dataType = dataType
		t.samples = t.samples[:0]
	}

	t.samples = append(t.samples, throughputSample{t: now, done: done})

	// Drop samples older than the trailing window.
	cutoff := now.Add(-throughputWindow)
	kept := t.samples[:0]
	for _, s := range t.samples {
		if s.t.After(cutoff) {
			kept = append(kept, s)
		}
	}
	t.samples = kept

	if len(t.samples) < 2 {
		return 0
	}

	first := t.samples[0]
	last := t.samples[len(t.samples)-1]

	elapsed := last.t.Sub(first.t).Seconds()
	progressed := last.done - first.done
	if elapsed <= 0 || progressed <= 0 {
		// Stalled (e.g. blocked upstream): let the throttle status explain it.
		return 0
	}

	remaining := total - last.done
	if remaining <= 0 {
		return 0
	}

	rate := float64(progressed) / elapsed // items per second
	return int(math.Ceil(float64(remaining) / rate))
}
