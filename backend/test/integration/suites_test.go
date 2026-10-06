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
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestSuites(t *testing.T) {
	t.Run("BE-INT-047_suites_select_test_cases_and_scope_partial_runs", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string, automated bool, tags ...string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: automated, Tags: tags})
			require.NoError(t, err)
			return tc
		}
		login, pay, refund := create("login", true, "smoke"), create("pay", true, "smoke"), create("refund", true)
		manual := create("manual", false, "smoke")
		gone := create("gone", true, "smoke")
		_, err := s.Catalog.Deprecate(ctx, gone.ID, etag.Match{})
		require.NoError(t, err)
		_, err = s.Catalog.Update(ctx, pay.ID, catalog.UpdateInput{Classification: map[string]*string{"risk": ptr("critical")}}, etag.Match{})
		require.NoError(t, err)

		// A static suite and two query suites.
		release, err := s.Catalog.CreateSuite(ctx, catalog.DefaultProjectID, catalog.SuiteInput{Key: "release", Name: "Release", Kind: catalog.SuiteKindStatic,
			TestCaseIDs: []int64{refund.ID, login.ID, refund.ID, manual.ID}})
		require.NoError(t, err)
		assert.Equal(t, []int64{login.ID, refund.ID, manual.ID}, release.CaseIDs)
		assert.Equal(t, int32(3), release.CaseCount)
		_, err = s.Catalog.CreateSuite(ctx, catalog.DefaultProjectID, catalog.SuiteInput{Key: "smoke", Name: "Smoke", Kind: catalog.SuiteKindQuery,
			Query: catalog.SuiteQuery{Tag: ptr("smoke")}})
		require.NoError(t, err)
		critical, err := s.Catalog.CreateSuite(ctx, catalog.DefaultProjectID, catalog.SuiteInput{Key: "critical", Name: "Critical", Kind: catalog.SuiteKindQuery,
			Query: catalog.SuiteQuery{Tag: ptr("smoke"), Classified: []string{"risk:critical"}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"risk:critical"}, critical.Query.Classified)
		_, err = s.Catalog.CreateSuite(ctx, catalog.DefaultProjectID, catalog.SuiteInput{Key: "smoke", Name: "again", Kind: catalog.SuiteKindQuery, Query: catalog.SuiteQuery{Tag: ptr("x")}})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		suites, err := s.Catalog.Suites(ctx, catalog.DefaultProjectID)
		require.NoError(t, err)
		assert.Equal(t, []string{"critical", "release", "smoke"}, []string{suites[0].Key, suites[1].Key, suites[2].Key})

		// The list narrowed to a suite: members (any status) or matches.
		list := func(f catalog.ListFilter) []int64 {
			res, err := s.Catalog.List(ctx, f, pagination.Page{Number: 1, Size: 20})
			require.NoError(t, err)
			ids := []int64{}
			for _, tc := range res.Items {
				ids = append(ids, tc.ID)
			}
			assert.Equal(t, int64(len(ids)), res.Total)
			return ids
		}
		assert.Equal(t, []int64{manual.ID, refund.ID, login.ID}, list(catalog.SuiteFilter(catalog.ListFilter{}, release)))
		assert.Equal(t, []int64{pay.ID}, list(catalog.SuiteFilter(catalog.ListFilter{}, critical)))

		// A run for a suite expects only the suite's active automated test cases; the rest is outside the universe.
		ingest := func(runID, suite string) (execution.TestRun, error) {
			m := meta(runID, 1)
			m.SuiteKey = suite
			out, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor(
				tcProp("login", login.Key(), ""), tcProp("pay", pay.Key(), `<failure message="x"/>`))))
			return out.Run, err
		}
		smokeRun, err := ingest("1", "smoke")
		require.NoError(t, err)
		assert.Equal(t, int32(2), smokeRun.ExpectedCount, "login and pay; manual and deprecated are not expected")
		assert.Equal(t, "smoke", smokeRun.SuiteKey)
		assert.Equal(t, "Smoke", smokeRun.SuiteName)
		// The test case history carries the run as the run itself reads (its suite and its report digest).
		h, err := s.Execution.History(ctx, login.ID, pagination.Default())
		require.NoError(t, err)
		require.Len(t, h.Items, 1)
		assert.Equal(t, "smoke", h.Items[0].Run.SuiteKey)
		assert.Equal(t, "Smoke", h.Items[0].Run.SuiteName)
		assert.Equal(t, smokeRun.ReportSHA256, h.Items[0].Run.ReportSHA256)
		assert.NotEmpty(t, h.Items[0].Run.ReportSHA256)
		sum, err := s.Execution.Summary(ctx, smokeRun.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(0), sum.Counts.Untested, "a partial run has no untested noise")
		assert.Equal(t, int32(0), sum.OutsideUniverse)

		releaseRun, err := ingest("2", "release")
		require.NoError(t, err)
		assert.Equal(t, int32(2), releaseRun.ExpectedCount, "login and refund")
		sum, err = s.Execution.Summary(ctx, releaseRun.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(1), sum.Counts.Untested, "refund did not run")
		assert.Equal(t, int32(1), sum.OutsideUniverse, "pay ran but is not in the suite")

		full, err := ingest("3", "")
		require.NoError(t, err)
		assert.Equal(t, int32(3), full.ExpectedCount, "without a suite: every active automated test case")
		assert.Empty(t, full.SuiteKey)

		// The suite's name at the time stays on the run; renaming (and re-querying) later does not change it.
		renamed, err := s.Catalog.UpdateSuite(ctx, catalog.DefaultProjectID, "smoke", catalog.UpdateSuiteInput{Name: ptr("Smoke v2"),
			Query: &catalog.SuiteQuery{Tag: ptr("smoke"), Classified: []string{"risk:critical"}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"risk:critical"}, renamed.Query.Classified)
		renamed, err = s.Catalog.UpdateSuite(ctx, catalog.DefaultProjectID, "smoke", catalog.UpdateSuiteInput{Description: ptr("fast")})
		require.NoError(t, err)
		assert.Equal(t, []string{"risk:critical"}, renamed.Query.Classified, "the query stays when not sent")
		again, err := s.Execution.GetRun(ctx, smokeRun.ID)
		require.NoError(t, err)
		assert.Equal(t, "Smoke", again.SuiteName)

		// Runs filter by suite; archived suites take no runs; unknown suites are not found.
		runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{SuiteKey: ptr("smoke")}, pagination.Page{Number: 1, Size: 10})
		require.NoError(t, err)
		// A listed run is the run itself, every field (the list and the detail read the same columns).
		got, err := s.Execution.GetRun(ctx, smokeRun.ID)
		require.NoError(t, err)
		require.NotEmpty(t, runs.Items)
		assert.Equal(t, got, runs.Items[len(runs.Items)-1])
		assert.NotEmpty(t, runs.Items[len(runs.Items)-1].ReportSHA256)
		require.NoError(t, err)
		assert.Equal(t, int64(1), runs.Total)
		assert.Equal(t, smokeRun.ID, runs.Items[0].ID)
		_, err = s.Catalog.UpdateSuite(ctx, catalog.DefaultProjectID, "release", catalog.UpdateSuiteInput{Archived: ptr(true)})
		require.NoError(t, err)
		_, err = ingest("4", "release")
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		_, err = ingest("5", "nope")
		assert.Equal(t, apperr.KindNotFound, kind(t, err))

		// Members are replaced; another project's test cases are refused.
		release, err = s.Catalog.SetSuiteCases(ctx, catalog.DefaultProjectID, "release", []int64{pay.ID})
		require.NoError(t, err)
		assert.Equal(t, []int64{pay.ID}, release.CaseIDs)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		other, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "other"})
		require.NoError(t, err)
		_, err = s.Catalog.SetSuiteCases(ctx, catalog.DefaultProjectID, "release", []int64{other.ID})
		assert.Equal(t, apperr.KindValidation, kind(t, err))

		// The database refuses what the service never does.
		suiteID := strconv.FormatInt(release.ID, 10)
		var smokeID int64
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT id FROM test_suites WHERE key = 'smoke'`).Scan(&smokeID))
		for name, sql := range map[string]string{
			"another project's test case": `INSERT INTO test_suite_cases VALUES (` + suiteID + `, 1, $1)`,
			"members of a query suite":    `INSERT INTO test_suite_cases VALUES (` + strconv.FormatInt(smokeID, 10) + `, 1, ` + strconv.FormatInt(login.ID, 10) + `) RETURNING $1::bigint`,
			"deleting a suite":            `DELETE FROM test_suites WHERE id = ` + suiteID + ` AND $1 > 0`,
			"renaming a key":              `UPDATE test_suites SET key = 'other' WHERE id = ` + suiteID + ` AND $1 > 0`,
			"changing the kind":           `UPDATE test_suites SET kind = 'query', query_tag = 'x' WHERE id = ` + suiteID + ` AND $1 > 0`,
			"a query on a static suite":   `UPDATE test_suites SET query_tag = 'x' WHERE id = ` + suiteID + ` AND $1 > 0`,
			"a run's suite changing":      `UPDATE test_runs SET suite_name = 'renamed' WHERE suite_key IS NOT NULL AND $1 > 0`,
			"a suite key without name":    `UPDATE test_runs SET suite_name = NULL WHERE suite_key IS NOT NULL AND $1 > 0`,
		} {
			_, err := db.Pool.Exec(ctx, sql, other.ID)
			assert.Error(t, err, name)
		}
	})
}
