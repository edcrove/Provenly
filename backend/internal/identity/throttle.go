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
//
// An attempt is reserved (counted as a failure) before the password is checked and settled afterwards, under the
// same lock, so parallel guesses cannot all pass the check before any of them is recorded.
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

// attempt reserves a sign-in attempt for username, or refuses it when the username has too many recent failures.
// The returned settle records the outcome: failed keeps the reservation as a failure, ok clears the username, and
// neither (an unrelated error) releases the reservation.
func (t *throttle) attempt(username string, now time.Time) (settle func(ok, failed bool), err error) {
	if t.max <= 0 {
		return func(bool, bool) {}, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	recent := t.recent(username, now)
	if len(recent) >= t.max {
		wait := t.window - now.Sub(recent[0])
		return nil, apperr.TooManyRequests("too many failed sign-ins for this username; try again in %d minutes", int(wait.Minutes())+1)
	}
	if _, tracked := t.failures[username]; !tracked {
		t.makeRoom(now)
	}
	t.failures[username] = append(recent, now)
	return func(ok, failed bool) {
		t.mu.Lock()
		defer t.mu.Unlock()
		switch {
		case ok:
			delete(t.failures, username)
		case !failed:
			t.release(username, now)
		}
	}, nil
}

// release removes one reservation made at "at".
func (t *throttle) release(username string, at time.Time) {
	times := t.failures[username]
	for i, v := range times {
		if v.Equal(at) {
			times = append(times[:i], times[i+1:]...)
			break
		}
	}
	if len(times) == 0 {
		delete(t.failures, username)
		return
	}
	t.failures[username] = times
}

// makeRoom keeps the map under maxThrottled before a new username is tracked: expired names go first, then the
// names with the fewest recent failures (oldest first), so a flood of made-up names never unlocks a locked one.
func (t *throttle) makeRoom(now time.Time) {
	if len(t.failures) < maxThrottled {
		return
	}
	for name := range t.failures {
		t.recent(name, now)
	}
	for len(t.failures) >= maxThrottled {
		victim, fewest, oldest := "", 0, time.Time{}
		for name, times := range t.failures {
			last := times[len(times)-1]
			if victim == "" || len(times) < fewest || (len(times) == fewest && last.Before(oldest)) {
				victim, fewest, oldest = name, len(times), last
			}
		}
		delete(t.failures, victim)
	}
}
