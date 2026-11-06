//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/integrations"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Runs that would otherwise stay running end (CHG-003, Ed 2026-11-06): CI finalizes live runs too, live and sharded
// runs without activity time out, and archiving a project closes its running runs.
func TestRunLifecycle(t *testing.T) {
	t.Run("BE-INT-080_finalize_ends_a_running_live_run_and_refuses_a_manual_run", func(t *testing.T) {
		s, ctx := fresh(t)
		m := ingestion.RunMeta{Provider: "github", ProviderRunID: "900", RunAttempt: 1, Pipeline: "ci"}
		run, err := s.Live.Start(ctx, m)
		require.NoError(t, err)

		out, err := s.Ingestion.FinalizeRun(ctx, m)
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, run.ID, out.Run.ID)
		assert.Equal(t, execution.RunInterrupted, out.Run.Status)
		assert.Equal(t, execution.ClosedFinalized, out.Run.ClosedReason)
		got, err := s.Execution.GetRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.ClosedFinalized, got.ClosedReason)
		assert.NotNil(t, got.CompletedAt)

		// Its final report arriving later is a replay of the ended run: it stays interrupted.
		late, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.False(t, late.Created)
		assert.Equal(t, execution.RunInterrupted, late.Run.Status)

		manual, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "Sign-off"})
		require.NoError(t, err)
		_, err = s.Ingestion.FinalizeRun(ctx, ingestion.RunMeta{Provider: manual.Provider, ProviderRunID: manual.ProviderRunID, RunAttempt: manual.RunAttempt})
		isConflict(t, err)
	})

	t.Run("BE-INT-081_live_and_sharded_runs_without_activity_time_out_and_manual_runs_never_do", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "login", Automated: true})
		require.NoError(t, err)
		stale, err := s.Live.Start(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "901", RunAttempt: 1})
		require.NoError(t, err)
		busy, err := s.Live.Start(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "902", RunAttempt: 1})
		require.NoError(t, err)
		sharded, err := s.Ingestion.IngestJUnit(ctx, shardMeta("903", 1, 2), strings.NewReader(junitFor(tcProp("login", tc.Key(), ""))))
		require.NoError(t, err)
		manual, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "Sign-off"})
		require.NoError(t, err)

		// An event after the cutoff keeps a run open.
		time.Sleep(1200 * time.Millisecond)
		_, err = s.Live.RecordEvents(ctx, busy.ID, []ingestion.EventInput{{EventID: "1", Sequence: 1, Type: execution.EventTestStarted, TestName: "login"}})
		require.NoError(t, err)
		s.Runs.CloseAbandoned(ctx, 600*time.Millisecond)

		for id, want := range map[int64]execution.RunStatus{stale.ID: execution.RunInterrupted, sharded.Run.ID: execution.RunInterrupted,
			busy.ID: execution.RunRunning, manual.ID: execution.RunRunning} {
			got, err := s.Execution.GetRun(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, want, got.Status, "run %d", id)
			if want == execution.RunInterrupted {
				assert.Equal(t, execution.ClosedTimeout, got.ClosedReason, "run %d", id)
			} else {
				assert.Empty(t, got.ClosedReason, "run %d", id)
			}
		}
		// A shard arriving after the timeout finds the run ended.
		_, err = s.Ingestion.IngestJUnit(ctx, shardMeta("903", 2, 2), strings.NewReader(junitFor()))
		isConflict(t, err)

		// The database keeps the reason with the run: set once, only on an ended run.
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET closed_reason = 'archived' WHERE id = $1`, stale.ID)
		assertPgError(t, err, "a finished test run keeps its status")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET closed_reason = 'timeout' WHERE id = $1`, manual.ID)
		assertPgError(t, err, "test_runs_closed_reason_ended")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET status = 'cancelled', closed_reason = 'later' WHERE id = $1`, manual.ID)
		assertPgError(t, err, "test_runs_closed_reason_check")
	})

	t.Run("BE-INT-082_archiving_a_project_closes_its_running_runs_and_their_webhooks_fire", func(t *testing.T) {
		s, ctx := freshIntegrations(t, "http://127.0.0.1:1")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		defer srv.Close()
		_, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		hook, _, err := s.Integrations.CreateWebhook(ctx, "CHK", srv.URL+"/hook", []string{integrations.EventRunCompleted})
		require.NoError(t, err)
		manual, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "CHK", Name: "Sign-off"})
		require.NoError(t, err)
		live, err := s.Live.Start(ctx, ingestion.RunMeta{ProjectKey: "CHK", Provider: "github", ProviderRunID: "904", RunAttempt: 1})
		require.NoError(t, err)
		other, err := s.Live.Start(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "905", RunAttempt: 1})
		require.NoError(t, err)

		_, err = s.Catalog.SetProjectArchived(ctx, "CHK", true)
		require.NoError(t, err)
		for id, want := range map[int64]execution.RunStatus{manual.ID: execution.RunCancelled, live.ID: execution.RunInterrupted} {
			got, err := s.Execution.GetRun(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, want, got.Status, "run %d", id)
			assert.Equal(t, execution.ClosedArchived, got.ClosedReason, "run %d", id)
		}
		got, err := s.Execution.GetRun(ctx, other.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.RunRunning, got.Status, "the default project's run stays open")

		// Archiving again closes nothing more; unarchiving reopens nothing.
		_, err = s.Catalog.SetProjectArchived(ctx, "CHK", true)
		require.NoError(t, err)
		_, err = s.Catalog.SetProjectArchived(ctx, "CHK", false)
		require.NoError(t, err)
		got, err = s.Execution.GetRun(ctx, live.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.RunInterrupted, got.Status)
		page, err := s.Integrations.Deliveries(ctx, "CHK", hook.ID, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(2), page.Total, "one run.completed delivery per closed run")
	})
}

func assertPgError(t *testing.T, err error, contains string) {
	t.Helper()
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg)
	assert.Contains(t, pg.Message+" "+pg.ConstraintName, contains)
}
