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

func TestIssues(t *testing.T) {
	t.Run("BE-INT-051_issue_verification_follows_state_and_latest_conclusive_results", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		login, pay := create("login"), create("pay")
		ingestCases := func(runID string, cases ...string) {
			_, err := s.Ingestion.IngestJUnit(ctx, meta(runID, 1), strings.NewReader(junitFor(cases...)))
			require.NoError(t, err)
		}

		// Native issues are numbered per project, concurrently without gaps; external ones keep their id.
		const n = 10
		var wg sync.WaitGroup
		ids := make(chan string, n)
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				is, err := s.Catalog.CreateIssue(ctx, catalog.DefaultProjectID, catalog.IssueInput{Title: "race"})
				assert.NoError(t, err)
				ids <- is.ExternalID
			}()
		}
		wg.Wait()
		close(ids)
		var got, want []string
		for id := range ids {
			got = append(got, id)
		}
		for i := range n {
			want = append(want, "I-"+strconv.Itoa(i+1))
		}
		slices.Sort(got)
		slices.Sort(want)
		assert.Equal(t, want, got)

		jira, err := s.Catalog.CreateIssue(ctx, catalog.DefaultProjectID, catalog.IssueInput{Provider: catalog.ProviderJira, ExternalID: "PAY-7", Title: "Login fails"})
		require.NoError(t, err)
		_, err = s.Catalog.CreateIssue(ctx, catalog.DefaultProjectID, catalog.IssueInput{Provider: catalog.ProviderJira, ExternalID: "PAY-7", Title: "again"})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		jira, err = s.Catalog.SetIssueTestCases(ctx, catalog.DefaultProjectID, jira.ID, []int64{login.ID, pay.ID})
		require.NoError(t, err)
		assert.Equal(t, catalog.VerificationUnverified, jira.Verification.Status)

		// Open + fail = known issue; the evidence is the run where it failed.
		ingestCases("1", tcProp("login", login.Key(), `<failure message="x"/>`), tcProp("pay", pay.Key(), ""))
		jira, _ = s.Catalog.Issue(ctx, catalog.DefaultProjectID, jira.ID)
		assert.Equal(t, catalog.VerificationKnownIssue, jira.Verification.Status)
		assert.Equal(t, catalog.VerificationNotReproducible, jira.Verification.Links[1].Status)
		failedRun := *jira.Verification.Links[0].EvidenceRunID

		// A skipped run is inconclusive: the previous evidence stands.
		ingestCases("2", tcProp("login", login.Key(), `<skipped/>`))
		jira, _ = s.Catalog.Issue(ctx, catalog.DefaultProjectID, jira.ID)
		assert.Equal(t, catalog.VerificationKnownIssue, jira.Verification.Status)
		assert.Equal(t, failedRun, *jira.Verification.Links[0].EvidenceRunID)
		assert.True(t, jira.Verification.Links[0].LatestInconclusive)

		// The tracker closes it (import) and the fix passes on a retry: validated fixed.
		res, err := s.Catalog.ImportIssues(ctx, catalog.DefaultProjectID, catalog.ProviderJira, []catalog.IssueInput{{ExternalID: "PAY-7", Title: "Login fails", State: catalog.IssueClosed, ProviderStatus: "Done"}})
		require.NoError(t, err)
		assert.Equal(t, catalog.ImportResult{Updated: 1}, res)
		jira, _ = s.Catalog.Issue(ctx, catalog.DefaultProjectID, jira.ID)
		assert.Equal(t, catalog.VerificationReopen, jira.Verification.Status, "closed but its latest conclusive evidence failed")
		assert.NotNil(t, jira.ClosedAt)
		ingestCases("3", tcProp("login", login.Key(), `<properties><property name="attempt" value="1"/></properties><failure message="x"/>`),
			`<testcase name="login" classname="c"><properties><property name="tc-id" value="`+login.Key()+`"/><property name="attempt" value="2"/></properties></testcase>`)
		jira, _ = s.Catalog.Issue(ctx, catalog.DefaultProjectID, jira.ID)
		assert.Equal(t, catalog.VerificationValidatedFixed, jira.Verification.Status)
		assert.False(t, jira.Verification.Links[0].LatestInconclusive)

		// A variant failing in a later run reopens it.
		ingestCases("4", tcProp("pay chrome", pay.Key(), ""), tcProp("pay firefox", pay.Key(), `<failure message="x"/>`))
		jira, _ = s.Catalog.Issue(ctx, catalog.DefaultProjectID, jira.ID)
		assert.Equal(t, catalog.VerificationReopen, jira.Verification.Status)

		// Reopening in Provenly clears the closing time; lists filter by state and test case.
		jira, err = s.Catalog.UpdateIssue(ctx, catalog.DefaultProjectID, jira.ID, catalog.UpdateIssueInput{State: ptr(catalog.IssueOpen), Description: ptr("d"),
			URL: ptr("https://x.test"), ProviderStatus: ptr("Reopened"), Title: ptr("Login fails again")})
		require.NoError(t, err)
		assert.Nil(t, jira.ClosedAt)
		assert.Equal(t, catalog.VerificationKnownIssue, jira.Verification.Status)
		closed := catalog.IssueClosed
		none, err := s.Catalog.Issues(ctx, catalog.DefaultProjectID, catalog.IssueFilter{State: &closed})
		require.NoError(t, err)
		assert.Empty(t, none)
		linked, err := s.Catalog.Issues(ctx, catalog.DefaultProjectID, catalog.IssueFilter{TestCaseID: &pay.ID})
		require.NoError(t, err)
		require.Len(t, linked, 1)
		assert.Equal(t, jira.ID, linked[0].ID)
		_, err = s.Catalog.UpdateIssue(ctx, catalog.DefaultProjectID, 987654, catalog.UpdateIssueInput{Title: ptr("x")})
		assert.Equal(t, apperr.KindNotFound, kind(t, err))

		// The database refuses what the service never does.
		id := strconv.FormatInt(jira.ID, 10)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		otherTC, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "other"})
		require.NoError(t, err)
		for name, sql := range map[string]string{
			"another project's test case": `INSERT INTO issue_test_cases VALUES (` + id + `, 1, $1)`,
			"deleting an issue":           `DELETE FROM issues WHERE id = ` + id + ` AND $1 > 0`,
			"changing the external id":    `UPDATE issues SET external_id = 'I-99' WHERE id = ` + id + ` AND $1 > 0`,
			"closed without a time":       `UPDATE issues SET state = 'closed', closed_at = NULL WHERE id = ` + id + ` AND $1 > 0`,
			"an unknown state":            `UPDATE issues SET state = 'done' WHERE id = ` + id + ` AND $1 > 0`,
			"a synced native issue":       `UPDATE issues SET last_synced_at = now() WHERE provider = 'provenly' AND $1 > 0`,
		} {
			_, err := db.Pool.Exec(ctx, sql, otherTC.ID)
			assert.Error(t, err, name)
		}
	})
}
