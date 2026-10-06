//go:build integration

package integration

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestCatalogPersistence(t *testing.T) {
	t.Run("BE-INT-002_tc_ids_are_assigned_by_the_database_and_never_reused", func(t *testing.T) {
		s, ctx := fresh(t)
		a, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		require.NoError(t, err)
		b, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "b"})
		require.NoError(t, err)
		assert.Greater(t, b.ID, a.ID)
		_, err = s.Catalog.Deprecate(ctx, b.ID, etag.Match{})
		require.NoError(t, err)
		c, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "c"})
		require.NoError(t, err)
		assert.Greater(t, c.ID, b.ID, "a deprecated id is never reused")

		// A failed insert consumes an identity value but never produces a duplicate.
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_cases (project_id, number, title) VALUES (1, 900, '')`)
		require.Error(t, err)
		d, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "d"})
		require.NoError(t, err)
		assert.Greater(t, d.ID, c.ID)

		_, err = db.Pool.Exec(ctx, `INSERT INTO test_cases (id, project_id, number, title) VALUES (999, 1, 900, 'forced')`)
		require.Error(t, err, "clients cannot choose a TC-ID")
	})

	t.Run("BE-INT-065_a_test_case_is_found_by_its_project_and_number", func(t *testing.T) {
		s, ctx := fresh(t)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		for _, in := range []catalog.CreateInput{{ProjectID: catalog.DefaultProjectID, Title: "tc"}, {ProjectID: chk.ID, Title: "pay"}, {ProjectID: chk.ID, Title: "refund"}} {
			_, err := s.Catalog.Create(ctx, in)
			require.NoError(t, err)
		}
		find := func(projects []int64, number int64) []string {
			res, err := s.Catalog.List(ctx, catalog.ListFilter{ProjectIDs: projects, Number: &number}, pagination.Default())
			require.NoError(t, err)
			keys := []string{}
			for _, tc := range res.Items {
				keys = append(keys, tc.Key())
			}
			assert.Equal(t, int64(len(keys)), res.Total)
			return keys
		}
		assert.Equal(t, []string{"CHK-2"}, find([]int64{chk.ID}, 2))
		assert.Equal(t, []string{"TC-1"}, find([]int64{catalog.DefaultProjectID}, 1))
		assert.ElementsMatch(t, []string{"TC-1", "CHK-1"}, find(nil, 1), "a number alone is not a key")
		assert.Empty(t, find([]int64{chk.ID}, 3))
	})

	t.Run("BE-INT-003_tc_ids_are_immutable_and_rows_cannot_be_deleted", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM test_cases WHERE id = $1`, tc.ID)
		assert.ErrorContains(t, err, "test cases cannot be deleted (TC-"+itoa(tc.ID)+"); deprecate instead")
		_, err = db.Pool.Exec(ctx, `UPDATE test_cases SET id = DEFAULT WHERE id = $1`, tc.ID)
		assert.ErrorContains(t, err, "test case identity is immutable (TC-"+itoa(tc.ID)+")")
		_, err = db.Pool.Exec(ctx, `UPDATE test_cases SET number = number + 1 WHERE id = $1`, tc.ID)
		assert.ErrorContains(t, err, "test case identity is immutable (TC-"+itoa(tc.ID)+")", "the number is part of the identity")
	})

	t.Run("BE-INT-026_database_enforces_test_case_integrity", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a", Description: strings.Repeat("d", 10000), ExpectedResult: strings.Repeat("e", 10000)})
		require.NoError(t, err, "10000 characters is the limit, not past it")
		for _, c := range []struct{ stmt, constraint string }{
			{`INSERT INTO test_cases (project_id, number, title) VALUES (1, 900, ' ' || chr(9) || ' ')`, "test_cases_title_not_blank"},
			{`INSERT INTO test_cases (project_id, number, title, description) VALUES (1, 900, 'd', repeat('d', 10001))`, "test_cases_description_length"},
			{`INSERT INTO test_cases (project_id, number, title, expected_result) VALUES (1, 900, 'e', repeat('é', 10001))`, "test_cases_expected_result_length"},
			{`INSERT INTO test_cases (project_id, number, title, status) VALUES (1, 900, 'no date', 'deprecated')`, "test_cases_deprecated_at_matches_status"},
			{`INSERT INTO test_cases (project_id, number, title, deprecated_at) VALUES (1, 900, 'active with date', now())`, "test_cases_deprecated_at_matches_status"},
			{`INSERT INTO test_cases (project_id, number, title, created_at, updated_at) VALUES (1, 900, 't', now(), now() - interval '1 second')`, "test_cases_updated_after_created"},
		} {
			_, err := db.Pool.Exec(ctx, c.stmt)
			assert.ErrorContains(t, err, c.constraint, c.stmt)
		}
		_, err = s.Catalog.Deprecate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err, "the service keeps status and deprecated_at in step")
		_, err = s.Catalog.Reactivate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)
	})

	t.Run("BE-INT-028_concurrent_partial_edits_of_different_fields_all_persist", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "orig", Description: "orig"})
		var wg sync.WaitGroup
		edits := []catalog.UpdateInput{{Title: ptr("new title")}, {Description: ptr("new description")},
			{ExpectedResult: ptr("new expected")}, {Automated: ptr(true)}}
		for _, in := range edits {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Catalog.Update(ctx, tc.ID, in, etag.Match{})
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
		got, err := s.Catalog.Get(ctx, tc.ID)
		require.NoError(t, err)
		assert.Equal(t, "new title", got.Title)
		assert.Equal(t, "new description", got.Description)
		assert.Equal(t, "new expected", got.ExpectedResult)
		assert.True(t, got.Automated, "a PATCH only writes the fields it sends, so concurrent edits of different fields never undo each other")
	})

	t.Run("BE-INT-023_database_enforces_step_integrity", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		other, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "b"})
		step, _, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "a", ExpectedResult: strings.Repeat("e", 2000)}, etag.Match{})
		require.NoError(t, err, "2000 characters is the limit, not past it")
		pos := int32(1)
		insert := func(action, expected string) error { // a fresh position each time, so only the CHECK under test can fail
			pos++
			_, err := db.Pool.Exec(ctx, `INSERT INTO test_steps (test_case_id, position, action, expected_result) VALUES ($1, $2, $3, $4)`, tc.ID, pos, action, expected)
			return err
		}
		assert.ErrorContains(t, insert(" \t\n ", ""), "test_steps_action_not_blank")
		assert.ErrorContains(t, insert("a", strings.Repeat("e", 2001)), "test_steps_expected_result_length")
		assert.ErrorContains(t, insert("a", strings.Repeat("é", 2001)), "test_steps_expected_result_length", "the limit counts characters")
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_steps (test_case_id, position, action) VALUES ($1, 1, 'x')`, 987654)
		assert.ErrorContains(t, err, "test_steps_test_case_id_fkey", "every step belongs to an existing test case")
		_, err = db.Pool.Exec(ctx, `UPDATE test_steps SET test_case_id = $1 WHERE id = $2`, other.ID, step.ID)
		assert.ErrorContains(t, err, "a test step cannot move to another test case")
		_, err = db.Pool.Exec(ctx, `UPDATE test_steps SET action = 'edited' WHERE id = $1`, step.ID)
		assert.NoError(t, err, "editing content is still allowed")
	})

	t.Run("BE-INT-004_test_case_crud_roundtrip", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "Login", Description: "d", ExpectedResult: "e", Automated: true})
		require.NoError(t, err)
		got, err := s.Catalog.Get(ctx, tc.ID)
		require.NoError(t, err)
		assert.Equal(t, tc.ID, got.ID)
		assert.Equal(t, "e", got.ExpectedResult)
		assert.Nil(t, got.DeprecatedAt)

		up, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("Login v2"), Automated: ptr(false)}, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, tc.ID, up.ID)
		assert.Equal(t, "Login v2", up.Title)
		assert.Equal(t, "d", up.Description, "unspecified fields are kept")
		assert.False(t, up.Automated)
		assert.True(t, up.UpdatedAt.After(tc.UpdatedAt) || up.UpdatedAt.Equal(tc.UpdatedAt))

		dep, err := s.Catalog.Deprecate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, catalog.StatusDeprecated, dep.Status)
		require.NotNil(t, dep.DeprecatedAt)
		again, err := s.Catalog.Deprecate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, dep.DeprecatedAt, again.DeprecatedAt, "deprecation is idempotent")
		assert.Equal(t, dep.UpdatedAt, again.UpdatedAt)

		back, err := s.Catalog.Reactivate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, tc.ID, back.ID)
		assert.Equal(t, catalog.StatusActive, back.Status)
		assert.Nil(t, back.DeprecatedAt)
		same, err := s.Catalog.Reactivate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, back.UpdatedAt, same.UpdatedAt, "reactivation is idempotent")
		_, err = s.Catalog.Deprecate(ctx, tc.ID, etag.Match{})
		require.NoError(t, err)

		for _, title := range []string{"x", "y", "z"} {
			_, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: title})
			require.NoError(t, err)
		}
		page, err := s.Catalog.List(ctx, catalog.ListFilter{}, pagination.Page{Number: 1, Size: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(4), page.Total)
		assert.Len(t, page.Items, 2)
		assert.Greater(t, page.Items[0].ID, page.Items[1].ID, "newest first")
		deprecated := catalog.StatusDeprecated
		page, err = s.Catalog.List(ctx, catalog.ListFilter{Status: &deprecated}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), page.Total)

		for _, err := range []error{
			func() error { _, err := s.Catalog.Get(ctx, 987654); return err }(),
			func() error {
				_, err := s.Catalog.Update(ctx, 987654, catalog.UpdateInput{Automated: ptr(true)}, etag.Match{})
				return err
			}(),
			func() error { _, err := s.Catalog.Deprecate(ctx, 987654, etag.Match{}); return err }(),
			func() error { _, err := s.Catalog.Reactivate(ctx, 987654, etag.Match{}); return err }(),
		} {
			e, ok := apperr.As(err)
			require.True(t, ok)
			assert.Equal(t, apperr.KindNotFound, e.Kind)
		}
	})

	t.Run("BE-INT-005_expected_universe_is_active_and_automated", func(t *testing.T) {
		s, ctx := fresh(t)
		auto, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "auto", Automated: true})
		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{Title: "manual"})
		gone, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "gone", Automated: true})
		_, _ = s.Catalog.Deprecate(ctx, gone.ID, etag.Match{})
		view, err := s.Catalog.IngestionView(ctx, catalog.DefaultProjectID, []int64{auto.Number, gone.Number, 987654})
		require.NoError(t, err)
		assert.Equal(t, []int64{auto.ID}, view.Expected)
		assert.Equal(t, map[int64]catalog.IngestionEntry{
			auto.Number: {ID: auto.ID, Status: catalog.StatusActive}, gone.Number: {ID: gone.ID, Status: catalog.StatusDeprecated},
		}, view.Entries,
			"only referenced ids have a status; unknown ids are absent")

		require.NoError(t, db.Reset(ctx))
		view, err = s.Catalog.IngestionView(ctx, catalog.DefaultProjectID, nil)
		require.NoError(t, err)
		assert.NotNil(t, view.Expected)
		assert.Empty(t, view.Expected)
	})

	t.Run("BE-INT-006_steps_keep_a_persistent_contiguous_order", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		a, _, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "a", ExpectedResult: "ra"}, etag.Match{})
		require.NoError(t, err)
		b, _, _ := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "b"}, etag.Match{})
		first, _, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "first", Position: ptr(int32(1))}, etag.Match{})
		require.NoError(t, err, "insert shifts positions inside the deferrable unique constraint")
		list := func() []catalog.TestStep {
			res, err := s.Catalog.ListSteps(ctx, tc.ID, pagination.Default())
			require.NoError(t, err)
			return res.Items
		}
		assert.Equal(t, []string{"first", "a", "b"}, actions(list()))

		up, _, err := s.Catalog.UpdateStep(ctx, tc.ID, a.ID, catalog.UpdateStepInput{ExpectedResult: ptr("ra2")}, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, "a", up.Action)
		assert.Equal(t, "ra2", up.ExpectedResult)

		reordered, _, err := s.Catalog.ReorderSteps(ctx, tc.ID, []int64{b.ID, first.ID, a.ID}, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, []string{"b", "first", "a"}, actions(reordered))

		_, err = s.Catalog.DeleteStep(ctx, tc.ID, first.ID, etag.Match{})
		require.NoError(t, err)
		items := list()
		assert.Equal(t, []string{"b", "a"}, actions(items))
		assert.Equal(t, []int32{1, 2}, []int32{items[0].Position, items[1].Position})

		page, err := s.Catalog.ListSteps(ctx, tc.ID, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), page.Total)
		assert.Equal(t, "a", page.Items[0].Action)

		_, _, err = s.Catalog.UpdateStep(ctx, tc.ID, 987654, catalog.UpdateStepInput{Action: ptr("x")}, etag.Match{})
		assert.Error(t, err)
		_, err = s.Catalog.DeleteStep(ctx, tc.ID, 987654, etag.Match{})
		assert.Error(t, err)
		_, _, err = s.Catalog.CreateStep(ctx, 987654, catalog.CreateStepInput{Action: "x"}, etag.Match{})
		assert.Error(t, err)

		after, _ := s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, tc.ID, after.ID)
	})

	t.Run("BE-INT-021_editing_content_and_steps_keeps_identity_and_history", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "login", ExpectedResult: "dashboard", Automated: true})
		a, _, _ := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "open"}, etag.Match{})
		b, _, _ := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "submit"}, etag.Match{})
		_, err := s.Ingestion.IngestJUnit(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "900", RunAttempt: 1},
			strings.NewReader(`<testsuite name="s"><testcase name="login TC-`+itoa(tc.ID)+`"><failure/></testcase></testsuite>`))
		require.NoError(t, err)
		before, err := s.Execution.History(ctx, tc.ID, pagination.Default())
		require.NoError(t, err)

		_, err = s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("login v2"), ExpectedResult: ptr("home page")}, etag.Match{})
		require.NoError(t, err)
		_, _, err = s.Catalog.UpdateStep(ctx, tc.ID, a.ID, catalog.UpdateStepInput{Action: ptr("open app")}, etag.Match{})
		require.NoError(t, err)
		_, _, err = s.Catalog.ReorderSteps(ctx, tc.ID, []int64{b.ID, a.ID}, etag.Match{})
		require.NoError(t, err)
		_, err = s.Catalog.DeleteStep(ctx, tc.ID, b.ID, etag.Match{})
		require.NoError(t, err)

		after, err := s.Execution.History(ctx, tc.ID, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, before, after, "old results are read against the same TC-ID, unchanged")
		got, _ := s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, tc.ID, got.ID)
		assert.Equal(t, "login v2", got.Title, "no new version: the test case itself changed")
	})

	t.Run("BE-INT-007_concurrent_step_creation_is_serialized_per_test_case", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		var wg sync.WaitGroup
		errs := make(chan error, 10)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "s", Position: ptr(int32(1))}, etag.Match{})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		res, err := s.Catalog.ListSteps(ctx, tc.ID, pagination.Default())
		require.NoError(t, err)
		for i, st := range res.Items {
			assert.Equal(t, int32(i+1), st.Position)
		}
		assert.Len(t, res.Items, 10)
	})

	t.Run("BE-INT-016_failed_transactions_roll_back", func(t *testing.T) {
		_, ctx := fresh(t)
		store := catalogpg.NewStore(db.Pool)
		var createdID int64
		err := store.InTx(ctx, func(r catalog.Repository) error {
			tc, err := r.CreateTestCase(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "rolled back"})
			createdID = tc.ID
			if err != nil {
				return err
			}
			return context.Canceled
		})
		require.ErrorIs(t, err, context.Canceled)
		_, err = store.GetTestCase(ctx, createdID)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		_, err = store.LockTestCase(ctx, createdID)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		_, err = store.DeleteTestStep(ctx, createdID, 1)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
	})
}

func actions(steps []catalog.TestStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Action
	}
	return out
}
