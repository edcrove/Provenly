package identity

import (
	"fmt"
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
	s = NewService(repo, c.now, func() Config { cfg := testConfig(); cfg.LoginMaxFailures, cfg.LoginWindow = 5, 15*time.Minute; return cfg }())

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
		off.fail("a", now)
	}
	assert.NoError(t, off.check("a", now))
	assert.Empty(t, off.failures)

	tr := newThrottle(3, time.Minute)
	for i := range maxThrottled {
		tr.fail(fmt.Sprint("old", i), now)
	}
	later := now.Add(2 * time.Minute)
	tr.fail("new", later)
	assert.Len(t, tr.failures, 1, "expired names are dropped when the map is full")
	for i := range maxThrottled {
		tr.fail(fmt.Sprint("live", i), later)
	}
	tr.fail("one more", later)
	assert.LessOrEqual(t, len(tr.failures), 2, "a map full of live names starts over rather than grow")
}
