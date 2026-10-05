package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

type stubAPI struct {
	err         error
	getErr      error
	gotFilter   ResultFilter
	gotProjects []int64
}

var sampleRun = TestRun{ID: 3, ExternalRunID: "github:1:1", Provider: "github", ProviderRunID: "1", RunAttempt: 1,
	Status: RunCompleted, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

var sampleResult = TestResult{ID: 8, TestRunID: 3, TestCaseID: ptr(int64(153)), RequestedTestCaseID: ptr("153"),
	Correlation: CorrelationValid, TestName: "login", Status: Passed, DurationMs: ptr(int64(12))}

func (s *stubAPI) GetRun(context.Context, int64) (TestRun, error) { return sampleRun, s.getErr }
func (s *stubAPI) ListRuns(_ context.Context, projectIDs []int64, p pagination.Page) (pagination.Result[TestRun], error) {
	s.gotProjects = projectIDs
	return pagination.Result[TestRun]{Items: []TestRun{sampleRun}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) ListRunResults(_ context.Context, _ int64, f ResultFilter, p pagination.Page) (pagination.Result[TestResult], error) {
	s.gotFilter = f
	return pagination.Result[TestResult]{Items: []TestResult{sampleResult}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) Summary(context.Context, int64) (Summary, error) {
	return ComputeSummary(3, []int64{153, 154}, []ValidResult{{153, Passed}}, []Diagnostic{{Correlation: CorrelationMissing}}), s.err
}
func (s *stubAPI) ListParseErrors(_ context.Context, _ int64, p pagination.Page) (pagination.Result[ParseError], error) {
	return pagination.Result[ParseError]{Items: []ParseError{{Index: 2, TestName: "t", Message: "m", Persisted: true, Severity: "warning"}}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) History(_ context.Context, _ int64, p pagination.Page) (pagination.Result[HistoryEntry], error) {
	return pagination.Result[HistoryEntry]{Items: []HistoryEntry{{Result: sampleResult, Run: sampleRun}}, Page: p, Total: 1}, s.err
}

type stubCatalog struct{ err, projectErr, keysErr error }

// Keys knows TC-153 and CHK-4 (id 154); other ids have no key.
func (c stubCatalog) Keys(_ context.Context, ids []int64) (map[int64]string, error) {
	known := map[int64]string{153: "TC-153", 154: "CHK-4"}
	keys := map[int64]string{}
	for _, id := range ids {
		if k, ok := known[id]; ok {
			keys[id] = k
		}
	}
	return keys, c.keysErr
}

// ProjectOf puts every test case in project 7.
func (c stubCatalog) ProjectOf(context.Context, int64) (int64, error) { return 7, c.err }
func (c stubCatalog) ProjectIDByKey(context.Context, string) (int64, error) {
	return 7, c.projectErr
}

func serve(api API, cat TestCaseChecker, target string) *httptest.ResponseRecorder {
	return serveAs(adminGuard, api, cat, target)
}

func serveAs(guard authz.Guard, api API, cat TestCaseChecker, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, cat, guard).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandlerHappyPaths(t *testing.T) {
	cases := map[string]string{
		"/api/v1/test-runs":   `"externalRunId":"github:1:1"`,
		"/api/v1/test-runs/3": `"providerRunId":"1"`,
		"/api/v1/test-runs/3/results?status=failed&correlation=valid": `"testCaseId":153,"testCaseKey":"TC-153"`,
		"/api/v1/test-runs/3/summary":                                 `"executionPercent":50`,
		"/api/v1/test-cases/153/results":                              `"testCaseKey":"TC-153"`,
		"/api/v1/test-runs/3/parse-errors":                            `"items":[{"index":2,"testName":"t","message":"m","persisted":true,"severity":"warning"}]`,
	}
	for target, want := range cases {
		rec := serve(&stubAPI{}, stubCatalog{}, target)
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), want, target)
	}
	rec := serve(&stubAPI{}, stubCatalog{}, "/api/v1/test-runs/3/summary")
	assert.Contains(t, rec.Body.String(), `"testCases":[{"testCaseId":153,"testCaseKey":"TC-153","status":"passed","resultCount":1},{"testCaseId":154,"testCaseKey":"CHK-4","status":"untested","resultCount":0}]`)
	assert.Contains(t, rec.Body.String(), `"diagnostics":{"missing":1,"malformed":0,"unknown":0,"deprecated":0,"wrongProject":0,"total":1}`)
}

// ?project=<KEY> narrows the run list to one project; without it every run is listed.
func TestHandlerProjectFilter(t *testing.T) {
	api := &stubAPI{}
	assert.Equal(t, http.StatusOK, serve(api, stubCatalog{}, "/api/v1/test-runs?project=CHK").Code)
	assert.Equal(t, []int64{7}, api.gotProjects)
	serve(api, stubCatalog{}, "/api/v1/test-runs")
	assert.Nil(t, api.gotProjects, "administrators: every project")

	assert.Equal(t, http.StatusNotFound, serve(api, stubCatalog{projectErr: apperr.NotFound("project CHK not found")}, "/api/v1/test-runs?project=CHK").Code)
	for _, target := range []string{"/api/v1/test-runs?project=", "/api/v1/test-runs?project=chk"} {
		assert.Equal(t, http.StatusBadRequest, serve(api, stubCatalog{}, target).Code, target)
	}
}

func TestHandlerFilter(t *testing.T) {
	api := &stubAPI{}
	serve(api, stubCatalog{}, "/api/v1/test-runs/3/results?status=error&correlation=unknown")
	require.NotNil(t, api.gotFilter.Status)
	assert.Equal(t, Error, *api.gotFilter.Status)
	assert.Equal(t, CorrelationUnknown, *api.gotFilter.Correlation)

	serve(api, stubCatalog{}, "/api/v1/test-runs/3/results")
	assert.Nil(t, api.gotFilter.Status)
	assert.Nil(t, api.gotFilter.Correlation)
}

func TestHandlerErrors(t *testing.T) {
	failing := &stubAPI{err: apperr.NotFound("missing")}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{getErr: apperr.NotFound("missing")}, stubCatalog{}, "/api/v1/test-runs/3").Code)
	for _, target := range []string{
		"/api/v1/test-runs", "/api/v1/test-runs/3/results",
		"/api/v1/test-runs/3/summary", "/api/v1/test-cases/1/results", "/api/v1/test-runs/3/parse-errors",
	} {
		assert.Equal(t, http.StatusNotFound, serve(failing, stubCatalog{}, target).Code, target)
	}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{}, stubCatalog{err: apperr.NotFound("TC-1")}, "/api/v1/test-cases/1/results").Code)
	for _, target := range []string{"/api/v1/test-runs/3/results", "/api/v1/test-runs/3/summary", "/api/v1/test-cases/153/results"} {
		assert.Equal(t, http.StatusInternalServerError, serve(&stubAPI{}, stubCatalog{keysErr: errors.New("db down")}, target).Code, "key lookup failure: "+target)
	}

	for _, target := range []string{
		"/api/v1/test-runs?page=x", "/api/v1/test-runs/x", "/api/v1/test-runs/x/results",
		"/api/v1/test-runs/3/results?page=0", "/api/v1/test-runs/3/results?status=untested",
		"/api/v1/test-runs/3/results?correlation=nope", "/api/v1/test-runs/x/summary",
		"/api/v1/test-cases/x/results", "/api/v1/test-cases/1/results?pageSize=0",
		"/api/v1/test-runs/x/parse-errors", "/api/v1/test-runs/3/parse-errors?page=0",
	} {
		assert.Equal(t, http.StatusBadRequest, serve(&stubAPI{}, stubCatalog{}, target).Code, target)
	}
}

// Runs, their results and the history of a test case are visible to anyone with a role in the project; other
// users get 404 (never 403). Lists are narrowed to the user's projects.
func TestHandlerAuthorization(t *testing.T) {
	api := &stubAPI{}
	viewer := memberOf(map[int64]authz.Role{sampleRun.ProjectID: authz.RoleViewer, 7: authz.RoleViewer})
	for _, target := range []string{"/api/v1/test-runs/3", "/api/v1/test-runs/3/results", "/api/v1/test-runs/3/summary",
		"/api/v1/test-runs/3/parse-errors", "/api/v1/test-cases/153/results"} {
		assert.Equal(t, http.StatusOK, serveAs(viewer, api, stubCatalog{}, target).Code, target)
		rec := serveAs(memberOf(nil), api, stubCatalog{}, target)
		assert.Equal(t, http.StatusNotFound, rec.Code, target)
		assert.Contains(t, rec.Body.String(), "not found", target)
	}
	serveAs(memberOf(map[int64]authz.Role{7: authz.RoleViewer}), api, stubCatalog{}, "/api/v1/test-runs")
	assert.Equal(t, []int64{7}, api.gotProjects)
	assert.Equal(t, http.StatusNotFound, serveAs(memberOf(nil), api, stubCatalog{}, "/api/v1/test-runs?project=CHK").Code)
	broken := stubGuard{err: errors.New("db down")}
	assert.Equal(t, http.StatusInternalServerError, serveAs(broken, api, stubCatalog{}, "/api/v1/test-runs").Code)
}
