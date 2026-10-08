//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/audit"
	auditpg "github.com/edcrove/provenly/backend/internal/audit/postgres"
	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/execution"
	executionpg "github.com/edcrove/provenly/backend/internal/execution/postgres"
	"github.com/edcrove/provenly/backend/internal/identity"
	identitypg "github.com/edcrove/provenly/backend/internal/identity/postgres"
	"github.com/edcrove/provenly/backend/internal/integrations"
	integrationspg "github.com/edcrove/provenly/backend/internal/integrations/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
)

// TestPersistenceFailures covers the error paths of the persistence adapters
// (and the sqlc queries under them) that the Unit gate excepts: every
// repository method must propagate a database failure instead of hiding it.
func TestPersistenceFailures(t *testing.T) {
	t.Run("BE-INT-020_persistence_adapters_propagate_database_failures", func(t *testing.T) {
		ctx := context.Background()
		pool, err := postgres.Open(ctx, db.URL)
		require.NoError(t, err)
		pool.Close()
		cat := catalogpg.NewStore(pool)
		exe := executionpg.NewStore(pool)
		idn := identitypg.NewStore(pool)
		str := func(s string) *string { return &s }
		status := catalog.StatusActive

		calls := map[string]func() error{
			"catalog.InTx":               func() error { return cat.InTx(ctx, func(catalog.Repository) error { return nil }) },
			"catalog.CreateTestCase":     func() error { _, err := cat.CreateTestCase(ctx, catalog.CreateInput{Title: "x"}); return err },
			"catalog.GetTestCase":        func() error { _, err := cat.GetTestCase(ctx, 1); return err },
			"catalog.LockTestCase":       func() error { _, err := cat.LockTestCase(ctx, 1); return err },
			"catalog.ListTestCases":      func() error { _, err := cat.ListTestCases(ctx, catalog.ListFilter{Status: &status}, 10, 0); return err },
			"catalog.CountTestCases":     func() error { _, err := cat.CountTestCases(ctx, catalog.ListFilter{}); return err },
			"catalog.UpdateTestCase":     func() error { _, err := cat.UpdateTestCase(ctx, 1, catalog.UpdateInput{Title: str("x")}); return err },
			"catalog.DeprecateTestCase":  func() error { _, err := cat.DeprecateTestCase(ctx, 1); return err },
			"catalog.ReactivateTestCase": func() error { _, err := cat.ReactivateTestCase(ctx, 1); return err },
			"catalog.ListIngestionView":  func() error { _, err := cat.ListIngestionView(ctx, 1, []int64{1}); return err },
			"catalog.ListTestSteps":      func() error { _, err := cat.ListTestSteps(ctx, 1, 10, 0); return err },
			"catalog.ListAllTestSteps":   func() error { _, err := cat.ListAllTestSteps(ctx, 1); return err },
			"catalog.CountTestSteps":     func() error { _, err := cat.CountTestSteps(ctx, 1); return err },
			"catalog.ShiftTestStepsDown": func() error { return cat.ShiftTestStepsDown(ctx, 1, 1) },
			"catalog.CreateTestStep":     func() error { _, err := cat.CreateTestStep(ctx, 1, 1, "a", ""); return err },
			"catalog.UpdateTestStep": func() error {
				_, err := cat.UpdateTestStep(ctx, 1, 1, catalog.UpdateStepInput{Action: str("a")})
				return err
			},
			"catalog.DeleteTestStep":      func() error { _, err := cat.DeleteTestStep(ctx, 1, 1); return err },
			"catalog.CloseTestStepGap":    func() error { return cat.CloseTestStepGap(ctx, 1, 1) },
			"catalog.SetTestStepPosition": func() error { return cat.SetTestStepPosition(ctx, 1, 1, 1) },
			"catalog.ListTestCaseKeys":    func() error { _, err := cat.ListTestCaseKeys(ctx, []int64{1}); return err },
			"catalog.ListDimensions":      func() error { _, err := cat.ListDimensions(ctx, 1); return err },
			"catalog.CreateDimension": func() error {
				_, err := cat.CreateDimension(ctx, 1, catalog.DimensionInput{Key: "x", Name: "x"})
				return err
			},
			"catalog.UpdateDimension": func() error {
				_, err := cat.UpdateDimension(ctx, 1, "risk", catalog.UpdateDimensionInput{Name: str("x")})
				return err
			},
			"catalog.CreateDimensionValue": func() error {
				_, err := cat.CreateDimensionValue(ctx, 1, catalog.DimensionInput{Key: "x", Name: "x"})
				return err
			},
			"catalog.UpdateDimensionValue": func() error {
				_, err := cat.UpdateDimensionValue(ctx, 1, "x", catalog.UpdateDimensionInput{Name: str("x")})
				return err
			},
			"catalog.SetTags":         func() error { return cat.SetTags(ctx, 1, []string{"a"}) },
			"catalog.ListTestCaseIDs": func() error { _, err := cat.ListTestCaseIDs(ctx, catalog.ListFilter{}); return err },
			"catalog.ListSuites":      func() error { _, err := cat.ListSuites(ctx, 1); return err },
			"catalog.GetSuite":        func() error { _, err := cat.GetSuite(ctx, 1, "s"); return err },
			"catalog.CreateSuite": func() error {
				_, err := cat.CreateSuite(ctx, 1, catalog.SuiteInput{Key: "s", Name: "s", Kind: catalog.SuiteKindStatic})
				return err
			},
			"catalog.UpdateSuite": func() error {
				_, err := cat.UpdateSuite(ctx, 1, "s", catalog.UpdateSuiteInput{Name: str("x")})
				return err
			},
			"catalog.SetSuiteCases":    func() error { return cat.SetSuiteCases(ctx, 1, 1, []int64{1}) },
			"catalog.ProjectCaseIDs":   func() error { _, err := cat.ProjectCaseIDs(ctx, 1, []int64{1}); return err },
			"catalog.ListRequirements": func() error { _, err := cat.ListRequirements(ctx, 1, nil); return err },
			"catalog.GetRequirement":   func() error { _, err := cat.GetRequirement(ctx, 1, 1); return err },
			"catalog.NextNativeRequirementNumber": func() error {
				_, err := cat.NextNativeRequirementNumber(ctx, 1)
				return err
			},
			"catalog.UpsertRequirement": func() error {
				_, _, _, err := cat.UpsertRequirement(ctx, 1, catalog.RequirementInput{Provider: "jira", ExternalID: "X-1", Title: "x"}, true, nil)
				return err
			},
			"catalog.UpdateRequirement": func() error {
				return cat.UpdateRequirement(ctx, 1, 1, catalog.UpdateRequirementInput{Title: str("x")})
			},
			"catalog.SetRequirementTestCases": func() error { return cat.SetRequirementTestCases(ctx, 1, 1, []int64{1}) },
			"execution.ListLatestResults":     func() error { _, err := exe.ListLatestResults(ctx, []int64{1}); return err },
			"execution.ListLatestConclusive":  func() error { _, err := exe.ListLatestConclusive(ctx, []int64{1}); return err },
			"execution.CountRunEvents":        func() error { _, err := exe.CountRunEvents(ctx, 1); return err },
			"execution.InsertRunEvent":        func() error { _, err := exe.InsertRunEvent(ctx, 1, execution.NewEvent{}); return err },
			"execution.GetRunProject":         func() error { _, err := exe.GetRunProject(ctx, 1); return err },
			"execution.ListRunEvents":         func() error { _, err := exe.ListRunEvents(ctx, 1); return err },
			"execution.CompleteLiveRun":       func() error { return exe.CompleteLiveRun(ctx, 1, execution.RunCompleted, "x") },
			"execution.InsertRunShard":        func() error { _, err := exe.InsertRunShard(ctx, 1, execution.RunShard{Shard: 1}); return err },
			"execution.ListRunShards":         func() error { _, err := exe.ListRunShards(ctx, 1); return err },
			"execution.FinishShardedRun":      func() error { return exe.FinishShardedRun(ctx, 1, execution.RunInterrupted) },
			"execution.ListLastExecuted":      func() error { _, err := exe.ListLastExecuted(ctx, []int64{1}); return err },
			"execution.ListFlakyCounts":       func() error { _, err := exe.ListFlakyCounts(ctx, 1, 20, 20); return err },
			"catalog.ListIssues":              func() error { _, err := cat.ListIssues(ctx, 1, catalog.IssueFilter{}); return err },
			"catalog.GetIssue":                func() error { _, err := cat.GetIssue(ctx, 1, 1); return err },
			"catalog.NextNativeIssueNumber":   func() error { _, err := cat.NextNativeIssueNumber(ctx, 1); return err },
			"catalog.UpsertIssue": func() error {
				_, _, _, err := cat.UpsertIssue(ctx, 1, catalog.IssueInput{Provider: "jira", ExternalID: "X-1", Title: "x", State: "open"}, true, nil)
				return err
			},
			"catalog.UpdateIssue":         func() error { return cat.UpdateIssue(ctx, 1, 1, catalog.UpdateIssueInput{Title: str("x")}) },
			"catalog.SetIssueTestCases":   func() error { return cat.SetIssueTestCases(ctx, 1, 1, []int64{1}) },
			"catalog.SetClassification":   func() error { return cat.SetClassification(ctx, 1, 1, 1, 1) },
			"catalog.ClearClassification": func() error { return cat.SetClassification(ctx, 1, 1, 1, 0) },
			"catalog.CreateProject": func() error {
				_, err := cat.CreateProject(ctx, catalog.CreateProjectInput{Key: "XX", Name: "x"})
				return err
			},
			"catalog.GetProject":      func() error { _, err := cat.GetProject(ctx, 1); return err },
			"catalog.GetProjectByKey": func() error { _, err := cat.GetProjectByKey(ctx, "XX"); return err },
			"catalog.ListProjects":    func() error { _, err := cat.ListProjects(ctx, nil, 10, 0); return err },
			"catalog.CountProjects":   func() error { _, err := cat.CountProjects(ctx, nil); return err },
			"catalog.UpdateProject": func() error {
				_, err := cat.UpdateProject(ctx, "XX", catalog.UpdateProjectInput{Name: str("x")})
				return err
			},

			"identity.InTx":                func() error { return idn.InTx(ctx, func(identity.Repository) error { return nil }) },
			"identity.CountUsers":          func() error { _, err := idn.CountUsers(ctx); return err },
			"identity.CreateUser":          func() error { _, err := idn.CreateUser(ctx, identity.NewUser{}); return err },
			"identity.GetUser":             func() error { _, err := idn.GetUser(ctx, 1); return err },
			"identity.GetUserByUsername":   func() error { _, err := idn.GetUserByUsername(ctx, "x"); return err },
			"identity.ListUsers":           func() error { _, err := idn.ListUsers(ctx, 10, 0); return err },
			"identity.SetUserDeactivated":  func() error { _, err := idn.SetUserDeactivated(ctx, 1, nil); return err },
			"identity.CountActiveAdmins":   func() error { _, err := idn.CountActiveAdmins(ctx); return err },
			"identity.VoidPasswordResets":  func() error { return idn.VoidPasswordResets(ctx, 1) },
			"identity.CreatePasswordReset": func() error { _, err := idn.CreatePasswordReset(ctx, identity.PasswordReset{}); return err },
			"identity.LockPasswordResetByToken": func() error {
				_, err := idn.LockPasswordResetByToken(ctx, make([]byte, 32))
				return err
			},
			"identity.MarkPasswordResetUsed":  func() error { return idn.MarkPasswordResetUsed(ctx, 1) },
			"identity.SetPasswordHash":        func() error { _, err := idn.SetPasswordHash(ctx, 1, "$2a$x"); return err },
			"identity.CreateInvitation":       func() error { _, err := idn.CreateInvitation(ctx, identity.NewInvitation{}); return err },
			"identity.ListInvitations":        func() error { _, err := idn.ListInvitations(ctx, 10, 0); return err },
			"identity.CountInvitations":       func() error { _, err := idn.CountInvitations(ctx); return err },
			"identity.GetInvitation":          func() error { _, err := idn.GetInvitation(ctx, 1); return err },
			"identity.LockInvitationByToken":  func() error { _, err := idn.LockInvitationByToken(ctx, []byte("x")); return err },
			"identity.MarkInvitationAccepted": func() error { return idn.MarkInvitationAccepted(ctx, 1, 1) },
			"identity.RevokeInvitation":       func() error { _, err := idn.RevokeInvitation(ctx, 1); return err },
			"identity.MemberRole":             func() error { _, err := idn.MemberRole(ctx, 1, 1); return err },
			"identity.ListUserMemberships":    func() error { _, err := idn.ListUserMemberships(ctx, 1); return err },
			"identity.ListProjectMembers":     func() error { _, err := idn.ListProjectMembers(ctx, 1, 10, 0); return err },
			"identity.CountProjectMembers":    func() error { _, err := idn.CountProjectMembers(ctx, 1); return err },
			"identity.UpsertMember":           func() error { return idn.UpsertMember(ctx, 1, 1, authz.RoleViewer) },
			"identity.DeleteMember":           func() error { _, err := idn.DeleteMember(ctx, 1, 1); return err },
			"identity.CreateAPIKey":           func() error { _, err := idn.CreateAPIKey(ctx, identity.NewAPIKey{}); return err },
			"identity.ListAPIKeys":            func() error { _, err := idn.ListAPIKeys(ctx, 1, 10, 0); return err },
			"identity.CountAPIKeys":           func() error { _, err := idn.CountAPIKeys(ctx, 1); return err },
			"identity.GetAPIKey":              func() error { _, err := idn.GetAPIKey(ctx, 1, 1); return err },
			"identity.GetAPIKeyByToken":       func() error { _, err := idn.GetAPIKeyByToken(ctx, []byte("x")); return err },
			"identity.RevokeAPIKey":           func() error { _, err := idn.RevokeAPIKey(ctx, 1, 1); return err },
			"identity.TouchAPIKey":            func() error { return idn.TouchAPIKey(ctx, 1) },
			"identity.CreateToken":            func() error { _, err := idn.CreateToken(ctx, identity.NewPersonalAccessToken{}); return err },
			"identity.ListTokens":             func() error { _, err := idn.ListTokens(ctx, 1, 10, 0); return err },
			"identity.CountTokens":            func() error { _, err := idn.CountTokens(ctx, 1); return err },
			"identity.GetToken":               func() error { _, err := idn.GetToken(ctx, 1, 1); return err },
			"identity.GetTokenByDigest":       func() error { _, err := idn.GetTokenByDigest(ctx, []byte("x")); return err },
			"identity.RevokeToken":            func() error { _, err := idn.RevokeToken(ctx, 1, 1); return err },
			"identity.RevokeUserTokens":       func() error { return idn.RevokeUserTokens(ctx, 1) },
			"identity.TouchToken":             func() error { return idn.TouchToken(ctx, 1) },

			"execution.InTx":                     func() error { return exe.InTx(ctx, func(execution.Repository) error { return nil }) },
			"execution.InsertTestRun":            func() error { _, _, err := exe.InsertTestRun(ctx, execution.InsertRunParams{}); return err },
			"execution.GetTestRunIDByExternalID": func() error { _, err := exe.GetTestRunIDByExternalID(ctx, 1, "x"); return err },
			"execution.InsertExpectedCases":      func() error { return exe.InsertExpectedCases(ctx, 1, []int64{1}) },
			"execution.InsertTestResults":        func() error { return exe.InsertTestResults(ctx, 1, []execution.NewResult{{}}) },
			"execution.InsertParseErrors":        func() error { return exe.InsertParseErrors(ctx, 1, []execution.ParseError{{}}) },
			"execution.ListParseErrors":          func() error { _, err := exe.ListParseErrors(ctx, 1, 10, 0); return err },
			"execution.CountParseErrors":         func() error { _, err := exe.CountParseErrors(ctx, 1); return err },
			"execution.GetTestRun":               func() error { _, err := exe.GetTestRun(ctx, 1); return err },
			"execution.ListTestRuns":             func() error { _, err := exe.ListTestRuns(ctx, execution.RunFilter{}, 10, 0); return err },
			"execution.LockTestRun":              func() error { _, _, err := exe.LockTestRun(ctx, 1); return err },
			"execution.IsInUniverse":             func() error { _, err := exe.IsInUniverse(ctx, 1, 1); return err },
			"execution.FinishTestRun":            func() error { return exe.FinishTestRun(ctx, 1, execution.RunCompleted) },
			"execution.InsertManualResult": func() error {
				id, key := int64(1), "TC-1"
				_, err := exe.InsertManualResult(ctx, 1, execution.NewResult{TestCaseID: &id, RequestedTestCaseID: &key, TestName: key, Status: execution.Passed})
				return err
			},
			"execution.CountTestRuns":     func() error { _, err := exe.CountTestRuns(ctx, execution.RunFilter{}); return err },
			"execution.ListRunResults":    func() error { _, err := exe.ListRunResults(ctx, 1, execution.ResultFilter{}, 10, 0); return err },
			"execution.CountRunResults":   func() error { _, err := exe.CountRunResults(ctx, 1, execution.ResultFilter{}); return err },
			"execution.ListSummaryInputs": func() error { _, err := exe.ListSummaryInputs(ctx, []int64{1}); return err },
			"execution.ListDiagnostics":   func() error { _, err := exe.ListDiagnostics(ctx, 1); return err },
			"execution.InsertAmendment":   func() error { _, err := exe.InsertAmendment(ctx, execution.NewAmendment{}); return err },
			"execution.ListAmendments":    func() error { _, err := exe.ListAmendments(ctx, 1, 10, 0); return err },
			"execution.CountAmendments":   func() error { _, err := exe.CountAmendments(ctx, 1); return err },
			"execution.ListResultsForTestCase": func() error {
				_, err := exe.ListResultsForTestCase(ctx, 1, execution.HistoryFilter{}, 10, 0)
				return err
			},
			"execution.CountResultsForTestCase": func() error { _, err := exe.CountResultsForTestCase(ctx, 1, execution.HistoryFilter{}); return err },
		}
		aud := auditpg.NewStore(pool)
		calls["audit.Insert"] = func() error { return aud.Insert(ctx, audit.Event{}) }
		calls["audit.List"] = func() error { _, err := aud.List(ctx, audit.Filter{}, 10, 0); return err }
		calls["audit.Count"] = func() error { _, err := aud.Count(ctx, audit.Filter{}); return err }
		itg := integrationspg.NewStore(pool)
		for name, call := range map[string]func() error{
			"integrations.CreateWebhook":   func() error { _, err := itg.CreateWebhook(ctx, integrations.Webhook{}); return err },
			"integrations.ListWebhooks":    func() error { _, err := itg.ListWebhooks(ctx, 1); return err },
			"integrations.GetWebhook":      func() error { _, err := itg.GetWebhook(ctx, 1, 1); return err },
			"integrations.GetWebhookByID":  func() error { _, err := itg.GetWebhookByID(ctx, 1); return err },
			"integrations.UpdateWebhook":   func() error { return itg.UpdateWebhook(ctx, 1, 1, integrations.UpdateWebhookInput{}) },
			"integrations.ListSubscribed":  func() error { _, err := itg.ListSubscribedWebhooks(ctx, 1, "run.completed"); return err },
			"integrations.InsertDelivery":  func() error { _, err := itg.InsertDelivery(ctx, 1, "ping", []byte("{}")); return err },
			"integrations.ClaimDue":        func() error { _, err := itg.ClaimDueDeliveries(ctx, 1); return err },
			"integrations.FinishAttempt":   func() error { return itg.FinishAttempt(ctx, 1, integrations.Attempt{}) },
			"integrations.ListDeliveries":  func() error { _, err := itg.ListDeliveries(ctx, 1, 10, 0); return err },
			"integrations.CountDeliveries": func() error { _, err := itg.CountDeliveries(ctx, 1); return err },
			"integrations.LastDeliveries":  func() error { _, err := itg.LastDeliveries(ctx, []int64{1}); return err },
			"integrations.UpsertGitHub":    func() error { return itg.UpsertGitHubConnection(ctx, integrations.GitHubConnection{}) },
			"integrations.GetGitHub":       func() error { _, err := itg.GetGitHubConnection(ctx, 1); return err },
			"integrations.DeleteGitHub":    func() error { _, err := itg.DeleteGitHubConnection(ctx, 1); return err },
			"integrations.RecordSync":      func() error { return itg.RecordGitHubSync(ctx, 1, nil, "") },
		} {
			calls[name] = call
		}
		for name, call := range calls {
			err := call()
			assert.Error(t, err, name)
			assert.NotErrorIs(t, err, integrations.ErrNotFound, name)
			assert.NotErrorIs(t, err, catalog.ErrNotFound, name)
			assert.NotErrorIs(t, err, execution.ErrNotFound, name)
			assert.NotErrorIs(t, err, identity.ErrNotFound, name)
		}
		assert.Error(t, postgres.Migrate(ctx, pool, "up"), "migrations report a closed pool")
	})

	t.Run("BE-INT-035_project_key_lookup_failures_propagate", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		require.NoError(t, err)
		// A store with a cold project-key cache whose key lookup fails after the test case query succeeded.
		cold := catalogpg.NewStore(db.Pool)
		_, err = db.Pool.Exec(ctx, `ALTER TABLE projects RENAME TO projects_hidden`)
		require.NoError(t, err)
		defer func() {
			_, err := db.Pool.Exec(ctx, `ALTER TABLE projects_hidden RENAME TO projects`)
			require.NoError(t, err)
		}()
		_, err = cold.ListTestCases(ctx, catalog.ListFilter{}, 10, 0)
		assert.ErrorContains(t, err, "projects")
		_, err = cold.ListTestCaseKeys(ctx, []int64{tc.ID})
		assert.ErrorContains(t, err, "projects")
		_, err = cold.GetTestCase(ctx, tc.ID)
		assert.ErrorContains(t, err, "projects")
	})

	t.Run("BE-INT-046_taxonomy_read_and_write_failures_propagate", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		require.NoError(t, err)
		store := catalogpg.NewStore(db.Pool)
		// Each table disappears after the queries before it succeeded: the error surfaces instead of a partial result.
		hidden := func(table string, calls ...func() error) {
			_, err := db.Pool.Exec(ctx, `ALTER TABLE `+table+` RENAME TO `+table+`_hidden`)
			require.NoError(t, err)
			defer func() {
				_, err := db.Pool.Exec(ctx, `ALTER TABLE `+table+`_hidden RENAME TO `+table)
				require.NoError(t, err)
			}()
			for _, call := range calls {
				assert.ErrorContains(t, call(), table)
			}
		}
		get := func() error { _, err := store.GetTestCase(ctx, tc.ID); return err }
		list := func() error { _, err := store.ListTestCases(ctx, catalog.ListFilter{}, 10, 0); return err }
		dims := func() error { _, err := store.ListDimensions(ctx, catalog.DefaultProjectID); return err }
		hidden("test_case_tags", get, list, func() error { return store.SetTags(ctx, tc.ID, []string{"a"}) })
		hidden("test_case_classifications", get, list)
		hidden("classification_values", dims)
	})
}
