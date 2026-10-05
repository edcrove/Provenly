//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/execution"
	executionpg "github.com/edcrove/provenly/backend/internal/execution/postgres"
	"github.com/edcrove/provenly/backend/internal/identity"
	identitypg "github.com/edcrove/provenly/backend/internal/identity/postgres"
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
			"catalog.LockTestCase":       func() error { return cat.LockTestCase(ctx, 1) },
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
			"catalog.CreateProject": func() error {
				_, err := cat.CreateProject(ctx, catalog.CreateProjectInput{Key: "XX", Name: "x"})
				return err
			},
			"catalog.GetProject":      func() error { _, err := cat.GetProject(ctx, 1); return err },
			"catalog.GetProjectByKey": func() error { _, err := cat.GetProjectByKey(ctx, "XX"); return err },
			"catalog.ListProjects":    func() error { _, err := cat.ListProjects(ctx, 10, 0); return err },
			"catalog.CountProjects":   func() error { _, err := cat.CountProjects(ctx); return err },
			"catalog.UpdateProject": func() error {
				_, err := cat.UpdateProject(ctx, "XX", catalog.UpdateProjectInput{Name: str("x")})
				return err
			},

			"identity.InTx":                   func() error { return idn.InTx(ctx, func(identity.Repository) error { return nil }) },
			"identity.CountUsers":             func() error { _, err := idn.CountUsers(ctx); return err },
			"identity.CreateUser":             func() error { _, err := idn.CreateUser(ctx, identity.NewUser{}); return err },
			"identity.GetUser":                func() error { _, err := idn.GetUser(ctx, 1); return err },
			"identity.GetUserByUsername":      func() error { _, err := idn.GetUserByUsername(ctx, "x"); return err },
			"identity.ListUsers":              func() error { _, err := idn.ListUsers(ctx, 10, 0); return err },
			"identity.SetPasswordHash":        func() error { _, err := idn.SetPasswordHash(ctx, 1, "$2a$x"); return err },
			"identity.CreateInvitation":       func() error { _, err := idn.CreateInvitation(ctx, identity.NewInvitation{}); return err },
			"identity.ListInvitations":        func() error { _, err := idn.ListInvitations(ctx, 10, 0); return err },
			"identity.CountInvitations":       func() error { _, err := idn.CountInvitations(ctx); return err },
			"identity.GetInvitation":          func() error { _, err := idn.GetInvitation(ctx, 1); return err },
			"identity.LockInvitationByToken":  func() error { _, err := idn.LockInvitationByToken(ctx, []byte("x")); return err },
			"identity.MarkInvitationAccepted": func() error { return idn.MarkInvitationAccepted(ctx, 1, 1) },
			"identity.RevokeInvitation":       func() error { _, err := idn.RevokeInvitation(ctx, 1); return err },

			"execution.InTx":                     func() error { return exe.InTx(ctx, func(execution.Repository) error { return nil }) },
			"execution.InsertTestRun":            func() error { _, _, err := exe.InsertTestRun(ctx, execution.InsertRunParams{}); return err },
			"execution.GetTestRunIDByExternalID": func() error { _, err := exe.GetTestRunIDByExternalID(ctx, 1, "x"); return err },
			"execution.InsertExpectedCases":      func() error { return exe.InsertExpectedCases(ctx, 1, []int64{1}) },
			"execution.InsertTestResults":        func() error { return exe.InsertTestResults(ctx, 1, []execution.NewResult{{}}) },
			"execution.InsertParseErrors":        func() error { return exe.InsertParseErrors(ctx, 1, []execution.ParseError{{}}) },
			"execution.ListParseErrors":          func() error { _, err := exe.ListParseErrors(ctx, 1, 10, 0); return err },
			"execution.CountParseErrors":         func() error { _, err := exe.CountParseErrors(ctx, 1); return err },
			"execution.GetTestRun":               func() error { _, err := exe.GetTestRun(ctx, 1); return err },
			"execution.ListTestRuns":             func() error { _, err := exe.ListTestRuns(ctx, nil, 10, 0); return err },
			"execution.CountTestRuns":            func() error { _, err := exe.CountTestRuns(ctx, nil); return err },
			"execution.ListRunResults":           func() error { _, err := exe.ListRunResults(ctx, 1, execution.ResultFilter{}, 10, 0); return err },
			"execution.CountRunResults":          func() error { _, err := exe.CountRunResults(ctx, 1, execution.ResultFilter{}); return err },
			"execution.ListSummaryInputs":        func() error { _, err := exe.ListSummaryInputs(ctx, []int64{1}); return err },
			"execution.ListDiagnostics":          func() error { _, err := exe.ListDiagnostics(ctx, 1); return err },
			"execution.ListResultsForTestCase":   func() error { _, err := exe.ListResultsForTestCase(ctx, 1, 10, 0); return err },
			"execution.CountResultsForTestCase":  func() error { _, err := exe.CountResultsForTestCase(ctx, 1); return err },
		}
		for name, call := range calls {
			err := call()
			assert.Error(t, err, name)
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
	})
}
