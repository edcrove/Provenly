package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerWiring(t *testing.T) {
	s := NewServices(nil, time.Now)
	require.NotNil(t, s.Catalog)
	require.NotNil(t, s.Execution)
	require.NotNil(t, s.Ingestion)
	require.NotNil(t, s.Identity)
	h := NewHandler(s, 1024)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/healthz", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	// Every module is mounted behind a session: without one the answer is 401 before anything else.
	for _, target := range []string{"/api/v1/test-cases/x", "/api/v1/test-runs/x", "/api/v1/test-cases/x/results", "/api/v1/auth/me", "/api/v1/users"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, target)
	}
	// Sign-in and accepting an invitation are public: validation runs first.
	for _, target := range []string{"/api/v1/auth/login", "/api/v1/invitations/accept"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code, target)
	}
	// Ingestion needs an API key or a session.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/junit", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRoutePatterns(t *testing.T) {
	patterns := RoutePatterns()
	assert.Len(t, patterns, 54)
	assert.Contains(t, patterns, "POST /api/v1/invitations/accept")
	assert.Contains(t, patterns, "PATCH /api/v1/projects/{projectKey}")
	assert.Contains(t, patterns, "GET /readyz")
	assert.Contains(t, patterns, "GET /healthz")
	assert.Contains(t, patterns, "POST /api/v1/ingestion/junit")
	assert.Contains(t, patterns, "GET /api/v1/test-cases/{testCaseId}/results")
}

func TestReadiness(t *testing.T) {
	ready := func(err error) http.Handler {
		return NewHandler(Services{Ready: func(context.Context) error { return err }}, 1024)
	}

	rec := httptest.NewRecorder()
	ready(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())

	rec = httptest.NewRecorder()
	ready(errors.New("connection refused")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"code":"service_unavailable"`)
	assert.NotContains(t, rec.Body.String(), "connection refused")
}
