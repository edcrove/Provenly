package ingestion

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

type manualCatalog struct {
	selection    []int64
	gotAutomated *bool
	gotSuite     string
	err          error
	keysErr      error
}

func (c *manualCatalog) ProjectByKey(_ context.Context, key string) (catalog.Project, error) {
	if key == "NOPE" {
		return catalog.Project{}, apperr.NotFound("project NOPE not found")
	}
	return catalog.Project{ID: 2, Key: key}, nil
}

func (c *manualCatalog) Selection(_ context.Context, _ int64, key string, automated *bool) (catalog.Suite, []int64, error) {
	c.gotSuite, c.gotAutomated = key, automated
	return catalog.Suite{Key: key, Name: "Suite " + key}, c.selection, c.err
}

func (c *manualCatalog) Keys(_ context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for _, id := range ids {
		if id != 404 {
			out[id] = "CHK-" + string(rune('0'+id))
		}
	}
	return out, c.keysErr
}

type manualRecorder struct {
	started  execution.NewRun
	expected []int64
	recorded execution.NewResult
	finished execution.RunStatus
	getErr   error
	err      error
}

func (r *manualRecorder) StartRun(_ context.Context, run execution.NewRun, expected []int64) (execution.TestRun, error) {
	r.started, r.expected = run, expected
	return execution.TestRun{ID: 9, ProjectID: run.ProjectID, Mode: execution.ModeManual, Status: execution.RunRunning}, r.err
}

func (r *manualRecorder) GetRun(_ context.Context, id int64) (execution.TestRun, error) {
	return execution.TestRun{ID: id, ProjectID: 2}, r.getErr
}

func (r *manualRecorder) RecordResult(_ context.Context, runID int64, res execution.NewResult) (execution.TestResult, error) {
	r.recorded = res
	return execution.TestResult{ID: 1, TestRunID: runID, TestCaseID: res.TestCaseID, TestName: res.TestName, Status: res.Status, RecordedBy: res.RecordedBy}, r.err
}

func (r *manualRecorder) FinishRun(_ context.Context, runID int64, status execution.RunStatus) (execution.TestRun, error) {
	r.finished = status
	return execution.TestRun{ID: runID, Status: status}, r.err
}

type manualAccess struct {
	role     authz.Role
	actorErr error
}

func (a manualAccess) Require(_ context.Context, _ int64, minRole authz.Role, notFound error) error {
	switch {
	case a.role == authz.RoleNone:
		return notFound
	case a.role < minRole:
		return apperr.Forbidden("needs %s", minRole)
	}
	return nil
}

func (a manualAccess) Actor(context.Context) (authz.Actor, error) {
	return authz.Actor{ID: 2, Username: "ana"}, a.actorErr
}

func TestManualStart(t *testing.T) {
	cat, rec := &manualCatalog{selection: []int64{3, 4}}, &manualRecorder{}
	m := NewManual(cat, rec, manualAccess{role: authz.RoleMember})
	ctx := context.Background()
	run, err := m.Start(ctx, ManualRunInput{ProjectKey: "CHK", Name: " Release sign-off ", Branch: "main"})
	require.NoError(t, err)
	assert.Equal(t, int64(9), run.ID)
	require.NotNil(t, cat.gotAutomated)
	assert.False(t, *cat.gotAutomated, "default scope: the manual test cases")
	assert.Equal(t, []int64{3, 4}, rec.expected)
	assert.Equal(t, "Release sign-off", rec.started.Pipeline)
	assert.Equal(t, "manual", rec.started.Provider)
	assert.Len(t, rec.started.ProviderRunID, 16)
	assert.Equal(t, "ana", rec.started.StartedBy)
	assert.Empty(t, rec.started.SuiteKey)

	_, err = m.Start(ctx, ManualRunInput{ProjectKey: "CHK", Name: "x", Scope: ScopeAll, SuiteKey: "smoke"})
	require.NoError(t, err)
	assert.Nil(t, cat.gotAutomated, "scope all: every active test case")
	assert.Equal(t, "smoke", rec.started.SuiteKey)
	assert.Equal(t, "Suite smoke", rec.started.SuiteName)

	for _, in := range []ManualRunInput{
		{ProjectKey: "chk", Name: "x"}, {ProjectKey: "CHK", Name: " "}, {ProjectKey: "CHK", Name: strings.Repeat("n", 201)},
		{ProjectKey: "CHK", Name: "x", Scope: "some"}, {ProjectKey: "CHK", Name: "x", SuiteKey: "Bad"},
		{ProjectKey: "CHK", Name: "x", Branch: strings.Repeat("b", 256)}, {ProjectKey: "CHK", Name: "x", Commit: strings.Repeat("c", 65)},
		{ProjectKey: "CHK", Name: "a\x00"},
	} {
		_, err := m.Start(ctx, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
	_, err = m.Start(ctx, ManualRunInput{ProjectKey: "NOPE", Name: "x"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = NewManual(cat, rec, manualAccess{role: authz.RoleViewer}).Start(ctx, ManualRunInput{ProjectKey: "CHK", Name: "x"})
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	_, err = NewManual(cat, rec, manualAccess{role: authz.RoleMember, actorErr: errBoomIngest}).Start(ctx, ManualRunInput{ProjectKey: "CHK", Name: "x"})
	assert.ErrorIs(t, err, errBoomIngest)
	cat.err = apperr.Conflict("archived")
	_, err = m.Start(ctx, ManualRunInput{ProjectKey: "CHK", Name: "x", SuiteKey: "smoke"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
}

var errBoomIngest = errors.New("boom")

func kindOf(t *testing.T, err error) apperr.Kind {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	return e.Kind
}

func TestManualRecordAndFinish(t *testing.T) {
	cat, rec := &manualCatalog{}, &manualRecorder{}
	m := NewManual(cat, rec, manualAccess{role: authz.RoleMember})
	ctx := context.Background()
	step := int32(2)
	res, err := m.Record(ctx, 9, ManualResultInput{TestCaseID: 3, Status: execution.Failed, Note: "button missing", FailedStep: &step})
	require.NoError(t, err)
	assert.Equal(t, "CHK-3", rec.recorded.TestName)
	assert.Equal(t, execution.ManualClass, rec.recorded.ClassName)
	assert.Equal(t, "button missing", rec.recorded.ErrorMessage)
	assert.Equal(t, "ana", rec.recorded.RecordedBy)
	assert.Equal(t, &step, rec.recorded.FailedStep)
	assert.Equal(t, "ana", res.RecordedBy)

	bad := []ManualResultInput{
		{TestCaseID: 0, Status: execution.Passed}, {TestCaseID: 3, Status: "blocked"}, {TestCaseID: 3, Status: execution.Passed, Note: strings.Repeat("n", 10001)},
		{TestCaseID: 3, Status: execution.Passed, Note: "a\x00"}, {TestCaseID: 3, Status: execution.Failed, FailedStep: ptrInt32(0)},
		{TestCaseID: 3, Status: execution.Passed, FailedStep: ptrInt32(1)}, {TestCaseID: 3, Status: execution.Passed, DurationMs: ptrInt64(-1)},
	}
	for _, in := range bad {
		_, err := m.Record(ctx, 9, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
	_, err = m.Record(ctx, 9, ManualResultInput{TestCaseID: 404, Status: execution.Passed})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "a test case that does not exist is not expected")
	cat.keysErr = errBoomIngest
	_, err = m.Record(ctx, 9, ManualResultInput{TestCaseID: 3, Status: execution.Passed})
	assert.ErrorIs(t, err, errBoomIngest)
	_, err = NewManual(cat, rec, manualAccess{role: authz.RoleMember, actorErr: errBoomIngest}).Record(ctx, 9, ManualResultInput{TestCaseID: 3, Status: execution.Passed})
	assert.ErrorIs(t, err, errBoomIngest)
	_, err = NewManual(cat, rec, manualAccess{}).Record(ctx, 9, ManualResultInput{TestCaseID: 3, Status: execution.Passed})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err), "a run of an invisible project is not found")

	run, err := m.Finish(ctx, 9, execution.RunCompleted)
	require.NoError(t, err)
	assert.Equal(t, execution.RunCompleted, run.Status)
	_, err = NewManual(cat, &manualRecorder{getErr: apperr.NotFound("run")}, manualAccess{role: authz.RoleMember}).Finish(ctx, 9, execution.RunCompleted)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }

type stubManual struct {
	start  ManualRunInput
	record ManualResultInput
	status execution.RunStatus
	err    error
}

func (s *stubManual) Start(_ context.Context, in ManualRunInput) (execution.TestRun, error) {
	s.start = in
	return execution.TestRun{ID: 9, Mode: execution.ModeManual, Status: execution.RunRunning, StartedBy: "ana"}, s.err
}

func (s *stubManual) Record(_ context.Context, runID int64, in ManualResultInput) (execution.TestResult, error) {
	s.record = in
	id := in.TestCaseID
	return execution.TestResult{ID: 1, TestRunID: runID, TestCaseID: &id, TestName: "CHK-3", Status: in.Status, RecordedBy: "ana"}, s.err
}

func (s *stubManual) Finish(_ context.Context, runID int64, status execution.RunStatus) (execution.TestRun, error) {
	s.status = status
	return execution.TestRun{ID: runID, Status: status}, s.err
}

func serveManual(api ManualAPI, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewManualHandler(api).Register(mux)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestManualHandlers(t *testing.T) {
	api := &stubManual{}
	rec := serveManual(api, "/api/v1/test-runs/manual", `{"project":"CHK","suite":"smoke","name":"Sign-off","scope":"all","branch":"main","commit":"abc"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"mode":"manual","startedBy":"ana"`)
	assert.Equal(t, ManualRunInput{ProjectKey: "CHK", SuiteKey: "smoke", Name: "Sign-off", Scope: ScopeAll, Branch: "main", Commit: "abc"}, api.start)

	rec = serveManual(api, "/api/v1/test-runs/9/manual-results", `{"testCaseId":3,"status":"failed","note":"n","failedStep":2}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"testCaseKey":"CHK-3"`)
	assert.Contains(t, rec.Body.String(), `"recordedBy":"ana"`)
	assert.Equal(t, ManualResultInput{TestCaseID: 3, Status: execution.Failed, Note: "n", FailedStep: ptrInt32(2)}, api.record)

	rec = serveManual(api, "/api/v1/test-runs/9/finish", `{"status":"cancelled"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, execution.RunCancelled, api.status)

	for _, c := range []struct{ target, body string }{
		{"/api/v1/test-runs/manual", `nope`}, {"/api/v1/test-runs/x/manual-results", `{}`}, {"/api/v1/test-runs/9/manual-results", `[]`},
		{"/api/v1/test-runs/0/finish", `{}`}, {"/api/v1/test-runs/9/finish", `nope`},
	} {
		assert.Equal(t, http.StatusBadRequest, serveManual(api, c.target, c.body).Code, c.target)
	}
	failing := &stubManual{err: apperr.Conflict("run 9 is completed")}
	for target, body := range map[string]string{"/api/v1/test-runs/manual": `{"project":"CHK"}`,
		"/api/v1/test-runs/9/manual-results": `{"testCaseId":3}`, "/api/v1/test-runs/9/finish": `{"status":"completed"}`} {
		assert.Equal(t, http.StatusConflict, serveManual(failing, target, body).Code, target)
	}
}
