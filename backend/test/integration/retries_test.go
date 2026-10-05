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
		assert.Equal(t, []string{
			"login#1 failed retried=true", "login#2 passed retried=false",
			"pay#1 failed retried=true", "pay#2 failed retried=false",
			"retry-by-property TC-" + id(flaky.ID) + "#2 passed retried=false",
		}, got)
		hist, err := s.Execution.History(ctx, flaky.ID, pagination.Default())
		require.NoError(t, err)
		assert.True(t, hist.Items[len(hist.Items)-1].Result.Retried, "the oldest attempt was retried")

		_, err = db.Pool.Exec(ctx, `UPDATE test_results SET attempt = 2`)
		assert.Error(t, err, "results stay immutable")
	})
}
