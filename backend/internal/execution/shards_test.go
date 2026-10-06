package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func shard(i, n int32, sha string) NewRun {
	r := run(1)
	r.Shard, r.ShardTotal, r.ReportSHA256 = i, n, sha
	return r
}

func conflict(t *testing.T, err error, contains string) {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, apperr.KindConflict, e.Kind)
	assert.Contains(t, e.Message, contains)
}

func TestShardedRunLifecycle(t *testing.T) {
	svc, repo, ctx := setup()
	first, created, err := svc.RecordRun(ctx, shard(1, 2, "a"), []int64{1, 2}, []NewResult{valid(1, Passed, "a")},
		[]ParseError{{Index: 0, Message: "m", Persisted: true, Severity: "warning"}})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, ModeSharded, first.Mode)
	assert.Equal(t, RunRunning, first.Status)
	assert.Nil(t, first.CompletedAt)
	assert.Empty(t, repo.runs[first.ID].ReportSHA256, "each shard keeps its own digest")
	assert.Equal(t, []RunShard{{Shard: 1, ReportSHA256: "a", Status: RunCompleted, ResultCount: 1}}, first.Shards)
	assert.Equal(t, []int32{2}, first.MissingShards())
	assert.Equal(t, int32(1), repo.parseErr[first.ID][0].Shard)

	replay, created, err := svc.RecordRun(ctx, shard(1, 2, "b"), nil, []NewResult{valid(2, Failed, "b")}, nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, int32(1), replay.ResultCount, "a shard is taken once")
	assert.Equal(t, "a", replay.Shards[0].ReportSHA256)

	_, _, err = svc.RecordRun(ctx, shard(1, 3, "a"), nil, nil, nil)
	conflict(t, err, "is split into 2 shards, not 3")
	_, _, err = svc.RecordRun(ctx, run(1), nil, nil, nil)
	conflict(t, err, "is sharded")

	last := shard(2, 2, "c")
	last.Status = RunCancelled
	done, created, err := svc.RecordRun(ctx, last, nil, []NewResult{valid(2, Failed, "b")}, nil)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, RunCancelled, done.Status)
	assert.Equal(t, []int32{1, 2}, done.ShardsReceived)
	assert.Equal(t, VerdictFailed, done.Outcome.Verdict)

	again, created, err := svc.RecordRun(ctx, shard(2, 2, "c"), nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, created, "a known shard after the end is a replay")
	assert.Equal(t, RunCancelled, again.Status)

	plain, _, err := svc.RecordRun(ctx, run(2), nil, nil, nil)
	require.NoError(t, err)
	r := shard(1, 2, "a")
	r.RunAttempt = plain.RunAttempt
	_, _, err = svc.RecordRun(ctx, r, nil, nil, nil)
	conflict(t, err, "it takes no shards")
}

func TestWorstStatus(t *testing.T) {
	sh := func(st ...RunStatus) []RunShard {
		out := make([]RunShard, len(st))
		for i, s := range st {
			out[i] = RunShard{Status: s}
		}
		return out
	}
	assert.Equal(t, RunCompleted, worstStatus(sh(RunCompleted, RunCompleted)))
	assert.Equal(t, RunInterrupted, worstStatus(sh(RunCompleted, RunInterrupted)))
	assert.Equal(t, RunCancelled, worstStatus(sh(RunInterrupted, RunCancelled, RunCompleted)))
}

func TestFinalizeShardedRun(t *testing.T) {
	svc, repo, ctx := setup()
	created, _, err := svc.RecordRun(ctx, shard(2, 3, "a"), nil, nil, nil)
	require.NoError(t, err)

	got, finished, err := svc.FinalizeShardedRun(ctx, 0, "github", "42", 1)
	require.NoError(t, err)
	assert.True(t, finished)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, RunInterrupted, got.Status)
	assert.Equal(t, []int32{1, 3}, got.MissingShards())

	got, finished, err = svc.FinalizeShardedRun(ctx, 0, "github", "42", 1)
	require.NoError(t, err)
	assert.False(t, finished)
	assert.Equal(t, RunInterrupted, got.Status)

	_, _, err = svc.RecordRun(ctx, shard(1, 3, "b"), nil, nil, nil)
	conflict(t, err, "arrived after it ended")

	_, _, err = svc.RecordRun(ctx, run(2), nil, nil, nil)
	require.NoError(t, err)
	_, _, err = svc.FinalizeShardedRun(ctx, 0, "github", "42", 2)
	conflict(t, err, "is not sharded")

	repo.errs["GetTestRunIDByExternalID"] = ErrNotFound
	_, _, err = svc.FinalizeShardedRun(ctx, 0, "github", "42", 9)
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindNotFound, e.Kind)
}

func TestShardedRunErrors(t *testing.T) {
	for _, m := range []string{"ListRunShards", "InsertRunShard", "InsertTestResults", "FinishShardedRun"} {
		svc, repo, ctx := setup()
		_, _, err := svc.RecordRun(ctx, shard(1, 2, "a"), nil, nil, nil)
		require.NoError(t, err)
		repo.errs[m] = errBoom
		_, _, err = svc.RecordRun(ctx, shard(2, 2, "b"), nil, nil, nil)
		assert.ErrorIs(t, err, errBoom, m)
	}
	// The second read of the shards, and of the run, after recording the new shard.
	for _, m := range []string{"ListRunShards", "GetTestRun"} {
		svc, repo, ctx := setup()
		_, _, err := svc.RecordRun(ctx, shard(1, 2, "a"), nil, nil, nil)
		require.NoError(t, err)
		repo.failOn, repo.calls = map[string]int{m: 2}, nil
		_, _, err = svc.RecordRun(ctx, shard(2, 2, "b"), nil, nil, nil)
		assert.ErrorIs(t, err, errBoom, m)
	}

	for _, m := range []string{"InTx", "GetTestRunIDByExternalID", "LockTestRun", "FinishShardedRun", "GetTestRun"} {
		svc, repo, ctx := setup()
		_, _, err := svc.RecordRun(ctx, shard(1, 2, "a"), nil, nil, nil)
		require.NoError(t, err)
		repo.errs[m] = errBoom
		_, _, err = svc.FinalizeShardedRun(ctx, 0, "github", "42", 1)
		assert.ErrorIs(t, err, errBoom, m)
	}
}
