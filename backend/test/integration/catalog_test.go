//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
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
		_, err = s.Catalog.Deprecate(ctx, b.ID)
		require.NoError(t, err)
		c, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "c"})
		require.NoError(t, err)
		assert.Greater(t, c.ID, b.ID, "a deprecated id is never reused")

		// A failed insert consumes an identity value but never produces a duplicate.
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_cases (title) VALUES ('')`)
		require.Error(t, err)
		d, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "d"})
		require.NoError(t, err)
		assert.Greater(t, d.ID, c.ID)

		_, err = db.Pool.Exec(ctx, `INSERT INTO test_cases (id, title) VALUES (999, 'forced')`)
		require.Error(t, err, "clients cannot choose a TC-ID")
	})

	t.Run("BE-INT-003_tc_ids_are_immutable_and_rows_cannot_be_deleted", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM test_cases WHERE id = $1`, tc.ID)
		assert.ErrorContains(t, err, "cannot be deleted")
		_, err = db.Pool.Exec(ctx, `UPDATE test_cases SET id = DEFAULT WHERE id = $1`, tc.ID)
		assert.ErrorContains(t, err, "immutable")
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

		up, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("Login v2"), Automated: ptr(false)})
		require.NoError(t, err)
		assert.Equal(t, tc.ID, up.ID)
		assert.Equal(t, "Login v2", up.Title)
		assert.Equal(t, "d", up.Description, "unspecified fields are kept")
		assert.False(t, up.Automated)
		assert.True(t, up.UpdatedAt.After(tc.UpdatedAt) || up.UpdatedAt.Equal(tc.UpdatedAt))

		dep, err := s.Catalog.Deprecate(ctx, tc.ID)
		require.NoError(t, err)
		assert.Equal(t, catalog.StatusDeprecated, dep.Status)
		require.NotNil(t, dep.DeprecatedAt)
		again, err := s.Catalog.Deprecate(ctx, tc.ID)
		require.NoError(t, err)
		assert.Equal(t, dep.DeprecatedAt, again.DeprecatedAt, "deprecation is idempotent")
		assert.Equal(t, dep.UpdatedAt, again.UpdatedAt)

		for _, title := range []string{"x", "y", "z"} {
			_, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: title})
			require.NoError(t, err)
		}
		page, err := s.Catalog.List(ctx, nil, pagination.Page{Number: 1, Size: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(4), page.Total)
		assert.Len(t, page.Items, 2)
		assert.Greater(t, page.Items[0].ID, page.Items[1].ID, "newest first")
		deprecated := catalog.StatusDeprecated
		page, err = s.Catalog.List(ctx, &deprecated, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), page.Total)

		for _, err := range []error{
			func() error { _, err := s.Catalog.Get(ctx, 987654); return err }(),
			func() error {
				_, err := s.Catalog.Update(ctx, 987654, catalog.UpdateInput{Automated: ptr(true)})
				return err
			}(),
			func() error { _, err := s.Catalog.Deprecate(ctx, 987654); return err }(),
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
		_, _ = s.Catalog.Deprecate(ctx, gone.ID)
		ids, err := s.Catalog.ExpectedUniverse(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int64{auto.ID}, ids)

		st, err := s.Catalog.Statuses(ctx, []int64{auto.ID, gone.ID, 987654})
		require.NoError(t, err)
		assert.Equal(t, map[int64]catalog.Status{auto.ID: catalog.StatusActive, gone.ID: catalog.StatusDeprecated}, st)

		require.NoError(t, db.Reset(ctx))
		ids, err = s.Catalog.ExpectedUniverse(ctx)
		require.NoError(t, err)
		assert.NotNil(t, ids)
		assert.Empty(t, ids)
	})

	t.Run("BE-INT-006_steps_keep_a_persistent_contiguous_order", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "a"})
		a, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "a", ExpectedResult: "ra"})
		require.NoError(t, err)
		b, _ := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "b"})
		first, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "first", Position: ptr(int32(1))})
		require.NoError(t, err, "insert shifts positions inside the deferrable unique constraint")
		list := func() []catalog.TestStep {
			res, err := s.Catalog.ListSteps(ctx, tc.ID, pagination.Default())
			require.NoError(t, err)
			return res.Items
		}
		assert.Equal(t, []string{"first", "a", "b"}, actions(list()))

		up, err := s.Catalog.UpdateStep(ctx, tc.ID, a.ID, catalog.UpdateStepInput{ExpectedResult: ptr("ra2")})
		require.NoError(t, err)
		assert.Equal(t, "a", up.Action)
		assert.Equal(t, "ra2", up.ExpectedResult)

		reordered, err := s.Catalog.ReorderSteps(ctx, tc.ID, []int64{b.ID, first.ID, a.ID})
		require.NoError(t, err)
		assert.Equal(t, []string{"b", "first", "a"}, actions(reordered))

		require.NoError(t, s.Catalog.DeleteStep(ctx, tc.ID, first.ID))
		items := list()
		assert.Equal(t, []string{"b", "a"}, actions(items))
		assert.Equal(t, []int32{1, 2}, []int32{items[0].Position, items[1].Position})

		page, err := s.Catalog.ListSteps(ctx, tc.ID, pagination.Page{Number: 2, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), page.Total)
		assert.Equal(t, "a", page.Items[0].Action)

		_, err = s.Catalog.UpdateStep(ctx, tc.ID, 987654, catalog.UpdateStepInput{Action: ptr("x")})
		assert.Error(t, err)
		assert.Error(t, s.Catalog.DeleteStep(ctx, tc.ID, 987654))
		_, err = s.Catalog.CreateStep(ctx, 987654, catalog.CreateStepInput{Action: "x"})
		assert.Error(t, err)

		after, _ := s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, tc.ID, after.ID)
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
				_, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "s", Position: ptr(int32(1))})
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
			tc, err := r.CreateTestCase(ctx, catalog.CreateInput{Title: "rolled back"})
			createdID = tc.ID
			if err != nil {
				return err
			}
			return context.Canceled
		})
		require.ErrorIs(t, err, context.Canceled)
		_, err = store.GetTestCase(ctx, createdID)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.ErrorIs(t, store.LockTestCase(ctx, createdID), catalog.ErrNotFound)
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
