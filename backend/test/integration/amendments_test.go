//go:build integration

package integration

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestAmendments(t *testing.T) {
	t.Run("BE-INT-043_snapshot_amendments_extend_the_universe_append_only_and_only_for_reported_tc_ids", func(t *testing.T) {
		s, ctx := fresh(t)
		auto, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "auto", Automated: true})
		manual, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "manual"})
		silent, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "not reported"})
		out, err := s.Ingestion.IngestJUnit(ctx, meta("1", 1), strings.NewReader(junitFor(
			tcProp("a", strconv.FormatInt(auto.ID, 10), ""), tcProp("m", strconv.FormatInt(manual.ID, 10), `<failure message="x"/>`))))
		require.NoError(t, err)
		runID := out.Run.ID
		by := authz.Actor{ID: 1, Username: "ana"}

		// Twenty maintainers amend the same TC-ID at once: one amendment is recorded, the others are conflicts.
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, conflicts := 0, 0
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Execution.Amend(ctx, runID, manual.ID, "marked manual by mistake", by)
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					ok++
				} else if e, isApp := apperr.As(err); isApp && e.Kind == apperr.KindConflict {
					conflicts++
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, [2]int{1, 19}, [2]int{ok, conflicts})

		sum, err := s.Execution.Summary(ctx, runID)
		require.NoError(t, err)
		assert.Equal(t, [3]int32{1, 2, 1}, [3]int32{sum.SnapshotTotal, sum.ExpectedTotal, sum.Counts.Failed})
		assert.Equal(t, []int64{manual.ID}, sum.AmendedIDs)
		run, _ := s.Execution.GetRun(ctx, runID)
		assert.Equal(t, int32(1), run.AmendmentCount)
		assert.Equal(t, int32(2), run.ExpectedCount)
		runs, _ := s.Execution.ListRuns(ctx, nil, pagination.Default())
		assert.Equal(t, int32(1), runs.Items[0].AmendmentCount)
		hist, _ := s.Execution.History(ctx, manual.ID, pagination.Default())
		assert.Equal(t, int32(1), hist.Items[0].Run.AmendmentCount)
		list, err := s.Execution.ListAmendments(ctx, runID, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, "ana", list.Items[0].AmendedByUsername)

		// The database refuses what the service refuses, and keeps amendments append-only.
		_, err = s.Execution.Amend(ctx, runID, silent.ID, "x", by)
		assert.Equal(t, apperr.KindValidation, kind(t, err))
		var expected int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM test_run_expected_cases WHERE test_run_id = $1`, runID).Scan(&expected))
		assert.Equal(t, 1, expected, "the snapshot itself never changes")
		for _, stmt := range []string{
			`UPDATE test_run_amendments SET reason = 'changed'`,
			`DELETE FROM test_run_amendments`,
			`INSERT INTO test_run_amendments (test_run_id, test_case_id, amended_by, amended_by_username, reason) VALUES (` +
				strconv.FormatInt(runID, 10) + `, ` + strconv.FormatInt(silent.ID, 10) + `, 1, 'ana', 'no result')`,
			`INSERT INTO test_run_amendments (test_run_id, test_case_id, amended_by, amended_by_username, reason) VALUES (` +
				strconv.FormatInt(runID, 10) + `, ` + strconv.FormatInt(auto.ID, 10) + `, 1, 'ana', 'in the snapshot')`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})
}
