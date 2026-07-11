package setruntime

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
)

// throttleMonitorInterval is how often the monitor polls the upstream throttlers.
// The throttlers are global, so this state is shared across all in-flight fetches;
// a couple of seconds of display latency is imperceptible for blocks that last 30s+.
const throttleMonitorInterval = 2 * time.Second

// aggregateThrottleState returns the most severe throttle state across the API
// clients involved in a set fetch (BrickLink for inventory, LEGO for details,
// Pick-a-Brick for prices), along with the latest resume time when blocked.
func aggregateThrottleState() (throttle.State, int64) {
	now := time.Now()
	statuses := []throttle.Status{
		bricklink.C().ThrottlerStatus(),
		lego.C().ThrottlerStatus(),
		pickabrick.C().ThrottlerStatus(),
	}

	worst := throttle.StateNormal
	var resumeAt int64
	for _, s := range statuses {
		state := s.SimpleState(now)
		if state.MoreSevereThan(worst) {
			worst = state
		}
		if state == throttle.StateBlocked && s.ThrottleEndsAt.After(now) {
			if ms := s.ThrottleEndsAt.UnixMilli(); ms > resumeAt {
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
	for {
		select {
		case <-stop:
			if last != throttle.StateNormal {
				h.PushThrottleStatus(rs.ID, throttle.StateNormal, 0)
			}
			return
		case <-ticker.C:
			state, resumeAt := aggregateThrottleState()
			if state == last {
				continue
			}
			last = state
			h.PushThrottleStatus(rs.ID, state, resumeAt)
		}
	}
}
