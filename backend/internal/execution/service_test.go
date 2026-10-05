package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

var errBoom = errors.New("boom")

func ptr[T any](v T) *T { return &v }

func setup() (*Service, *fakeRepo, context.Context) {
	repo := newFakeRepo()
	return NewService(repo, func() time.Time { return fixedNow }), repo, context.Background()
}

func run(attempt int32) NewRun {
	return NewRun{Provider: "github", ProviderRunID: "42", RunAttempt: attempt, Branch: "main", Commit: "abc"}
}

func valid(id int64, s ResultStatus, name string) NewResult {
	return NewResult{TestCaseID: &id, RequestedTestCaseID: ptr("TC-x"), Correlation: CorrelationValid, TestName: name, Status: s}
}

func TestRecordRunIsIdempotentPerAttempt(t *testing.T) {
	svc, _, ctx := setup()
	first, created, err := svc.RecordRun(ctx, run(1), []int64{1, 2}, []NewResult{valid(1, Passed, "a")}, nil)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "github:42:1", first.ExternalRunID)
	assert.Equal(t, RunCompleted, first.Status)
	assert.Equal(t, int32(2), first.ExpectedCount)
	assert.Equal(t, int32(1), first.ResultCount)
	assert.Equal(t, fixedNow, *first.CompletedAt)

	replay, created, err := svc.RecordRun(ctx, run(1), []int64{1, 2, 3}, []NewResult{valid(1, Failed, "a"), valid(2, Passed, "b")}, nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, replay.ID)
	assert.Equal(t, int32(2), replay.ExpectedCount, "snapshot is not recomputed on replay")
	assert.Equal(t, int32(1), replay.ResultCount, "replay does not add results")

	rerun, created, err := svc.RecordRun(ctx, run(2), []int64{1}, nil, nil)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, first.ID, rerun.ID)
	assert.Equal(t, "github:42:2", rerun.ExternalRunID)

	old, err := svc.GetRun(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), old.ResultCount, "previous attempt history is preserved")
}

func TestRecordRunStoresTheReportedStatus(t *testing.T) {
	svc, _, ctx := setup()
	r := run(1)
	r.Status = RunInterrupted
	got, _, err := svc.RecordRun(ctx, r, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, RunInterrupted, got.Status)
}

func TestRecordRunLeavesAFutureStartUnknown(t *testing.T) {
	svc, _, ctx := setup()
	r := run(1)
	r.StartedAt = ptr(fixedNow.Add(time.Second))
	got, _, err := svc.RecordRun(ctx, r, nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, got.StartedAt, "a run cannot start after it was recorded")

	r = run(3)
	r.StartedAt = ptr(time.Unix(0, 0).UTC())
	got, _, err = svc.RecordRun(ctx, r, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(0, 0).UTC(), *got.StartedAt, "an old timestamp is kept as reported")

	r = run(2)
	r.StartedAt = ptr(fixedNow)
	got, _, err = svc.RecordRun(ctx, r, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, fixedNow, *got.StartedAt)
}

func TestRecordRunErrors(t *testing.T) {
	for _, m := range []string{"InTx", "InsertTestRun", "InsertExpectedCases", "InsertTestResults", "InsertParseErrors", "GetTestRun"} {
		svc, repo, ctx := setup()
		repo.errs[m] = errBoom
		_, _, err := svc.RecordRun(ctx, run(1), []int64{1}, nil, nil)
		assert.ErrorIs(t, err, errBoom, m)
	}
	svc, repo, ctx := setup()
	_, _, _ = svc.RecordRun(ctx, run(1), nil, nil, nil)
	repo.errs["GetTestRunIDByExternalID"] = errBoom
	_, _, err := svc.RecordRun(ctx, run(1), nil, nil, nil)
	assert.ErrorIs(t, err, errBoom)
}

func TestGetAndListRuns(t *testing.T) {
	svc, repo, ctx := setup()
	_, _, _ = svc.RecordRun(ctx, run(1), nil, nil, nil)
	_, _, _ = svc.RecordRun(ctx, run(2), nil, nil, nil)
	res, err := svc.ListRuns(ctx, nil, pagination.Page{Number: 1, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)
	assert.Equal(t, int64(2), res.Items[0].ID)

	_, err = svc.GetRun(ctx, 99)
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindNotFound, e.Kind)

	repo.errs["GetTestRun"] = errBoom
	_, err = svc.GetRun(ctx, 1)
	assert.ErrorIs(t, err, errBoom)

	repo.errs["CountTestRuns"] = errBoom
	_, err = svc.ListRuns(ctx, nil, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListTestRuns"] = errBoom
	_, err = svc.ListRuns(ctx, nil, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestListRunResultsAndDiagnostics(t *testing.T) {
	svc, repo, ctx := setup()
	r, _, _ := svc.RecordRun(ctx, run(1), []int64{1}, []NewResult{
		valid(1, Passed, "chrome"), valid(1, Failed, "firefox"),
		{Correlation: CorrelationUnknown, RequestedTestCaseID: ptr("TC-9"), TestName: "ghost", Status: Passed},
	}, nil)
	res, err := svc.ListRunResults(ctx, r.ID, ResultFilter{Status: ptr(Failed)}, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
	assert.Equal(t, "firefox", res.Items[0].TestName)

	res, err = svc.ListRunResults(ctx, r.ID, ResultFilter{Correlation: ptr(CorrelationUnknown)}, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, "ghost", res.Items[0].TestName)

	diags, err := svc.Diagnostics(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, []Diagnostic{{TestName: "ghost", Correlation: CorrelationUnknown, RequestedTestCaseID: ptr("TC-9")}}, diags)

	_, err = svc.ListRunResults(ctx, 99, ResultFilter{}, pagination.Default())
	assert.Error(t, err)
	repo.errs["CountRunResults"] = errBoom
	_, err = svc.ListRunResults(ctx, r.ID, ResultFilter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListRunResults"] = errBoom
	_, err = svc.ListRunResults(ctx, r.ID, ResultFilter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestSummaryUsesSnapshot(t *testing.T) {
	svc, repo, ctx := setup()
	r, _, _ := svc.RecordRun(ctx, run(1), []int64{1, 2}, []NewResult{valid(1, Passed, "chrome"), valid(1, Failed, "firefox")}, nil)
	s, err := svc.Summary(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCounts{Untested: 1, Failed: 1}, s.Counts)
	assert.Equal(t, 50.0, s.ExecutionPercent)

	_, err = svc.Summary(ctx, 99)
	assert.Error(t, err)
	for _, m := range []string{"ListDiagnostics", "ListSummaryInputs"} {
		repo.errs[m] = errBoom
		_, err = svc.Summary(ctx, r.ID)
		assert.ErrorIs(t, err, errBoom, m)
	}
}

func TestHistory(t *testing.T) {
	svc, repo, ctx := setup()
	_, _, _ = svc.RecordRun(ctx, run(1), []int64{1}, []NewResult{valid(1, Passed, "a")}, nil)
	_, _, _ = svc.RecordRun(ctx, run(2), []int64{1}, []NewResult{valid(1, Failed, "a")}, nil)
	h, err := svc.History(ctx, 1, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(2), h.Total)
	assert.Equal(t, Failed, h.Items[0].Result.Status)
	assert.Equal(t, "github:42:2", h.Items[0].Run.ExternalRunID)

	// A run repeated on a history page (one row per result) is read and summarized once.
	_, _, _ = svc.RecordRun(ctx, run(3), []int64{1}, []NewResult{valid(1, Passed, "chrome"), valid(1, Failed, "firefox"), valid(1, Passed, "safari")}, nil)
	repo.summaryReads = nil
	h, err = svc.History(ctx, 1, pagination.Page{Number: 1, Size: 3})
	require.NoError(t, err)
	require.Len(t, h.Items, 3)
	assert.Equal(t, [][]int64{{h.Items[0].Run.ID}}, repo.summaryReads)
	for _, it := range h.Items {
		assert.Equal(t, RunOutcome{Verdict: VerdictFailed, Executed: 1, Failed: 1}, it.Run.Outcome)
	}

	repo.errs["CountResultsForTestCase"] = errBoom
	_, err = svc.History(ctx, 1, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListResultsForTestCase"] = errBoom
	_, err = svc.History(ctx, 1, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestParseErrors(t *testing.T) {
	svc, repo, ctx := setup()
	pe := []ParseError{{Index: 0, TestName: "", Message: "no name"}, {Index: 3, TestName: "t", Message: "bad time", Persisted: true}}
	r, _, err := svc.RecordRun(ctx, run(1), nil, nil, pe)
	require.NoError(t, err)

	all, err := svc.ParseErrors(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, pe, all)

	page, err := svc.ListParseErrors(ctx, r.ID, pagination.Page{Number: 2, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, pe[1:], page.Items)

	_, err = svc.ListParseErrors(ctx, 99, pagination.Default())
	assert.Error(t, err)
	repo.errs["CountParseErrors"] = errBoom
	_, err = svc.ListParseErrors(ctx, r.ID, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListParseErrors"] = errBoom
	_, err = svc.ListParseErrors(ctx, r.ID, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestRunsCarryTheirOutcome(t *testing.T) {
	svc, repo, ctx := setup()
	r, _, err := svc.RecordRun(ctx, run(1), []int64{1, 2}, []NewResult{valid(1, Passed, "a"), valid(2, Failed, "b")}, nil)
	require.NoError(t, err)
	assert.Equal(t, RunOutcome{Verdict: VerdictFailed, Executed: 2, Passed: 1, Failed: 1, PassRate: 50}, r.Outcome)
	got, err := svc.GetRun(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.Outcome, got.Outcome)
	list, err := svc.ListRuns(ctx, nil, pagination.Page{Number: 1, Size: 10})
	require.NoError(t, err)
	assert.Equal(t, r.Outcome, list.Items[0].Outcome)
	hist, err := svc.History(ctx, 1, pagination.Page{Number: 1, Size: 10})
	require.NoError(t, err)
	assert.Equal(t, r.Outcome, hist.Items[0].Run.Outcome)

	repo.errs["ListSummaryInputs"] = errBoom
	_, _, err = svc.RecordRun(ctx, run(2), nil, nil, nil)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.GetRun(ctx, r.ID)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.ListRuns(ctx, nil, pagination.Page{Number: 1, Size: 10})
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.History(ctx, 1, pagination.Page{Number: 1, Size: 10})
	assert.ErrorIs(t, err, errBoom)
}

// The same externalRunId in two projects is two runs; the run list can be narrowed to one project.
func TestRunsArePerProject(t *testing.T) {
	svc, _, ctx := setup()
	a, b := run(1), run(1)
	a.ProjectID, b.ProjectID = 1, 2
	first, created, err := svc.RecordRun(ctx, a, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, created)
	second, created, err := svc.RecordRun(ctx, b, nil, nil, nil)
	require.NoError(t, err)
	assert.True(t, created, "same externalRunId, other project: a new run")
	assert.NotEqual(t, first.ID, second.ID)
	assert.Equal(t, int64(2), second.ProjectID)
	replay, created, err := svc.RecordRun(ctx, b, nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, second.ID, replay.ID)

	two := int64(2)
	res, err := svc.ListRuns(ctx, []int64{two}, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
	assert.Equal(t, second.ID, res.Items[0].ID)
}

// DEC-42: a TC-ID with valid results outside the snapshot can be amended into the run's universe; the snapshot is
// kept, the summary and outcome count it, the run is marked as edited and the amendment records who and why.
func TestAmend(t *testing.T) {
	svc, repo, ctx := setup()
	r, _, err := svc.RecordRun(ctx, run(1), []int64{1}, []NewResult{valid(1, Passed, "a"), valid(5, Failed, "b"), valid(5, Passed, "c")}, nil)
	require.NoError(t, err)
	admin := authz.Actor{ID: 9, Username: "ana"}

	before, _ := svc.Summary(ctx, r.ID)
	assert.Equal(t, []int64{5}, before.OutsideUniverseIDs)

	a, err := svc.Amend(ctx, r.ID, 5, "  it was marked manual by mistake ", admin)
	require.NoError(t, err)
	assert.Equal(t, Amendment{ID: 1, TestRunID: r.ID, TestCaseID: 5, AmendedBy: 9, AmendedByUsername: "ana",
		Reason: "it was marked manual by mistake", CreatedAt: a.CreatedAt}, a)

	s, err := svc.Summary(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(2), s.ExpectedTotal)
	assert.Equal(t, int32(1), s.SnapshotTotal, "the snapshot is unchanged")
	assert.Equal(t, []int64{5}, s.AmendedIDs)
	assert.Empty(t, s.OutsideUniverseIDs)
	assert.Equal(t, int32(1), s.Counts.Failed)
	got, _ := svc.GetRun(ctx, r.ID)
	assert.Equal(t, int32(1), got.AmendmentCount)
	assert.Equal(t, int32(2), got.ExpectedCount)
	assert.Equal(t, VerdictFailed, got.Outcome.Verdict, "the outcome follows the amended universe")

	page, err := svc.ListAmendments(ctx, r.ID, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)

	for _, c := range []struct {
		tc     int64
		reason string
		kind   apperr.Kind
	}{
		{5, "again", apperr.KindConflict}, {1, "in the snapshot", apperr.KindConflict}, {7, "no results", apperr.KindValidation},
		{5, " ", apperr.KindValidation}, {5, string(make([]rune, 501)), apperr.KindValidation}, {5, "a\x00", apperr.KindValidation},
		{0, "x", apperr.KindValidation},
	} {
		_, err := svc.Amend(ctx, r.ID, c.tc, c.reason, admin)
		assert.Equal(t, c.kind, kindOf(t, err), "%d %q", c.tc, c.reason)
	}
	_, err = svc.Amend(ctx, 99, 5, "x", admin)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.ListAmendments(ctx, 99, pagination.Default())
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	// A concurrent amendment of the same TC-ID (the unique row already exists) is a conflict, not a duplicate.
	r2, _, _ := svc.RecordRun(ctx, run(2), nil, []NewResult{valid(5, Passed, "b")}, nil)
	repo.errs["InsertAmendment"] = ErrConflict
	_, err = svc.Amend(ctx, r2.ID, 5, "x", admin)
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	delete(repo.errs, "InsertAmendment")

	for method, call := range map[string]func() error{
		"ListSummaryInputs": func() error { _, err := svc.Amend(ctx, r.ID, 7, "x", admin); return err },
		"InsertAmendment": func() error {
			r3, _, _ := svc.RecordRun(ctx, run(3), nil, []NewResult{valid(8, Passed, "z")}, nil)
			_, err := svc.Amend(ctx, r3.ID, 8, "x", admin)
			return err
		},
		"ListAmendments":  func() error { _, err := svc.ListAmendments(ctx, r.ID, pagination.Default()); return err },
		"CountAmendments": func() error { _, err := svc.ListAmendments(ctx, r.ID, pagination.Default()); return err },
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
}

func kindOf(t *testing.T, err error) apperr.Kind {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	return e.Kind
}
