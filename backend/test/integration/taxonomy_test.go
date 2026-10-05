//go:build integration

package integration

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func dimensionKeys(dims []catalog.Dimension) []string {
	out := make([]string, len(dims))
	for i, d := range dims {
		out[i] = d.Key
	}
	return out
}

func TestTaxonomy(t *testing.T) {
	t.Run("BE-INT-045_dimensions_values_tags_and_classification_keep_their_invariants", func(t *testing.T) {
		s, ctx := fresh(t)
		builtIns := []string{"feature", "component", "level", "depth", "type", "risk", "platform"}

		// Every project starts with the built-in dimensions, in order, with their seeded values.
		dims, err := s.Catalog.Dimensions(ctx, catalog.DefaultProjectID)
		require.NoError(t, err)
		assert.Equal(t, builtIns, dimensionKeys(dims))
		assert.True(t, dims[0].BuiltIn)
		assert.Empty(t, dims[0].Values, "feature values are the project's own")
		risk := dims[5]
		assert.Equal(t, []string{"critical", "high", "medium", "low"}, []string{risk.Values[0].Key, risk.Values[1].Key, risk.Values[2].Key, risk.Values[3].Key})
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		chkDims, err := s.Catalog.Dimensions(ctx, chk.ID)
		require.NoError(t, err)
		assert.Equal(t, builtIns, dimensionKeys(chkDims), "seeded by the database for new projects too")

		// Project dimensions and values: duplicates conflict, values append in order, archive and restore.
		d, err := s.Catalog.CreateDimension(ctx, catalog.DefaultProjectID, catalog.DimensionInput{Key: "browser", Name: "Browser"})
		require.NoError(t, err)
		assert.False(t, d.BuiltIn)
		_, err = s.Catalog.CreateDimension(ctx, catalog.DefaultProjectID, catalog.DimensionInput{Key: "browser", Name: "x"})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		_, err = s.Catalog.CreateDimension(ctx, chk.ID, catalog.DimensionInput{Key: "browser", Name: "Browser"})
		require.NoError(t, err, "keys are per project")
		for _, v := range []string{"chrome", "firefox"} {
			d, err = s.Catalog.CreateDimensionValue(ctx, catalog.DefaultProjectID, "browser", catalog.DimensionInput{Key: v, Name: v})
			require.NoError(t, err)
		}
		assert.Equal(t, int32(2), d.Values[1].Position)
		_, err = s.Catalog.CreateDimensionValue(ctx, catalog.DefaultProjectID, "browser", catalog.DimensionInput{Key: "chrome", Name: "again"})
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		d, err = s.Catalog.UpdateDimensionValue(ctx, catalog.DefaultProjectID, "browser", "firefox", catalog.UpdateDimensionInput{Name: ptr("Firefox"), Archived: ptr(true)})
		require.NoError(t, err)
		assert.Equal(t, "Firefox", d.Values[1].Name)
		firstArchive := d.Values[1].ArchivedAt
		require.NotNil(t, firstArchive)
		d, err = s.Catalog.UpdateDimensionValue(ctx, catalog.DefaultProjectID, "browser", "firefox", catalog.UpdateDimensionInput{Archived: ptr(true)})
		require.NoError(t, err)
		assert.Equal(t, firstArchive, d.Values[1].ArchivedAt, "archiving again keeps the first date")
		_, err = s.Catalog.UpdateDimensionValue(ctx, catalog.DefaultProjectID, "browser", "edge", catalog.UpdateDimensionInput{Name: ptr("Edge")})
		assert.Equal(t, apperr.KindNotFound, kind(t, err))
		d, err = s.Catalog.UpdateDimension(ctx, catalog.DefaultProjectID, "browser", catalog.UpdateDimensionInput{Archived: ptr(true)})
		require.NoError(t, err)
		assert.NotNil(t, d.ArchivedAt)
		d, err = s.Catalog.UpdateDimension(ctx, catalog.DefaultProjectID, "browser", catalog.UpdateDimensionInput{Name: ptr("Browsers"), Archived: ptr(false)})
		require.NoError(t, err)
		assert.Nil(t, d.ArchivedAt)
		assert.Equal(t, "Browsers", d.Name)
		_, err = s.Catalog.UpdateDimension(ctx, catalog.DefaultProjectID, "os", catalog.UpdateDimensionInput{Name: ptr("OS")})
		assert.Equal(t, apperr.KindNotFound, kind(t, err))

		// Tags and classification are test case content: they advance the version, a no-op does not.
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "pay",
			Tags: []string{"Smoke", "checkout"}, Classification: map[string]string{"risk": "critical", "browser": "chrome"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"checkout", "smoke"}, tc.Tags)
		assert.Equal(t, map[string]string{"risk": "critical", "browser": "chrome"}, tc.Classification)
		v := tc.Version
		same, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Tags: &[]string{"smoke", "checkout"}, Classification: map[string]*string{"risk": ptr("critical")}}, ifMatch(t, v))
		require.NoError(t, err)
		assert.Equal(t, v, same.Version, "the same tags and value keep the version")
		up, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Tags: &[]string{"smoke"}, Classification: map[string]*string{"risk": ptr("high"), "browser": nil}}, ifMatch(t, v))
		require.NoError(t, err)
		assert.Greater(t, up.Version, v)
		assert.Equal(t, []string{"smoke"}, up.Tags)
		assert.Equal(t, map[string]string{"risk": "high"}, up.Classification)
		_, err = s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Tags: &[]string{"x"}}, ifMatch(t, v))
		assert.Equal(t, apperr.KindPreconditionFailed, kind(t, err), "a stale If-Match refuses taxonomy changes too")

		// Archived values are kept on test cases but cannot be newly assigned; a failed write changes nothing.
		_, err = s.Catalog.UpdateDimensionValue(ctx, catalog.DefaultProjectID, "risk", "high", catalog.UpdateDimensionInput{Archived: ptr(true)})
		require.NoError(t, err)
		kept, err := s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Title: ptr("pay v2"), Classification: map[string]*string{"risk": ptr("high")}}, etag.Match{})
		require.NoError(t, err)
		assert.Equal(t, "high", kept.Classification["risk"])
		_, err = s.Catalog.Update(ctx, tc.ID, catalog.UpdateInput{Tags: &[]string{"lost"}, Classification: map[string]*string{"browser": ptr("firefox")}}, etag.Match{})
		assert.Equal(t, apperr.KindValidation, kind(t, err))
		got, _ := s.Catalog.Get(ctx, tc.ID)
		assert.Equal(t, []string{"smoke"}, got.Tags, "the transaction rolled back the tags")

		// Filters: a tag, and dimension:value pairs that must all hold.
		other, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "login", Tags: []string{"smoke"}, Classification: map[string]string{"risk": "critical"}})
		require.NoError(t, err)
		_, err = s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "plain"})
		require.NoError(t, err)
		list := func(f catalog.ListFilter) []int64 {
			res, err := s.Catalog.List(ctx, f, pagination.Page{Number: 1, Size: 10})
			require.NoError(t, err)
			ids := []int64{}
			for _, tc := range res.Items {
				ids = append(ids, tc.ID)
			}
			assert.Equal(t, int64(len(ids)), res.Total, "count agrees with the page")
			return ids
		}
		assert.Equal(t, []int64{other.ID, tc.ID}, list(catalog.ListFilter{Tag: ptr("smoke")}))
		assert.Equal(t, []int64{other.ID}, list(catalog.ListFilter{Classified: []string{"risk:critical"}}))
		assert.Equal(t, []int64{other.ID}, list(catalog.ListFilter{Tag: ptr("smoke"), Classified: []string{"risk:critical"}}))
		assert.Equal(t, []int64{}, list(catalog.ListFilter{Classified: []string{"risk:critical", "risk:high"}}))
		assert.Equal(t, []int64{}, list(catalog.ListFilter{Tag: ptr("nope")}))
		all := list(catalog.ListFilter{})
		assert.Len(t, all, 3)
		listed, _ := s.Catalog.List(ctx, catalog.ListFilter{}, pagination.Page{Number: 1, Size: 10})
		assert.Equal(t, []string{"smoke"}, listed.Items[1].Tags, "lists carry tags and classification")
		assert.Equal(t, map[string]string{}, listed.Items[0].Classification)

		// Twenty concurrent tag edits from the same read: exactly one wins.
		base, _ := s.Catalog.Get(ctx, other.ID)
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := s.Catalog.Update(ctx, other.ID, catalog.UpdateInput{Tags: &[]string{"t" + strconv.Itoa(i)}}, ifMatch(t, base.Version))
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					wins++
				}
			}(i)
		}
		wg.Wait()
		assert.Equal(t, 1, wins)
		final, _ := s.Catalog.Get(ctx, other.ID)
		assert.Len(t, final.Tags, 1)

		// The database refuses what the service never does.
		var chkRisk, chkCritical int64
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT d.id, v.id FROM classification_dimensions d JOIN classification_values v ON v.dimension_id = d.id
			WHERE d.project_id = $1 AND d.key = 'risk' AND v.key = 'critical'`, chk.ID).Scan(&chkRisk, &chkCritical))
		var tcRisk int64
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT id FROM classification_dimensions WHERE project_id = 1 AND key = 'risk'`).Scan(&tcRisk))
		refused := map[string]string{
			"another project's dimension":   `INSERT INTO test_case_classifications VALUES ($1, 1, ` + strconv.FormatInt(chkRisk, 10) + `, ` + strconv.FormatInt(chkCritical, 10) + `)`,
			"another dimension's value":     `INSERT INTO test_case_classifications VALUES ($1, 1, (SELECT id FROM classification_dimensions WHERE project_id = 1 AND key = 'browser'), ` + strconv.FormatInt(chkCritical, 10) + `)`,
			"a second value of a dimension": `INSERT INTO test_case_classifications SELECT $1, 1, ` + strconv.FormatInt(tcRisk, 10) + `, id FROM classification_values WHERE dimension_id = ` + strconv.FormatInt(tcRisk, 10) + ` AND key = 'low'`,
			"a project mismatch":            `INSERT INTO test_case_classifications SELECT $1, ` + strconv.FormatInt(chk.ID, 10) + `, ` + strconv.FormatInt(chkRisk, 10) + `, ` + strconv.FormatInt(chkCritical, 10),
			"a malformed tag":               `INSERT INTO test_case_tags VALUES ($1, 'Upper')`,
			"deleting a value":              `DELETE FROM classification_values WHERE key = 'low' AND $1 > 0`,
			"deleting a dimension":          `DELETE FROM classification_dimensions WHERE key = 'browser' AND $1 > 0`,
			"renaming a dimension key":      `UPDATE classification_dimensions SET key = 'web' WHERE key = 'browser' AND $1 > 0`,
			"renaming a value key":          `UPDATE classification_values SET key = 'lowest' WHERE key = 'low' AND $1 > 0`,
			"moving a dimension":            `UPDATE classification_dimensions SET project_id = ` + strconv.FormatInt(chk.ID, 10) + ` WHERE project_id = 1 AND key = 'browser' AND $1 > 0`,
			"turning a dimension built-in":  `UPDATE classification_dimensions SET built_in = true WHERE project_id = 1 AND key = 'browser' AND $1 > 0`,
		}
		for name, sql := range refused {
			_, err := db.Pool.Exec(ctx, sql, tc.ID)
			assert.Error(t, err, name)
		}
	})
}
