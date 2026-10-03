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

const q = "provider=github&runId=7&runAttempt=2&pipeline=ci&branch=main&commit=abc&status=interrupted"

func TestIngestHandlerCreatedAndReplay(t *testing.T) {
	api := &stubAPI{out: Outcome{
		Created: true, Run: execution.TestRun{ID: 1, ExternalRunID: "github:7:2"}, Received: 2, Persisted: 1,
		Diagnostics: []Diagnostic{{TestName: "x", Correlation: execution.CorrelationMissing, Message: "m"}},
		ParseErrors: []execution.ParseError{{Index: 1, Message: "bad", Persisted: true, Severity: "error"}},
		Warnings:    []string{"w"},
	}}
	rec := post(api, 1024, q, "application/xml; charset=utf-8", "<testsuites/>")
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, RunMeta{Provider: "github", ProviderRunID: "7", RunAttempt: 2, Pipeline: "ci", Branch: "main", Commit: "abc", Status: execution.RunInterrupted, Charset: "utf-8"}, api.gotMeta)
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

	rec = post(&stubAPI{}, 1024, "provider=github&runId=7&runAttempt=1&status=", "application/xml", "<x/>")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"field":"status"`)

	rec = post(&stubAPI{err: apperr.InvalidDocument("bad")}, 1024, q, "application/xml", "<x")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_junit")

	rec = post(&stubAPI{}, 4, q, "application/xml", "<testsuites/>")
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), "payload_too_large")
}

// Found validating CI/CD Result Ingestion API (docs/review.md finding 24).
func TestIngestHandlerReportsEveryParameterError(t *testing.T) {
	rec := post(&stubAPI{}, 1024, "", "application/xml", "<x/>")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	for _, field := range []string{`"field":"provider"`, `"field":"runId"`, `"field":"runAttempt"`} {
		assert.Contains(t, rec.Body.String(), field)
	}
	assert.Equal(t, 1, strings.Count(rec.Body.String(), `"field":"runAttempt"`), "runAttempt is reported once")
	rec = post(&stubAPI{}, 1024, "provider=Bad&runId=7&runAttempt=x&status=", "application/xml", "<x/>")
	for _, field := range []string{`"field":"provider"`, `"field":"runAttempt"`, `"field":"status"`} {
		assert.Contains(t, rec.Body.String(), field)
	}
}

func TestIngestHandlerMediaTypes(t *testing.T) {
	for _, ct := range []string{"application/xml", "text/xml", "Application/XML; charset=utf-8", "application/junit+xml", "application/vnd.surefire+xml"} {
		assert.Equal(t, http.StatusOK, post(&stubAPI{}, 1024, q, ct, "<x/>").Code, ct)
	}
	for _, ct := range []string{"application/json", "text/plain", "application/octet-stream", "application/xml; charset=shift_jis"} {
		rec := post(&stubAPI{}, 1024, q, ct, "<x/>")
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code, ct)
		assert.Contains(t, rec.Body.String(), "unsupported_media_type", ct)
	}
	api := &stubAPI{}
	post(api, 1024, q, "application/xml; charset=ISO-8859-1", "<x/>")
	assert.Equal(t, "ISO-8859-1", api.gotMeta.Charset, "the charset parameter is passed on: it overrides the XML declaration")
}

func TestIngestHandlerRejectsCompressedBodies(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(&stubAPI{}, 1024).Register(mux)
	for _, enc := range []string{"gzip", "br", "deflate"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/junit?"+q, strings.NewReader("\x1f\x8b"))
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Content-Encoding", enc)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code, enc)
		assert.Contains(t, rec.Body.String(), "Content-Encoding", enc)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/junit?"+q, strings.NewReader("<x/>"))
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Content-Encoding", "identity")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "identity is no encoding")
}
