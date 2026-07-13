package setruntime

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/rebrickable"
	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
)

// throttleMonitorInterval is how often the monitor polls the upstream throttlers.
// The throttlers are global, so this state is shared across all in-flight fetches;
// a couple of seconds of display latency is imperceptible for blocks that last 30s+.
const throttleMonitorInterval = 2 * time.Second

// resumeDriftThreshold is the minimum change in the resume time (epoch ms) that
// triggers a re-emit while the state is unchanged, so the client countdown keeps
// updating during a pause/block without spamming a packet on every tick.
const resumeDriftThreshold = 1000

// aggregateThrottleState returns the most severe throttle state across the API
// clients involved in a set fetch (Rebrickable for inventory, LEGO for details,
// Pick-a-Brick for prices), along with the latest resume time when blocked.
func aggregateThrottleState() (throttle.State, int64) {
	now := time.Now()
	statuses := []throttle.Status{
		rebrickable.C().ThrottlerStatus(),
		lego.C().ThrottlerStatus(),
		pickabrick.C().ThrottlerStatus(),
	}

	worst := throttle.StateNormal
	for _, s := range statuses {
		if state := s.SimpleState(now); state.MoreSevereThan(worst) {
			worst = state
		}
	}

	// Capture the latest resume time among the clients that are in the worst state,
	// reading the field that matches it (block end for blocked, pause end for paused).
	var resumeAt int64
	for _, s := range statuses {
		if s.SimpleState(now) != worst {
			continue
		}
		var end time.Time
		switch worst {
		case throttle.StateBlocked:
			end = s.ThrottleEndsAt
		case throttle.StatePaused:
			end = s.PausedUntil
		}
		if end.After(now) {
			if ms := end.UnixMilli(); ms > resumeAt {
				resumeAt = ms
			}
		}
	}
	return worst, resumeAt
}

// monitorThrottle polls the upstream throttlers and pushes a throttle_status
// packet to the runtime set whenever the aggregated state changes. It runs for
// the lifetime of a single fetch and returns when stop is closed, emitting a
// final "normal" state so the frontend can clear any throttle banner.
func (h *Handler) monitorThrottle(rs *RuntimeSet, stop <-chan struct{}) {
	ticker := time.NewTicker(throttleMonitorInterval)
	defer ticker.Stop()

	last := throttle.StateNormal
	var lastResumeAt int64
	for {
		select {
		case <-stop:
			if last != throttle.StateNormal {
				h.PushThrottleStatus(rs.ID, throttle.StateNormal, 0)
			}
			return
		case <-ticker.C:
			state, resumeAt := aggregateThrottleState()

			// Re-emit on a state change, or — while the state is unchanged but
			// carries a countdown — when the resume time drifts enough to keep the
			// client countdown accurate (e.g. a sustained pause whose next-slot
			// estimate keeps sliding forward).
			changed := state != last
			drifted := resumeAt > 0 && absInt64(resumeAt-lastResumeAt) >= resumeDriftThreshold
			if !changed && !drifted {
				continue
			}

			last = state
			lastResumeAt = resumeAt
			h.PushThrottleStatus(rs.ID, state, resumeAt)
		}
	}
}

// absInt64 returns the absolute value of an int64.
func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
