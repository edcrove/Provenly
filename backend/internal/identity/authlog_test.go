package identity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedAuth struct{ events []AuthEvent }

func (r *recordedAuth) AuthEvent(_ context.Context, e AuthEvent) { r.events = append(r.events, e) }

// Sign-ins, failures, lockouts, sign-outs, accepted invitations and password resets are reported, with the username
// only when that account exists (card #49).
func TestAuthEvents(t *testing.T) {
	s, repo, c, ctx := setup(t)
	a := admin(ctx, t, s)
	s = NewService(repo, c.now, func() Config {
		cfg := testConfig()
		cfg.LoginMaxFailures, cfg.LoginWindow = 2, 15*time.Minute
		return cfg
	}())
	log := &recordedAuth{}
	s.Logout(ctx, "no-log-yet") // without a log nothing breaks
	s.SetAuthLog(log)

	session, err := s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err)
	_, _ = s.Login(ctx, "admin", "wrong password")
	_, _ = s.Login(ctx, "ghost", "x")
	_, _ = s.Login(ctx, "admin", "wrong password")
	_, _ = s.Login(ctx, "admin", "correct horse") // locked out now
	for range 2 {
		_, _ = s.Login(ctx, "ghost", "x")
	}
	s.Logout(ctx, session.Token)
	s.Logout(ctx, "")
	s.Logout(ctx, "not-a-token")

	_, token, err := s.CreateInvitation(ctx, a, CreateInvitationInput{})
	require.NoError(t, err)
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "ana", DisplayName: "Ana", Password: "ana password"})
	require.NoError(t, err)
	_, link, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	_, err = s.ResetPassword(ctx, link, "ana's new password")
	require.NoError(t, err)

	assert.Equal(t, []AuthEvent{
		{Kind: AuthLogin, Username: "admin", Status: 200},
		{Kind: AuthLoginFailed, Username: "admin", Status: 401},
		{Kind: AuthLoginFailed, Username: "", Status: 401},
		{Kind: AuthLoginFailed, Username: "admin", Status: 401},
		{Kind: AuthLockedOut, Username: "admin", Status: 429},
		{Kind: AuthLoginFailed, Username: "", Status: 401},
		{Kind: AuthLockedOut, Username: "", Status: 429},
		{Kind: AuthLogout, Username: "admin", Status: 204},
		{Kind: AuthInvitationAccepted, Username: "ana", Status: 201},
		{Kind: AuthPasswordReset, Username: "ana", Status: 200},
	}, log.events)
	assert.Empty(t, s.existing(ctx, "bad\x00name"))
}
