//go:build integration

package integration

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
)

func ifMatch(t *testing.T, v int64) etag.Match {
	t.Helper()
	m, err := etag.Parse(etag.Tag(v))
	require.NoError(t, err)
	return m
}

func TestVersions(t *testing.T) {
	t.Run("BE-INT-042_test_case_versions_advance_with_every_change_and_guard_concurrent_writes", func(t *testing.T) {
		s, ctx := fresh(t)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "login"})
		require.NoError(t, err)
		require.Equal(t, int64(1), tc.Version)

		// The database advances the version: content changes and every step change do, a no-op update does not.
		up, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("login v2")}, ifMatch(t, 1))
		require.NoError(t, err)
		assert.Equal(t, int64(2), up.Version)
		same, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("login v2")}, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, int64(2), same.Version, "saving the same content keeps the version")
		st, v, err := s.Catalog.CreateStep(ctx, tc.ID, catalog.CreateStepInput{Action: "open"}, ifMatch(t, 2))
		require.NoError(t, err)
		assert.Greater(t, v, int64(2))
		_, v2, err := s.Catalog.UpdateStep(ctx, tc.ID, st.ID, catalog.UpdateStepInput{Action: ptr("open app")}, ifMatch(t, v))
		require.NoError(t, err)
		assert.Greater(t, v2, v)
		v3, err := s.Catalog.DeleteStep(ctx, tc.ID, st.ID, ifMatch(t, v2))
		require.NoError(t, err)
		assert.Greater(t, v3, v2)
		got, _ := s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, v3, got.Version, "the returned version is the stored one")

		// A stale version is refused and changes nothing.
		_, err = s.Catalog.Deprecate(ctx, tc.ID, ifMatch(t, v2))
		assert.Equal(t, apperr.KindPreconditionFailed, kind(t, err))
		got, _ = s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, catalog.StatusActive, got.Status)

		// Twenty people save from the same read: exactly one wins, the others get 412.
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins, stale := 0, 0
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("edit " + string(rune('a'+i)))}, ifMatch(t, v3))
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					wins++
				} else if e, ok := apperr.As(err); ok && e.Kind == apperr.KindPreconditionFailed {
					stale++
				}
			}(i)
		}
		wg.Wait()
		assert.Equal(t, [2]int{1, 19}, [2]int{wins, stale})

		// The version never goes back.
		_, err = db.Pool.Exec(ctx, `UPDATE test_cases SET version = 1 WHERE id = $1`, tc.ID)
		assert.Error(t, err)
	})
}
