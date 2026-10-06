package identity

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Five failed sign-ins of one username in 15 minutes lock it (even with the right password) until the window
// passes; a success clears the count; other usernames are not affected.
func TestLoginThrottle(t *testing.T) {
	s, repo, c, ctx := setup(t)
	admin(ctx, t, s)
	s = NewService(repo, c.now, func() Config {
		cfg := testConfig()
		cfg.LoginMaxFailures, cfg.LoginWindow = 5, 15*time.Minute
		return cfg
	}())

	for range 4 {
		_, err := s.Login(ctx, "Admin", "wrong password")
		require.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
	}
	_, err := s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err, "four failures do not lock")
	for range 5 {
		_, _ = s.Login(ctx, "admin", "wrong password")
	}
	c.t = c.t.Add(time.Minute)
	_, err = s.Login(ctx, "admin", "correct horse")
	require.Equal(t, apperr.KindTooManyRequests, kindOf(t, err))
	assert.Equal(t, "too many failed sign-ins for this username; try again in 15 minutes", err.Error())
	_, err = s.Login(ctx, "nobody", "x")
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err), "other usernames are not locked")

	c.t = c.t.Add(14*time.Minute + time.Second)
	_, err = s.Login(ctx, "admin", "correct horse")
	assert.NoError(t, err, "the lock ends with the window")

	// Unknown usernames are throttled the same way (no hint that the account exists).
	for range 5 {
		_, _ = s.Login(ctx, "ghost", "x")
	}
	_, err = s.Login(ctx, "ghost", "x")
	assert.Equal(t, apperr.KindTooManyRequests, kindOf(t, err))
}

func TestThrottleDisabledAndBounded(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	off := newThrottle(0, time.Minute)
	for range 10 {
		settle, err := off.attempt("a", now)
		require.NoError(t, err)
		settle(false, true)
	}
	assert.Empty(t, off.failures)

	fail := func(tr *throttle, name string, at time.Time) {
		settle, err := tr.attempt(name, at)
		require.NoError(t, err)
		settle(false, true)
	}
	tr := newThrottle(3, time.Minute)
	for i := range maxThrottled {
		fail(tr, fmt.Sprint("old", i), now)
	}
	later := now.Add(2 * time.Minute)
	fail(tr, "new", later)
	assert.Len(t, tr.failures, 1, "expired names are dropped when the map is full")

	// A flood of made-up names never unlocks a locked username: the names with the fewest failures go first.
	for range 3 {
		fail(tr, "victim", later)
	}
	for i := range maxThrottled + 50 {
		fail(tr, fmt.Sprint("spray", i), later)
	}
	assert.LessOrEqual(t, len(tr.failures), maxThrottled)
	_, err := tr.attempt("victim", later)
	assert.Equal(t, apperr.KindTooManyRequests, kindOf(t, err), "the locked username is still locked")
}

// Parallel wrong passwords cannot all pass the check before any failure is recorded: a burst gets exactly
// MaxFailures guesses, the rest are refused (429).
func TestLoginThrottleHoldsUnderConcurrency(t *testing.T) {
	s, repo, c, ctx := setup(t)
	admin(ctx, t, s)
	s = NewService(repo, c.now, func() Config {
		cfg := testConfig()
		cfg.LoginMaxFailures, cfg.LoginWindow = 5, 15*time.Minute
		return cfg
	}())
	const n = 50
	kinds := make(chan apperr.Kind, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Login(ctx, "admin", "wrong password")
			e, _ := apperr.As(err)
			kinds <- e.Kind
		}()
	}
	close(start)
	wg.Wait()
	close(kinds)
	counts := map[apperr.Kind]int{}
	for k := range kinds {
		counts[k]++
	}
	assert.Equal(t, map[apperr.Kind]int{apperr.KindUnauthorized: 5, apperr.KindTooManyRequests: n - 5}, counts)
}

// A success clears the failures; failures spread over more than the window never lock; an unrelated error does
// not count as a failure.
func TestLoginThrottleSettles(t *testing.T) {
	s, repo, c, ctx := setup(t)
	admin(ctx, t, s)
	s = NewService(repo, c.now, func() Config {
		cfg := testConfig()
		cfg.LoginMaxFailures, cfg.LoginWindow = 5, 15*time.Minute
		return cfg
	}())
	wrong := func(times int) {
		for range times {
			_, err := s.Login(ctx, "admin", "wrong password")
			require.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
		}
	}
	wrong(4)
	_, err := s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err)
	wrong(4)
	_, err = s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err, "the earlier failures were cleared by the success")

	wrong(4)
	c.t = c.t.Add(14 * time.Minute)
	wrong(1)
	c.t = c.t.Add(time.Minute + time.Second)
	wrong(1)
	_, err = s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err, "the window slides: the first four failures expired")

	repo.errs["GetUserByUsername"] = errors.New("database down")
	_, err = s.Login(ctx, "admin", "correct horse")
	require.EqualError(t, err, "database down")
	delete(repo.errs, "GetUserByUsername")
	wrong(1)
	repo.errs["GetUserByUsername"] = errors.New("database down")
	for range 10 {
		_, err = s.Login(ctx, "admin", "correct horse")
		require.EqualError(t, err, "database down")
	}
	delete(repo.errs, "GetUserByUsername")
	_, err = s.Login(ctx, "admin", "correct horse")
	assert.NoError(t, err, "infrastructure errors are not counted as failed sign-ins (the earlier failure stays)")
}
