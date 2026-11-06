package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startAt starts a live run and backdates its creation (the fake stamps the wall clock).
func startAt(t *testing.T, svc *Service, repo *fakeRepo, project int64, id string, created time.Time) TestRun {
	t.Helper()
	r, err := svc.StartRun(context.Background(), NewRun{ProjectID: project, Provider: "github", ProviderRunID: id, RunAttempt: 1, Mode: ModeLive}, nil)
	require.NoError(t, err)
	stored := repo.runs[r.ID]
	stored.CreatedAt = created
	repo.runs[r.ID] = stored
	return stored
}

func TestFinalizeLiveAndManualRuns(t *testing.T) {
	svc, repo, ctx := setup()
	live := startAt(t, svc, repo, 0, "77", fixedNow)

	got, finished, err := svc.FinalizeRun(ctx, 0, "github", "77", 1)
	require.NoError(t, err)
	assert.True(t, finished)
	assert.Equal(t, live.ID, got.ID)
	assert.Equal(t, RunInterrupted, got.Status)
	assert.Equal(t, ClosedFinalized, got.ClosedReason)

	_, err = svc.StartRun(ctx, manualRun(), nil)
	require.NoError(t, err)
	_, _, err = svc.FinalizeRun(ctx, 1, "manual", "abc", 1)
	conflict(t, err, "is a manual run")
	assert.Equal(t, RunRunning, repo.runs[2].Status)
}

func TestCloseAbandonedRuns(t *testing.T) {
	svc, repo, ctx := setup()
	idle := 6 * time.Hour
	old := fixedNow.Add(-7 * time.Hour)
	staleLive := startAt(t, svc, repo, 1, "1", old)
	busyLive := startAt(t, svc, repo, 1, "2", old)
	repo.events = map[int64][]Event{busyLive.ID: {{ReceivedAt: fixedNow.Add(-time.Hour)}}}
	freshLive := startAt(t, svc, repo, 1, "3", fixedNow.Add(-time.Hour))
	staleShards, _, err := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "4", RunAttempt: 1, Shard: 1, ShardTotal: 2}, nil, nil, nil)
	require.NoError(t, err)
	s := repo.runs[staleShards.ID]
	s.CreatedAt = old
	repo.runs[staleShards.ID] = s
	manual, err := svc.StartRun(ctx, manualRun(), nil)
	require.NoError(t, err)
	m := repo.runs[manual.ID]
	m.CreatedAt = old.Add(-30 * 24 * time.Hour)
	repo.runs[manual.ID] = m

	closed, err := svc.CloseAbandonedRuns(ctx, idle)
	require.NoError(t, err)
	require.Len(t, closed, 2)
	assert.Equal(t, []int64{staleLive.ID, staleShards.ID}, []int64{closed[0].ID, closed[1].ID})
	for _, r := range closed {
		assert.Equal(t, RunInterrupted, r.Status)
		assert.Equal(t, ClosedTimeout, r.ClosedReason)
		assert.NotNil(t, r.CompletedAt)
	}
	assert.Equal(t, VerdictNoTests, closed[0].Outcome.Verdict, "the closed runs come back with their outcome")
	require.NotNil(t, RunDTO(closed[0]).ClosedReason)
	assert.Equal(t, ClosedTimeout, *RunDTO(closed[0]).ClosedReason, "the wire form says why")
	assert.Nil(t, RunDTO(repo.runs[busyLive.ID]).ClosedReason, "null while the run is open")
	for _, id := range []int64{busyLive.ID, freshLive.ID, manual.ID} {
		assert.Equal(t, RunRunning, repo.runs[id].Status, "run %d", id)
	}

	// A second sweep finds nothing left to close.
	closed, err = svc.CloseAbandonedRuns(ctx, idle)
	require.NoError(t, err)
	assert.Empty(t, closed)
}

func TestCloseAbandonedRunsIsBounded(t *testing.T) {
	svc, repo, ctx := setup()
	for i := range MaxAbandonedPerSweep + 1 {
		startAt(t, svc, repo, 1, time.Duration(i).String(), fixedNow.Add(-7*time.Hour))
	}
	closed, err := svc.CloseAbandonedRuns(ctx, 6*time.Hour)
	require.NoError(t, err)
	assert.Len(t, closed, MaxAbandonedPerSweep)
	closed, err = svc.CloseAbandonedRuns(ctx, 6*time.Hour)
	require.NoError(t, err)
	assert.Len(t, closed, 1)
}

func TestCloseProjectRuns(t *testing.T) {
	svc, repo, ctx := setup()
	manual, err := svc.StartRun(ctx, manualRun(), nil) // project 1
	require.NoError(t, err)
	live := startAt(t, svc, repo, 1, "5", fixedNow)
	other := startAt(t, svc, repo, 2, "6", fixedNow)
	batch, _, err := svc.RecordRun(ctx, NewRun{ProjectID: 1, Provider: "github", ProviderRunID: "7", RunAttempt: 1}, nil, nil, nil)
	require.NoError(t, err)

	closed, err := svc.CloseProjectRuns(ctx, 1)
	require.NoError(t, err)
	require.Len(t, closed, 2)
	assert.Equal(t, manual.ID, closed[0].ID)
	assert.Equal(t, RunCancelled, closed[0].Status)
	assert.Equal(t, live.ID, closed[1].ID)
	assert.Equal(t, RunInterrupted, closed[1].Status)
	for _, r := range closed {
		assert.Equal(t, ClosedArchived, r.ClosedReason)
	}
	assert.Equal(t, RunRunning, repo.runs[other.ID].Status, "another project's run stays open")
	assert.Equal(t, RunCompleted, repo.runs[batch.ID].Status)
	assert.Empty(t, repo.runs[batch.ID].ClosedReason)

	closed, err = svc.CloseProjectRuns(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, closed, "archiving again closes nothing more")
}

func TestCloseRunsErrors(t *testing.T) {
	for _, m := range []string{"InTx", "ListAbandonedRuns", "CloseRunningRun", "GetTestRun", "ListSummaryInputs"} {
		svc, repo, ctx := setup()
		startAt(t, svc, repo, 1, "1", fixedNow.Add(-7*time.Hour))
		repo.errs[m] = errBoom
		_, err := svc.CloseAbandonedRuns(ctx, 6*time.Hour)
		assert.ErrorIs(t, err, errBoom, m)
	}
	for _, m := range []string{"ListRunningRunsOfProject", "CloseRunningRun"} {
		svc, repo, ctx := setup()
		startAt(t, svc, repo, 1, "1", fixedNow)
		repo.errs[m] = errBoom
		_, err := svc.CloseProjectRuns(ctx, 1)
		assert.ErrorIs(t, err, errBoom, m)
	}
	// Nothing closed is not an error.
	svc, _, ctx := setup()
	closed, err := svc.CloseProjectRuns(ctx, 9)
	require.NoError(t, err)
	assert.Empty(t, closed)
}
