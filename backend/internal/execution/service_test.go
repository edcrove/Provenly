package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
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
	r.Status = RunFailed
	got, _, err := svc.RecordRun(ctx, r, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, RunFailed, got.Status)
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
	res, err := svc.ListRuns(ctx, pagination.Page{Number: 1, Size: 1})
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
	_, err = svc.ListRuns(ctx, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListTestRuns"] = errBoom
	_, err = svc.ListRuns(ctx, pagination.Default())
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
	for _, m := range []string{"ListDiagnostics", "ListValidResults", "ListExpectedCaseIDs"} {
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
