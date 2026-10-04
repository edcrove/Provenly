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
		str := func(s string) *string { return &s }
		status := catalog.StatusActive

		calls := map[string]func() error{
			"catalog.InTx":               func() error { return cat.InTx(ctx, func(catalog.Repository) error { return nil }) },
			"catalog.CreateTestCase":     func() error { _, err := cat.CreateTestCase(ctx, catalog.CreateInput{Title: "x"}); return err },
			"catalog.GetTestCase":        func() error { _, err := cat.GetTestCase(ctx, 1); return err },
			"catalog.LockTestCase":       func() error { return cat.LockTestCase(ctx, 1) },
			"catalog.ListTestCases":      func() error { _, err := cat.ListTestCases(ctx, &status, 10, 0); return err },
			"catalog.CountTestCases":     func() error { _, err := cat.CountTestCases(ctx, nil); return err },
			"catalog.UpdateTestCase":     func() error { _, err := cat.UpdateTestCase(ctx, 1, catalog.UpdateInput{Title: str("x")}); return err },
			"catalog.DeprecateTestCase":  func() error { _, err := cat.DeprecateTestCase(ctx, 1); return err },
			"catalog.ReactivateTestCase": func() error { _, err := cat.ReactivateTestCase(ctx, 1); return err },
			"catalog.ListIngestionView":  func() error { _, err := cat.ListIngestionView(ctx, []int64{1}); return err },
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

			"execution.InTx":                     func() error { return exe.InTx(ctx, func(execution.Repository) error { return nil }) },
			"execution.InsertTestRun":            func() error { _, _, err := exe.InsertTestRun(ctx, execution.InsertRunParams{}); return err },
			"execution.GetTestRunIDByExternalID": func() error { _, err := exe.GetTestRunIDByExternalID(ctx, "x"); return err },
			"execution.InsertExpectedCases":      func() error { return exe.InsertExpectedCases(ctx, 1, []int64{1}) },
			"execution.InsertTestResults":        func() error { return exe.InsertTestResults(ctx, 1, []execution.NewResult{{}}) },
			"execution.InsertParseErrors":        func() error { return exe.InsertParseErrors(ctx, 1, []execution.ParseError{{}}) },
			"execution.ListParseErrors":          func() error { _, err := exe.ListParseErrors(ctx, 1, 10, 0); return err },
			"execution.CountParseErrors":         func() error { _, err := exe.CountParseErrors(ctx, 1); return err },
			"execution.GetTestRun":               func() error { _, err := exe.GetTestRun(ctx, 1); return err },
			"execution.ListTestRuns":             func() error { _, err := exe.ListTestRuns(ctx, 10, 0); return err },
			"execution.CountTestRuns":            func() error { _, err := exe.CountTestRuns(ctx); return err },
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
		}
		assert.Error(t, postgres.Migrate(ctx, pool, "up"), "migrations report a closed pool")
	})
}
