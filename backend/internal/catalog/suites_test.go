package catalog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
)

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, apperr.KindValidation, e.Kind, "%v", err)
	return e.Fields[0].Field
}

func TestSuitesLifecycle(t *testing.T) {
	svc, repo, ctx := setup(t)
	a, _ := svc.Create(ctx, CreateInput{Title: "a", Automated: true, Tags: []string{"smoke"}})
	b, _ := svc.Create(ctx, CreateInput{Title: "b", Automated: true, Tags: []string{"smoke"}, Classification: map[string]string{"risk": "critical"}})
	manual, _ := svc.Create(ctx, CreateInput{Title: "manual", Tags: []string{"smoke"}})
	gone, _ := svc.Create(ctx, CreateInput{Title: "gone", Automated: true, Tags: []string{"smoke"}})
	_, _ = svc.Deprecate(ctx, gone.ID, etag.Match{})
	chk, _ := svc.CreateProject(ctx, CreateProjectInput{Key: "CHK", Name: "Checkout"})
	other, _ := svc.Create(ctx, CreateInput{ProjectID: chk.ID, Title: "other"})

	// Static: listed test cases, deduplicated; only the project's.
	st, err := svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "release", Name: " Release ", Kind: SuiteKindStatic,
		TestCaseIDs: []int64{b.ID, a.ID, b.ID, manual.ID, gone.ID}})
	require.NoError(t, err)
	assert.Equal(t, "Release", st.Name)
	assert.Equal(t, []int64{a.ID, b.ID, manual.ID, gone.ID}, st.CaseIDs)
	_, err = svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "x", Name: "x", Kind: SuiteKindStatic, TestCaseIDs: []int64{other.ID, 999}})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))
	assert.ErrorContains(t, err, "request validation failed")
	_, err = svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "release", Name: "again", Kind: SuiteKindStatic})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))

	// Query: a tag and pairs, normalized.
	q, err := svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "critical", Name: "Critical", Kind: SuiteKindQuery,
		Query: SuiteQuery{Tag: ptr(" Smoke "), Classified: []string{"risk:critical", "risk:critical"}}})
	require.NoError(t, err)
	assert.Equal(t, SuiteQuery{Tag: ptr("smoke"), Classified: []string{"risk:critical"}}, q.Query)

	// A run's selection: active automated test cases the suite selects.
	_, ids, err := svc.SuiteSelection(ctx, DefaultProjectID, "release")
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, b.ID}, ids)
	_, ids, err = svc.SuiteSelection(ctx, DefaultProjectID, "critical")
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, ids)
	_, _, err = svc.SuiteSelection(ctx, DefaultProjectID, "nope")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	// Members are replaced; query suites have none to replace.
	st, err = svc.SetSuiteCases(ctx, DefaultProjectID, "release", []int64{b.ID})
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, st.CaseIDs)
	_, err = svc.SetSuiteCases(ctx, DefaultProjectID, "critical", []int64{b.ID})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))
	_, err = svc.SetSuiteCases(ctx, DefaultProjectID, "nope", nil)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.SetSuiteCases(ctx, DefaultProjectID, "release", []int64{other.ID})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))

	// Updates: rename, re-query, archive (no runs) and restore.
	q, err = svc.UpdateSuite(ctx, DefaultProjectID, "critical", UpdateSuiteInput{Name: ptr("Critical smoke"), Description: ptr("d"),
		Query: &SuiteQuery{Classified: []string{"risk:high"}}, Archived: ptr(true)})
	require.NoError(t, err)
	assert.Equal(t, "Critical smoke", q.Name)
	assert.Nil(t, q.Query.Tag)
	assert.NotNil(t, q.ArchivedAt)
	_, _, err = svc.SuiteSelection(ctx, DefaultProjectID, "critical")
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	q, err = svc.UpdateSuite(ctx, DefaultProjectID, "critical", UpdateSuiteInput{Archived: ptr(false)})
	require.NoError(t, err)
	assert.Nil(t, q.ArchivedAt)
	_, err = svc.UpdateSuite(ctx, DefaultProjectID, "release", UpdateSuiteInput{Query: &SuiteQuery{Tag: ptr("x")}})
	assert.Equal(t, "query", fieldOf(t, err))
	_, err = svc.UpdateSuite(ctx, DefaultProjectID, "nope", UpdateSuiteInput{Name: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	list, err := svc.Suites(ctx, DefaultProjectID)
	require.NoError(t, err)
	assert.Equal(t, []string{"critical", "release"}, []string{list[0].Key, list[1].Key})
	assert.Equal(t, int32(1), list[1].CaseCount)
	got, err := svc.Suite(ctx, DefaultProjectID, "release")
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, got.CaseIDs)
	_, err = svc.Suite(ctx, DefaultProjectID, "nope")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	// SuiteFilter narrows a list to the suite.
	assert.Equal(t, &got.ID, SuiteFilter(ListFilter{}, got).SuiteID)
	assert.Equal(t, []string{"risk:high"}, SuiteFilter(ListFilter{}, q).Classified)

	// Repository failures surface.
	for _, method := range []string{"InTx", "ProjectCaseIDs", "CreateSuite", "SetSuiteCases", "GetSuite"} {
		repo.errs[method] = errBoom
		_, err := svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "f" + strings.ToLower(method), Name: "x", Kind: SuiteKindStatic, TestCaseIDs: []int64{a.ID}})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	for _, method := range []string{"GetSuite", "UpdateSuite"} {
		repo.errs[method] = errBoom
		_, err := svc.UpdateSuite(ctx, DefaultProjectID, "release", UpdateSuiteInput{Name: ptr("x")})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	for _, method := range []string{"GetSuite", "ProjectCaseIDs", "SetSuiteCases"} {
		repo.errs[method] = errBoom
		_, err := svc.SetSuiteCases(ctx, DefaultProjectID, "release", []int64{a.ID})
		assert.ErrorIs(t, err, errBoom, method)
		delete(repo.errs, method)
	}
	repo.errs["ListTestCaseIDs"] = errBoom
	_, _, err = svc.SuiteSelection(ctx, DefaultProjectID, "release")
	assert.ErrorIs(t, err, errBoom)
}

func TestSuiteValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	bad := []struct {
		in    SuiteInput
		field string
	}{
		{SuiteInput{Key: "Bad", Name: "x", Kind: SuiteKindStatic}, "key"},
		{SuiteInput{Key: "x", Name: " ", Kind: SuiteKindStatic}, "name"},
		{SuiteInput{Key: "x", Name: strings.Repeat("n", 101), Kind: SuiteKindStatic}, "name"},
		{SuiteInput{Key: "x", Name: "x", Description: strings.Repeat("d", 2001), Kind: SuiteKindStatic}, "description"},
		{SuiteInput{Key: "x", Name: "x", Kind: "dynamic"}, "kind"},
		{SuiteInput{Key: "x", Name: "x", Kind: SuiteKindStatic, Query: SuiteQuery{Tag: ptr("a")}}, "query"},
		{SuiteInput{Key: "x", Name: "x", Kind: SuiteKindQuery}, "query"},
		{SuiteInput{Key: "x", Name: "x", Kind: SuiteKindQuery, Query: SuiteQuery{Tag: ptr("a b")}}, "query.tag"},
		{SuiteInput{Key: "x", Name: "x", Kind: SuiteKindQuery, Query: SuiteQuery{Classified: []string{"risk"}}}, "query.classification"},
		{SuiteInput{Key: "x", Name: "x", Kind: SuiteKindQuery, Query: SuiteQuery{Tag: ptr("a")}, TestCaseIDs: []int64{1}}, "testCaseIds"},
	}
	for _, b := range bad {
		_, err := svc.CreateSuite(ctx, DefaultProjectID, b.in)
		assert.Equal(t, b.field, fieldOf(t, err), "%+v", b.in)
	}
	many := make([]string, MaxClassified+1)
	for i := range many {
		many[i] = "d" + strconv.Itoa(i) + ":v"
	}
	_, err := svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "x", Name: "x", Kind: SuiteKindQuery, Query: SuiteQuery{Classified: many}})
	assert.Equal(t, "query.classification", fieldOf(t, err))
	ids := make([]int64, MaxSuiteCases+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	_, err = svc.CreateSuite(ctx, DefaultProjectID, SuiteInput{Key: "x", Name: "x", Kind: SuiteKindStatic, TestCaseIDs: ids})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))
	for _, in := range []UpdateSuiteInput{{}, {Name: ptr("")}, {Query: &SuiteQuery{}}} {
		_, err := svc.UpdateSuite(ctx, DefaultProjectID, "x", in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
}
