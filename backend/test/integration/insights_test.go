//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/insights"
)

func TestInsights(t *testing.T) {
	t.Run("BE-INT-052_quality_counts_automation_stale_and_flaky_test_cases_from_history", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string, automated bool) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: automated})
			require.NoError(t, err)
			return tc
		}
		login, pay, old, never, hand := create("login", true), create("pay", true), create("old", true), create("never", true), create("hand", false)
		ingestCases := func(runID string, cases ...string) {
			_, err := s.Ingestion.IngestJUnit(ctx, meta(runID, 1), strings.NewReader(junitFor(cases...)))
			require.NoError(t, err)
		}
		retried := func(name, key string) string {
			return `<testcase name="` + name + `" classname="c"><properties><property name="tc-id" value="` + key + `"/><property name="attempt" value="1"/></properties><failure message="x"/></testcase>` +
				`<testcase name="` + name + `" classname="c"><properties><property name="tc-id" value="` + key + `"/><property name="attempt" value="2"/></properties></testcase>`
		}
		// The first run started long ago but its report arrives now (a late upload): it counts by its start.
		oldReport := strings.Replace(junitFor(tcProp("old", old.Key(), ""), retried("pay", pay.Key())), "2026-09-28T10:00:00", "2025-01-01T10:00:00", 1)
		_, err := s.Ingestion.IngestJUnit(ctx, meta("1", 1), strings.NewReader(oldReport))
		require.NoError(t, err)
		ingestCases("2", retried("login", login.Key()), retried("pay", pay.Key()))
		ingestCases("3", retried("login", login.Key()), tcProp("pay", pay.Key(), `<failure message="still failing"/>`))

		// Manual re-tests are never flaky.
		run, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "sign-off"})
		require.NoError(t, err)
		for _, st := range []execution.ResultStatus{execution.Failed, execution.Passed} {
			_, err = s.Manual.Record(ctx, run.ID, ingestion.ManualResultInput{TestCaseID: hand.ID, Status: st})
			require.NoError(t, err)
		}

		q, err := s.Insights.Quality(ctx, insights.Query{ProjectKey: "TC"})
		require.NoError(t, err)
		assert.Equal(t, int32(5), q.Active)
		assert.Equal(t, int32(4), q.Automated)
		assert.Equal(t, float64(80), q.AutomationRate)
		assert.Equal(t, int32(1), q.NeverExecuted)
		assert.Equal(t, int32(1), q.Stale)
		require.Len(t, q.StaleCases, 2)
		assert.Equal(t, never.Key(), q.StaleCases[0].Key, "never executed first")
		assert.Equal(t, old.Key(), q.StaleCases[1].Key)
		// The last execution is the first run's start, not its (recent) upload (card #52).
		assert.True(t, time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC).Equal(*q.StaleCases[1].LastExecutedAt), "last executed %v", *q.StaleCases[1].LastExecutedAt)
		assert.Equal(t, []insights.FlakyCase{{TestCaseID: login.ID, Key: login.Key(), Runs: 2}, {TestCaseID: pay.ID, Key: pay.Key(), Runs: 2}}, q.Flaky,
			"pay failed for good in run 3: flaky in runs 1 and 2 only")

		q, err = s.Insights.Quality(ctx, insights.Query{ProjectKey: "TC", Window: 2, StaleDays: 365})
		require.NoError(t, err)
		assert.Equal(t, int32(1), q.Stale, "old started in 2025: stale even over a year, though uploaded today")
		assert.Equal(t, []insights.FlakyCase{{TestCaseID: login.ID, Key: login.Key(), Runs: 1}}, q.Flaky,
			"the latest two runs are the manual one and run 3")
	})

	t.Run("BE-INT-068_last_execution_falls_back_to_the_upload_time_when_the_report_has_no_start", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "no start", Automated: true})
		require.NoError(t, err)
		_, err = s.Ingestion.IngestJUnit(ctx, meta("nostart", 1), strings.NewReader(`<testsuite name="s">`+tcProp("x", tc.Key(), "")+`</testsuite>`))
		require.NoError(t, err)
		q, err := s.Insights.Quality(ctx, insights.Query{ProjectKey: "TC", StaleDays: 1})
		require.NoError(t, err)
		assert.Equal(t, int32(0), q.Stale, "uploaded now, no start: executed now")
	})
}
