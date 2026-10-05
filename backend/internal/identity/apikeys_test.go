package identity

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestAPIKeys(t *testing.T) {
	s, repo, c, ctx := setup(t)
	asAdmin := WithUser(ctx, admin(ctx, t, s))
	asAna, ana := member(ctx, t, s, repo, "ana")
	require.NoError(t, repo.UpsertMember(ctx, 1, ana.ID, authz.RoleMaintainer))
	asBob, bob := member(ctx, t, s, repo, "bob")
	require.NoError(t, repo.UpsertMember(ctx, 1, bob.ID, authz.RoleMember))

	k, token, err := s.CreateAPIKey(asAna, 1, "  GitHub Actions  ")
	require.NoError(t, err)
	assert.Equal(t, "GitHub Actions", k.Name)
	assert.Regexp(t, `^pvk_[0-9a-f]{8}_[A-Za-z0-9_-]{43}$`, token)
	assert.Equal(t, token[:12], k.Prefix)
	assert.Equal(t, ana.ID, k.CreatedBy)
	assert.Equal(t, TokenDigest(token), k.TokenSHA256, "only the digest is stored")
	_, other, _ := s.CreateAPIKey(asAdmin, 1, "nightly")
	assert.NotEqual(t, token, other)

	page, err := s.ListAPIKeys(asAna, 1, pagination.Page{Number: 1, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, "nightly", page.Items[0].Name, "newest first")

	// Using a key records it and authenticates as the key, not as a person.
	c.t = c.t.Add(time.Hour)
	got, err := s.AuthenticateKey(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, k.ID, got.ID)
	assert.Equal(t, c.t, *repo.keys[k.ID].LastUsedAt)
	for _, bad := range []string{"", "pvk_", token + "x", strings.Replace(token, "pvk_", "pvx_", 1), tampered(token)} {
		_, err := s.AuthenticateKey(ctx, bad)
		assert.Equal(t, errBadKey, err, bad)
	}

	revoked, err := s.RevokeAPIKey(asAna, 1, k.ID)
	require.NoError(t, err)
	assert.NotNil(t, revoked.RevokedAt)
	_, err = s.AuthenticateKey(ctx, token)
	assert.Equal(t, errBadKey, err, "a revoked key stops working at once")
	_, err = s.RevokeAPIKey(asAna, 1, k.ID)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	_, err = s.RevokeAPIKey(asAna, 1, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.RevokeAPIKey(asAdmin, 2, k.ID)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err), "a key is revoked through its own project")

	// Only maintainers manage keys; other projects are invisible.
	_, _, err = s.CreateAPIKey(asBob, 1, "x")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.ListAPIKeys(asBob, 1, pagination.Default())
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = s.RevokeAPIKey(asBob, 1, 2)
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, _, err = s.CreateAPIKey(asAna, 2, "x")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	for _, name := range []string{"", "   ", strings.Repeat("n", 101), "a\x00b"} {
		_, _, err := s.CreateAPIKey(asAna, 1, name)
		e, _ := apperr.As(err)
		require.NotNil(t, e, "%q", name)
		assert.Equal(t, "name", e.Fields[0].Field)
	}
	_, _, err = s.CreateAPIKey(asAna, 1, strings.Repeat("ñ", 100))
	assert.NoError(t, err, "100 characters, not bytes")

	for method, call := range map[string]func() error{
		"CreateAPIKey":     func() error { _, _, err := s.CreateAPIKey(asAdmin, 1, "x"); return err },
		"CountAPIKeys":     func() error { _, err := s.ListAPIKeys(asAdmin, 1, pagination.Default()); return err },
		"ListAPIKeys":      func() error { _, err := s.ListAPIKeys(asAdmin, 1, pagination.Default()); return err },
		"RevokeAPIKey":     func() error { _, err := s.RevokeAPIKey(asAdmin, 1, 2); return err },
		"GetAPIKey":        func() error { _, err := s.RevokeAPIKey(asAdmin, 1, k.ID); return err },
		"GetAPIKeyByToken": func() error { _, err := s.AuthenticateKey(ctx, other); return err },
		"TouchAPIKey":      func() error { _, err := s.AuthenticateKey(ctx, other); return err },
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
}

func TestAPIKeyAccess(t *testing.T) {
	s, _, _, ctx := setup(t)
	gone := apperr.NotFound("gone")
	asKey := WithAPIKey(ctx, APIKey{ID: 7, ProjectID: 2})

	id, ok := s.KeyProject(asKey)
	assert.True(t, ok)
	assert.Equal(t, int64(2), id)
	_, ok = s.KeyProject(ctx)
	assert.False(t, ok)

	assert.NoError(t, s.Require(asKey, 2, authz.RoleMember, gone), "a key reports runs into its project")
	assert.Equal(t, gone, s.Require(asKey, 1, authz.RoleViewer, gone), "and sees no other")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, s.Require(asKey, 2, authz.RoleMaintainer, gone)))
}

func TestAPIKeyRoutes(t *testing.T) {
	h := newHarness(t)
	// A route CI calls: it accepts a session or a key and echoes who called.
	ProtectWithKeys(h.mux, h.svc).HandleFunc("POST /ci", func(w http.ResponseWriter, r *http.Request) {
		if k, ok := APIKeyFrom(r.Context()); ok {
			_, _ = w.Write([]byte("key " + k.Name))
			return
		}
		u, _ := UserFrom(r.Context())
		_, _ = w.Write([]byte("user " + u.Username))
	})
	token, _ := h.login("admin", "correct horse")
	auth := bearer(token)

	rec := h.do("POST", "/api/v1/projects/CHK/api-keys", `{"name":"GitHub Actions"}`, auth)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		APIKey struct {
			ID     int64
			Prefix string
			Status string
		}
		Token string
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "active", created.APIKey.Status)
	assert.True(t, strings.HasPrefix(created.Token, created.APIKey.Prefix))
	assert.NotContains(t, rec.Body.String(), "tokenSha256")

	assert.Equal(t, "key GitHub Actions", h.do("POST", "/ci", "", bearer(created.Token)).Body.String())
	assert.Equal(t, "user admin", h.do("POST", "/ci", "", auth).Body.String())
	assert.Equal(t, http.StatusUnauthorized, h.do("POST", "/ci", "", bearer("pvk_nope")).Code)
	assert.Equal(t, http.StatusUnauthorized, h.do("POST", "/ci", "").Code)
	// Found by the probe sweep: a key sent as the session cookie used to be accepted.
	assert.Equal(t, http.StatusUnauthorized, h.do("POST", "/ci", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: created.Token})
	}).Code, "a key is only read from the Authorization header")
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", bearer(created.Token)).Code,
		"a key is not a session: it only opens the routes CI calls")

	rec = h.do("GET", "/api/v1/projects/CHK/api-keys", "", auth)
	assert.Contains(t, rec.Body.String(), `"totalItems":1`)
	assert.Contains(t, rec.Body.String(), `"lastUsedAt":"`)
	rec = h.do("POST", "/api/v1/projects/CHK/api-keys/1/revoke", "", auth)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"revoked"`)
	assert.Equal(t, http.StatusUnauthorized, h.do("POST", "/ci", "", bearer(created.Token)).Code)

	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{"GET", "/api/v1/projects/chk/api-keys", "", 400},
		{"GET", "/api/v1/projects/NOPE/api-keys", "", 404},
		{"GET", "/api/v1/projects/CHK/api-keys?page=0", "", 400},
		{"POST", "/api/v1/projects/NOPE/api-keys", `{"name":"x"}`, 404},
		{"POST", "/api/v1/projects/CHK/api-keys", `{"name":""}`, 400},
		{"POST", "/api/v1/projects/CHK/api-keys", "", 415},
		{"POST", "/api/v1/projects/CHK/api-keys/1/revoke", "", 409},
		{"POST", "/api/v1/projects/CHK/api-keys/99/revoke", "", 404},
		{"POST", "/api/v1/projects/CHK/api-keys/0/revoke", "", 400},
		{"POST", "/api/v1/projects/NOPE/api-keys/1/revoke", "", 404},
	} {
		assert.Equal(t, c.want, h.do(c.method, c.target, c.body, auth).Code, c.method+" "+c.target)
	}
	h.svc.repo.(*fakeRepo).errs["CountAPIKeys"] = errBoom
	assert.Equal(t, http.StatusInternalServerError, h.do("GET", "/api/v1/projects/CHK/api-keys", "", auth).Code)
}

// tampered changes the last character of a key (never to itself, which would leave the key valid).
func tampered(token string) string {
	last := "A"
	if token[len(token)-1] == 'A' {
		last = "B"
	}
	return token[:len(token)-1] + last
}
