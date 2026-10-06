package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// invite creates a user through an invitation and returns their session.
func invite(ctx context.Context, t *testing.T, s *Service, a User, username string) Session {
	t.Helper()
	_, tok, err := s.CreateInvitation(ctx, a, CreateInvitationInput{})
	require.NoError(t, err)
	sess, err := s.AcceptInvitation(ctx, AcceptInput{Token: tok, Username: username, DisplayName: username, Password: username + " password"})
	require.NoError(t, err)
	return sess
}

// Deactivation (card #61): sessions stop at once, sign-in gets the generic answer, reset links stop working;
// reactivation gives access back.
func TestDeactivate(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	ana := invite(ctx, t, s, a, "ana")
	link, _, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)

	_, err = s.Deactivate(ctx, ana.User, "admin")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err), "administrators only")
	_, err = s.Deactivate(ctx, a, "nobody")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.Deactivate(ctx, a, "BAD NAME")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.Deactivate(ctx, a, "admin")
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "not yourself")

	u, err := s.Deactivate(ctx, a, " Ana ")
	require.NoError(t, err)
	require.NotNil(t, u.DeactivatedAt)
	_, err = s.Authenticate(ctx, ana.Token)
	assert.Equal(t, errSignIn, err, "the session stops at once")
	_, err = s.Login(ctx, "ana", "ana password")
	assert.Equal(t, errBadCredentials, err, "the same answer as a wrong password")
	assert.NotNil(t, repo.resets[link.ID].UsedAt, "pending reset links stop working")
	again, err := s.Deactivate(ctx, a, "ana")
	require.NoError(t, err)
	assert.Equal(t, u.DeactivatedAt, again.DeactivatedAt, "deactivating twice changes nothing")
	_, _, err = s.CreatePasswordReset(ctx, a, "ana")
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "no reset link for a deactivated user")

	back, err := s.Reactivate(ctx, a, "ana")
	require.NoError(t, err)
	assert.Nil(t, back.DeactivatedAt)
	_, err = s.Login(ctx, "ana", "ana password")
	require.NoError(t, err)
	same, err := s.Reactivate(ctx, a, "ana")
	require.NoError(t, err)
	assert.Nil(t, same.DeactivatedAt)
	_, err = s.Reactivate(ctx, ana.User, "ana")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.Reactivate(ctx, a, "nobody")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

// The last active administrator stays: with two, one can go; then the other cannot.
func TestDeactivateKeepsAnAdministrator(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	bob := invite(ctx, t, s, a, "bob")
	b := repo.users[bob.User.ID]
	b.IsAdmin = true
	repo.users[b.ID] = b
	_, err := s.Deactivate(ctx, a, "bob")
	require.NoError(t, err, "two active administrators: one can go")
	_, err = s.Deactivate(ctx, b, "admin")
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "admin is now the last active one")
	assert.Nil(t, repo.users[a.ID].DeactivatedAt)
}

func TestDeactivateErrors(t *testing.T) {
	for _, m := range []string{"InTx", "GetUserByUsername", "CountActiveAdmins", "SetUserDeactivated", "VoidPasswordResets"} {
		s, repo, _, ctx := setup(t)
		a := admin(ctx, t, s)
		b := invite(ctx, t, s, a, "bob")
		u := repo.users[b.User.ID]
		u.IsAdmin = true
		repo.users[u.ID] = u
		repo.errs[m] = errBoom
		_, err := s.Deactivate(ctx, a, "bob")
		assert.ErrorIs(t, err, errBoom, m)
	}
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	invite(ctx, t, s, a, "bob")
	_, err := s.Deactivate(ctx, a, "bob")
	require.NoError(t, err)
	repo.errs["SetUserDeactivated"] = errBoom
	_, err = s.Reactivate(ctx, a, "bob")
	assert.ErrorIs(t, err, errBoom)
}

// A reset link works once, before it expires, for an active user, and signs every older session out.
func TestResetPassword(t *testing.T) {
	s, repo, c, ctx := setup(t)
	a := admin(ctx, t, s)
	ana := invite(ctx, t, s, a, "ana")
	_, _, err := s.CreatePasswordReset(ctx, ana.User, "ana")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, _, err = s.CreatePasswordReset(ctx, a, "nobody")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	first, oldTok, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	link, tok, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	assert.Equal(t, c.t.Add(24*time.Hour), link.ExpiresAt)
	assert.Equal(t, &a.ID, link.CreatedBy)
	assert.NotNil(t, repo.resets[first.ID].UsedAt, "a new link voids the earlier one")
	_, err = s.ResetPassword(ctx, oldTok, "a new password")
	assert.Equal(t, errResetLink, err)

	_, err = s.ResetPassword(ctx, "", "short")
	fields := map[string]bool{}
	e, _ := apperr.As(err)
	for _, f := range e.Fields {
		fields[f.Field] = true
	}
	assert.Equal(t, map[string]bool{"token": true, "password": true}, fields)
	_, err = s.ResetPassword(ctx, "made up", "a new password")
	assert.Equal(t, errResetLink, err)

	sess, err := s.ResetPassword(ctx, tok, "a new password")
	require.NoError(t, err)
	assert.Equal(t, "ana", sess.User.Username)
	_, err = s.Authenticate(ctx, ana.Token)
	assert.Equal(t, errSignIn, err, "older sessions end")
	_, err = s.Login(ctx, "ana", "a new password")
	require.NoError(t, err)
	_, err = s.ResetPassword(ctx, tok, "another password")
	assert.Equal(t, errResetLink, err, "once")

	_, late, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	c.t = c.t.Add(24 * time.Hour)
	_, err = s.ResetPassword(ctx, late, "a third password")
	assert.Equal(t, errResetLink, err, "expired")

	_, gone, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	u := repo.users[ana.User.ID]
	now := c.t
	u.DeactivatedAt = &now
	repo.users[u.ID] = u
	_, err = s.ResetPassword(ctx, gone, "a third password")
	assert.Equal(t, errResetLink, err, "a deactivated user's link does nothing")
}

func TestResetPasswordErrors(t *testing.T) {
	for _, m := range []string{"InTx", "LockPasswordResetByToken", "GetUser", "SetPasswordHash", "MarkPasswordResetUsed", "VoidPasswordResets"} {
		s, repo, _, ctx := setup(t)
		a := admin(ctx, t, s)
		invite(ctx, t, s, a, "ana")
		_, tok, err := s.CreatePasswordReset(ctx, a, "ana")
		require.NoError(t, err)
		repo.errs[m] = errBoom
		_, err = s.ResetPassword(ctx, tok, "a new password")
		assert.ErrorIs(t, err, errBoom, m)
	}
	for _, m := range []string{"InTx", "VoidPasswordResets", "CreatePasswordReset", "GetUserByUsername"} {
		s, repo, _, ctx := setup(t)
		a := admin(ctx, t, s)
		invite(ctx, t, s, a, "ana")
		repo.errs[m] = errBoom
		_, _, err := s.CreatePasswordReset(ctx, a, "ana")
		assert.ErrorIs(t, err, errBoom, m)
	}
	s, _, _, ctx := setup(t)
	a := admin(ctx, t, s)
	invite(ctx, t, s, a, "ana")
	_, tok, err := s.CreatePasswordReset(ctx, a, "ana")
	require.NoError(t, err)
	s.cfg.BcryptCost = 99
	_, err = s.ResetPassword(ctx, tok, "a new password")
	assert.Error(t, err, "the hash fails")
}

// The break-glass command makes a link for any user, reactivating them, without an administrator.
func TestBreakGlassReset(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	invite(ctx, t, s, a, "bob")
	_, err := s.Deactivate(ctx, a, "bob")
	require.NoError(t, err)
	link, tok, err := s.BreakGlassReset(ctx, "bob")
	require.NoError(t, err)
	assert.Nil(t, link.CreatedBy)
	_, err = s.ResetPassword(ctx, tok, "bob's new password")
	require.NoError(t, err)
	_, err = s.Login(ctx, "bob", "bob's new password")
	require.NoError(t, err)

	_, _, err = s.BreakGlassReset(ctx, "nobody")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.Deactivate(ctx, a, "bob")
	require.NoError(t, err)
	repo.errs["SetUserDeactivated"] = errBoom
	_, _, err = s.BreakGlassReset(ctx, "bob")
	assert.ErrorIs(t, err, errBoom)
}

func TestOffboardingHandlers(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.login("admin", "correct horse")
	ctx := context.Background()
	a, err := h.svc.Authenticate(ctx, tok)
	require.NoError(t, err)
	ana := invite(ctx, t, h.svc, a, "ana")

	rec := h.do("POST", "/api/v1/users/ana/deactivate", "", bearer(tok))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"username":"ana"`)
	assert.NotContains(t, rec.Body.String(), `"deactivatedAt":null`)
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", bearer(ana.Token)).Code)
	assert.Equal(t, http.StatusConflict, h.do("POST", "/api/v1/users/ana/password-reset", "", bearer(tok)).Code)
	rec = h.do("POST", "/api/v1/users/ana/reactivate", "", bearer(tok))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"deactivatedAt":null`)
	assert.Equal(t, http.StatusNotFound, h.do("POST", "/api/v1/users/nobody/deactivate", "", bearer(tok)).Code)
	assert.Equal(t, http.StatusUnauthorized, h.do("POST", "/api/v1/users/ana/deactivate", "").Code)

	rec = h.do("POST", "/api/v1/users/ana/password-reset", "", bearer(tok))
	require.Equal(t, http.StatusCreated, rec.Code)
	var link struct{ Username, Token string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &link))
	assert.Equal(t, "ana", link.Username)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.do("POST", "/api/v1/password-reset", "").Code)
	assert.Equal(t, http.StatusBadRequest, h.do("POST", "/api/v1/password-reset", `{"token":"","password":"x"}`).Code)
	assert.Equal(t, http.StatusNotFound, h.do("POST", "/api/v1/password-reset", `{"token":"made-up","password":"a new password"}`).Code)
	rec = h.do("POST", "/api/v1/password-reset", `{"token":"`+link.Token+`","password":"a new password"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "provenly_session=", "signed in")
}
