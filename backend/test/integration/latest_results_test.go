//go:build integration

package integration

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
)

// The latest results of a test case (requirement coverage, issue verification) cost the same whether its last result
// is in the newest run or deep in history: the lookup walks the test case's own runs (index), not the whole results
// table. Relative bound: a stale test case costs no more than a few times a fresh one.
func TestLatestResultsCost(t *testing.T) {
	t.Run("BE-INT-058_latest_results_cost_does_not_grow_with_unrelated_history", func(t *testing.T) {
		s, ctx := fresh(t)
		create := func(title string) catalog.TestCase {
			tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: title, Automated: true})
			require.NoError(t, err)
			return tc
		}
		stale, busy := create("stale"), create("busy")
		_, err := s.Ingestion.IngestJUnit(ctx, meta("old", 1), strings.NewReader(junitFor(tcProp("stale", itoa(stale.ID), `<failure/>`))))
		require.NoError(t, err)
		// Later history that never mentions the stale test case: 5 runs of 20,000 results each.
		for run := range 5 {
			cases := make([]string, 20000)
			for i := range cases {
				cases[i] = tcProp("b"+itoa(int64(i)), itoa(busy.ID), "")
			}
			_, err := s.Ingestion.IngestJUnit(ctx, meta("busy"+itoa(int64(run)), 1), strings.NewReader(junitFor(cases...)))
			require.NoError(t, err)
		}

		median := func(read func() error) time.Duration {
			var times []time.Duration
			for range 7 {
				start := time.Now()
				require.NoError(t, read())
				times = append(times, time.Since(start))
			}
			slices.Sort(times)
			return times[len(times)/2]
		}
		statuses := func(id int64) func() error {
			return func() error { _, err := s.Execution.LatestStatuses(ctx, []int64{id}); return err }
		}
		conclusive := func(id int64) func() error {
			return func() error { _, _, err := s.Execution.LatestConclusive(ctx, []int64{id}); return err }
		}
		got, err := s.Execution.LatestStatuses(ctx, []int64{stale.ID})
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{stale.ID: "failed"}, got, "the stale test case keeps its old result")
		status, _, err := s.Execution.LatestConclusive(ctx, []int64{stale.ID})
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{stale.ID: "failed"}, status)

		// Before the index and the rewrite, the stale lookup walked every later result backwards (and the
		// conclusive one aggregated every run of the busy history): orders of magnitude slower than the fresh one.
		// The fresh lookup reads one run of 20,000 results, so a stale one must not cost more than it.
		freshStatuses, staleStatuses := median(statuses(busy.ID)), median(statuses(stale.ID))
		assert.Less(t, staleStatuses, 2*freshStatuses, "latest statuses: stale %v, fresh %v", staleStatuses, freshStatuses)
		freshConclusive, staleConclusive := median(conclusive(busy.ID)), median(conclusive(stale.ID))
		assert.Less(t, staleConclusive, 2*freshConclusive, "latest conclusive: stale %v, fresh %v", staleConclusive, freshConclusive)
	})
}
