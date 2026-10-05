//go:build integration

package integration

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func TestLiveRuns(t *testing.T) {
	t.Run("BE-INT-053_live_runs_take_events_while_running_and_the_final_report_completes_them", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		login, pay := create("login"), create("pay")
		meta := ingestion.RunMeta{Provider: "github", ProviderRunID: "500", RunAttempt: 1, Pipeline: "ci"}
		run, err := s.Live.Start(ctx, meta)
		require.NoError(t, err)
		assert.Equal(t, execution.ModeLive, run.Mode)
		assert.Equal(t, int32(2), run.ExpectedCount)
		again, err := s.Live.Start(ctx, meta)
		require.NoError(t, err)
		assert.Equal(t, run.ID, again.ID)

		res, err := s.Live.RecordEvents(ctx, run.ID, []ingestion.EventInput{
			{EventID: "1", Sequence: 1, Type: execution.EventTestStarted, TestName: "login", TestCase: login.Key()},
			{EventID: "2", Sequence: 2, Type: execution.EventTestFinished, TestName: "login", TestCase: login.Key(), Status: execution.Passed},
			{EventID: "2", Sequence: 2, Type: execution.EventTestFinished, TestName: "login", TestCase: login.Key(), Status: execution.Passed},
		})
		require.NoError(t, err)
		assert.Equal(t, execution.EventsResult{Accepted: 2, Duplicates: 1}, res)
		live, err := s.Execution.Live(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.ReconciliationPending, live.Reconciliation)
		assert.Equal(t, []execution.LiveCase{{TestCaseID: login.ID, State: "passed"}, {TestCaseID: pay.ID, State: execution.LiveWaiting}}, live.Cases)

		// The final report completes the run once; replays keep it.
		report := junitFor(tcProp("login", login.Key(), ""), tcProp("pay", pay.Key(), `<failure message="x"/>`))
		out, err := s.Ingestion.IngestJUnit(ctx, meta, strings.NewReader(report))
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, execution.RunCompleted, out.Run.Status)
		assert.NotEmpty(t, out.Run.ReportSHA256)
		assert.Equal(t, execution.VerdictFailed, out.Run.Outcome.Verdict)
		replay, err := s.Ingestion.IngestJUnit(ctx, meta, strings.NewReader(report))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Empty(t, replay.Warnings)
		live, err = s.Execution.Live(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.ReconciliationMismatch, live.Reconciliation)
		require.Len(t, live.Mismatches, 1)
		assert.Equal(t, execution.MismatchFinalOnly, live.Mismatches[0].Kind)

		_, err = s.Live.RecordEvents(ctx, run.ID, []ingestion.EventInput{{EventID: "3", Sequence: 3, Type: execution.EventRunFinished}})
		assert.Equal(t, apperr.KindConflict, kind(t, err))

		// The database refuses what the service never does.
		id := strconv.FormatInt(run.ID, 10)
		batch, err := s.Ingestion.IngestJUnit(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "501", RunAttempt: 1}, strings.NewReader(junitFor()))
		require.NoError(t, err)
		bid := strconv.FormatInt(batch.Run.ID, 10)
		for name, sql := range map[string]string{
			"an event of a finished run": `INSERT INTO test_run_events (test_run_id, event_id, sequence, event_type, occurred_at) VALUES (` + id + `, 'x', 1, 'run.finished', now())`,
			"an event of a batch run":    `INSERT INTO test_run_events (test_run_id, event_id, sequence, event_type, occurred_at) VALUES (` + bid + `, 'x', 1, 'run.finished', now())`,
			"editing an event":           `UPDATE test_run_events SET sequence = 9 WHERE test_run_id = ` + id,
			"deleting an event":          `DELETE FROM test_run_events WHERE test_run_id = ` + id,
			"changing the digest again":  `UPDATE test_runs SET report_sha256 = 'x' WHERE id = ` + id,
			"a batch run's digest":       `UPDATE test_runs SET report_sha256 = 'x' WHERE id = ` + bid,
		} {
			_, err := db.Pool.Exec(ctx, sql)
			assert.Error(t, err, name)
		}
		var n int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM test_run_events WHERE test_run_id = `+id).Scan(&n))
		assert.Equal(t, 2, n)
	})
}
