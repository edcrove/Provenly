//go:build integration

package integration

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestRetries(t *testing.T) {
	t.Run("BE-INT-044_attempts_are_stored_and_the_last_one_is_the_logical_result_flaky_when_it_passed_after_failing", func(t *testing.T) {
		s, ctx := fresh(t)
		flaky, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "flaky", Automated: true})
		broken, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "broken", Automated: true})
		id := func(n int64) string { return strconv.FormatInt(n, 10) }
		out, err := s.Ingestion.IngestJUnit(ctx, meta("r1", 1), strings.NewReader(junitFor(
			tcProp("login", id(flaky.ID), `<flakyFailure message="timeout"/>`),
			tcProp("pay", id(broken.ID), `<failure message="first"/><rerunFailure message="again"/>`),
			`<testcase name="retry-by-property TC-`+id(flaky.ID)+`"><properties><property name="retry" value="1"/></properties></testcase>`,
		)))
		require.NoError(t, err)
		assert.Equal(t, 5, out.Persisted)

		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		byID := map[int64]execution.TestCaseOutcome{}
		for _, c := range sum.TestCases {
			byID[c.TestCaseID] = c
		}
		assert.Equal(t, execution.TestCaseOutcome{TestCaseID: flaky.ID, Status: "passed", ResultCount: 3, Flaky: true}, byID[flaky.ID])
		assert.Equal(t, execution.TestCaseOutcome{TestCaseID: broken.ID, Status: "failed", ResultCount: 2}, byID[broken.ID])
		assert.Equal(t, int32(1), sum.Flaky)
		run, _ := s.Execution.GetRun(ctx, out.Run.ID)
		assert.Equal(t, int32(1), run.Outcome.Flaky)

		results, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		var got []string
		for _, r := range results.Items {
			got = append(got, r.TestName+"#"+strconv.Itoa(int(r.Attempt))+" "+string(r.Status)+" retried="+strconv.FormatBool(r.Retried))
		}
		// Tests whose last attempt failed come first; a test's attempts stay together (deployed audit).
		assert.Equal(t, []string{
			"pay#1 failed retried=true", "pay#2 failed retried=false",
			"login#1 failed retried=true", "login#2 passed retried=false",
			"retry-by-property TC-" + id(flaky.ID) + "#2 passed retried=false",
		}, got)
		hist, err := s.Execution.History(ctx, flaky.ID, pagination.Default())
		require.NoError(t, err)
		assert.True(t, hist.Items[len(hist.Items)-1].Result.Retried, "the oldest attempt was retried")

		_, err = db.Pool.Exec(ctx, `UPDATE test_results SET attempt = 2`)
		assert.Error(t, err, "results stay immutable")
	})

	t.Run("BE-INT-061_repeated_names_without_an_attempt_signal_are_variants_a_failure_fails_the_test_case_and_is_never_flaky", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		same, mixed := create("same"), create("mixed")
		id := func(n int64) string { return strconv.FormatInt(n, 10) }
		out, err := s.Ingestion.IngestJUnit(ctx, meta("v1", 1), strings.NewReader(junitFor(
			tcProp("checkout", id(same.ID), `<failure message="boom"/>`),
			tcProp("checkout", id(same.ID), ""),                                // same suite, class and name, no attempt signal: a variant
			tcProp("login", id(mixed.ID), `<flakyFailure message="timeout"/>`), // passed on a retry...
			tcProp("logout", id(mixed.ID), `<failure message="for good"/>`),    // ...but a variant failed for good
		)))
		require.NoError(t, err)
		assert.Contains(t, out.Warnings, fmt.Sprintf(junit.VariantsNotice, 1))

		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		byID := map[int64]execution.TestCaseOutcome{}
		for _, c := range sum.TestCases {
			byID[c.TestCaseID] = c
		}
		assert.Equal(t, execution.TestCaseOutcome{TestCaseID: same.ID, Status: "failed", ResultCount: 2}, byID[same.ID])
		assert.Equal(t, execution.TestCaseOutcome{TestCaseID: mixed.ID, Status: "failed", ResultCount: 3}, byID[mixed.ID])
		assert.Equal(t, int32(0), sum.Flaky)

		results, err := s.Execution.ListRunResults(ctx, out.Run.ID, execution.ResultFilter{}, pagination.Default())
		require.NoError(t, err)
		retried := 0
		for _, r := range results.Items {
			if r.Retried {
				retried++
			}
		}
		assert.Equal(t, 1, retried, "both variants are listed and neither is a retry; only login's first attempt is")

		conclusive, _, err := s.Execution.LatestConclusive(ctx, []int64{same.ID, mixed.ID})
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{same.ID: "failed", mixed.ID: "failed"}, conclusive)

		flaky, err := s.Execution.FlakyCounts(ctx, catalog.DefaultProjectID, 20, 20)
		require.NoError(t, err)
		assert.Empty(t, flaky, "a test case that failed in the run is not flaky")
	})
}
