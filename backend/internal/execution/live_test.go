package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func ev(id string, seq int64, typ EventType, tc int64, status ResultStatus) NewEvent {
	e := NewEvent{EventID: id, Sequence: seq, Type: typ, TestName: "t", OccurredAt: fixedNow}
	if tc != 0 {
		e.TestCaseID = &tc
	}
	if status != "" {
		e.Status = &status
	}
	return e
}

func stored(events ...NewEvent) []Event {
	out := make([]Event, len(events))
	for i, e := range events {
		out[i] = Event{NewEvent: e}
	}
	return out
}

func TestLiveStateAndReconcile(t *testing.T) {
	bad := "TC-999"
	invalid := NewEvent{EventID: "x", Sequence: 9, Type: EventTestStarted, RequestedTestCaseID: &bad}
	events := stored(
		ev("1", 1, EventTestStarted, 1, ""), ev("2", 2, EventStepStarted, 1, ""), ev("3", 3, EventTestFinished, 1, Passed),
		ev("4", 4, EventTestStarted, 2, ""), ev("5", 5, EventTestFinished, 3, Failed), ev("6", 6, EventTestFinished, 3, Failed),
		ev("7", 7, EventTestFinished, 4, Passed), invalid, ev("8", 10, EventRunFinished, 0, ""),
	)
	live, byCase := liveState([]int64{1, 2, 3, 5}, events)
	assert.Equal(t, []LiveCase{{1, "passed"}, {2, LiveRunning}, {3, "failed"}, {5, LiveWaiting}}, live.Cases)
	assert.Equal(t, [3]int32{1, 1, 2}, [3]int32{live.Waiting, live.Running, live.Finished})
	assert.Equal(t, int32(9), live.Events)
	assert.Equal(t, int64(10), *live.LastSequence)
	assert.True(t, live.RunFinished)

	got := reconcile(byCase, map[int64]string{1: "failed", 2: "passed", 3: "failed", 5: "passed"}, events)
	kinds := map[string][]int64{}
	for _, m := range got {
		if m.TestCaseID != nil {
			kinds[m.Kind] = append(kinds[m.Kind], *m.TestCaseID)
		} else {
			assert.Equal(t, "TC-999", m.Requested)
			kinds[m.Kind] = append(kinds[m.Kind], 0)
		}
	}
	assert.Equal(t, map[string][]int64{
		MismatchStatus: {1}, MismatchStartedNotFinished: {2}, MismatchDuplicate: {3}, MismatchLiveOnly: {4},
		MismatchFinalOnly: {5}, MismatchInvalidCorrelation: {0},
	}, kinds)
	assert.Equal(t, Mismatch{Kind: MismatchStatus, TestCaseID: ptr(int64(1)), LiveStatus: "passed", FinalStatus: "failed"}, got[0])
	assert.Empty(t, reconcile(map[int64][]Event{1: stored(ev("1", 1, EventTestFinished, 1, Passed))}, map[int64]string{1: "passed"}, nil))
}

func TestLiveRuns(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, func() time.Time { return fixedNow })
	ctx := context.Background()
	start := NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "77", RunAttempt: 1, Mode: ModeLive}
	run, err := svc.StartRun(ctx, start, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, ModeLive, run.Mode)
	assert.Equal(t, RunRunning, run.Status)
	require.NotNil(t, run.StartedAt, "a live run starts when it is created")
	assert.Equal(t, fixedNow, *run.StartedAt)
	again, err := svc.StartRun(ctx, start, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, run.ID, again.ID, "a runner retrying the start gets the same run")

	res, err := svc.RecordEvents(ctx, run.ID, []NewEvent{ev("a", 1, EventTestStarted, 1, ""), ev("b", 2, EventTestFinished, 1, Passed), ev("a", 1, EventTestStarted, 1, "")})
	require.NoError(t, err)
	assert.Equal(t, EventsResult{Accepted: 2, Duplicates: 1}, res)
	live, err := svc.Live(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, ReconciliationPending, live.Reconciliation)
	assert.Equal(t, []LiveCase{{1, "passed"}, {2, LiveWaiting}}, live.Cases)

	// A test case outside the universe (e.g. marked manual) also runs and streams its events.
	_, err = svc.RecordEvents(ctx, run.ID, []NewEvent{ev("c", 4, EventTestStarted, 3, ""), ev("d", 5, EventTestFinished, 3, Passed)})
	require.NoError(t, err)

	// The final report completes the run; the live events are reconciled with it, outside the universe too.
	tc1, tc2, tc3 := int64(1), int64(2), int64(3)
	done, created, err := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "77", RunAttempt: 1, ReportSHA256: "abc"}, []int64{9},
		[]NewResult{{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Passed, Attempt: 1},
			{TestCaseID: &tc2, Correlation: CorrelationValid, TestName: "b", Status: Failed, Attempt: 1},
			{TestCaseID: &tc3, Correlation: CorrelationValid, TestName: "c", Status: Passed, Attempt: 1}}, nil)
	require.NoError(t, err)
	assert.True(t, created, "the report's results were recorded")
	assert.Equal(t, RunCompleted, done.Status)
	assert.Equal(t, "abc", repo.runs[run.ID].ReportSHA256)
	live, err = svc.Live(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, ReconciliationMismatch, live.Reconciliation)
	assert.Equal(t, []Mismatch{{Kind: MismatchFinalOnly, TestCaseID: &tc2, FinalStatus: "failed"}}, live.Mismatches,
		"TC 3 ran outside the universe and agrees: not live_only")
	_, created, err = svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "77", RunAttempt: 1}, nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, created, "a completed live run is replayed like any run")

	// Refusals.
	_, err = svc.RecordEvents(ctx, run.ID, []NewEvent{ev("c", 3, EventTestStarted, 1, "")})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "finished")
	batch, _, _ := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "78", RunAttempt: 1}, nil, nil, nil)
	_, err = svc.RecordEvents(ctx, batch.ID, nil)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "not live")
	_, err = svc.Live(ctx, batch.ID)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	_, err = svc.RecordEvents(ctx, 999, nil)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.Live(ctx, 999)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.StartRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "78", RunAttempt: 1, Mode: ModeLive}, nil)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "a batch run with the id")
	full, _ := svc.StartRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "79", RunAttempt: 1, Mode: ModeLive}, nil)
	for i := range MaxRunEvents {
		repo.events[full.ID] = append(repo.events[full.ID], Event{NewEvent: NewEvent{EventID: string(rune(i))}})
	}
	_, err = svc.RecordEvents(ctx, full.ID, []NewEvent{ev("z", 1, EventRunFinished, 0, "")})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err), "full")

	// Repository failures surface.
	live2, _ := svc.StartRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "80", RunAttempt: 1, Mode: ModeLive}, nil)
	for _, m := range []string{"LockTestRun", "CountRunEvents", "InsertRunEvent"} {
		repo.errs[m] = errBoom
		_, err = svc.RecordEvents(ctx, live2.ID, []NewEvent{ev("q", 1, EventRunFinished, 0, "")})
		assert.ErrorIs(t, err, errBoom, m)
		delete(repo.errs, m)
	}
	for _, m := range []string{"ListRunEvents", "ListSummaryInputs"} {
		repo.errs[m] = errBoom
		_, err = svc.Live(ctx, live2.ID)
		assert.ErrorIs(t, err, errBoom, m)
		delete(repo.errs, m)
	}
	finalize := func() error {
		_, _, err := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "80", RunAttempt: 1}, nil,
			[]NewResult{{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Passed, Attempt: 1}}, []ParseError{{TestName: "x"}})
		return err
	}
	for _, m := range []string{"LockTestRun", "InsertTestResults", "InsertParseErrors", "CompleteLiveRun"} {
		repo.errs[m] = errBoom
		assert.ErrorIs(t, finalize(), errBoom, m)
		delete(repo.errs, m)
	}
	repo.errs["GetTestRunIDByExternalID"] = errBoom
	_, err = svc.StartRun(ctx, start, nil)
	assert.ErrorIs(t, err, errBoom)
	delete(repo.errs, "GetTestRunIDByExternalID")
	repo.errs["GetTestRun"] = errBoom
	_, err = svc.StartRun(ctx, start, nil)
	assert.ErrorIs(t, err, errBoom)
}

// Retries are new attempts, not duplicates; variants of one test case aggregate like summaries.
func TestLiveStatus(t *testing.T) {
	at := func(name string, attempt int32, typ EventType, status ResultStatus) Event {
		e := Event{NewEvent: ev(name, 0, typ, 1, status)}
		e.TestName, e.Attempt = name, attempt
		return e
	}
	status, dup, unfinished := liveStatus([]Event{at("a", 1, EventTestStarted, ""), at("a", 1, EventTestFinished, Failed), at("a", 2, EventTestStarted, ""), at("a", 2, EventTestFinished, Passed)})
	assert.Equal(t, "passed", status)
	assert.False(t, dup)
	assert.False(t, unfinished)
	status, dup, _ = liveStatus([]Event{at("chrome", 1, EventTestFinished, Passed), at("firefox", 1, EventTestFinished, Skipped), at("firefox", 1, EventTestFinished, Skipped)})
	assert.Equal(t, "skipped", status)
	assert.True(t, dup)
	status, _, unfinished = liveStatus([]Event{at("a", 1, EventTestFinished, Passed), at("b", 1, EventTestStarted, "")})
	assert.Equal(t, "passed", status)
	assert.True(t, unfinished)
	status, _, unfinished = liveStatus([]Event{at("b", 1, EventTestStarted, "")})
	assert.Equal(t, "", status)
	assert.True(t, unfinished)
}
