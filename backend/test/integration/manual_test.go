//go:build integration

package integration

import (
	"strconv"
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

func TestManualRuns(t *testing.T) {
	t.Run("BE-INT-048_manual_runs_take_results_while_running_and_keep_them", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string, automated bool) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: automated})
			require.NoError(t, err)
			return tc
		}
		checkout, refund, ci := create("checkout", false), create("refund", false), create("ci", true)

		run, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "Release sign-off", Branch: "release/2.4"})
		require.NoError(t, err)
		assert.Equal(t, execution.RunRunning, run.Status)
		assert.Equal(t, execution.ModeManual, run.Mode)
		assert.Equal(t, "integration", run.StartedBy)
		assert.Equal(t, int32(2), run.ExpectedCount, "the manual test cases; CI runs the automated one")
		assert.Nil(t, run.CompletedAt)
		all, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "Everything", Scope: ingestion.ScopeAll})
		require.NoError(t, err)
		assert.Equal(t, int32(3), all.ExpectedCount)

		// Results arrive one by one; a re-test is the next attempt and the last one counts (not flaky).
		step := int32(2)
		res, err := s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: checkout.ID, Status: execution.Failed, Note: "Pay button missing", FailedStep: &step})
		require.NoError(t, err)
		assert.Equal(t, int32(1), res.Attempt)
		assert.Equal(t, checkout.Key(), res.TestName)
		assert.Equal(t, &step, res.FailedStep)
		assert.Equal(t, "integration", res.RecordedBy)
		took := int64(90000)
		res, err = s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: checkout.ID, Status: execution.Passed, DurationMs: &took})
		require.NoError(t, err)
		assert.Equal(t, int32(2), res.Attempt)
		assert.Equal(t, &took, res.DurationMs)
		_, err = s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: ci.ID, Status: execution.Passed})
		assert.Equal(t, apperr.KindConflict, kind(t, err), "not expected in this run")
		sum, err := s.Execution.Summary(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.StatusCounts{Untested: 1, Passed: 1}, sum.Counts)
		assert.Equal(t, int32(0), sum.Flaky, "a manual re-test is a fix, not flakiness")
		// The test case history says the results came from a manual run and who started it.
		h, err := s.Execution.History(ctx, checkout.ID, pagination.Default())
		require.NoError(t, err)
		require.Len(t, h.Items, 2)
		assert.Equal(t, execution.ModeManual, h.Items[0].Run.Mode)
		assert.Equal(t, "integration", h.Items[0].Run.StartedBy)

		// Twenty people record the same test case at once: twenty attempts, numbered without gaps.
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: refund.ID, Status: execution.Passed})
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
		var attempts []int32
		rows, err := db.Pool.Query(ctx, `SELECT attempt FROM test_results WHERE test_run_id = $1 AND test_name = $2 ORDER BY attempt`, run.ID, refund.Key())
		require.NoError(t, err)
		for rows.Next() {
			var a int32
			require.NoError(t, rows.Scan(&a))
			attempts = append(attempts, a)
		}
		require.Len(t, attempts, 20)
		assert.Equal(t, int32(1), attempts[0])
		assert.Equal(t, int32(20), attempts[19])

		// Finishing closes the run: no more results, the status stays.
		done, err := s.Manual.Finish(ctx, run.ID, execution.RunCompleted)
		require.NoError(t, err)
		assert.Equal(t, execution.RunCompleted, done.Status)
		assert.NotNil(t, done.CompletedAt)
		assert.Equal(t, execution.VerdictPassed, done.Outcome.Verdict)
		_, err = s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: refund.ID, Status: execution.Failed})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		_, err = s.Manual.Finish(ctx, run.ID, execution.RunCancelled)
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		cancelled, err := s.Manual.Finish(ctx, all.ID, execution.RunCancelled)
		require.NoError(t, err)
		assert.Equal(t, execution.RunCancelled, cancelled.Status)

		// CI runs never take manual results.
		out, err := s.Ingestion.IngestJUnit(ctx, meta("1", 1), strings.NewReader(junitFor(tcProp("ci", ci.Key(), ""))))
		require.NoError(t, err)
		_, err = s.Manual.Record(ctx, out.Run.ID, ingestion.ManualResultInput{TestCaseID: ci.ID, Status: execution.Failed})
		assert.Equal(t, apperr.KindConflict, kind(t, err))

		// The database refuses what the service never does.
		running, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "open"})
		require.NoError(t, err)
		insert := func(runID int64) string {
			return `INSERT INTO test_results (test_run_id, test_case_id, correlation, test_name, status) VALUES (` +
				strconv.FormatInt(runID, 10) + `, $1, 'valid', 'x', 'passed')`
		}
		_, err = db.Pool.Exec(ctx, insert(running.ID), checkout.ID)
		require.NoError(t, err, "a running manual run takes results")
		for name, sql := range map[string]string{
			"a result in a finished manual run":       insert(run.ID),
			"a result in a CI run after its creation": insert(out.Run.ID),
			"reopening a finished run":                `UPDATE test_runs SET status = 'running' WHERE id = ` + strconv.FormatInt(run.ID, 10) + ` AND $1 > 0`,
			"changing a finished run's status":        `UPDATE test_runs SET status = 'cancelled' WHERE id = ` + strconv.FormatInt(run.ID, 10) + ` AND $1 > 0`,
			"a running batch run":                     `UPDATE test_runs SET status = 'running' WHERE id = ` + strconv.FormatInt(out.Run.ID, 10) + ` AND $1 > 0`,
			"changing a run's mode":                   `UPDATE test_runs SET mode = 'batch' WHERE id = ` + strconv.FormatInt(running.ID, 10) + ` AND $1 > 0`,
			"changing who started it":                 `UPDATE test_runs SET started_by = 'eve' WHERE id = ` + strconv.FormatInt(running.ID, 10) + ` AND $1 > 0`,
			"editing a manual result":                 `UPDATE test_results SET status = 'failed' WHERE test_run_id = ` + strconv.FormatInt(running.ID, 10) + ` AND $1 > 0`,
			"a failed step below 1":                   `INSERT INTO test_results (test_run_id, test_case_id, correlation, test_name, status, failed_step) VALUES (` + strconv.FormatInt(running.ID, 10) + `, $1, 'valid', 'y', 'failed', 0)`,
		} {
			_, err := db.Pool.Exec(ctx, sql, checkout.ID)
			assert.Error(t, err, name)
		}
	})
}
