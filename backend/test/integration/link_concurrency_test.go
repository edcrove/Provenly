//go:build integration

package integration

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
)

// Concurrent replacements of a requirement's, an issue's or a static suite's test cases end as exactly one caller's
// list, never a union; concurrent taxonomy creations respect the limits and number values without gaps or repeats.
func TestLinkAndTaxonomyConcurrency(t *testing.T) {
	t.Run("BE-INT-059_concurrent_link_replacements_keep_one_callers_set", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) int64 {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc.ID
		}
		a, b := create("a"), create("b")
		req, err := s.Catalog.CreateRequirement(ctx, catalog.DefaultProjectID, catalog.RequirementInput{Title: "R"})
		require.NoError(t, err)
		iss, err := s.Catalog.CreateIssue(ctx, catalog.DefaultProjectID, catalog.IssueInput{Title: "I"})
		require.NoError(t, err)
		_, err = s.Catalog.CreateSuite(ctx, catalog.DefaultProjectID, catalog.SuiteInput{Key: "race", Name: "Race", Kind: catalog.SuiteKindStatic})
		require.NoError(t, err)

		race := func(replace func(ids []int64) error, read func() []int64) {
			for round := range 10 {
				var wg sync.WaitGroup
				for i := range 20 {
					ids := []int64{a}
					if i%2 == 1 {
						ids = []int64{b}
					}
					wg.Add(1)
					go func() { defer wg.Done(); assert.NoError(t, replace(ids)) }()
				}
				wg.Wait()
				got := read()
				assert.True(t, slices.Equal(got, []int64{a}) || slices.Equal(got, []int64{b}), "round %d: %v is one caller's set", round, got)
			}
		}
		race(func(ids []int64) error {
			_, err := s.Catalog.SetRequirementTestCases(ctx, catalog.DefaultProjectID, req.ID, ids)
			return err
		}, func() []int64 {
			v, err := s.Catalog.Requirement(ctx, catalog.DefaultProjectID, req.ID)
			require.NoError(t, err)
			return v.TestCaseIDs
		})
		race(func(ids []int64) error {
			_, err := s.Catalog.SetIssueTestCases(ctx, catalog.DefaultProjectID, iss.ID, ids)
			return err
		}, func() []int64 {
			v, err := s.Catalog.Issue(ctx, catalog.DefaultProjectID, iss.ID)
			require.NoError(t, err)
			return v.TestCaseIDs
		})
		race(func(ids []int64) error {
			_, err := s.Catalog.SetSuiteCases(ctx, catalog.DefaultProjectID, "race", ids)
			return err
		}, func() []int64 {
			v, err := s.Catalog.Suite(ctx, catalog.DefaultProjectID, "race")
			require.NoError(t, err)
			return v.CaseIDs
		})
	})

	t.Run("BE-INT-060_concurrent_taxonomy_creations_respect_limits_and_positions", func(t *testing.T) {
		s, ctx := fresh(t)
		dims, err := s.Catalog.Dimensions(ctx, catalog.DefaultProjectID)
		require.NoError(t, err)
		for i := len(dims); i < 29; i++ {
			_, err := s.Catalog.CreateDimension(ctx, catalog.DefaultProjectID, catalog.DimensionInput{Key: fmt.Sprintf("d%d", i), Name: "D"})
			require.NoError(t, err)
		}
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = s.Catalog.CreateDimension(ctx, catalog.DefaultProjectID, catalog.DimensionInput{Key: fmt.Sprintf("race%d", i), Name: "R"})
			}()
		}
		wg.Wait()
		dims, err = s.Catalog.Dimensions(ctx, catalog.DefaultProjectID)
		require.NoError(t, err)
		assert.Len(t, dims, 30, "the 30-dimension limit holds under concurrent creation")
		winner := dims[len(dims)-1].Key // the one racing creation that fit

		for i := range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Catalog.CreateDimensionValue(ctx, catalog.DefaultProjectID, winner, catalog.DimensionInput{Key: fmt.Sprintf("v%d", i), Name: "V"})
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
		dims, err = s.Catalog.Dimensions(ctx, catalog.DefaultProjectID)
		require.NoError(t, err)
		var positions []int32
		for _, d := range dims {
			if d.Key == winner {
				for _, v := range d.Values {
					positions = append(positions, v.Position)
				}
			}
		}
		slices.Sort(positions)
		want := make([]int32, 20)
		for i := range want {
			want[i] = int32(i + 1)
		}
		assert.Equal(t, want, positions, "concurrent values get distinct, gapless positions")
	})
}
