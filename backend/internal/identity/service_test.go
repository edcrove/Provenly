package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

var errBoom = errors.New("boom")

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func testConfig() Config {
	return Config{Secret: []byte(strings.Repeat("s", 32)), SessionTTL: time.Hour, InvitationTTL: 24 * time.Hour, BcryptCost: bcrypt.MinCost}
}

func setup(t *testing.T) (*Service, *fakeRepo, *clock, context.Context) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	repo := newFakeRepo(c.now)
	return NewService(repo, c.now, testConfig()), repo, c, context.Background()
}

func kindOf(t *testing.T, err error) apperr.Kind {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	return e.Kind
}

func admin(ctx context.Context, t *testing.T, s *Service) User {
	t.Helper()
	require.NoError(t, s.Bootstrap(ctx, " Admin ", "correct horse"))
	sess, err := s.Login(ctx, "admin", "correct horse")
	require.NoError(t, err)
	return sess.User
}

func TestBootstrapCreatesTheFirstAdminOnce(t *testing.T) {
	s, repo, _, ctx := setup(t)
	u := admin(ctx, t, s)
	assert.Equal(t, "admin", u.Username, "usernames are trimmed and lower-cased")
	assert.True(t, u.IsAdmin)
	require.NoError(t, s.Bootstrap(ctx, "other", "another password"), "with users present it does nothing")
	assert.Len(t, repo.users, 1)

	s2, repo2, _, _ := setup(t)
	err := s2.Bootstrap(ctx, "x", "short")
	assert.ErrorContains(t, err, "PROVENLY_ADMIN_USERNAME")
	assert.Empty(t, repo2.users)

	repo2.errs["CountUsers"] = errBoom
	assert.ErrorIs(t, s2.Bootstrap(ctx, "admin", "correct horse"), errBoom)
	delete(repo2.errs, "CountUsers")
	repo2.errs["CreateUser"] = ErrConflict
	assert.NoError(t, s2.Bootstrap(ctx, "admin", "correct horse"), "another instance created it first")
	repo2.errs["CreateUser"] = errBoom
	assert.ErrorIs(t, s2.Bootstrap(ctx, "admin", "correct horse"), errBoom)
	s3 := NewService(repo2, time.Now, Config{BcryptCost: 99})
	delete(repo2.errs, "CreateUser")
	assert.Error(t, s3.Bootstrap(ctx, "admin", "correct horse"), "an invalid bcrypt cost is reported")
}

func TestLoginAndAuthenticate(t *testing.T) {
	s, repo, c, ctx := setup(t)
	u := admin(ctx, t, s)

	for _, bad := range [][2]string{{"admin", "wrong password"}, {"nobody", "correct horse"}} {
		_, err := s.Login(ctx, bad[0], bad[1])
		assert.Equal(t, "invalid username or password", err.Error(), "the same answer whether or not the user exists")
		assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
	}
	// A username that cannot exist (NUL, invalid UTF-8, too long) is a failed sign-in, never a database error.
	repo.errs["GetUserByUsername"] = errBoom
	for _, name := range []string{"admin\x00", "\xffadmin", strings.Repeat("a", 10000)} {
		_, err := s.Login(ctx, name, "correct horse")
		assert.Equal(t, "invalid username or password", err.Error(), "%q", name)
	}
	delete(repo.errs, "GetUserByUsername")
	sess, err := s.Login(ctx, "ADMIN", "correct horse")
	require.NoError(t, err)
	assert.Equal(t, c.t.Add(time.Hour), sess.ExpiresAt)
	got, err := s.Authenticate(ctx, sess.Token)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)

	// Expired, tampered, foreign-signed, other algorithms, wrong issuer and missing users are all "sign in".
	c.t = c.t.Add(time.Hour + time.Second)
	_, err = s.Authenticate(ctx, sess.Token)
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
	c.t = c.t.Add(-time.Hour)
	for _, token := range []string{"", "not-a-jwt", sess.Token + "x", sign(t, "other-secret-other-secret-other-s", jwt.SigningMethodHS256, "provenly", "1"),
		sign(t, string(testConfig().Secret), jwt.SigningMethodHS384, "provenly", "1"),
		sign(t, string(testConfig().Secret), jwt.SigningMethodHS256, "someone-else", "1"),
		sign(t, string(testConfig().Secret), jwt.SigningMethodHS256, "provenly", "abc"),
		sign(t, string(testConfig().Secret), jwt.SigningMethodHS256, "provenly", "999")} {
		_, err := s.Authenticate(ctx, token)
		assert.Equal(t, "sign in to continue", err.Error(), token)
	}
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{Issuer: "provenly", Subject: "1",
		ExpiresAt: jwt.NewNumericDate(c.t.Add(time.Hour))}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	_, err = s.Authenticate(ctx, none)
	assert.Error(t, err, "alg=none is never accepted")

	repo.errs["GetUser"] = errBoom
	_, err = s.Authenticate(ctx, sess.Token)
	assert.ErrorIs(t, err, errBoom)
	repo.errs["GetUserByUsername"] = errBoom
	_, err = s.Login(ctx, "admin", "correct horse")
	assert.ErrorIs(t, err, errBoom)
}

func sign(t *testing.T, secret string, m jwt.SigningMethod, iss, sub string) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(m, jwt.RegisteredClaims{Issuer: iss, Subject: sub,
		ExpiresAt: jwt.NewNumericDate(time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC))}).SignedString([]byte(secret))
	require.NoError(t, err)
	return tok
}

func TestChangePasswordSignsOtherSessionsOut(t *testing.T) {
	s, repo, _, ctx := setup(t)
	u := admin(ctx, t, s)
	old, _ := s.Login(ctx, "admin", "correct horse")

	_, err := s.ChangePassword(ctx, u, "wrong", "a brand new password")
	e, _ := apperr.As(err)
	assert.Equal(t, []apperr.FieldError{{Field: "currentPassword", Message: "is not your current password"}}, e.Fields)
	_, err = s.ChangePassword(ctx, u, "correct horse", "short")
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))

	fresh, err := s.ChangePassword(ctx, u, "correct horse", "a brand new password")
	require.NoError(t, err)
	_, err = s.Authenticate(ctx, old.Token)
	assert.Error(t, err, "sessions issued before the change are no longer valid")
	_, err = s.Authenticate(ctx, fresh.Token)
	assert.NoError(t, err)
	_, err = s.Login(ctx, "admin", "a brand new password")
	assert.NoError(t, err)

	repo.errs["SetPasswordHash"] = errBoom
	_, err = s.ChangePassword(ctx, fresh.User, "a brand new password", "yet another password")
	assert.ErrorIs(t, err, errBoom)
	bad := NewService(repo, time.Now, Config{BcryptCost: 99})
	_, err = bad.ChangePassword(ctx, User{PasswordHash: fresh.User.PasswordHash}, "a brand new password", "yet another password")
	assert.Error(t, err)
}

func TestPasswordRules(t *testing.T) {
	var v apperr.Validator
	validatePassword(&v, "password", strings.Repeat("x", MinPasswordBytes))
	validatePassword(&v, "password", strings.Repeat("x", MaxPasswordBytes))
	assert.NoError(t, v.Err())
	for _, p := range []string{"", strings.Repeat("x", MinPasswordBytes-1), strings.Repeat("x", MaxPasswordBytes+1), "abcdefghij\x00", "\xffabcdefghij"} {
		var v apperr.Validator
		validatePassword(&v, "password", p)
		assert.Error(t, v.Err(), "%q", p)
	}
}

func TestInvitationsLifecycle(t *testing.T) {
	s, repo, c, ctx := setup(t)
	a := admin(ctx, t, s)
	email := " Ana@Example.com "
	inv, token, err := s.CreateInvitation(ctx, a, CreateInvitationInput{Email: &email, Note: " QA team "})
	require.NoError(t, err)
	assert.Len(t, token, 43, "32 random bytes, base64url")
	assert.Equal(t, TokenDigest(token), inv.TokenSHA256, "only the digest is stored")
	assert.Equal(t, "Ana@Example.com", *inv.Email)
	assert.Equal(t, "QA team", inv.Note)
	assert.Equal(t, c.t.Add(24*time.Hour), inv.ExpiresAt)
	assert.Equal(t, InvitationPending, inv.Status(c.t))

	page, err := s.ListInvitations(ctx, a, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)

	sess, err := s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: " Ana ", DisplayName: " Ana Pérez ", Password: "ana's password"})
	require.NoError(t, err)
	assert.Equal(t, "ana", sess.User.Username)
	assert.Equal(t, "Ana Pérez", sess.User.DisplayName)
	assert.Equal(t, "Ana@Example.com", *sess.User.Email, "the invitation's email is kept when none is given")
	assert.False(t, sess.User.IsAdmin)
	got, _ := repo.GetInvitation(ctx, inv.ID)
	assert.Equal(t, InvitationAccepted, got.Status(c.t))
	assert.Equal(t, sess.User.ID, *got.AcceptedUserID)

	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "ana2", DisplayName: "x", Password: "ana's password"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err), "a link is used once")
	_, err = s.RevokeInvitation(ctx, a, inv.ID)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "an accepted invitation cannot be revoked")

	// Non-admins cannot manage invitations or list users.
	_, _, err = s.CreateInvitation(ctx, sess.User, CreateInvitationInput{})
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.ListInvitations(ctx, sess.User, pagination.Default())
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.RevokeInvitation(ctx, sess.User, inv.ID)
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.ListUsers(ctx, sess.User, pagination.Default())
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	users, err := s.ListUsers(ctx, a, pagination.Page{Number: 1, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), users.Total)
	assert.Equal(t, "admin", users.Items[0].Username)

	// Revoked and expired links are unusable; a revoke of an unknown id is 404.
	inv2, token2, _ := s.CreateInvitation(ctx, a, CreateInvitationInput{})
	assert.Nil(t, inv2.Email)
	revoked, err := s.RevokeInvitation(ctx, a, inv2.ID)
	require.NoError(t, err)
	assert.Equal(t, InvitationRevoked, revoked.Status(c.t))
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token2, Username: "bob", DisplayName: "Bob", Password: "bob's password"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.RevokeInvitation(ctx, a, 999)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	inv3, token3, _ := s.CreateInvitation(ctx, a, CreateInvitationInput{})
	c.t = inv3.ExpiresAt
	assert.Equal(t, InvitationExpired, inv3.Status(c.t))
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token3, Username: "bob", DisplayName: "Bob", Password: "bob's password"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: "unknown", Username: "bob", DisplayName: "Bob", Password: "bob's password"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestAcceptInvitationValidationAndConflicts(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	_, token, _ := s.CreateInvitation(ctx, a, CreateInvitationInput{})

	_, err := s.AcceptInvitation(ctx, AcceptInput{})
	e, _ := apperr.As(err)
	fields := map[string]bool{}
	for _, f := range e.Fields {
		fields[f.Field] = true
	}
	assert.Equal(t, map[string]bool{"token": true, "username": true, "displayName": true, "password": true}, fields)
	bad := "not-an-email"
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "ok_user", DisplayName: strings.Repeat("d", 101), Email: &bad, Password: "a long password"})
	e, _ = apperr.As(err)
	assert.Len(t, e.Fields, 2)

	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "admin", DisplayName: "Twin", Password: "a long password"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	got, _ := repo.LockInvitationByToken(ctx, TokenDigest(token))
	assert.Nil(t, got.AcceptedAt, "the transaction rolled back: the link still works")

	blank := "  "
	sess, err := s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "carla", DisplayName: "Carla", Email: &blank, Password: "a long password"})
	require.NoError(t, err)
	assert.Nil(t, sess.User.Email, "a blank email is no email")

	for _, method := range []string{"InTx", "LockInvitationByToken", "CreateUser", "MarkInvitationAccepted"} {
		_, token, _ := s.CreateInvitation(ctx, a, CreateInvitationInput{})
		repo.errs[method] = errBoom
		_, err := s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "dan_" + strings.ToLower(method[:3]), DisplayName: "Dan", Password: "a long password"})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	broken := NewService(repo, time.Now, Config{BcryptCost: 99})
	_, err = broken.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "eve", DisplayName: "Eve", Password: "a long password"})
	assert.Error(t, err)
	// A made-up token is refused before any password hashing (the broken hasher is never reached).
	_, err = broken.AcceptInvitation(ctx, AcceptInput{Token: "pvi_made_up", Username: "eve", DisplayName: "Eve", Password: "a long password"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestInvitationValidationAndRepositoryFailures(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	bad := "x@"
	_, _, err := s.CreateInvitation(ctx, a, CreateInvitationInput{Email: &bad, Note: strings.Repeat("n", 201)})
	e, _ := apperr.As(err)
	assert.Len(t, e.Fields, 2)
	long := strings.Repeat("a", 250) + "@b.co"
	_, _, err = s.CreateInvitation(ctx, a, CreateInvitationInput{Email: &long})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))

	cases := []struct {
		method string
		call   func() error
	}{
		{"CreateInvitation", func() error { _, _, err := s.CreateInvitation(ctx, a, CreateInvitationInput{}); return err }},
		{"CountInvitations", func() error { _, err := s.ListInvitations(ctx, a, pagination.Default()); return err }},
		{"ListInvitations", func() error { _, err := s.ListInvitations(ctx, a, pagination.Default()); return err }},
		{"CountUsers", func() error { _, err := s.ListUsers(ctx, a, pagination.Default()); return err }},
		{"ListUsers", func() error { _, err := s.ListUsers(ctx, a, pagination.Default()); return err }},
		{"RevokeInvitation", func() error { _, err := s.RevokeInvitation(ctx, a, 1); return err }},
	}
	for _, c := range cases {
		repo.errs[c.method] = errBoom
		assert.ErrorIs(t, c.call(), errBoom, c.method)
		delete(repo.errs, c.method)
	}
	repo.errs["GetInvitation"] = errBoom
	_, err = s.RevokeInvitation(ctx, a, 1)
	assert.ErrorIs(t, err, errBoom)
}

func TestDefaultsAndContext(t *testing.T) {
	cfg := DefaultConfig([]byte("k"))
	assert.Equal(t, 12*time.Hour, cfg.SessionTTL)
	assert.Equal(t, 7*24*time.Hour, cfg.InvitationTTL)
	assert.Equal(t, 12, cfg.BcryptCost)
	assert.Len(t, RandomSecret(), 32)
	assert.NotEqual(t, RandomSecret(), RandomSecret())

	_, ok := UserFrom(context.Background())
	assert.False(t, ok)
	u, ok := UserFrom(WithUser(context.Background(), User{ID: 7}))
	assert.True(t, ok)
	assert.Equal(t, int64(7), u.ID)
}
