package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	gotSuite    *string
	gotAmend    []any
	// runErr fails GetRun alone (after the cheap RunProject authorized the request); runReads counts GetRun calls.
	runErr   error
	runReads int
}

var sampleRun = TestRun{ID: 3, ExternalRunID: "github:1:1", Provider: "github", ProviderRunID: "1", RunAttempt: 1,
	Status: RunCompleted, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

var sampleResult = TestResult{ID: 8, TestRunID: 3, TestCaseID: ptr(int64(153)), RequestedTestCaseID: ptr("153"),
	Correlation: CorrelationValid, TestName: "login", Status: Passed, DurationMs: ptr(int64(12))}

func (s *stubAPI) GetRun(context.Context, int64) (TestRun, error) {
	s.runReads++
	return sampleRun, s.runErr
}
func (s *stubAPI) RunProject(context.Context, int64) (int64, error) {
	return sampleRun.ProjectID, s.getErr
}
func (s *stubAPI) ListRuns(_ context.Context, f RunFilter, p pagination.Page) (pagination.Result[TestRun], error) {
	s.gotProjects, s.gotSuite = f.ProjectIDs, f.SuiteKey
	return pagination.Result[TestRun]{Items: []TestRun{sampleRun}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) ListRunResults(_ context.Context, _ int64, f ResultFilter, p pagination.Page) (pagination.Result[TestResult], error) {
	s.gotFilter = f
	return pagination.Result[TestResult]{Items: []TestResult{sampleResult}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) Summary(context.Context, int64) (Summary, error) {
	return ComputeSummary(3, []int64{153, 154}, []ValidResult{{TestCaseID: 153, Status: Passed}}, []Diagnostic{{Correlation: CorrelationMissing}}), s.err
}
func (s *stubAPI) Live(context.Context, int64) (Live, error) {
	seq, tc := int64(4), int64(153)
	return Live{Cases: []LiveCase{{TestCaseID: 153, State: "passed"}, {TestCaseID: 154, State: LiveWaiting}}, Waiting: 1, Finished: 1, Events: 2,
		LastSequence: &seq, Reconciliation: ReconciliationMismatch, Mismatches: []Mismatch{
			{Kind: MismatchStatus, TestCaseID: &tc, LiveStatus: "passed", FinalStatus: "failed"}, {Kind: MismatchInvalidCorrelation, Requested: "TC-9"}}}, s.err
}
func (s *stubAPI) ListParseErrors(_ context.Context, _ int64, p pagination.Page) (pagination.Result[ParseError], error) {
	return pagination.Result[ParseError]{Items: []ParseError{{Index: 2, TestName: "t", Message: "m", Persisted: true, Severity: "warning"}}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) History(_ context.Context, _ int64, p pagination.Page) (pagination.Result[HistoryEntry], error) {
	return pagination.Result[HistoryEntry]{Items: []HistoryEntry{{Result: sampleResult, Run: sampleRun}}, Page: p, Total: 1}, s.err
}

var sampleAmendment = Amendment{ID: 1, TestRunID: 3, TestCaseID: 154, AmendedBy: 1, AmendedByUsername: "admin", Reason: "was manual",
	CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func (s *stubAPI) Amend(_ context.Context, runID, testCaseID int64, reason string, by authz.Actor) (Amendment, error) {
	s.gotAmend = []any{runID, testCaseID, reason, by}
	return sampleAmendment, s.err
}
func (s *stubAPI) ListAmendments(_ context.Context, _ int64, p pagination.Page) (pagination.Result[Amendment], error) {
	return pagination.Result[Amendment]{Items: []Amendment{sampleAmendment}, Page: p, Total: 1}, s.err
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
		"/api/v1/test-runs/3/parse-errors":                            `"items":[{"index":2,"testName":"t","message":"m","persisted":true,"severity":"warning","shard":null}]`,
		"/api/v1/test-runs/3/live": `{"reconciliation":"mismatch","events":2,"lastSequence":4,"runFinished":false,"waiting":1,"running":0,"finished":1,` +
			`"testCases":[{"testCaseId":153,"testCaseKey":"TC-153","state":"passed"},{"testCaseId":154,"testCaseKey":"CHK-4","state":"waiting"}],` +
			`"mismatches":[{"kind":"status_mismatch","testCaseId":153,"testCaseKey":"TC-153","requestedTestCaseId":null,"liveStatus":"passed","finalStatus":"failed"},` +
			`{"kind":"invalid_correlation","testCaseId":null,"testCaseKey":null,"requestedTestCaseId":"TC-9","liveStatus":null,"finalStatus":null}]}`,
	}
	for target, want := range cases {
		rec := serve(&stubAPI{}, stubCatalog{}, target)
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), want, target)
	}
	rec := serve(&stubAPI{}, stubCatalog{}, "/api/v1/test-runs/3/summary")
	assert.Contains(t, rec.Body.String(), `"testCases":[{"testCaseId":153,"testCaseKey":"TC-153","status":"passed","resultCount":1,"flaky":false},{"testCaseId":154,"testCaseKey":"CHK-4","status":"untested","resultCount":0,"flaky":false}]`)
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
	assert.Nil(t, api.gotFilter.Shard)

	serve(api, stubCatalog{}, "/api/v1/test-runs/3/results?shard=100")
	assert.Equal(t, ptr(int32(100)), api.gotFilter.Shard)
}

func TestHandlerErrors(t *testing.T) {
	failing := &stubAPI{err: apperr.NotFound("missing")}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{getErr: apperr.NotFound("missing")}, stubCatalog{}, "/api/v1/test-runs/3").Code)
	for _, target := range []string{
		"/api/v1/test-runs", "/api/v1/test-runs/3/results",
		"/api/v1/test-runs/3/summary", "/api/v1/test-cases/1/results", "/api/v1/test-runs/3/parse-errors", "/api/v1/test-runs/3/live",
	} {
		assert.Equal(t, http.StatusNotFound, serve(failing, stubCatalog{}, target).Code, target)
	}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{}, stubCatalog{err: apperr.NotFound("TC-1")}, "/api/v1/test-cases/1/results").Code)
	for _, target := range []string{"/api/v1/test-runs/3/results", "/api/v1/test-runs/3/summary", "/api/v1/test-cases/153/results", "/api/v1/test-runs/3/live"} {
		assert.Equal(t, http.StatusInternalServerError, serve(&stubAPI{}, stubCatalog{keysErr: errors.New("db down")}, target).Code, "key lookup failure: "+target)
	}

	for _, target := range []string{
		"/api/v1/test-runs?page=x", "/api/v1/test-runs/x", "/api/v1/test-runs/x/results",
		"/api/v1/test-runs/3/results?page=0", "/api/v1/test-runs/3/results?status=untested",
		"/api/v1/test-runs/3/results?correlation=nope", "/api/v1/test-runs/x/summary", "/api/v1/test-runs/x/live",
		"/api/v1/test-runs/3/results?shard=0", "/api/v1/test-runs/3/results?shard=101", "/api/v1/test-runs/3/results?shard=",
		"/api/v1/test-runs/3/results?shard=01",
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

func serveBody(guard authz.Guard, api API, cat TestCaseChecker, method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, cat, guard).Register(mux)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// DEC-42 over HTTP: maintainers amend a run (the actor is recorded), anyone who sees the run lists the amendments,
// and runs and summaries say whether and how the universe was edited.
func TestHandlerAmendments(t *testing.T) {
	api := &stubAPI{}
	rec := serveBody(adminGuard, api, stubCatalog{}, "POST", "/api/v1/test-runs/3/amendments", `{"testCaseId":154,"reason":"was manual"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"testCaseKey":"CHK-4"`)
	assert.Contains(t, rec.Body.String(), `"amendedByUsername":"admin"`)
	assert.Equal(t, []any{int64(3), int64(154), "was manual", authz.Actor{ID: 1, Username: "admin"}}, api.gotAmend)
	assert.Contains(t, serve(api, stubCatalog{}, "/api/v1/test-runs/3/amendments").Body.String(), `"totalItems":1`)
	assert.Contains(t, serve(api, stubCatalog{}, "/api/v1/test-runs/3").Body.String(), `"amendmentCount":0`)
	assert.Contains(t, serve(api, stubCatalog{}, "/api/v1/test-runs/3/summary").Body.String(), `"snapshotTotal":2,"amendedTestCaseIds":[]`)

	viewer := memberOf(map[int64]authz.Role{sampleRun.ProjectID: authz.RoleMember})
	assert.Equal(t, http.StatusForbidden, serveBody(viewer, api, stubCatalog{}, "POST", "/api/v1/test-runs/3/amendments", `{"testCaseId":154,"reason":"x"}`).Code)
	assert.Equal(t, http.StatusOK, serveBody(viewer, api, stubCatalog{}, "GET", "/api/v1/test-runs/3/amendments", "").Code)

	for _, c := range []struct {
		api          API
		cat          stubCatalog
		guard        authz.Guard
		method, path string
		body         string
		want         int
	}{
		{api, stubCatalog{}, adminGuard, "POST", "/api/v1/test-runs/x/amendments", `{}`, 400},
		{api, stubCatalog{}, adminGuard, "POST", "/api/v1/test-runs/3/amendments", "", 415},
		{api, stubCatalog{}, adminGuard, "GET", "/api/v1/test-runs/x/amendments", "", 400},
		{api, stubCatalog{}, adminGuard, "GET", "/api/v1/test-runs/3/amendments?page=0", "", 400},
		{&stubAPI{err: apperr.Conflict("already")}, stubCatalog{}, adminGuard, "POST", "/api/v1/test-runs/3/amendments", `{}`, 409},
		{&stubAPI{err: apperr.NotFound("gone")}, stubCatalog{}, adminGuard, "GET", "/api/v1/test-runs/3/amendments", "", 404},
		{api, stubCatalog{keysErr: errors.New("db down")}, adminGuard, "POST", "/api/v1/test-runs/3/amendments", `{}`, 500},
		{api, stubCatalog{keysErr: errors.New("db down")}, adminGuard, "GET", "/api/v1/test-runs/3/amendments", "", 500},
		{api, stubCatalog{}, actorless{adminGuard}, "POST", "/api/v1/test-runs/3/amendments", `{}`, 401},
	} {
		assert.Equal(t, c.want, serveBody(c.guard, c.api, c.cat, c.method, c.path, c.body).Code, c.method+" "+c.path)
	}
}

// actorless authorizes like its guard but has no signed-in person (e.g. an API key).
type actorless struct{ stubGuard }

func (actorless) Actor(context.Context) (authz.Actor, error) {
	return authz.Actor{}, apperr.Unauthorized("sign in to continue")
}

// A run carries the suite it was reported for; ?suite=<key> narrows the run list (MVP D2).
func TestHandlerSuite(t *testing.T) {
	api := &stubAPI{}
	rec := serve(api, stubCatalog{}, "/api/v1/test-runs?suite=smoke")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, ptr("smoke"), api.gotSuite)
	assert.Contains(t, rec.Body.String(), `"suite":null`)
	serve(api, stubCatalog{}, "/api/v1/test-runs")
	assert.Nil(t, api.gotSuite)
	for _, q := range []string{"suite=", "suite=Smoke", "suite=-x"} {
		assert.Equal(t, http.StatusBadRequest, serve(api, stubCatalog{}, "/api/v1/test-runs?"+q).Code, q)
	}
	run := sampleRun
	run.SuiteKey, run.SuiteName = "smoke", "Smoke"
	assert.Equal(t, &SuiteRefDTO{Key: "smoke", Name: "Smoke"}, RunDTO(run).Suite)
}

// Manual runs carry their mode and who started them; manual results who recorded them and the failed step.
func TestManualDTOs(t *testing.T) {
	run := sampleRun
	assert.Equal(t, ModeBatch, RunDTO(run).Mode, "runs before modes are batch")
	assert.Nil(t, RunDTO(run).StartedBy)
	run.Mode, run.StartedBy = ModeManual, "ana"
	assert.Equal(t, ModeManual, RunDTO(run).Mode)
	assert.Equal(t, ptr("ana"), RunDTO(run).StartedBy)

	res := sampleResult
	assert.Nil(t, ResultDTO(res, nil).RecordedBy)
	res.RecordedBy, res.FailedStep = "ana", ptr(int32(2))
	dto := ResultDTO(res, map[int64]string{153: "TC-153"})
	assert.Equal(t, ptr("ana"), dto.RecordedBy)
	assert.Equal(t, ptr(int32(2)), dto.FailedStep)
	assert.Equal(t, ptr("TC-153"), dto.TestCaseKey)
}

func TestShardDTOs(t *testing.T) {
	assert.Nil(t, RunDTO(sampleRun).Shards)
	run := sampleRun
	run.Mode, run.ShardTotal = ModeSharded, 3
	assert.Equal(t, &ShardsDTO{Total: 3, Received: []int32{}, Missing: []int32{1, 2, 3}}, RunDTO(run).Shards)
	run.ShardsReceived = []int32{1, 2, 3}
	assert.Equal(t, &ShardsDTO{Total: 3, Received: []int32{1, 2, 3}, Missing: []int32{}}, RunDTO(run).Shards)

	assert.Nil(t, ToParseErrorDTO(ParseError{Index: 1}).Shard)
	assert.Equal(t, ptr(int32(2)), ToParseErrorDTO(ParseError{Index: 1, Shard: 2}).Shard)
	res := sampleResult
	res.Shard = ptr(int32(2))
	assert.Equal(t, ptr(int32(2)), ResultDTO(res, nil).Shard)
}

// Routes on a run authorize it with its project alone: only GET /test-runs/{id} loads the run and its outcome (#44).
func TestHandlerRunReads(t *testing.T) {
	for _, target := range []string{"/api/v1/test-runs/3/results", "/api/v1/test-runs/3/summary", "/api/v1/test-runs/3/parse-errors",
		"/api/v1/test-runs/3/live", "/api/v1/test-runs/3/amendments"} {
		api := &stubAPI{}
		assert.Equal(t, http.StatusOK, serve(api, stubCatalog{}, target).Code, target)
		assert.Zero(t, api.runReads, "no full run read: %s", target)
	}
	api := &stubAPI{}
	serve(api, stubCatalog{}, "/api/v1/test-runs/3")
	assert.Equal(t, 1, api.runReads)
	assert.Equal(t, http.StatusInternalServerError, serve(&stubAPI{runErr: errors.New("db down")}, stubCatalog{}, "/api/v1/test-runs/3").Code)
}
