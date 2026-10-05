package identity

import (
	"sync"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// maxThrottled bounds the usernames the sign-in throttle remembers (memory under a flood of made-up names).
const maxThrottled = 10_000

// throttle counts failed sign-ins per username: after MaxFailures within Window the username is refused (429)
// until the oldest of those failures leaves the window. A successful sign-in clears it. It lives in memory: one
// instance, reset on restart (the prototype's deployment).
type throttle struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
}

func newThrottle(maxFailures int, window time.Duration) *throttle {
	return &throttle{max: maxFailures, window: window, failures: map[string][]time.Time{}}
}

// recent returns the failures of username still inside the window at now (and forgets the older ones).
func (t *throttle) recent(username string, now time.Time) []time.Time {
	kept := t.failures[username][:0]
	for _, at := range t.failures[username] {
		if now.Sub(at) < t.window {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(t.failures, username)
		return nil
	}
	t.failures[username] = kept
	return kept
}

// check refuses a username with too many recent failures.
func (t *throttle) check(username string, now time.Time) error {
	if t.max <= 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	recent := t.recent(username, now)
	if len(recent) < t.max {
		return nil
	}
	wait := t.window - now.Sub(recent[0])
	return apperr.TooManyRequests("too many failed sign-ins for this username; try again in %d minutes", int(wait.Minutes())+1)
}

// fail records a failed sign-in.
func (t *throttle) fail(username string, now time.Time) {
	if t.max <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.failures) >= maxThrottled {
		for name := range t.failures {
			t.recent(name, now)
		}
		if len(t.failures) >= maxThrottled {
			t.failures = map[string][]time.Time{}
		}
	}
	t.failures[username] = append(t.recent(username, now), now)
}

// succeed clears a username's failures.
func (t *throttle) succeed(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, username)
}
