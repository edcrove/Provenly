package execution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

type stubAPI struct {
	err       error
	gotFilter ResultFilter
}

var sampleRun = TestRun{ID: 3, ExternalRunID: "github:1:1", Provider: "github", ProviderRunID: "1", RunAttempt: 1,
	Status: RunCompleted, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

var sampleResult = TestResult{ID: 8, TestRunID: 3, TestCaseID: ptr(int64(153)), RequestedTestCaseID: ptr("153"),
	Correlation: CorrelationValid, TestName: "login", Status: Passed, DurationMs: 12}

func (s *stubAPI) GetRun(context.Context, int64) (TestRun, error) { return sampleRun, s.err }
func (s *stubAPI) ListRuns(_ context.Context, p pagination.Page) (pagination.Result[TestRun], error) {
	return pagination.Result[TestRun]{Items: []TestRun{sampleRun}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) ListRunResults(_ context.Context, _ int64, f ResultFilter, p pagination.Page) (pagination.Result[TestResult], error) {
	s.gotFilter = f
	return pagination.Result[TestResult]{Items: []TestResult{sampleResult}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) Summary(context.Context, int64) (Summary, error) {
	return ComputeSummary(3, []int64{153, 154}, []ValidResult{{153, Passed}}, []Diagnostic{{Correlation: CorrelationMissing}}), s.err
}
func (s *stubAPI) History(_ context.Context, _ int64, p pagination.Page) (pagination.Result[HistoryEntry], error) {
	return pagination.Result[HistoryEntry]{Items: []HistoryEntry{{Result: sampleResult, Run: sampleRun}}, Page: p, Total: 1}, s.err
}

type stubCatalog struct{ err error }

func (c stubCatalog) EnsureExists(context.Context, int64) error { return c.err }

func serve(api API, cat TestCaseChecker, method, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, cat).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHandlerHappyPaths(t *testing.T) {
	cases := map[string]string{
		"/api/v1/test-runs":   `"externalRunId":"github:1:1"`,
		"/api/v1/test-runs/3": `"providerRunId":"1"`,
		"/api/v1/test-runs/3/results?status=failed&correlation=valid": `"testCaseId":153`,
		"/api/v1/test-runs/3/summary":                                 `"executionPercent":50`,
		"/api/v1/test-cases/153/results":                              `"run":{"id":3`,
	}
	for target, want := range cases {
		rec := serve(&stubAPI{}, stubCatalog{}, "GET", target)
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), want, target)
	}
	rec := serve(&stubAPI{}, stubCatalog{}, "GET", "/api/v1/test-runs/3/summary")
	assert.Contains(t, rec.Body.String(), `"testCases":[{"testCaseId":153,"status":"passed","resultCount":1},{"testCaseId":154,"status":"untested","resultCount":0}]`)
	assert.Contains(t, rec.Body.String(), `"diagnostics":{"missing":1,"malformed":0,"unknown":0,"deprecated":0,"total":1}`)
}

func TestHandlerFilter(t *testing.T) {
	api := &stubAPI{}
	serve(api, stubCatalog{}, "GET", "/api/v1/test-runs/3/results?status=error&correlation=unknown")
	require.NotNil(t, api.gotFilter.Status)
	assert.Equal(t, Error, *api.gotFilter.Status)
	assert.Equal(t, CorrelationUnknown, *api.gotFilter.Correlation)

	serve(api, stubCatalog{}, "GET", "/api/v1/test-runs/3/results")
	assert.Nil(t, api.gotFilter.Status)
	assert.Nil(t, api.gotFilter.Correlation)
}

func TestHandlerErrors(t *testing.T) {
	failing := &stubAPI{err: apperr.NotFound("missing")}
	for _, target := range []string{
		"/api/v1/test-runs", "/api/v1/test-runs/3", "/api/v1/test-runs/3/results",
		"/api/v1/test-runs/3/summary", "/api/v1/test-cases/1/results",
	} {
		assert.Equal(t, http.StatusNotFound, serve(failing, stubCatalog{}, "GET", target).Code, target)
	}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{}, stubCatalog{err: apperr.NotFound("TC-1")}, "GET", "/api/v1/test-cases/1/results").Code)

	for _, target := range []string{
		"/api/v1/test-runs?page=x", "/api/v1/test-runs/x", "/api/v1/test-runs/x/results",
		"/api/v1/test-runs/3/results?page=0", "/api/v1/test-runs/3/results?status=untested",
		"/api/v1/test-runs/3/results?correlation=nope", "/api/v1/test-runs/x/summary",
		"/api/v1/test-cases/x/results", "/api/v1/test-cases/1/results?pageSize=0",
	} {
		assert.Equal(t, http.StatusBadRequest, serve(&stubAPI{}, stubCatalog{}, "GET", target).Code, target)
	}
}
