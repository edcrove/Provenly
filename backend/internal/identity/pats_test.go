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

func days(n int) *int { return &n }

func TestPersonalAccessTokens(t *testing.T) {
	s, repo, c, ctx := setup(t)
	a := admin(ctx, t, s)
	asAna, ana := member(ctx, t, s, repo, "ana")
	require.NoError(t, repo.UpsertMember(ctx, 1, ana.ID, authz.RoleViewer))
	require.NoError(t, repo.UpsertMember(ctx, 2, ana.ID, authz.RoleMember))

	tok, secret, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "  Claude Desktop  ", ProjectIDs: []int64{2, 1, 2}})
	require.NoError(t, err)
	assert.Equal(t, "Claude Desktop", tok.Name)
	assert.Regexp(t, `^pvly_pat_[0-9a-f]{8}_[A-Za-z0-9_-]{43}$`, secret)
	assert.Equal(t, secret[:17], tok.Prefix)
	assert.Equal(t, TokenDigest(secret), tok.TokenSHA256, "only the digest is stored")
	assert.Equal(t, []int64{1, 2}, tok.ProjectIDs, "each project once")
	assert.Equal(t, c.t.Add(90*24*time.Hour), tok.ExpiresAt, "90 days by default")
	assert.Equal(t, TokenActive, tok.Status(c.t))

	year, _, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "year", ProjectIDs: []int64{2}, ExpiresInDays: days(365)})
	require.NoError(t, err)
	assert.Equal(t, c.t.Add(365*24*time.Hour), year.ExpiresAt)
	page, err := s.ListTokens(asAna, ana, pagination.Page{Number: 1, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, "year", page.Items[0].Name, "newest first")
	page, _ = s.ListTokens(WithUser(ctx, a), a, pagination.Default())
	assert.Zero(t, page.Total, "a person sees only their own tokens")

	// Using a token authenticates as its person and records the use.
	c.t = c.t.Add(time.Hour)
	u, got, err := s.AuthenticateToken(ctx, secret)
	require.NoError(t, err)
	assert.Equal(t, ana.ID, u.ID)
	assert.Equal(t, tok.ID, got.ID)
	assert.Equal(t, c.t, *repo.tokens[tok.ID].LastUsedAt)
	for _, bad := range []string{"", "pvly_pat_", secret + "x", strings.Replace(secret, "pvly_pat_", "pvly_pax_", 1), tampered(secret)} {
		_, _, err := s.AuthenticateToken(ctx, bad)
		assert.Equal(t, errBadToken, err, bad)
	}

	// Expired: a 401 like any bad token.
	c.t = tok.ExpiresAt
	assert.Equal(t, TokenExpired, repo.tokens[tok.ID].Status(c.t))
	_, _, err = s.AuthenticateToken(ctx, secret)
	assert.Equal(t, errBadToken, err, "a token stops at its expiry")
	c.t = tok.ExpiresAt.Add(-time.Minute)

	revoked, err := s.RevokeToken(asAna, ana, tok.ID)
	require.NoError(t, err)
	assert.Equal(t, TokenRevoked, revoked.Status(c.t))
	_, _, err = s.AuthenticateToken(ctx, secret)
	assert.Equal(t, errBadToken, err, "a revoked token stops at once")
	_, err = s.RevokeToken(asAna, ana, tok.ID)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	_, err = s.RevokeToken(asAna, ana, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.RevokeToken(WithUser(ctx, a), a, year.ID)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err), "nobody revokes another person's token")

	// Deactivating a person revokes their tokens; a token of a deactivated person is refused anyway.
	_, other, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "other", ProjectIDs: []int64{1}})
	require.NoError(t, err)
	_, err = s.Deactivate(WithUser(ctx, a), a, "ana")
	require.NoError(t, err)
	assert.NotNil(t, repo.tokens[year.ID].RevokedAt)
	_, _, err = s.AuthenticateToken(ctx, other)
	assert.Equal(t, errBadToken, err)
	stored := repo.tokens[3]
	stored.RevokedAt = nil
	repo.tokens[3] = stored
	_, _, err = s.AuthenticateToken(ctx, other)
	assert.Equal(t, errBadToken, err, "even unrevoked, a deactivated person's token is refused")
}

func TestPersonalAccessTokenValidation(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	asAdmin := WithUser(ctx, a)
	asAna, ana := member(ctx, t, s, repo, "ana")
	require.NoError(t, repo.UpsertMember(ctx, 1, ana.ID, authz.RoleViewer))

	for _, c := range []struct {
		in    CreateTokenInput
		field string
	}{
		{CreateTokenInput{Name: "", ProjectIDs: []int64{1}}, "name"},
		{CreateTokenInput{Name: strings.Repeat("n", 101), ProjectIDs: []int64{1}}, "name"},
		{CreateTokenInput{Name: "a\x00b", ProjectIDs: []int64{1}}, "name"},
		{CreateTokenInput{Name: "x", ProjectIDs: []int64{1}, ExpiresInDays: days(0)}, "expiresInDays"},
		{CreateTokenInput{Name: "x", ProjectIDs: []int64{1}, ExpiresInDays: days(366)}, "expiresInDays"},
		{CreateTokenInput{Name: "x"}, "projects"},
		{CreateTokenInput{Name: "x", ProjectIDs: make([]int64, 51)}, "projects"},
		{CreateTokenInput{Name: "x", ProjectIDs: []int64{2}}, "projects"},
		{CreateTokenInput{Name: "x", ProjectIDs: []int64{0}}, "projects"},
	} {
		_, _, err := s.CreateToken(asAna, ana, c.in)
		e, ok := apperr.As(err)
		require.True(t, ok, "%+v", c.in)
		assert.Equal(t, c.field, e.Fields[0].Field, "%+v", c.in)
	}
	many := make([]int64, 51)
	for i := range many {
		many[i] = int64(i + 1)
	}
	_, _, err := s.CreateToken(asAdmin, a, CreateTokenInput{Name: "x", ProjectIDs: many})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err), "at most 50 projects")
	_, _, err = s.CreateToken(asAdmin, a, CreateTokenInput{Name: "x", ProjectIDs: []int64{2}})
	assert.NoError(t, err, "administrators may name any project")

	for method, call := range map[string]func() error{
		"MemberRole": func() error {
			_, _, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "x", ProjectIDs: []int64{1}})
			return err
		},
		"InTx": func() error {
			_, _, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "x", ProjectIDs: []int64{1}})
			return err
		},
		"CreateToken": func() error {
			_, _, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "x", ProjectIDs: []int64{1}})
			return err
		},
		"CountTokens": func() error { _, err := s.ListTokens(asAna, ana, pagination.Default()); return err },
		"ListTokens":  func() error { _, err := s.ListTokens(asAna, ana, pagination.Default()); return err },
		"RevokeToken": func() error { _, err := s.RevokeToken(asAna, ana, 1); return err },
		"GetToken":    func() error { _, err := s.RevokeToken(asAna, ana, 99); return err },
		"GetTokenByDigest": func() error {
			_, _, err := s.AuthenticateToken(ctx, "pvly_pat_00000000_"+strings.Repeat("a", 43))
			return err
		},
		"RevokeUserTokens": func() error { _, err := s.Deactivate(asAdmin, a, "ana"); return err },
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
	_, secret, err := s.CreateToken(asAna, ana, CreateTokenInput{Name: "x", ProjectIDs: []int64{1}})
	require.NoError(t, err)
	for _, method := range []string{"GetUser", "TouchToken"} {
		repo.errs[method] = errBoom
		_, _, err := s.AuthenticateToken(ctx, secret)
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
}

func TestPersonalAccessTokenAccess(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	gone := apperr.NotFound("gone")
	_, ana := member(ctx, t, s, repo, "ana")
	require.NoError(t, repo.UpsertMember(ctx, 1, ana.ID, authz.RoleMaintainer))
	require.NoError(t, repo.UpsertMember(ctx, 2, ana.ID, authz.RoleViewer))
	asAnaToken := WithToken(WithUser(ctx, ana), PersonalAccessToken{ProjectIDs: []int64{1, 3}})

	// The token reads its projects with the person's role; another of their projects is a 403, a hidden one a 404.
	assert.NoError(t, s.Require(asAnaToken, 1, authz.RoleMaintainer, gone))
	assert.Equal(t, errTokenProject, s.Require(asAnaToken, 2, authz.RoleViewer, gone))
	assert.Equal(t, gone, s.Require(asAnaToken, 3, authz.RoleViewer, gone), "named in the token, but no longer a member")
	scope, err := s.Scope(asAnaToken)
	require.NoError(t, err)
	assert.Equal(t, authz.Scope{Roles: map[int64]authz.Role{1: authz.RoleMaintainer}}, scope)

	// An administrator's token sees only its projects, and never administers.
	asAdminToken := WithToken(WithUser(ctx, a), PersonalAccessToken{ProjectIDs: []int64{2}})
	scope, err = s.Scope(asAdminToken)
	require.NoError(t, err)
	assert.Equal(t, authz.Scope{Roles: map[int64]authz.Role{2: authz.RoleAdmin}}, scope)
	assert.Equal(t, errTokenAdmin, s.RequireAdmin(asAdminToken))
	_, err = s.ListUsers(asAdminToken, a, pagination.Default())
	assert.Equal(t, errTokenAdmin, err)
	assert.Equal(t, errTokenProject, s.Require(asAdminToken, 1, authz.RoleViewer, gone))

	repo.errs["ListUserMemberships"] = errBoom
	_, err = s.Scope(asAnaToken)
	assert.ErrorIs(t, err, errBoom)
}

func TestPersonalAccessTokenRoutes(t *testing.T) {
	h := newHarness(t)
	session, _ := h.login("admin", "correct horse")
	auth := bearer(session)

	rec := h.do("POST", "/api/v1/auth/tokens", `{"name":"MCP","projects":["CHK"],"expiresInDays":30}`, auth)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		PersonalAccessToken struct {
			ID       int64
			Prefix   string
			Status   string
			Projects []string
		}
		Token string
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "active", created.PersonalAccessToken.Status)
	assert.Equal(t, []string{"CHK"}, created.PersonalAccessToken.Projects)
	assert.True(t, strings.HasPrefix(created.Token, created.PersonalAccessToken.Prefix))
	assert.NotContains(t, rec.Body.String(), "tokenSha256")
	pat := bearer(created.Token)

	// The token reads as its person; changes, other routes' writes and MCP aside, are 403.
	rec = h.do("GET", "/api/v1/auth/me", "", pat)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"username":"admin"`)
	assert.Equal(t, http.StatusForbidden, h.do("GET", "/api/v1/users", "", pat).Code, "no administration")
	assert.Equal(t, http.StatusForbidden, h.do("GET", "/api/v1/projects/TC/members", "", pat).Code, "outside the token")
	assert.Equal(t, http.StatusOK, h.do("GET", "/api/v1/projects/CHK/members", "", pat).Code)
	rec = h.do("POST", "/api/v1/auth/tokens", `{"name":"x","projects":["CHK"]}`, pat)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a token cannot make tokens")
	assert.Contains(t, rec.Body.String(), "read-only")
	Protect(h.mux, h.svc).HandleFunc("POST /api/v1/mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	assert.Equal(t, http.StatusAccepted, h.do("POST", "/api/v1/mcp", "", pat).Code, "MCP tools only read")
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: created.Token})
	}).Code, "a token is only read from the Authorization header")

	rec = h.do("GET", "/api/v1/auth/tokens", "", auth)
	assert.Contains(t, rec.Body.String(), `"totalItems":1`)
	assert.Contains(t, rec.Body.String(), `"lastUsedAt":"`)
	assert.Contains(t, rec.Body.String(), `"projects":["CHK"]`)
	rec = h.do("POST", "/api/v1/auth/tokens/1/revoke", "", auth)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"revoked"`)
	rec = h.do("GET", "/api/v1/auth/me", "", pat)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "revoked: 401")
	assert.Contains(t, rec.Body.String(), "expired or revoked")

	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{"GET", "/api/v1/auth/tokens?page=0", "", 400},
		{"POST", "/api/v1/auth/tokens", `{"name":"x","projects":["NOPE"]}`, 400},
		{"POST", "/api/v1/auth/tokens", `{"name":"x","projects":["chk"]}`, 400},
		{"POST", "/api/v1/auth/tokens", `{"name":"x","projects":[]}`, 400},
		{"POST", "/api/v1/auth/tokens", `{"name":"x","projects":["CHK"],"expiresInDays":366}`, 400},
		{"POST", "/api/v1/auth/tokens", "", 415},
		{"POST", "/api/v1/auth/tokens/1/revoke", "", 409},
		{"POST", "/api/v1/auth/tokens/99/revoke", "", 404},
		{"POST", "/api/v1/auth/tokens/0/revoke", "", 400},
	} {
		assert.Equal(t, c.want, h.do(c.method, c.target, c.body, auth).Code, c.method+" "+c.target)
	}
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/tokens", "").Code)

	repo := h.svc.repo.(*fakeRepo)
	assert.Equal(t, http.StatusInternalServerError, h.do("POST", "/api/v1/auth/tokens", `{"name":"x","projects":["BOOM"]}`, auth).Code)
	require.Equal(t, http.StatusCreated, h.do("POST", "/api/v1/auth/tokens", `{"name":"second","projects":["CHK"]}`, auth).Code)
	rec = h.do("GET", "/api/v1/auth/tokens", "", auth)
	assert.Equal(t, 2, strings.Count(rec.Body.String(), `"projects":["CHK"]`), "each project named once per token")
	// A project that cannot be named fails the answer, never with a wrong key.
	repo.tokens[9] = PersonalAccessToken{ID: 9, UserID: 1, Name: "lost", ProjectIDs: []int64{66}, ExpiresAt: time.Now().Add(time.Hour)}
	assert.Equal(t, http.StatusInternalServerError, h.do("GET", "/api/v1/auth/tokens", "", auth).Code)
	assert.Equal(t, http.StatusInternalServerError, h.do("POST", "/api/v1/auth/tokens/9/revoke", "", auth).Code)
	delete(repo.tokens, 9)
	repo.errs["CountTokens"] = errBoom
	assert.Equal(t, http.StatusInternalServerError, h.do("GET", "/api/v1/auth/tokens", "", auth).Code)
	delete(repo.errs, "CountTokens")
	repo.errs["CreateToken"] = errBoom
	assert.Equal(t, http.StatusInternalServerError, h.do("POST", "/api/v1/auth/tokens", `{"name":"x","projects":["CHK"]}`, auth).Code)
	delete(repo.errs, "CreateToken")
	repo.errs["GetTokenByDigest"] = errBoom
	assert.Equal(t, http.StatusInternalServerError, h.do("GET", "/api/v1/auth/me", "", pat).Code)
	delete(repo.errs, "GetTokenByDigest")
}
