//go:build integration

package integration

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func meta(runID string, attempt int32) ingestion.RunMeta {
	return ingestion.RunMeta{Provider: "github", ProviderRunID: runID, RunAttempt: attempt, Pipeline: "ci", Branch: "main", Commit: "abc123"}
}

func junitFor(cases ...string) string {
	return `<testsuites><testsuite name="suite" timestamp="2026-09-28T10:00:00">` + strings.Join(cases, "") + `</testsuite></testsuites>`
}

func tcProp(name string, id string, inner string) string {
	return `<testcase name="` + name + `" classname="c" time="0.25"><properties><property name="tc-id" value="` + id + `"/></properties>` + inner + `</testcase>`
}

func TestExecutionPersistence(t *testing.T) {
	t.Run("BE-INT-012_junit_ingestion_persists_run_results_and_diagnostics", func(t *testing.T) {
		s, ctx := fresh(t)
		login, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "login", Automated: true})
		logout, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "logout", Automated: true})
		old, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "old", Automated: true})
		_, _ = s.Catalog.Deprecate(ctx, old.ID, etag.Match{})

		doc := junitFor(
			tcProp("login chrome", itoa(login.ID), ""),
			tcProp("login firefox", itoa(login.ID), `<failure message="boom">trace</failure>`),
			`<testcase name="no id"/>`,
			`<testcase name="bad TC-x1"/>`,
			`<testcase name="ghost TC-987654"/>`,
			`<testcase name="old TC-`+itoa(old.ID)+`"><skipped/></testcase>`,
			`<testcase name="login slow TC-`+itoa(login.ID)+`" time="later"/>`,
			`<testcase name=""/>`,
		)
		out, err := s.Ingestion.IngestJUnit(ctx, meta("100", 1), strings.NewReader(doc))
		require.NoError(t, err)
		assert.True(t, out.Created)
		assert.Equal(t, 8, out.Received)
		assert.Equal(t, 7, out.Persisted)
		require.Len(t, out.ParseErrors, 2)
		assert.True(t, out.ParseErrors[0].Persisted, "an invalid time keeps the result")
		assert.False(t, out.ParseErrors[1].Persisted, "a nameless testcase is discarded")
		require.Len(t, out.Diagnostics, 4)
		assert.Equal(t, int32(2), out.Run.ExpectedCount)
		assert.Equal(t, "github:100:1", out.Run.ExternalRunID)
		assert.Equal(t, execution.RunCompleted, out.Run.Status)
		require.NotNil(t, out.Run.StartedAt)
		require.NotNil(t, out.Run.CompletedAt)

		replay, err := s.Ingestion.IngestJUnit(ctx, meta("100", 1), strings.NewReader(doc))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, out.Diagnostics, replay.Diagnostics, "a replay returns the diagnostics stored at creation")
		assert.Equal(t, out.ParseErrors, replay.ParseErrors)
		assert.Equal(t, out.Received, replay.Received)
		assert.Equal(t, out.Persisted, replay.Persisted)

		res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(7), res.Total)
		first := res.Items[0]
		assert.Equal(t, login.ID, *first.TestCaseID)
		assert.Equal(t, itoa(login.ID), *first.RequestedTestCaseID)
		assert.Equal(t, int64(250), *first.DurationMs)
		assert.Equal(t, "suite", first.SuiteName)
		assert.Equal(t, "c", first.ClassName)
		assert.Equal(t, "boom", res.Items[1].ErrorMessage)
		assert.Equal(t, "trace", res.Items[1].ErrorDetails)

		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.StatusCounts{Untested: 1, Failed: 1}, sum.Counts)
		assert.Equal(t, 50.0, sum.ExecutionPercent)
		assert.Equal(t, execution.DiagnosticCounts{Missing: 1, Malformed: 1, Unknown: 1, Deprecated: 1, Total: 4}, sum.Diagnostics)
		assert.Equal(t, []execution.TestCaseOutcome{{TestCaseID: login.ID, Status: "failed", ResultCount: 3}, {TestCaseID: logout.ID, Status: execution.Untested}}, sum.TestCases)
		// The run carries the same outcome as its summary, from the store's batch read.
		want := execution.RunOutcome{Verdict: execution.VerdictFailed, Executed: 1, Failed: 1, Untested: 1}
		assert.Equal(t, want, out.Run.Outcome)
		got, err := s.Execution.GetRun(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, want, got.Outcome)

		var count int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM test_cases`).Scan(&count))
		assert.Equal(t, 3, count, "unknown TC-IDs never create test cases")

		// A result received after deprecation stays in the deprecated test case's history.
		h, err := s.Execution.History(ctx, old.ID, pagination.Default())
		require.NoError(t, err)
		require.Equal(t, int64(1), h.Total)
		assert.Equal(t, execution.CorrelationDeprecated, h.Items[0].Result.Correlation)
		assert.Equal(t, old.ID, *h.Items[0].Result.TestCaseID)
	})

	t.Run("BE-INT-018_parse_errors_and_unknown_durations_are_stored_with_the_run", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		doc := junitFor(
			`<testcase name="slow TC-`+itoa(tc.ID)+`" time="soon"><failure/></testcase>`,
			`<testcase name="no time TC-`+itoa(tc.ID)+`"/>`,
			`<testcase name=""/>`,
			`<testcase name="instant TC-`+itoa(tc.ID)+`" time="0.0001"/>`,
		)
		out, err := s.Ingestion.IngestJUnit(ctx, meta("900", 1), strings.NewReader(doc))
		require.NoError(t, err)
		require.Len(t, out.ParseErrors, 3)
		assert.Equal(t, int32(0), out.ParseErrors[0].Index)
		assert.Equal(t, "error", out.ParseErrors[0].Severity)
		assert.Equal(t, int32(2), out.ParseErrors[1].Index)
		assert.Equal(t, int32(3), out.ParseErrors[2].Index)
		assert.Equal(t, "warning", out.ParseErrors[2].Severity, "a 0 ms pass is flagged for review")

		res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		require.Len(t, res.Items, 3)
		assert.Equal(t, int64(0), *res.Items[2].DurationMs, "sub-millisecond durations are stored as 0")
		assert.Nil(t, res.Items[0].DurationMs, "invalid time is stored as unknown, never 0")
		assert.Equal(t, execution.Failed, res.Items[0].Status)
		assert.Nil(t, res.Items[1].DurationMs, "absent time is unknown")

		page, err := s.Execution.ListParseErrors(ctx, out.Run.ID, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(3), page.Total)
		assert.False(t, page.Items[0].Persisted)

		replay, err := s.Ingestion.IngestJUnit(ctx, meta("900", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, out.ParseErrors, replay.ParseErrors, "a replay reports the stored parse errors")
	})

	t.Run("BE-INT-019_ci_reported_run_status_is_stored_and_not_changed_by_replays", func(t *testing.T) {
		s, ctx := fresh(t)
		m := meta("950", 1)
		m.Status = execution.RunInterrupted
		out, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.Equal(t, execution.RunInterrupted, out.Run.Status)
		run, err := s.Execution.GetRun(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.RunInterrupted, run.Status)

		replay, err := s.Ingestion.IngestJUnit(ctx, meta("950", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.Equal(t, execution.RunInterrupted, replay.Run.Status, "a replay never changes the recorded status")
		require.Len(t, replay.Warnings, 1)
		assert.Contains(t, replay.Warnings[0], `status "completed" differs from "interrupted"`)
	})

	t.Run("BE-INT-008_duplicate_ingestion_is_idempotent_even_when_concurrent", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		doc := junitFor(tcProp("a", itoa(tc.ID), ""))
		var wg sync.WaitGroup
		created := make(chan bool, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := s.Ingestion.IngestJUnit(ctx, meta("200", 1), strings.NewReader(doc))
				if assert.NoError(t, err) {
					created <- out.Created
				}
			}()
		}
		wg.Wait()
		close(created)
		n := 0
		for c := range created {
			if c {
				n++
			}
		}
		assert.Equal(t, 1, n, "exactly one request creates the run")
		runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), runs.Total)
		assert.Equal(t, int32(1), runs.Items[0].ResultCount)

		same, err := s.Ingestion.IngestJUnit(ctx, meta("200", 1), strings.NewReader(doc))
		require.NoError(t, err)
		assert.False(t, same.Created)
		assert.Empty(t, same.Warnings, "an identical replay is silent")

		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{Title: "added after the run", Automated: true})
		replay, err := s.Ingestion.IngestJUnit(ctx, meta("200", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, 1, replay.Persisted, "a different report is not applied")
		assert.Equal(t, []string{ingestion.ReportDiffersWarning}, replay.Warnings)
		assert.Equal(t, int32(1), replay.Run.ExpectedCount, "a replay never recomputes the snapshot")
		sum, err := s.Execution.Summary(ctx, replay.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(1), sum.ExpectedTotal)
	})

	t.Run("BE-INT-009_rerun_attempt_creates_new_run_and_preserves_history", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		first, err := s.Ingestion.IngestJUnit(ctx, meta("300", 1), strings.NewReader(junitFor(tcProp("a", itoa(tc.ID), `<failure/>`))))
		require.NoError(t, err)
		second, err := s.Ingestion.IngestJUnit(ctx, meta("300", 2), strings.NewReader(junitFor(tcProp("a", itoa(tc.ID), ""))))
		require.NoError(t, err)
		assert.True(t, second.Created)
		assert.NotEqual(t, first.Run.ID, second.Run.ID)
		assert.Equal(t, "github:300:2", second.Run.ExternalRunID)

		old, err := s.Execution.GetRun(ctx, first.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(1), old.ResultCount)

		runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{}, pagination.Page{Number: 1, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), runs.Total)
		assert.Equal(t, second.Run.ID, runs.Items[0].ID, "newest first")
	})

	t.Run("BE-INT-010_snapshot_is_immutable_and_isolated_from_later_catalog_changes", func(t *testing.T) {
		s, ctx := fresh(t)
		a, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		b, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "b", Automated: true})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("400", 1), strings.NewReader(junitFor(tcProp("a", itoa(a.ID), ""))))
		require.NoError(t, err)
		before, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)

		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{Title: "new", Automated: true})
		_, _ = s.Catalog.Deprecate(ctx, b.ID, etag.Match{})
		_, _ = s.Catalog.Update(ctx, a.ID, catalog.UpdateInput{Title: ptr("renamed"), Automated: ptr(false)}, etag.Match{})

		after, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		assert.Equal(t, int32(2), after.ExpectedTotal)

		_, err = db.Pool.Exec(ctx, `DELETE FROM test_run_expected_cases WHERE test_run_id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "immutable")
		_, err = db.Pool.Exec(ctx, `UPDATE test_run_expected_cases SET test_case_id = 1 WHERE test_run_id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "immutable")
	})

	t.Run("BE-INT-024_database_protects_run_identity_and_snapshot", func(t *testing.T) {
		s, ctx := fresh(t)
		a, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("500", 1), strings.NewReader(junitFor(tcProp("a", itoa(a.ID), ""))))
		require.NoError(t, err)
		late, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "late", Automated: true})

		_, err = db.Pool.Exec(ctx, `INSERT INTO test_run_expected_cases (test_run_id, test_case_id) VALUES ($1, $2)`, out.Run.ID, late.ID)
		assert.ErrorContains(t, err, "the expected-universe snapshot of a test run is immutable", "no TC-ID joins an existing snapshot")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET external_run_id = 'x:y:1', provider = 'x', provider_run_id = 'y' WHERE id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "identity of a test run is immutable")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET report_sha256 = 'other' WHERE id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "identity of a test run is immutable")
		_, err = db.Pool.Exec(ctx, `DELETE FROM test_runs WHERE id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "test runs cannot be deleted")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET status = 'interrupted' WHERE id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "a finished test run keeps its status", "only running (manual or live) runs change status (P11-5)")
		_, err = db.Pool.Exec(ctx, `UPDATE test_runs SET started_at = completed_at + interval '1 second' WHERE id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "test_runs_started_before_completed")

		future, err := s.Ingestion.IngestJUnit(ctx, meta("501", 1),
			strings.NewReader(`<testsuite name="s" timestamp="2099-01-01T00:00:00Z"><testcase name="t"/></testsuite>`))
		require.NoError(t, err)
		assert.Nil(t, future.Run.StartedAt)
		assert.Equal(t, []string{fmt.Sprintf(ingestion.FutureStartWarning, "2099-01-01T00:00:00Z")}, future.Warnings)

		m := meta("500", 1)
		m.Branch, m.Commit, m.Status = "release", "def456", execution.RunInterrupted
		replay, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor(tcProp("a", itoa(a.ID), ""))))
		require.NoError(t, err)
		assert.Equal(t, []string{
			`status "interrupted" differs from "completed", recorded for this attempt; it was not applied`,
			`branch "release" differs from "main", recorded for this attempt; it was not applied`,
			`commit "def456" differs from "abc123", recorded for this attempt; it was not applied`,
		}, replay.Warnings)
		assert.Equal(t, "main", replay.Run.Branch, "a replay never changes the recorded metadata")
	})

	t.Run("BE-INT-025_ingested_results_and_parse_errors_are_immutable", func(t *testing.T) {
		s, ctx := fresh(t)
		a, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("600", 1), strings.NewReader(junitFor(
			tcProp("a", itoa(a.ID), `<failure message="boom"/>`), `<testcase name="bad time" time="soon"/>`)))
		require.NoError(t, err)
		require.Len(t, out.ParseErrors, 1)

		for _, stmt := range []string{
			`INSERT INTO test_results (test_run_id, correlation, test_name, status) VALUES ($1, 'missing', 'late', 'passed')`,
			`UPDATE test_results SET status = 'passed' WHERE test_run_id = $1`,
			`DELETE FROM test_results WHERE test_run_id = $1`,
		} {
			_, err = db.Pool.Exec(ctx, stmt, out.Run.ID)
			assert.ErrorContains(t, err, "ingested test results are immutable", stmt)
		}
		for _, stmt := range []string{
			`INSERT INTO test_run_parse_errors (test_run_id, case_index, test_name, message, persisted) VALUES ($1, 9, 'late', 'x', false)`,
			`UPDATE test_run_parse_errors SET message = 'edited' WHERE test_run_id = $1`,
			`DELETE FROM test_run_parse_errors WHERE test_run_id = $1`,
		} {
			_, err = db.Pool.Exec(ctx, stmt, out.Run.ID)
			assert.ErrorContains(t, err, "parse errors of a test run are immutable", stmt)
		}
		run, err := s.Execution.GetRun(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, execution.VerdictFailed, run.Outcome.Verdict, "the ingested failure still decides the verdict")
	})

	t.Run("BE-INT-027_snapshot_and_correlation_agree_under_concurrent_deprecation", func(t *testing.T) {
		s, ctx := fresh(t)
		for i := 0; i < 30; i++ {
			tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "racy", Automated: true})
			var (
				wg  sync.WaitGroup
				out ingestion.Outcome
				err error
			)
			wg.Add(2)
			go func() {
				defer wg.Done()
				out, err = s.Ingestion.IngestJUnit(ctx, meta("race"+itoa(int64(i)), 1), strings.NewReader(junitFor(tcProp("t", itoa(tc.ID), ""))))
			}()
			go func() { defer wg.Done(); _, _ = s.Catalog.Deprecate(ctx, tc.ID, etag.Match{}) }()
			wg.Wait()
			require.NoError(t, err)
			sum, err := s.Execution.Summary(ctx, out.Run.ID)
			require.NoError(t, err)
			res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
			require.NoError(t, err)
			inSnapshot := sum.ExpectedTotal == 1
			valid := res.Items[0].Correlation == execution.CorrelationValid
			assert.Equal(t, inSnapshot, valid, "iteration %d: a TC in the snapshot is valid, one deprecated before the read is in neither", i)
		}
	})

	t.Run("BE-INT-029_deep_pages_cost_no_more_than_the_first", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "busy", Automated: true})
		cases := make([]string, 20000)
		for i := range cases {
			cases[i] = tcProp("c"+itoa(int64(i)), itoa(tc.ID), "")
		}
		_, err := s.Ingestion.IngestJUnit(ctx, meta("900", 1), strings.NewReader(junitFor(cases...)))
		require.NoError(t, err)
		// Median of 7 reads of both lists, so one slow read (GC, a busy CI runner) does not decide.
		timed := func(page int32) time.Duration {
			var times []time.Duration
			for range 7 {
				start := time.Now()
				_, err := s.Execution.History(ctx, tc.ID, pagination.Page{Number: page, Size: 20})
				require.NoError(t, err)
				_, err = s.Execution.ListRuns(ctx, execution.RunFilter{}, pagination.Page{Number: page, Size: 20})
				require.NoError(t, err)
				times = append(times, time.Since(start))
			}
			slices.Sort(times)
			return times[len(times)/2]
		}
		first := max(timed(1), 5*time.Millisecond) // below the timer's useful resolution on a busy runner
		last := timed(1000)
		// Before the fix the per-run counts ran for every row skipped by OFFSET, so the
		// last page took orders of magnitude longer than the first.
		assert.Less(t, last, 5*first, "first page %v, last page %v", first, last)
	})

	t.Run("BE-INT-030_summary_of_a_mixed_run_matches_the_hand_computed_numbers", func(t *testing.T) {
		s, ctx := fresh(t)
		auto := func(title string) int64 {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: title, Automated: true})
			require.NoError(t, err)
			return tc.ID
		}
		a, b, c, e, f, g, l := auto("A"), auto("B"), auto("C"), auto("E"), auto("F"), auto("G"), auto("L")
		manual, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "M"})
		dep := auto("D")
		_, _ = s.Catalog.Deprecate(ctx, dep, etag.Match{})
		tcase := func(name string, id int64, body string) string {
			return `<testcase name="` + name + ` TC-` + itoa(id) + `">` + body + `</testcase>`
		}
		out, err := s.Ingestion.IngestJUnit(ctx, meta("1000", 1), strings.NewReader(junitFor(
			tcase("chrome", a, ""), tcase("firefox", a, "<failure/>"), // failed > passed
			tcase("b1", b, "<error/>"), tcase("b2", b, "<skipped/>"), // error > skipped
			tcase("c1", c, "<skipped/>"), tcase("c2", c, ""), // skipped > passed
			tcase("e", e, ""), tcase("g1", g, ""), tcase("g2", g, ""), tcase("l", l, "<failure/>"),
			tcase("m", manual.ID, ""), tcase("d", dep, ""), // outside the universe / deprecated
			`<testcase name="unknown TC-987654"/>`, `<testcase name="mal TC-x"/>`, `<testcase name="none"/>`,
		)))
		require.NoError(t, err)
		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		// Universe: A B C E F G L (7; M is manual, D deprecated). F has no result.
		assert.Equal(t, int32(7), sum.ExpectedTotal)
		assert.Equal(t, int32(6), sum.ExecutedTotal)
		assert.Equal(t, execution.StatusCounts{Untested: 1, Passed: 2, Failed: 2, Error: 1, Skipped: 1}, sum.Counts)
		assert.Equal(t, execution.StatusPercentages{Untested: 14.285714, Passed: 28.571429, Failed: 28.571429, Error: 14.285714, Skipped: 14.285714}, sum.PercentOfExpected)
		assert.Equal(t, execution.ExecutedPercentages{Passed: 33.333333, Failed: 33.333333, Error: 16.666667, Skipped: 16.666667}, sum.PercentOfExecuted)
		assert.Equal(t, 85.714286, sum.ExecutionPercent)
		assert.Equal(t, execution.DiagnosticCounts{Missing: 1, Malformed: 1, Unknown: 1, Deprecated: 1, Total: 4}, sum.Diagnostics)
		assert.Equal(t, int32(1), sum.OutsideUniverse)
		assert.Equal(t, []int64{manual.ID}, sum.OutsideUniverseIDs)
		statuses := map[int64]execution.SummaryStatus{}
		for _, tc := range sum.TestCases {
			statuses[tc.TestCaseID] = tc.Status
		}
		assert.Equal(t, map[int64]execution.SummaryStatus{a: "failed", b: "error", c: "skipped", e: "passed", f: "untested", g: "passed", l: "failed"}, statuses)
		res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(15), res.Total, "every individual result is still listed")

		// Later catalog changes never touch the summary of an existing run.
		_, _ = s.Catalog.Deprecate(ctx, l, etag.Match{})
		_, _ = s.Catalog.Deprecate(ctx, e, etag.Match{})
		_, _ = s.Catalog.Update(ctx, a, catalog.UpdateInput{Title: ptr("renamed"), Automated: ptr(false)}, etag.Match{})
		auto("added later")
		again, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, sum, again)

		// A run where nothing was executed: everything untested, 0% executed, no division by zero.
		empty, err := s.Ingestion.IngestJUnit(ctx, meta("1001", 1), strings.NewReader(`<testsuite name="e"/>`))
		require.NoError(t, err)
		none, err := s.Execution.Summary(ctx, empty.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(5), none.ExpectedTotal, "B C F G + added later")
		assert.Equal(t, float64(100), none.PercentOfExpected.Untested)
		assert.Equal(t, execution.ExecutedPercentages{}, none.PercentOfExecuted)
		assert.Zero(t, none.ExecutionPercent)
	})

	t.Run("BE-INT-011_database_enforces_result_and_run_invariants", func(t *testing.T) {
		_, ctx := fresh(t)
		// Results can only be written by the transaction that creates their run, so
		// each invalid row is tried in a fresh run's transaction (rolled back).
		n := 0
		insertResult := func(cols, values string) error {
			n++
			tx, err := db.Pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			var runID int64
			require.NoError(t, tx.QueryRow(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status)
				VALUES (1, $1, 'github', 'inv', $2, 'completed') RETURNING id`, "github:inv:"+itoa(int64(n)), n).Scan(&runID))
			_, err = tx.Exec(ctx, `INSERT INTO test_results (test_run_id, `+cols+`) VALUES ($1, `+values+`)`, runID)
			return err
		}
		assert.ErrorContains(t, insertResult("correlation, test_name, status", "'valid', 'x', 'passed'"), "test_results_linked_has_test_case")
		assert.ErrorContains(t, insertResult("test_case_id, correlation, test_name, status", "1, 'unknown', 'x', 'passed'"), "test_results_linked_has_test_case")
		assert.ErrorContains(t, insertResult("correlation, test_name, status", "'deprecated', 'x', 'passed'"), "test_results_linked_has_test_case",
			"deprecated results must keep the TC-ID link")
		assert.Error(t, insertResult("correlation, test_name, status", "'missing', 'x', 'untested'"), "untested is never persisted")
		assert.NoError(t, insertResult("correlation, test_name, status", "'missing', 'x', 'passed'"), "a valid row is accepted by its run's transaction")
		_, err := db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status) VALUES (1, 'x:y:1', 'github', '1', 1, 'completed')`)
		assert.ErrorContains(t, err, "external_run_id_format")

		// The full lifecycle is supported by the model; running is for manual and live runs (P11-5).
		for i, status := range []string{"created", "running", "completed", "interrupted", "cancelled"} {
			mode := "batch"
			if status == "running" {
				mode = "manual"
			}
			_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status, mode) VALUES (1, $1, 'github', 'life', $2, $3, $4)`,
				"github:life:"+itoa(int64(i+1)), i+1, status, mode)
			assert.NoError(t, err, status)
		}
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status) VALUES (1, 'github:life:8', 'github', 'life', 8, 'running')`)
		assert.ErrorContains(t, err, "test_runs_running_is_not_batch", "a CI report is never running")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status) VALUES (1, 'github:life:9', 'github', 'life', 9, 'paused')`)
		assert.Error(t, err, "unknown run statuses are rejected")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, status) VALUES (1, 'github:life:10', 'github', 'life', 10, 'failed')`)
		assert.Error(t, err, "failed was renamed interrupted (migration 00007)")
	})

	t.Run("BE-INT-013_run_results_filters_and_pagination", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("600", 1), strings.NewReader(junitFor(
			tcProp("p1", itoa(tc.ID), ""), tcProp("p2", itoa(tc.ID), ""), tcProp("f1", itoa(tc.ID), "<failure/>"),
			`<testcase name="e1"><error/></testcase>`,
		)))
		require.NoError(t, err)
		passed := execution.Passed
		res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{Status: &passed}, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), res.Total)
		assert.Equal(t, "p2", res.Items[0].TestName)
		missing := execution.CorrelationMissing
		res, err = s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{Correlation: &missing}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, []string{"e1"}, []string{res.Items[0].TestName})
		assert.Nil(t, res.Items[0].TestCaseID)
		assert.Nil(t, res.Items[0].RequestedTestCaseID)

		_, err = s.Execution.ListRunResults(ctx, 987654, execution.ResultFilter{}, pagination.Default())
		e, ok := apperr.As(err)
		require.True(t, ok)
		assert.Equal(t, apperr.KindNotFound, e.Kind)
		_, err = s.Execution.Summary(ctx, 987654)
		assert.Error(t, err)
	})

	t.Run("BE-INT-014_history_of_a_tc_id_across_runs", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Automated: true})
		_, err := s.Ingestion.IngestJUnit(ctx, meta("700", 1), strings.NewReader(junitFor(tcProp("a", itoa(tc.ID), ""))))
		require.NoError(t, err)
		_, err = s.Ingestion.IngestJUnit(ctx, meta("701", 1), strings.NewReader(junitFor(tcProp("a renamed", itoa(tc.ID), "<failure/>"))))
		require.NoError(t, err)
		_, _ = s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("edited later")}, etag.Match{})

		h, err := s.Execution.History(ctx, tc.ID, pagination.Default())
		require.NoError(t, err)
		require.Equal(t, int64(2), h.Total)
		assert.Equal(t, execution.Failed, h.Items[0].Result.Status)
		assert.Equal(t, "a renamed", h.Items[0].Result.TestName, "observed metadata is kept as executed")
		assert.Equal(t, "github:701:1", h.Items[0].Run.ExternalRunID)
		assert.Equal(t, "abc123", h.Items[0].Run.Commit)
		assert.Equal(t, int32(1), h.Items[0].Run.ExpectedCount)
		assert.Equal(t, execution.Passed, h.Items[1].Result.Status)

		page, err := s.Execution.History(ctx, tc.ID, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, "github:700:1", page.Items[0].Run.ExternalRunID)

		// A run with many results for the TC: pages are chosen before the per-run
		// counts are computed, and each row still carries its run's counts.
		cases := make([]string, 250)
		for i := range cases {
			cases[i] = tcProp("browser "+itoa(int64(i)), itoa(tc.ID), "")
		}
		big, err := s.Ingestion.IngestJUnit(ctx, meta("702", 1), strings.NewReader(junitFor(cases...)))
		require.NoError(t, err)
		deep, err := s.Execution.History(ctx, tc.ID, pagination.Page{Number: 26, Size: 10})
		require.NoError(t, err)
		assert.Equal(t, int64(252), deep.Total)
		require.Len(t, deep.Items, 2)
		assert.Equal(t, "github:701:1", deep.Items[0].Run.ExternalRunID)
		assert.Equal(t, "github:700:1", deep.Items[1].Run.ExternalRunID)
		first, err := s.Execution.History(ctx, tc.ID, pagination.Page{Number: 1, Size: 10})
		require.NoError(t, err)
		for _, it := range first.Items {
			assert.Equal(t, big.Run.ID, it.Run.ID)
			assert.Equal(t, int32(250), it.Run.ResultCount)
			assert.Equal(t, int32(1), it.Run.ExpectedCount)
		}
		assert.Equal(t, "browser 249", first.Items[0].Result.TestName, "newest first")
		runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{}, pagination.Page{Number: 2, Size: 2})
		require.NoError(t, err)
		require.Len(t, runs.Items, 1)
		assert.Equal(t, "github:700:1", runs.Items[0].ExternalRunID)
		assert.Equal(t, int32(1), runs.Items[0].ResultCount)
	})

	t.Run("BE-INT-017_outside_universe_results_are_valid_but_excluded", func(t *testing.T) {
		s, ctx := fresh(t)
		manual, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "manual"})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("800", 1), strings.NewReader(junitFor(tcProp("m", itoa(manual.ID), ""))))
		require.NoError(t, err)
		assert.Empty(t, out.Diagnostics)
		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(0), sum.ExpectedTotal)
		assert.Equal(t, int32(1), sum.OutsideUniverse)
		assert.Equal(t, []int64{manual.ID}, sum.OutsideUniverseIDs)
	})
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
