//go:build integration

package integration

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func shardMeta(runID string, shard, total int32) ingestion.RunMeta {
	m := meta(runID, 1)
	m.Shard, m.ShardTotal = shard, total
	return m
}

func isConflict(t *testing.T, err error) {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, apperr.KindConflict, e.Kind, e.Message)
}

func TestShardedRuns(t *testing.T) {
	t.Run("BE-INT-062_a_sharded_run_takes_each_shard_once_and_completes_when_every_shard_arrived", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		login, pay, cart := create("login"), create("pay"), create("cart")
		ingest := func(m ingestion.RunMeta, cases ...string) (ingestion.Outcome, error) {
			return s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor(cases...)))
		}

		// The first shard creates the run with its snapshot; it waits for the others.
		out, err := ingest(shardMeta("s1", 1, 3), tcProp("login", login.Key(), ""), `<testcase name="no id"/>`)
		require.NoError(t, err)
		assert.True(t, out.Created)
		run := out.Run
		assert.Equal(t, execution.ModeSharded, run.Mode)
		assert.Equal(t, execution.RunRunning, run.Status)
		assert.Nil(t, run.CompletedAt)
		assert.Equal(t, int32(3), run.ExpectedCount)
		assert.Equal(t, int32(3), run.ShardTotal)
		assert.Equal(t, []int32{1}, run.ShardsReceived)
		assert.Equal(t, []int32{2, 3}, run.MissingShards())
		assert.Equal(t, execution.VerdictIncomplete, run.Outcome.Verdict, "untested shards' test cases keep it incomplete")
		assert.Equal(t, 2, out.Persisted)
		assert.Len(t, out.Diagnostics, 1)
		assert.Contains(t, out.Warnings, fmt.Sprintf(ingestion.ShardsPendingWarning, 1, 3, "2, 3"))

		// Re-sending a shard is a replay; with another report it says so.
		replay, err := ingest(shardMeta("s1", 1, 3), tcProp("login", login.Key(), ""), `<testcase name="no id"/>`)
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, 2, replay.Persisted)
		assert.NotContains(t, replay.Warnings, ingestion.ReportDiffersWarning)
		differs, err := ingest(shardMeta("s1", 1, 3), tcProp("login", login.Key(), `<failure/>`))
		require.NoError(t, err)
		assert.Contains(t, differs.Warnings, ingestion.ReportDiffersWarning)

		// A different shard count, or a report without shards, does not fit the run.
		_, err = ingest(shardMeta("s1", 2, 4), tcProp("pay", pay.Key(), ""))
		isConflict(t, err)
		_, err = ingest(meta("s1", 1), tcProp("pay", pay.Key(), ""))
		isConflict(t, err)

		// The third shard reports an interrupted job: the run keeps waiting for shard 2.
		m3 := shardMeta("s1", 3, 3)
		m3.Status = execution.RunInterrupted
		out, err = ingest(m3, tcProp("cart", cart.Key(), `<failure message="x"/>`))
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, execution.RunRunning, out.Run.Status)
		assert.Equal(t, []int32{2}, out.Run.MissingShards())

		// The last shard completes it, with the worst status of its shards, and the results of every shard.
		out, err = ingest(shardMeta("s1", 2, 3), tcProp("pay", pay.Key(), ""))
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, execution.RunInterrupted, out.Run.Status)
		assert.NotNil(t, out.Run.CompletedAt)
		assert.Empty(t, out.Run.MissingShards())
		assert.Equal(t, execution.RunOutcome{Verdict: execution.VerdictFailed, Executed: 3, Passed: 2, Failed: 1, PassRate: out.Run.Outcome.PassRate}, out.Run.Outcome)
		assert.Equal(t, 1, out.Persisted, "a shard answers for its own results")
		assert.Empty(t, out.Diagnostics, "the first shard's diagnostic is not this shard's")
		for _, w := range out.Warnings {
			assert.NotContains(t, w, "recorded; the run completes")
		}

		// Results tell their shard and filter by it.
		two := int32(2)
		results, err := s.Execution.ListRunResults(ctx, run.ID, execution.ResultFilter{Shard: &two}, pagination.Default())
		require.NoError(t, err)
		require.Len(t, results.Items, 1)
		assert.Equal(t, &two, results.Items[0].Shard)
		assert.Equal(t, "pay", results.Items[0].TestName)

		// After the run ended, a known shard is a replay and nothing else gets in.
		again, err := ingest(shardMeta("s1", 2, 3), tcProp("pay", pay.Key(), ""))
		require.NoError(t, err)
		assert.False(t, again.Created)
		got, err := s.Execution.GetRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(4), got.ResultCount)
	})

	t.Run("BE-INT-063_finalizing_a_sharded_run_ends_it_interrupted_with_its_missing_shards", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "login", Automated: true})
		require.NoError(t, err)
		_, err = s.Ingestion.IngestJUnit(ctx, shardMeta("f1", 1, 3), strings.NewReader(junitFor(tcProp("login", tc.Key(), ""))))
		require.NoError(t, err)

		out, err := s.Ingestion.FinalizeShards(ctx, meta("f1", 1))
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, execution.RunInterrupted, out.Run.Status)
		assert.Equal(t, []int32{2, 3}, out.Run.MissingShards())
		assert.Equal(t, []string{fmt.Sprintf(ingestion.ShardsMissingWarning, "2, 3", 3)}, out.Warnings)

		again, err := s.Ingestion.FinalizeShards(ctx, meta("f1", 1))
		require.NoError(t, err)
		assert.False(t, again.Created, "finalizing an ended run changes nothing")
		assert.Equal(t, execution.RunInterrupted, again.Run.Status)

		_, err = s.Ingestion.IngestJUnit(ctx, shardMeta("f1", 2, 3), strings.NewReader(junitFor()))
		isConflict(t, err)

		_, err = s.Ingestion.IngestJUnit(ctx, meta("plain", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		_, err = s.Ingestion.FinalizeShards(ctx, meta("plain", 1))
		isConflict(t, err)
		_, err = s.Ingestion.FinalizeShards(ctx, meta("unknown", 1))
		e, ok := apperr.As(err)
		require.True(t, ok)
		assert.Equal(t, apperr.KindNotFound, e.Kind)
	})

	t.Run("BE-INT-064_concurrent_shards_build_one_run_completed_once_and_the_database_guards_shards", func(t *testing.T) {
		s, ctx := fresh(t)
		const n = 8
		var wg sync.WaitGroup
		outs := make([]ingestion.Outcome, n)
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				outs[i], errs[i] = s.Ingestion.IngestJUnit(ctx, shardMeta("c1", int32(i+1), n),
					strings.NewReader(junitFor(fmt.Sprintf(`<testcase name="t%d"/>`, i))))
			}()
		}
		wg.Wait()
		completed := 0
		for i := range n {
			require.NoError(t, errs[i])
			assert.True(t, outs[i].Created)
			assert.Equal(t, outs[0].Run.ID, outs[i].Run.ID, "one run")
			if outs[i].Run.Status == execution.RunCompleted {
				completed++
			}
		}
		assert.Equal(t, 1, completed, "exactly one shard completes the run")
		var runs, shards, results int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM test_runs), (SELECT count(*) FROM test_run_shards), (SELECT count(*) FROM test_results)`).
			Scan(&runs, &shards, &results))
		assert.Equal(t, [3]int{1, n, n}, [3]int{runs, shards, results})

		// Shards are append-only, only for running sharded runs and within their count.
		id := outs[0].Run.ID
		_, err := db.Pool.Exec(ctx, `UPDATE test_run_shards SET status = 'cancelled' WHERE test_run_id = $1`, id)
		assert.ErrorContains(t, err, "received shards are immutable")
		_, err = db.Pool.Exec(ctx, `DELETE FROM test_run_shards WHERE test_run_id = $1`, id)
		assert.ErrorContains(t, err, "received shards are immutable")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_run_shards (test_run_id, shard, report_sha256, status) VALUES ($1, 9, repeat('a', 64), 'completed')`, id)
		assert.ErrorContains(t, err, "only while a sharded run is running")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET shard_total = 9 WHERE id = $1`, id)
		assert.ErrorContains(t, err, "identity of a test run is immutable")

		running, err := s.Ingestion.IngestJUnit(ctx, shardMeta("c2", 1, 2), strings.NewReader(junitFor()))
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_run_shards (test_run_id, shard, report_sha256, status) VALUES ($1, 3, repeat('a', 64), 'completed')`, running.Run.ID)
		assert.ErrorContains(t, err, "outside the run's 2 shards")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET mode = 'batch', shard_total = NULL WHERE id = $1`, running.Run.ID)
		assert.Error(t, err)
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status, mode)
			VALUES (1, 'x:1:1', 'x', '1', 1, 'running', 'sharded')`)
		assert.ErrorContains(t, err, "test_runs_sharded_total", "a sharded run declares its shard count")
	})
}
