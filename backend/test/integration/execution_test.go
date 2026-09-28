//go:build integration

package integration

import (
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
		_, _ = s.Catalog.Deprecate(ctx, old.ID)

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
		)
		out, err := s.Ingestion.IngestJUnit(ctx, meta("900", 1), strings.NewReader(doc))
		require.NoError(t, err)
		require.Len(t, out.ParseErrors, 2)
		assert.Equal(t, int32(0), out.ParseErrors[0].Index)
		assert.Equal(t, int32(2), out.ParseErrors[1].Index)

		res, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		require.Len(t, res.Items, 2)
		assert.Nil(t, res.Items[0].DurationMs, "invalid time is stored as unknown, never 0")
		assert.Equal(t, execution.Failed, res.Items[0].Status)
		assert.Nil(t, res.Items[1].DurationMs, "absent time is unknown")

		page, err := s.Execution.ListParseErrors(ctx, out.Run.ID, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), page.Total)
		assert.False(t, page.Items[0].Persisted)

		replay, err := s.Ingestion.IngestJUnit(ctx, meta("900", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, out.ParseErrors, replay.ParseErrors, "a replay reports the stored parse errors")
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
		runs, err := s.Execution.ListRuns(ctx, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), runs.Total)
		assert.Equal(t, int32(1), runs.Items[0].ResultCount)

		replay, err := s.Ingestion.IngestJUnit(ctx, meta("200", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		assert.False(t, replay.Created)
		assert.Equal(t, 1, replay.Persisted)
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

		runs, err := s.Execution.ListRuns(ctx, pagination.Page{Number: 1, Size: 1})
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
		_, _ = s.Catalog.Deprecate(ctx, b.ID)
		_, _ = s.Catalog.Update(ctx, a.ID, catalog.UpdateInput{Title: ptr("renamed"), Automated: ptr(false)})

		after, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		assert.Equal(t, int32(2), after.ExpectedTotal)

		_, err = db.Pool.Exec(ctx, `DELETE FROM test_run_expected_cases WHERE test_run_id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "immutable")
		_, err = db.Pool.Exec(ctx, `UPDATE test_run_expected_cases SET test_case_id = 1 WHERE test_run_id = $1`, out.Run.ID)
		assert.ErrorContains(t, err, "immutable")
	})

	t.Run("BE-INT-011_database_enforces_result_and_run_invariants", func(t *testing.T) {
		s, ctx := fresh(t)
		out, err := s.Ingestion.IngestJUnit(ctx, meta("500", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_results (test_run_id, correlation, test_name, status) VALUES ($1, 'valid', 'x', 'passed')`, out.Run.ID)
		assert.ErrorContains(t, err, "test_results_linked_has_test_case")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_results (test_run_id, test_case_id, correlation, test_name, status) VALUES ($1, 1, 'unknown', 'x', 'passed')`, out.Run.ID)
		assert.ErrorContains(t, err, "test_results_linked_has_test_case")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_results (test_run_id, correlation, test_name, status) VALUES ($1, 'deprecated', 'x', 'passed')`, out.Run.ID)
		assert.ErrorContains(t, err, "test_results_linked_has_test_case", "deprecated results must keep the TC-ID link")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_results (test_run_id, correlation, test_name, status) VALUES ($1, 'missing', 'x', 'untested')`, out.Run.ID)
		assert.Error(t, err, "untested is never persisted")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (external_run_id, provider, provider_run_id, run_attempt, status) VALUES ('x:y:1', 'github', '1', 1, 'completed')`)
		assert.ErrorContains(t, err, "external_run_id_format")
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
		_, _ = s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("edited later")})

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

func itoa(id int64) string { return strings.TrimPrefix(catalog.FormatKey(id), "TC-") }
