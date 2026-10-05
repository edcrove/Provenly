package identity

import (
	"context"

	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProjects knows TC (1) and CHK (2).
type stubProjects struct{}

func (stubProjects) ProjectIDByKey(_ context.Context, key string) (int64, error) {
	if id, ok := map[string]int64{"TC": 1, "CHK": 2}[key]; ok {
		return id, nil
	}
	return 0, apperr.NotFound("project %s not found", key)
}

type harness struct {
	t   *testing.T
	mux *http.ServeMux
	svc *Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	s, _, c, ctx := setup(t)
	admin(ctx, t, s)
	mux := http.NewServeMux()
	h := NewHandler(s, stubProjects{}, c.now)
	h.RegisterPublic(mux)
	h.RegisterProtected(Protect(mux, s))
	return &harness{t: t, mux: mux, svc: s}
}

func (h *harness) do(method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func (h *harness) login(username, password string) (string, *httptest.ResponseRecorder) {
	rec := h.do("POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"`+password+`"}`)
	var body struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Token, rec
}

func TestLoginSetsASessionCookieAndToken(t *testing.T) {
	h := newHarness(t)
	token, rec := h.login("admin", "correct horse")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"username":"admin"`)
	assert.Contains(t, rec.Body.String(), `"isAdmin":true`)
	assert.NotContains(t, rec.Body.String(), "$2", "never the password hash")
	cookie := rec.Result().Cookies()[0]
	assert.Equal(t, CookieName, cookie.Name)
	assert.Equal(t, token, cookie.Value)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.False(t, cookie.Secure, "plain HTTP (local) cannot keep a Secure cookie")
	assert.Equal(t, 3600, cookie.MaxAge)

	// Behind TLS (directly or through a proxy) the cookie is Secure.
	for _, m := range []func(*http.Request){
		func(r *http.Request) { r.TLS = &tls.ConnectionState{} },
		func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
	} {
		rec := h.do("POST", "/api/v1/auth/login", `{"username":"admin","password":"correct horse"}`, m)
		assert.True(t, rec.Result().Cookies()[0].Secure)
	}

	_, rec = h.login("admin", "nope nope nope")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"unauthorized"`)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.do("POST", "/api/v1/auth/login", "").Code)

	rec = h.do("POST", "/api/v1/auth/logout", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, -1, rec.Result().Cookies()[0].MaxAge, "logout clears the cookie")
}

func TestProtectedRoutesNeedASession(t *testing.T) {
	h := newHarness(t)
	token, rec := h.login("admin", "correct horse")
	cookie := rec.Result().Cookies()[0]

	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "").Code)
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", func(r *http.Request) { r.Header.Set("Authorization", "Basic abc") }).Code)
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", bearer("garbage")).Code)
	// A non-Bearer Authorization header is not overridden by the cookie.
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Basic abc")
		r.AddCookie(cookie)
	}).Code)

	rec = h.do("GET", "/api/v1/auth/me", "", bearer(token))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"displayName":"admin"`)
	assert.Equal(t, http.StatusOK, h.do("GET", "/api/v1/auth/me", "", func(r *http.Request) { r.AddCookie(cookie) }).Code)
	assert.Equal(t, http.StatusOK, h.do("GET", "/api/v1/auth/me", "", func(r *http.Request) { r.Header.Set("Authorization", "bearer  "+token) }).Code)
}

func TestInvitationAndUserRoutes(t *testing.T) {
	h := newHarness(t)
	token, _ := h.login("admin", "correct horse")
	auth := bearer(token)

	rec := h.do("POST", "/api/v1/invitations", `{"email":"ana@example.com","note":"QA"}`, auth)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created struct {
		Invitation struct {
			ID     int64
			Status string
		}
		Token string
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "pending", created.Invitation.Status)
	assert.NotEmpty(t, created.Token)
	assert.Equal(t, http.StatusBadRequest, h.do("POST", "/api/v1/invitations", `{"email":"x"}`, auth).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.do("POST", "/api/v1/invitations", "", auth).Code)

	rec = h.do("GET", "/api/v1/invitations", "", auth)
	assert.Contains(t, rec.Body.String(), `"totalItems":1`)
	assert.Contains(t, rec.Body.String(), `"note":"QA"`)
	assert.NotContains(t, rec.Body.String(), created.Token, "the token is shown once, at creation")
	assert.Equal(t, http.StatusBadRequest, h.do("GET", "/api/v1/invitations?page=0", "", auth).Code)

	rec = h.do("POST", "/api/v1/invitations/accept", `{"token":"`+created.Token+`","username":"ana","displayName":"Ana","password":"ana's password"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, CookieName, rec.Result().Cookies()[0].Name, "accepting signs the new user in")
	assert.Equal(t, http.StatusNotFound, h.do("POST", "/api/v1/invitations/accept", `{"token":"`+created.Token+`","username":"ana2","displayName":"Ana","password":"ana's password"}`).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.do("POST", "/api/v1/invitations/accept", "").Code)
	anaToken, _ := h.login("ana", "ana's password")

	rec = h.do("GET", "/api/v1/users", "", auth)
	assert.Contains(t, rec.Body.String(), `"totalItems":2`)
	assert.Equal(t, http.StatusForbidden, h.do("GET", "/api/v1/users", "", bearer(anaToken)).Code)
	assert.Contains(t, h.do("GET", "/api/v1/users", "", bearer(anaToken)).Body.String(), `"code":"forbidden"`)
	assert.Equal(t, http.StatusBadRequest, h.do("GET", "/api/v1/users?pageSize=0", "", auth).Code)
	assert.Equal(t, http.StatusForbidden, h.do("GET", "/api/v1/invitations", "", bearer(anaToken)).Code)

	rec = h.do("POST", "/api/v1/invitations/"+itoa(created.Invitation.ID)+"/revoke", "", auth)
	assert.Equal(t, http.StatusConflict, rec.Code)
	_ = h.do("POST", "/api/v1/invitations", `{}`, auth)
	rec = h.do("POST", "/api/v1/invitations/2/revoke", "", auth)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"revoked"`)
	assert.Equal(t, http.StatusBadRequest, h.do("POST", "/api/v1/invitations/x/revoke", "", auth).Code)
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestChangePasswordRoute(t *testing.T) {
	h := newHarness(t)
	token, _ := h.login("admin", "correct horse")
	assert.Equal(t, http.StatusBadRequest, h.do("POST", "/api/v1/auth/password", `{"currentPassword":"wrong one!","newPassword":"a brand new password"}`, bearer(token)).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.do("POST", "/api/v1/auth/password", "", bearer(token)).Code)
	rec := h.do("POST", "/api/v1/auth/password", `{"currentPassword":"correct horse","newPassword":"a brand new password"}`, bearer(token))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, CookieName, rec.Result().Cookies()[0].Name, "a fresh session replaces the old one")
	assert.Equal(t, http.StatusUnauthorized, h.do("GET", "/api/v1/auth/me", "", bearer(token)).Code, "the old token is signed out")
}
