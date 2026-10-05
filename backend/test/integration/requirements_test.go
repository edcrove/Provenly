//go:build integration

package integration

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func TestRequirements(t *testing.T) {
	t.Run("BE-INT-050_concurrent_native_requirements_get_distinct_numbers", func(t *testing.T) {
		s, ctx := fresh(t)
		const n = 20
		var wg sync.WaitGroup
		ids := make(chan string, n)
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Title: "race"})
				assert.NoError(t, err)
				ids <- r.ExternalID
			}()
		}
		wg.Wait()
		close(ids)
		var got []string
		for id := range ids {
			got = append(got, id)
		}
		want := make([]string, n)
		for i := range want {
			want[i] = "R-" + strconv.Itoa(i+1)
		}
		slices.Sort(got)
		slices.Sort(want)
		assert.Equal(t, want, got, "every creation gets its own number, none skipped")
	})

	t.Run("BE-INT-049_requirements_mirror_sources_and_read_coverage_from_latest_results", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		login, pay, refund := create("login"), create("pay"), create("refund")

		// Native requirements are numbered per project; external ones keep their id and conflict once.
		r1, err := s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Title: "Sign in"})
		require.NoError(t, err)
		assert.Equal(t, "R-1", r1.ExternalID)
		r2, err := s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Title: "Pay"})
		require.NoError(t, err)
		assert.Equal(t, "R-2", r2.ExternalID)
		jira, err := s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Provider: catalog.ProviderJira, ExternalID: "PAY-12", Title: "Refunds"})
		require.NoError(t, err)
		_, err = s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Provider: catalog.ProviderJira, ExternalID: "PAY-12", Title: "again"})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		other, err := s.Catalog.CreateRequirement(ctx, chk.ID, catalog.RequirementInput{Title: "Elsewhere"})
		require.NoError(t, err)
		assert.Equal(t, "R-1", other.ExternalID, "numbers are per project")

		// Import mirrors by external id: creates, updates, keeps links and archiving.
		_, err = s.Catalog.SetRequirementTestCases(ctx, catalog.DefaultProjectID, jira.ID, []int64{refund.ID})
		require.NoError(t, err)
		res, err := s.Catalog.ImportRequirements(ctx, catalog.DefaultProjectID, catalog.ProviderJira, []catalog.RequirementInput{
			{ExternalID: "PAY-12", Title: "Refunds v2", ProviderStatus: "Done"}, {ExternalID: "PAY-13", Title: "Chargebacks"},
		})
		require.NoError(t, err)
		assert.Equal(t, catalog.ImportResult{Created: 1, Updated: 1}, res)
		jira, err = s.Catalog.Requirement(ctx, catalog.DefaultProjectID, jira.ID)
		require.NoError(t, err)
		assert.Equal(t, "Refunds v2", jira.Title)
		assert.Equal(t, "Done", jira.ProviderStatus)
		assert.NotNil(t, jira.LastSyncedAt)
		assert.Equal(t, []int64{refund.ID}, jira.TestCaseIDs, "an import keeps the links")

		// Coverage from the latest result of each covering test case (its logical status in its latest run).
		_, err = s.Catalog.SetRequirementTestCases(ctx, catalog.DefaultProjectID, r1.ID, []int64{login.ID, pay.ID})
		require.NoError(t, err)
		r1, _ = s.Catalog.Requirement(ctx, catalog.DefaultProjectID, r1.ID)
		assert.Equal(t, catalog.CoverageNotRun, r1.Coverage.Status)
		ingestCases := func(runID string, cases ...string) {
			_, err := s.Ingestion.IngestJUnit(ctx, meta(runID, 1), strings.NewReader(junitFor(cases...)))
			require.NoError(t, err)
		}
		ingestCases("1", tcProp("login", login.Key(), `<failure message="x"/>`), tcProp("pay", pay.Key(), ""))
		r1, _ = s.Catalog.Requirement(ctx, catalog.DefaultProjectID, r1.ID)
		assert.Equal(t, catalog.CoverageFailing, r1.Coverage.Status)
		assert.Equal(t, map[int64]string{login.ID: "failed", pay.ID: "passed"}, r1.Coverage.Latest)
		ingestCases("2", tcProp("login", login.Key(), `<properties><property name="attempt" value="1"/></properties><failure message="x"/>`),
			`<testcase name="login" classname="c"><properties><property name="tc-id" value="`+login.Key()+`"/><property name="attempt" value="2"/></properties></testcase>`)
		r1, _ = s.Catalog.Requirement(ctx, catalog.DefaultProjectID, r1.ID)
		assert.Equal(t, catalog.CoveragePassing, r1.Coverage.Status, "login passed on retry in run 2; pay passed in run 1, its latest")
		ingestCases("3", tcProp("pay chrome", pay.Key(), ""), tcProp("pay firefox", pay.Key(), `<skipped/>`))
		r1, _ = s.Catalog.Requirement(ctx, catalog.DefaultProjectID, r1.ID)
		assert.Equal(t, catalog.CoveragePartial, r1.Coverage.Status, "pay skipped in a variant of its latest run")

		// Lists: newest first, narrowed to what a test case covers.
		all, err := s.Catalog.Requirements(ctx, catalog.DefaultProjectID, nil)
		require.NoError(t, err)
		assert.Len(t, all, 4)
		assert.Equal(t, "PAY-13", all[0].ExternalID)
		covering, err := s.Catalog.Requirements(ctx, catalog.DefaultProjectID, &login.ID)
		require.NoError(t, err)
		require.Len(t, covering, 1)
		assert.Equal(t, r1.ID, covering[0].ID)

		// Updates, archiving and refusals.
		r2, err = s.Catalog.UpdateRequirement(ctx, catalog.DefaultProjectID, r2.ID, catalog.UpdateRequirementInput{Title: ptr("Pay by card"), Description: ptr("d"),
			URL: ptr("https://x.test"), ProviderStatus: ptr("Open"), Archived: ptr(true)})
		require.NoError(t, err)
		assert.Equal(t, "Pay by card", r2.Title)
		assert.NotNil(t, r2.ArchivedAt)
		_, err = s.Catalog.UpdateRequirement(ctx, catalog.DefaultProjectID, 987654, catalog.UpdateRequirementInput{Title: ptr("x")})
		assert.Equal(t, apperr.KindNotFound, kind(t, err))
		otherTC, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "other"})
		require.NoError(t, err)
		_, err = s.Catalog.SetRequirementTestCases(ctx, catalog.DefaultProjectID, r1.ID, []int64{otherTC.ID})
		assert.Equal(t, apperr.KindValidation, kind(t, err))
		_, err = s.Catalog.Requirement(ctx, chk.ID, r1.ID)
		assert.Equal(t, apperr.KindNotFound, kind(t, err), "another project's requirement is not found")

		// The database refuses what the service never does.
		id := strconv.FormatInt(r1.ID, 10)
		for name, sql := range map[string]string{
			"another project's test case": `INSERT INTO requirement_test_cases VALUES (` + id + `, 1, $1)`,
			"deleting a requirement":      `DELETE FROM requirements WHERE id = ` + id + ` AND $1 > 0`,
			"changing the external id":    `UPDATE requirements SET external_id = 'R-99' WHERE id = ` + id + ` AND $1 > 0`,
			"changing the provider":       `UPDATE requirements SET provider = 'jira' WHERE id = ` + id + ` AND $1 > 0`,
			"a synced native requirement": `UPDATE requirements SET last_synced_at = now() WHERE id = ` + id + ` AND $1 > 0`,
			"an unknown provider":         `INSERT INTO requirements (project_id, provider, external_id, title) VALUES (1, 'trello', 'x', 'x') RETURNING $1::bigint`,
			"a non-http url":              `UPDATE requirements SET url = 'javascript:alert(1)' WHERE id = ` + id + ` AND $1 > 0`,
			"a duplicate external id":     `INSERT INTO requirements (project_id, provider, external_id, title) VALUES (1, 'jira', 'PAY-12', 'x') RETURNING $1::bigint`,
		} {
			_, err := db.Pool.Exec(ctx, sql, otherTC.ID)
			assert.Error(t, err, name)
		}
	})
}
