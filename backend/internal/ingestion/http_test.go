package ingestion

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

type stubAPI struct {
	out     Outcome
	err     error
	gotMeta RunMeta
	gotBody string
}

func (s *stubAPI) IngestJUnit(_ context.Context, m RunMeta, body io.Reader) (Outcome, error) {
	s.gotMeta = m
	b, err := io.ReadAll(body)
	if err != nil {
		return Outcome{}, err
	}
	s.gotBody = string(b)
	return s.out, s.err
}

func post(api API, maxBytes int64, query, contentType, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, maxBytes).Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/junit?"+query, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const q = "provider=github&runId=7&runAttempt=2&pipeline=ci&branch=main&commit=abc"

func TestIngestHandlerCreatedAndReplay(t *testing.T) {
	api := &stubAPI{out: Outcome{
		Created: true, Run: execution.TestRun{ID: 1, ExternalRunID: "github:7:2"}, Received: 2, Persisted: 1,
		Diagnostics: []Diagnostic{{TestName: "x", Correlation: execution.CorrelationMissing, Message: "m"}},
		ParseErrors: []execution.ParseError{{Index: 1, Message: "bad", Persisted: true, Severity: "error"}},
		Warnings:    []string{"w"},
	}}
	rec := post(api, 1024, q, "application/xml; charset=utf-8", "<testsuites/>")
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, RunMeta{Provider: "github", ProviderRunID: "7", RunAttempt: 2, Pipeline: "ci", Branch: "main", Commit: "abc"}, api.gotMeta)
	assert.Equal(t, "<testsuites/>", api.gotBody)
	body := rec.Body.String()
	assert.Contains(t, body, `"created":true`)
	assert.Contains(t, body, `"diagnostics":[{"testName":"x","correlation":"missing","requestedTestCaseId":null,"message":"m"}]`)
	assert.Contains(t, body, `"parseErrors":[{"index":1,"testName":"","message":"bad","persisted":true,"severity":"error"}]`)
	assert.Contains(t, body, `"warnings":["w"]`)

	api.out.Created = false
	rec = post(api, 1024, q, "text/xml", "<testsuites/>")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestIngestHandlerErrors(t *testing.T) {
	rec := post(&stubAPI{}, 1024, q, "application/json", "{}")
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Contains(t, rec.Body.String(), "unsupported_media_type")

	assert.Equal(t, http.StatusUnsupportedMediaType, post(&stubAPI{}, 1024, q, "", "x").Code)

	rec = post(&stubAPI{}, 1024, "provider=github&runId=7&runAttempt=abc", "application/xml", "<x/>")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "runAttempt")

	rec = post(&stubAPI{err: apperr.InvalidDocument("bad")}, 1024, q, "application/xml", "<x")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_junit")

	rec = post(&stubAPI{}, 4, q, "application/xml", "<testsuites/>")
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), "payload_too_large")
}
