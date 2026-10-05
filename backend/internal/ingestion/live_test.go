package ingestion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

var liveNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

type liveRecorder struct {
	started  execution.NewRun
	expected []int64
	events   []execution.NewEvent
	run      execution.TestRun
	getErr   error
	err      error
}

func (r *liveRecorder) StartRun(_ context.Context, run execution.NewRun, expected []int64) (execution.TestRun, error) {
	r.started, r.expected = run, expected
	return execution.TestRun{ID: 5, ProjectID: run.ProjectID, Mode: run.Mode, Status: execution.RunRunning}, r.err
}

func (r *liveRecorder) GetRun(context.Context, int64) (execution.TestRun, error) {
	return r.run, r.getErr
}

func (r *liveRecorder) RecordEvents(_ context.Context, _ int64, events []execution.NewEvent) (execution.EventsResult, error) {
	r.events = events
	return execution.EventsResult{Accepted: len(events)}, r.err
}

func newLive(cat *fakeCatalog, rec *liveRecorder, access fakeAccess) *Live {
	return NewLive(NewService(cat, nil, access), rec, func() time.Time { return liveNow })
}

func TestLiveStart(t *testing.T) {
	cat, rec := &fakeCatalog{universe: []int64{1, 2, 3}, selection: []int64{2, 3, 9}}, &liveRecorder{}
	l := newLive(cat, rec, fakeAccess{})
	ctx := context.Background()
	run, err := l.Start(ctx, RunMeta{ProjectKey: "CHK", Provider: "github", ProviderRunID: "7", RunAttempt: 1, Pipeline: "ci", SuiteKey: "smoke"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), run.ID)
	assert.Equal(t, execution.ModeLive, rec.started.Mode)
	assert.Equal(t, int64(2), rec.started.ProjectID)
	assert.Equal(t, "smoke", rec.started.SuiteKey)
	assert.Equal(t, []int64{2, 3}, rec.expected)
	_, err = l.Start(ctx, RunMeta{Provider: "github", ProviderRunID: "7", RunAttempt: 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, rec.expected, "no suite: the active automated test cases")

	for _, c := range []struct {
		meta  RunMeta
		field string
	}{
		{RunMeta{Provider: "", ProviderRunID: "7", RunAttempt: 1}, "provider"},
		{RunMeta{Provider: "github", ProviderRunID: "7", RunAttempt: 1, Status: execution.RunCompleted}, "status"},
	} {
		_, err := l.Start(ctx, c.meta)
		e, ok := apperr.As(err)
		require.True(t, ok)
		assert.Equal(t, c.field, e.Fields[0].Field)
	}
	meta := RunMeta{Provider: "github", ProviderRunID: "7", RunAttempt: 1, SuiteKey: "smoke"}
	for name, l := range map[string]*Live{
		"project": newLive(&fakeCatalog{projectErr: errBoom}, &liveRecorder{}, fakeAccess{}),
		"suite":   newLive(&fakeCatalog{suiteErr: errBoom}, &liveRecorder{}, fakeAccess{}),
		"view":    newLive(&fakeCatalog{viewErr: errBoom}, &liveRecorder{}, fakeAccess{}),
	} {
		_, err := l.Start(ctx, meta)
		assert.ErrorIs(t, err, errBoom, name)
	}
}

func TestLiveEvents(t *testing.T) {
	cat := &fakeCatalog{statuses: map[int64]catalog.Status{3: catalog.StatusActive}}
	rec := &liveRecorder{run: execution.TestRun{ID: 5, ProjectID: 2}}
	l := newLive(cat, rec, fakeAccess{})
	ctx := context.Background()
	at := liveNow.Add(-time.Minute)
	res, err := l.RecordEvents(ctx, 5, []EventInput{
		{EventID: "e1", Sequence: 1, Type: execution.EventTestStarted, TestName: "pay", TestCase: "CHK-3", OccurredAt: &at},
		{EventID: "e2", Sequence: 2, Type: execution.EventTestFinished, TestName: "pay", TestCase: "3", Status: execution.Failed, Attempt: 2},
		{EventID: "e3", Sequence: 3, Type: execution.EventTestFinished, TestName: "x", TestCase: "WEB-3", Status: execution.Passed},
		{EventID: "e4", Sequence: 4, Type: execution.EventTestStarted, TestCase: "CHK-99"},
		{EventID: "e5", Sequence: 5, Type: execution.EventRunFinished},
	})
	require.NoError(t, err)
	assert.Equal(t, 5, res.Accepted)
	assert.Equal(t, int64(2), cat.gotProject)
	assert.Equal(t, []int64{3, 3, 99}, cat.askedIDs)
	three := int64(3)
	assert.Equal(t, &three, rec.events[0].TestCaseID)
	assert.Equal(t, at, rec.events[0].OccurredAt)
	assert.Equal(t, liveNow, rec.events[1].OccurredAt)
	assert.Equal(t, execution.Failed, *rec.events[1].Status)
	assert.Equal(t, int32(1), rec.events[0].Attempt, "default attempt")
	assert.Equal(t, int32(2), rec.events[1].Attempt)
	assert.Nil(t, rec.events[2].TestCaseID, "another project's TC-ID")
	assert.Equal(t, "WEB-3", *rec.events[2].RequestedTestCaseID)
	assert.Nil(t, rec.events[3].TestCaseID, "unknown")
	assert.Nil(t, rec.events[4].RequestedTestCaseID)

	many := make([]EventInput, MaxEventBatch+1)
	for _, c := range []struct {
		events []EventInput
		field  string
	}{
		{nil, "events"},
		{many, "events"},
		{[]EventInput{{EventID: "bad id", Type: execution.EventRunFinished}}, "events[0].eventId"},
		{[]EventInput{{EventID: "a", Sequence: -1, Type: execution.EventRunFinished}}, "events[0].sequence"},
		{[]EventInput{{EventID: "a", Type: execution.EventRunFinished, Attempt: 101}}, "events[0].attempt"},
		{[]EventInput{{EventID: "a", Type: execution.EventRunFinished, Attempt: -1}}, "events[0].attempt"},
		{[]EventInput{{EventID: "a", Type: "test.paused"}}, "events[0].type"},
		{[]EventInput{{EventID: "a", Type: execution.EventTestFinished}}, "events[0].status"},
		{[]EventInput{{EventID: "a", Type: execution.EventTestStarted, Status: execution.Passed}}, "events[0].status"},
		{[]EventInput{{EventID: "a", Type: execution.EventTestStarted, TestName: strings.Repeat("n", 1001)}}, "events[0].testName"},
		{[]EventInput{{EventID: "a", Type: execution.EventTestStarted, TestCase: strings.Repeat("1", 101)}}, "events[0].testCase"},
	} {
		_, err := l.RecordEvents(ctx, 5, c.events)
		e, ok := apperr.As(err)
		require.True(t, ok, c.field)
		assert.Equal(t, c.field, e.Fields[0].Field)
	}
	ok := []EventInput{{EventID: "a", Type: execution.EventRunFinished}}
	_, err = newLive(cat, &liveRecorder{getErr: apperr.NotFound("x")}, fakeAccess{}).RecordEvents(ctx, 5, ok)
	assert.Error(t, err)
	_, err = newLive(cat, &liveRecorder{run: execution.TestRun{ProjectID: 2}}, fakeAccess{denied: map[int64]error{2: nil}}).RecordEvents(ctx, 5, ok)
	e, _ := apperr.As(err)
	assert.Equal(t, apperr.KindNotFound, e.Kind)
	_, err = newLive(&fakeCatalog{projectErr: errBoom}, &liveRecorder{}, fakeAccess{}).RecordEvents(ctx, 5, ok)
	assert.ErrorIs(t, err, errBoom)
	_, err = newLive(&fakeCatalog{viewErr: errBoom}, &liveRecorder{}, fakeAccess{}).RecordEvents(ctx, 5, ok)
	assert.ErrorIs(t, err, errBoom)
}

type stubLive struct {
	meta   RunMeta
	events []EventInput
	err    error
}

func (s *stubLive) Start(_ context.Context, meta RunMeta) (execution.TestRun, error) {
	s.meta = meta
	return execution.TestRun{ID: 5, Mode: execution.ModeLive, Status: execution.RunRunning}, s.err
}

func (s *stubLive) RecordEvents(_ context.Context, _ int64, events []EventInput) (execution.EventsResult, error) {
	s.events = events
	return execution.EventsResult{Accepted: 1, Duplicates: 1}, s.err
}

func TestLiveHandler(t *testing.T) {
	serve := func(api LiveAPI, target, body string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		NewLiveHandler(api).Register(mux)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		return rec
	}
	api := &stubLive{}
	rec := serve(api, "/api/v1/test-runs/live", `{"project":"CHK","suite":"smoke","provider":"github","runId":"7","runAttempt":2,"pipeline":"ci","branch":"main","commit":"abc"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"mode":"live"`)
	assert.Equal(t, RunMeta{ProjectKey: "CHK", SuiteKey: "smoke", Provider: "github", ProviderRunID: "7", RunAttempt: 2, Pipeline: "ci", Branch: "main", Commit: "abc"}, api.meta)
	rec = serve(api, "/api/v1/test-runs/5/events", `{"events":[{"eventId":"a","sequence":1,"type":"test.finished","testName":"t","testCase":"CHK-1","status":"passed","occurredAt":"2026-10-05T12:00:00Z"}]}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"accepted":1,"duplicates":1}`, rec.Body.String())
	assert.Equal(t, liveNow, *api.events[0].OccurredAt)
	assert.Equal(t, "CHK-1", api.events[0].TestCase)

	for _, c := range []struct{ target, body string }{
		{"/api/v1/test-runs/live", `nope`}, {"/api/v1/test-runs/x/events", `{}`}, {"/api/v1/test-runs/5/events", `nope`},
	} {
		assert.Equal(t, http.StatusBadRequest, serve(&stubLive{}, c.target, c.body).Code, c.target)
	}
	failing := &stubLive{err: apperr.Conflict("x")}
	assert.Equal(t, http.StatusConflict, serve(failing, "/api/v1/test-runs/live", `{}`).Code)
	assert.Equal(t, http.StatusConflict, serve(failing, "/api/v1/test-runs/5/events", `{"events":[]}`).Code)
}
