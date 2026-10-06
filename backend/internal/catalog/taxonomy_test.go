package catalog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestNormalizeTags(t *testing.T) {
	var v apperr.Validator
	assert.Equal(t, []string{"api", "smoke"}, normalizeTags(&v, []string{" Smoke ", "api", "SMOKE"}), "trimmed, lower-cased, sorted, unique")
	assert.Equal(t, []string{}, normalizeTags(&v, nil))
	require.NoError(t, v.Err())

	for _, bad := range []string{"", "-x", "a b", "ä", strings.Repeat("a", 41), "x/y"} {
		var v apperr.Validator
		normalizeTags(&v, []string{bad})
		assert.Error(t, v.Err(), "%q", bad)
	}
	many := make([]string, MaxTags+1)
	for i := range many {
		many[i] = "t" + strconv.Itoa(i)
	}
	var tooMany apperr.Validator
	normalizeTags(&tooMany, many)
	assert.Error(t, tooMany.Err())
	var enough apperr.Validator
	normalizeTags(&enough, append(many[:MaxTags], "t0"))
	assert.NoError(t, enough.Err(), "duplicates do not count")
}

func TestCreateAndUpdateWithTaxonomy(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{Title: "Pay", Tags: []string{"Smoke", "api"}, Classification: map[string]string{"risk": "critical"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "smoke"}, tc.Tags)
	assert.Equal(t, map[string]string{"risk": "critical"}, tc.Classification)

	plain, err := svc.Create(ctx, CreateInput{Title: "Plain"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), plain.Version)
	assert.Empty(t, plain.Tags)

	// Classification merges: risk changes, feature stays unset; tags are replaced.
	v := tc.Version
	tc, err = svc.Update(ctx, tc.ID, UpdateInput{Tags: &[]string{"regression"}, Classification: map[string]*string{"risk": ptr("high")}}, etag.Match{})
	require.NoError(t, err)
	assert.Equal(t, []string{"regression"}, tc.Tags)
	assert.Equal(t, map[string]string{"risk": "high"}, tc.Classification)
	assert.Greater(t, tc.Version, v)

	// Only the classification: tags stay. A null value clears the dimension.
	tc, err = svc.Update(ctx, tc.ID, UpdateInput{Classification: map[string]*string{"risk": nil}}, etag.Match{})
	require.NoError(t, err)
	assert.Equal(t, []string{"regression"}, tc.Tags)
	assert.Empty(t, tc.Classification)

	// Only tags: the classification stays.
	tc, err = svc.Update(ctx, tc.ID, UpdateInput{Tags: &[]string{"regression", "api"}, Classification: map[string]*string{}}, etag.Match{})
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "regression"}, tc.Tags)

	// A title-only update leaves the taxonomy alone.
	tc, err = svc.Update(ctx, tc.ID, UpdateInput{Title: ptr("Pay 2")}, etag.Match{})
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "regression"}, tc.Tags)
}

func TestTaxonomyValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{Title: "Pay", Classification: map[string]string{"risk": "low"}})
	require.NoError(t, err)

	field := func(err error) string {
		e, ok := apperr.As(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, apperr.KindValidation, e.Kind, "%v", err)
		return e.Fields[0].Field + ": " + e.Fields[0].Message
	}
	_, err = svc.Create(ctx, CreateInput{Title: "x", Classification: map[string]string{"browser": "chrome"}})
	assert.Equal(t, "classification.browser: is not a dimension of the project", field(err))
	_, err = svc.Create(ctx, CreateInput{Title: "x", Classification: map[string]string{"risk": "none"}})
	assert.Equal(t, `classification.risk: "none" is not a value of Risk`, field(err))
	_, err = svc.Create(ctx, CreateInput{Title: "x", Tags: []string{"no spaces"}})
	assert.Contains(t, field(err), "tags: ")
	_, err = svc.Update(ctx, tc.ID, UpdateInput{Tags: &[]string{"-x"}}, etag.Match{})
	assert.Contains(t, field(err), "tags: ")

	// Archived values and dimensions cannot be newly assigned, but the current value can be sent again.
	_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "risk", "low", UpdateDimensionInput{Archived: ptr(true)})
	require.NoError(t, err)
	_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "risk", "high", UpdateDimensionInput{Archived: ptr(true)})
	require.NoError(t, err)
	_, err = svc.Update(ctx, tc.ID, UpdateInput{Classification: map[string]*string{"risk": ptr("high")}}, etag.Match{})
	assert.Equal(t, `classification.risk: "high" is archived`, field(err))
	same, err := svc.Update(ctx, tc.ID, UpdateInput{Classification: map[string]*string{"risk": ptr("low")}}, etag.Match{})
	require.NoError(t, err)
	assert.Equal(t, "low", same.Classification["risk"])

	_, err = svc.UpdateDimension(ctx, DefaultProjectID, "risk", UpdateDimensionInput{Archived: ptr(true)})
	require.NoError(t, err)
	_, err = svc.Update(ctx, tc.ID, UpdateInput{Classification: map[string]*string{"risk": ptr("critical")}}, etag.Match{})
	assert.Equal(t, "classification.risk: the dimension is archived", field(err))
	cleared, err := svc.Update(ctx, tc.ID, UpdateInput{Classification: map[string]*string{"risk": nil}}, etag.Match{})
	require.NoError(t, err, "clearing an archived dimension is allowed")
	assert.Empty(t, cleared.Classification)
}

func TestTaxonomyRepositoryErrors(t *testing.T) {
	svc, repo, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{Title: "Pay"})
	require.NoError(t, err)
	for _, method := range []string{"SetTags", "ListDimensions", "SetClassification", "GetTestCase"} {
		repo.errs[method] = errBoom
		_, err := svc.Create(ctx, CreateInput{Title: "x", Tags: []string{"a"}, Classification: map[string]string{"risk": "critical"}})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	repo.errs["UpdateTestCase"] = errBoom
	_, err = svc.Update(ctx, tc.ID, UpdateInput{Tags: &[]string{}}, etag.Match{})
	assert.ErrorIs(t, err, errBoom)
	delete(repo.errs, "UpdateTestCase")
	repo.errs["SetTags"] = errBoom
	_, err = svc.Update(ctx, tc.ID, UpdateInput{Tags: &[]string{}}, etag.Match{})
	assert.ErrorIs(t, err, errBoom)
}

func TestListFiltersByTaxonomy(t *testing.T) {
	svc, _, ctx := setup(t)
	_, err := svc.CreateDimensionValue(ctx, DefaultProjectID, "feature", DimensionInput{Key: "pay", Name: "Payments"})
	require.NoError(t, err)
	a, _ := svc.Create(ctx, CreateInput{Title: "a", Tags: []string{"smoke"}, Classification: map[string]string{"risk": "critical", "feature": "pay"}})
	b, _ := svc.Create(ctx, CreateInput{Title: "b", Tags: []string{"smoke"}, Classification: map[string]string{"risk": "high"}})
	_, _ = svc.Create(ctx, CreateInput{Title: "c"})
	ids := func(f ListFilter) []int64 {
		res, err := svc.List(ctx, f, pagination.Page{Number: 1, Size: 10})
		require.NoError(t, err)
		out := []int64{}
		for _, tc := range res.Items {
			out = append(out, tc.ID)
		}
		return out
	}
	assert.Equal(t, []int64{b.ID, a.ID}, ids(ListFilter{Tag: ptr("smoke")}))
	assert.Equal(t, []int64{a.ID}, ids(ListFilter{Classified: []string{"feature:pay", "risk:critical"}}))
	assert.Equal(t, []int64{}, ids(ListFilter{Classified: []string{"risk:critical", "risk:high"}}), "one value per dimension")
}

func TestDimensionsLifecycle(t *testing.T) {
	svc, repo, ctx := setup(t)
	dims, err := svc.Dimensions(ctx, DefaultProjectID)
	require.NoError(t, err)
	require.Len(t, dims, 2)
	assert.True(t, dims[0].BuiltIn)

	d, err := svc.CreateDimension(ctx, DefaultProjectID, DimensionInput{Key: "browser", Name: " Browser "})
	require.NoError(t, err)
	assert.Equal(t, "Browser", d.Name)
	assert.False(t, d.BuiltIn)
	_, err = svc.CreateDimension(ctx, DefaultProjectID, DimensionInput{Key: "browser", Name: "Again"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))

	d, err = svc.CreateDimensionValue(ctx, DefaultProjectID, "browser", DimensionInput{Key: "chrome", Name: "Chrome"})
	require.NoError(t, err)
	assert.Equal(t, "chrome", d.Values[0].Key)
	_, err = svc.CreateDimensionValue(ctx, DefaultProjectID, "browser", DimensionInput{Key: "chrome", Name: "Chrome"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	_, err = svc.CreateDimensionValue(ctx, DefaultProjectID, "os", DimensionInput{Key: "linux", Name: "Linux"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	d, err = svc.UpdateDimension(ctx, DefaultProjectID, "browser", UpdateDimensionInput{Name: ptr("Browsers"), Archived: ptr(true)})
	require.NoError(t, err)
	assert.Equal(t, "Browsers", d.Name)
	assert.NotNil(t, d.ArchivedAt)
	d, err = svc.UpdateDimension(ctx, DefaultProjectID, "browser", UpdateDimensionInput{Archived: ptr(false)})
	require.NoError(t, err)
	assert.Nil(t, d.ArchivedAt, "restored")
	_, err = svc.UpdateDimension(ctx, DefaultProjectID, "os", UpdateDimensionInput{Name: ptr("OS")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	d, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "browser", "chrome", UpdateDimensionInput{Name: ptr("Google Chrome")})
	require.NoError(t, err)
	assert.Equal(t, "Google Chrome", d.Values[0].Name)
	_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "browser", "edge", UpdateDimensionInput{Name: ptr("Edge")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "os", "edge", UpdateDimensionInput{Name: ptr("Edge")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	// Validation.
	for _, in := range []DimensionInput{{Key: "Browser", Name: "x"}, {Key: "1x", Name: "x"}, {Key: "x", Name: " "}, {Key: "x", Name: strings.Repeat("n", 61)}, {Key: "x", Name: "a\x00"}} {
		_, err := svc.CreateDimension(ctx, DefaultProjectID, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
	for _, in := range []DimensionInput{{Key: "-a", Name: "x"}, {Key: "A", Name: "x"}, {Key: "a", Name: ""}} {
		_, err := svc.CreateDimensionValue(ctx, DefaultProjectID, "browser", in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
	for _, in := range []UpdateDimensionInput{{}, {Name: ptr("")}} {
		_, err := svc.UpdateDimension(ctx, DefaultProjectID, "browser", in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
		_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "browser", "chrome", in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}

	// Limits.
	for i := len(dims) + 1; i < maxDimensions; i++ {
		_, err := svc.CreateDimension(ctx, DefaultProjectID, DimensionInput{Key: "d" + strconv.Itoa(i), Name: "D"})
		require.NoError(t, err)
	}
	_, err = svc.CreateDimension(ctx, DefaultProjectID, DimensionInput{Key: "one-more", Name: "D"})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	for i := 1; i < maxDimensionValues; i++ {
		_, err := svc.CreateDimensionValue(ctx, DefaultProjectID, "browser", DimensionInput{Key: "v" + strconv.Itoa(i), Name: "V"})
		require.NoError(t, err)
	}
	_, err = svc.CreateDimensionValue(ctx, DefaultProjectID, "browser", DimensionInput{Key: "one-more", Name: "V"})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))

	// Repository failures surface.
	for _, method := range []string{"ListDimensions", "CreateDimension", "InTx", "LockScope"} {
		repo.errs[method] = errBoom
		_, err := svc.CreateDimension(ctx, 2, DimensionInput{Key: "x", Name: "X"})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	for _, method := range []string{"ListDimensions", "CreateDimensionValue", "LockScope"} {
		repo.errs[method] = errBoom
		_, err := svc.CreateDimensionValue(ctx, DefaultProjectID, "risk", DimensionInput{Key: "x", Name: "X"})
		assert.ErrorIs(t, err, errBoom, method)
		_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "risk", "critical", UpdateDimensionInput{Name: ptr("X")})
		if method == "ListDimensions" {
			assert.ErrorIs(t, err, errBoom, method)
		}
		delete(repo.errs, method)
	}
	repo.errs["UpdateDimension"] = errBoom
	_, err = svc.UpdateDimension(ctx, DefaultProjectID, "risk", UpdateDimensionInput{Name: ptr("X")})
	assert.ErrorIs(t, err, errBoom)
	delete(repo.errs, "UpdateDimension")
	repo.errs["UpdateDimensionValue"] = errBoom
	_, err = svc.UpdateDimensionValue(ctx, DefaultProjectID, "risk", "critical", UpdateDimensionInput{Name: ptr("X")})
	assert.ErrorIs(t, err, errBoom)
}
