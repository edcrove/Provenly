package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func manualRun() NewRun {
	return NewRun{ProjectID: 1, Provider: "manual", ProviderRunID: "abc", RunAttempt: 1, Pipeline: "Release sign-off", StartedBy: "ana"}
}

func manualResult(tc int64, status ResultStatus) NewResult {
	key := "TC-" + string(rune('0'+tc))
	return NewResult{TestCaseID: &tc, RequestedTestCaseID: &key, TestName: key, Status: status, RecordedBy: "ana"}
}

func kindIs(t *testing.T, err error, k apperr.Kind) {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, k, e.Kind, "%v", err)
}

func TestManualRunLifecycle(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, func() time.Time { return fixedNow })
	ctx := context.Background()

	run, err := svc.StartRun(ctx, manualRun(), []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, RunRunning, run.Status)
	assert.Equal(t, ModeManual, run.Mode)
	assert.Nil(t, run.CompletedAt)
	assert.Equal(t, int32(2), run.ExpectedCount)
	assert.Equal(t, int32(2), run.Outcome.Untested)
	_, err = svc.StartRun(ctx, manualRun(), nil)
	kindIs(t, err, apperr.KindConflict)

	// Results arrive one by one; a re-test is the next attempt and the last one counts.
	res, err := svc.RecordResult(ctx, run.ID, manualResult(1, Failed))
	require.NoError(t, err)
	assert.Equal(t, int32(1), res.Attempt)
	res, err = svc.RecordResult(ctx, run.ID, manualResult(1, Passed))
	require.NoError(t, err)
	assert.Equal(t, int32(2), res.Attempt)
	_, err = svc.RecordResult(ctx, run.ID, manualResult(9, Passed))
	kindIs(t, err, apperr.KindConflict)
	for i := 1; i <= MaxAttempts; i++ {
		_, err = svc.RecordResult(ctx, run.ID, manualResult(2, Passed))
		require.NoError(t, err, "attempt %d", i)
	}
	_, err = svc.RecordResult(ctx, run.ID, manualResult(2, Passed))
	kindIs(t, err, apperr.KindConflict)

	done, err := svc.FinishRun(ctx, run.ID, RunCompleted)
	require.NoError(t, err)
	assert.Equal(t, RunCompleted, done.Status)
	assert.NotNil(t, done.CompletedAt)
	_, err = svc.RecordResult(ctx, run.ID, manualResult(2, Passed))
	kindIs(t, err, apperr.KindConflict)
	_, err = svc.FinishRun(ctx, run.ID, RunCancelled)
	kindIs(t, err, apperr.KindConflict)

	// Batch runs take no manual results; unknown runs are not found; only completed or cancelled finish a run.
	batch, _, err := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "1", RunAttempt: 1}, []int64{1}, nil, nil)
	require.NoError(t, err)
	_, err = svc.RecordResult(ctx, batch.ID, manualResult(1, Passed))
	kindIs(t, err, apperr.KindConflict)
	_, err = svc.FinishRun(ctx, 999, RunCompleted)
	kindIs(t, err, apperr.KindNotFound)
	_, err = svc.FinishRun(ctx, run.ID, RunInterrupted)
	kindIs(t, err, apperr.KindValidation)
}

func TestManualRunRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	for _, method := range []string{"InsertTestRun", "InsertExpectedCases", "GetTestRun", "ListSummaryInputs"} {
		repo := newFakeRepo()
		repo.errs[method] = errBoom
		_, err := NewService(repo, func() time.Time { return fixedNow }).StartRun(ctx, manualRun(), []int64{1})
		assert.ErrorIs(t, err, errBoom, method)
	}
	for _, method := range []string{"LockTestRun", "IsInUniverse", "InsertManualResult"} {
		repo := newFakeRepo()
		svc := NewService(repo, func() time.Time { return fixedNow })
		run, err := svc.StartRun(ctx, manualRun(), []int64{1})
		require.NoError(t, err)
		repo.errs[method] = errBoom
		_, err = svc.RecordResult(ctx, run.ID, manualResult(1, Passed))
		assert.ErrorIs(t, err, errBoom, method)
	}
	for _, method := range []string{"LockTestRun", "FinishTestRun", "GetTestRun", "ListSummaryInputs"} {
		repo := newFakeRepo()
		svc := NewService(repo, func() time.Time { return fixedNow })
		run, err := svc.StartRun(ctx, manualRun(), []int64{1})
		require.NoError(t, err)
		repo.errs[method] = errBoom
		_, err = svc.FinishRun(ctx, run.ID, RunCompleted)
		assert.ErrorIs(t, err, errBoom, method)
	}
}

// A manual re-test that passes after a failure is a fix, not flakiness; CI retries still are.
func TestManualRetestIsNotFlaky(t *testing.T) {
	manual := "\x1f" + ManualClass + "\x1fTC-1"
	s := ComputeSummary(1, []int64{1, 2}, []ValidResult{
		{TestCaseID: 1, Execution: manual, Attempt: 1, Status: Failed}, {TestCaseID: 1, Execution: manual, Attempt: 2, Status: Passed},
		{TestCaseID: 2, Execution: "s\x1fc\x1flogin", Attempt: 1, Status: Failed}, {TestCaseID: 2, Execution: "s\x1fc\x1flogin", Attempt: 2, Status: Passed},
	}, nil)
	assert.Equal(t, int32(2), s.Counts.Passed)
	assert.Equal(t, int32(1), s.Flaky)
}

// The latest status of a test case is its logical status in the latest run with a result for it.
func TestLatestStatuses(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, func() time.Time { return fixedNow })
	ctx := context.Background()
	tc1, tc2 := int64(1), int64(2)
	repo.results[1] = []TestResult{{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Failed, Attempt: 1}}
	repo.results[2] = []TestResult{
		{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Failed, Attempt: 1},
		{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Passed, Attempt: 2},
		{TestCaseID: &tc2, Correlation: CorrelationValid, TestName: "chrome", Status: Passed, Attempt: 1},
		{TestCaseID: &tc2, Correlation: CorrelationValid, TestName: "firefox", Status: Skipped, Attempt: 1},
	}
	got, err := svc.LatestStatuses(ctx, []int64{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{1: "passed", 2: "skipped"}, got)
	repo.errs["ListLatestResults"] = errBoom
	_, err = svc.LatestStatuses(ctx, []int64{1})
	assert.ErrorIs(t, err, errBoom)
}

// The latest conclusive status of a test case skips runs where it was only skipped.
func TestLatestConclusive(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, func() time.Time { return fixedNow })
	ctx := context.Background()
	tc1, tc2 := int64(1), int64(2)
	repo.results[1] = []TestResult{{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Failed, Attempt: 1}}
	repo.results[2] = []TestResult{
		{TestCaseID: &tc1, Correlation: CorrelationValid, TestName: "a", Status: Skipped, Attempt: 1},
		{TestCaseID: &tc2, Correlation: CorrelationValid, TestName: "b", Status: Error, Attempt: 1},
		{TestCaseID: &tc2, Correlation: CorrelationValid, TestName: "b", Status: Passed, Attempt: 2},
	}
	statuses, runs, err := svc.LatestConclusive(ctx, []int64{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{1: "failed", 2: "passed"}, statuses)
	assert.Equal(t, map[int64]int64{1: 1, 2: 2}, runs)
	repo.errs["ListLatestConclusive"] = errBoom
	_, _, err = svc.LatestConclusive(ctx, []int64{1})
	assert.ErrorIs(t, err, errBoom)
}

// Last executions and flaky counts come from the repository.
func TestQualityReads(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, func() time.Time { return fixedNow })
	ctx := context.Background()
	tc1 := int64(1)
	repo.runs[1] = TestRun{ID: 1, CreatedAt: fixedNow.Add(-time.Hour)}
	repo.runs[2] = TestRun{ID: 2, CreatedAt: fixedNow}
	repo.results[1] = []TestResult{{TestCaseID: &tc1, Correlation: CorrelationValid, Status: Passed}}
	repo.results[2] = []TestResult{{TestCaseID: &tc1, Correlation: CorrelationValid, Status: Failed}, {Correlation: CorrelationMissing}}
	last, err := svc.LastExecuted(ctx, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, map[int64]time.Time{1: fixedNow}, last)
	repo.flaky = []FlakyCount{{TestCaseID: 1, Runs: 3}, {TestCaseID: 2, Runs: 1}}
	flaky, err := svc.FlakyCounts(ctx, 1, 20, 1)
	require.NoError(t, err)
	assert.Equal(t, []FlakyCount{{TestCaseID: 1, Runs: 3}}, flaky)
	repo.errs["ListLastExecuted"] = errBoom
	_, err = svc.LastExecuted(ctx, []int64{1})
	assert.ErrorIs(t, err, errBoom)
}
