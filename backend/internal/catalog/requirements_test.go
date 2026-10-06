package catalog

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

type fakeResults struct {
	latest     map[int64]string
	conclusive map[int64]string
	runs       map[int64]int64
	err        error
	errConcl   error
	asked      []int64
}

func (f *fakeResults) LatestConclusive(_ context.Context, ids []int64) (map[int64]string, map[int64]int64, error) {
	f.asked = ids
	return f.conclusive, f.runs, f.errConcl
}

func (f *fakeResults) LatestStatuses(_ context.Context, ids []int64) (map[int64]string, error) {
	f.asked = ids
	return f.latest, f.err
}

func TestCoverage(t *testing.T) {
	cases := []struct {
		linked []int64
		latest map[int64]string
		want   Coverage
	}{
		{nil, nil, Coverage{Status: CoverageUncovered, Latest: map[int64]string{}}},
		{[]int64{1, 2}, map[int64]string{}, Coverage{Status: CoverageNotRun, Linked: 2, NotRun: 2, Latest: map[int64]string{1: "", 2: ""}}},
		{[]int64{1, 2}, map[int64]string{1: "passed", 2: "error"}, Coverage{Status: CoverageFailing, Linked: 2, Passed: 1, Failed: 1, Latest: map[int64]string{1: "passed", 2: "error"}}},
		{[]int64{1, 2}, map[int64]string{1: "passed", 2: "passed"}, Coverage{Status: CoveragePassing, Linked: 2, Passed: 2, Latest: map[int64]string{1: "passed", 2: "passed"}}},
		{[]int64{1, 2}, map[int64]string{1: "passed"}, Coverage{Status: CoveragePartial, Linked: 2, Passed: 1, NotRun: 1, Latest: map[int64]string{1: "passed", 2: ""}}},
		{[]int64{1}, map[int64]string{1: "skipped"}, Coverage{Status: CoveragePartial, Linked: 1, NotRun: 1, Latest: map[int64]string{1: "skipped"}}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, coverage(c.linked, c.latest), "%v %v", c.linked, c.latest)
	}
}

func TestRequirementsLifecycle(t *testing.T) {
	svc, repo, ctx := setup(t)
	results := &fakeResults{latest: map[int64]string{}}
	a, _ := svc.Create(ctx, CreateInput{Title: "a"})
	b, _ := svc.Create(ctx, CreateInput{Title: "b"})

	// Without a results reader nothing has run.
	r1, err := svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Title: " Pay by card ", Description: "d", URL: " https://x.test/1 "})
	require.NoError(t, err)
	assert.Equal(t, "R-1", r1.ExternalID)
	assert.Equal(t, ProviderProvenly, r1.Provider)
	assert.Equal(t, "Pay by card", r1.Title)
	assert.Equal(t, "https://x.test/1", r1.URL)
	assert.Equal(t, CoverageUncovered, r1.Coverage.Status)
	r1, err = svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, []int64{b.ID, a.ID, a.ID})
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, b.ID}, r1.TestCaseIDs)
	assert.Equal(t, CoverageNotRun, r1.Coverage.Status)

	svc.SetResults(results)
	results.latest = map[int64]string{a.ID: "passed", b.ID: "failed"}
	r1, err = svc.Requirement(ctx, DefaultProjectID, r1.ID)
	require.NoError(t, err)
	assert.Equal(t, CoverageFailing, r1.Coverage.Status)
	assert.Equal(t, []int64{a.ID, b.ID}, results.asked)

	r2, err := svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Provider: ProviderJira, ExternalID: "PAY-12", Title: "Refunds"})
	require.NoError(t, err)
	assert.Equal(t, "PAY-12", r2.ExternalID)
	_, err = svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Provider: ProviderJira, ExternalID: "PAY-12", Title: "again"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	r3, err := svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Title: "Second native"})
	require.NoError(t, err)
	assert.Equal(t, "R-2", r3.ExternalID)

	list, err := svc.Requirements(ctx, DefaultProjectID, nil)
	require.NoError(t, err)
	assert.Equal(t, []int64{r3.ID, r2.ID, r1.ID}, []int64{list[0].ID, list[1].ID, list[2].ID})
	covering, err := svc.Requirements(ctx, DefaultProjectID, &a.ID)
	require.NoError(t, err)
	require.Len(t, covering, 1)
	assert.Equal(t, r1.ID, covering[0].ID)

	// Import mirrors external requirements: creates and updates by external id.
	res, err := svc.ImportRequirements(ctx, DefaultProjectID, ProviderJira, []RequirementInput{
		{ExternalID: "PAY-12", Title: "Refunds v2", ProviderStatus: "In Progress"}, {ExternalID: "PAY-13", Title: "Chargebacks"},
	})
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Created: 1, Updated: 1}, res)
	r2, _ = svc.Requirement(ctx, DefaultProjectID, r2.ID)
	assert.Equal(t, "Refunds v2", r2.Title)
	assert.Equal(t, "In Progress", r2.ProviderStatus)
	assert.NotNil(t, r2.LastSyncedAt)

	// Updates and archiving.
	r2, err = svc.UpdateRequirement(ctx, DefaultProjectID, r2.ID, UpdateRequirementInput{Title: ptr("Refunds v3"), Description: ptr("x"), URL: ptr(""), ProviderStatus: ptr("Done"), Archived: ptr(true)})
	require.NoError(t, err)
	assert.Equal(t, "Refunds v3", r2.Title)
	assert.NotNil(t, r2.ArchivedAt)
	_, err = svc.UpdateRequirement(ctx, DefaultProjectID, 999, UpdateRequirementInput{Title: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.Requirement(ctx, DefaultProjectID, 999)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.SetRequirementTestCases(ctx, DefaultProjectID, 999, nil)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	chk, _ := svc.CreateProject(ctx, CreateProjectInput{Key: "CHK", Name: "Checkout"})
	other, _ := svc.Create(ctx, CreateInput{ProjectID: chk.ID, Title: "other"})
	_, err = svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, []int64{other.ID})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))
	many := make([]int64, 1001)
	_, err = svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, many)
	assert.Equal(t, "testCaseIds", fieldOf(t, err))

	// Repository and reader failures surface.
	results.err = errBoom
	_, err = svc.Requirements(ctx, DefaultProjectID, nil)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.Requirement(ctx, DefaultProjectID, r1.ID)
	assert.ErrorIs(t, err, errBoom)
	results.err = nil
	for method, call := range map[string]func() error{
		"ListRequirements": func() error { _, err := svc.Requirements(ctx, DefaultProjectID, nil); return err },
		"ListTestCaseKeys": func() error { _, err := svc.Requirements(ctx, DefaultProjectID, nil); return err },
		"GetRequirement":   func() error { _, err := svc.Requirement(ctx, DefaultProjectID, r1.ID); return err },
		"NextNativeRequirementNumber": func() error {
			_, err := svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Title: "x"})
			return err
		},
		"UpsertRequirement": func() error {
			_, err := svc.ImportRequirements(ctx, DefaultProjectID, ProviderGitHub, []RequirementInput{{ExternalID: "GH-1", Title: "x"}})
			return err
		},
		"UpdateRequirement": func() error {
			_, err := svc.UpdateRequirement(ctx, DefaultProjectID, r1.ID, UpdateRequirementInput{Title: ptr("x")})
			return err
		},
		"SetRequirementTestCases": func() error {
			_, err := svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, []int64{a.ID})
			return err
		},
		"ProjectCaseIDs": func() error {
			_, err := svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, []int64{a.ID})
			return err
		},
		"InTx": func() error {
			_, err := svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Title: "x"})
			return err
		},
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
	repo.errs["GetRequirement"] = errBoom
	_, err = svc.SetRequirementTestCases(ctx, DefaultProjectID, r1.ID, nil)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.CreateRequirement(ctx, DefaultProjectID, RequirementInput{Title: "x"})
	assert.ErrorIs(t, err, errBoom, "reading the created requirement")
}

func TestRequirementValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	for _, c := range []struct {
		in    RequirementInput
		field string
	}{
		{RequirementInput{Title: " "}, "title"},
		{RequirementInput{Title: strings.Repeat("t", 301)}, "title"},
		{RequirementInput{Title: "x", Description: strings.Repeat("d", 10001)}, "description"},
		{RequirementInput{Title: "x", URL: "ftp://x"}, "url"},
		{RequirementInput{Title: "x", URL: "https://" + strings.Repeat("u", 2000)}, "url"},
		{RequirementInput{Title: "x", ProviderStatus: strings.Repeat("s", 51)}, "providerStatus"},
		{RequirementInput{Title: "x", ExternalID: "R-9"}, "externalId"},
		{RequirementInput{Title: "x", Provider: "trello", ExternalID: "1"}, "provider"},
		{RequirementInput{Title: "x", Provider: ProviderJira}, "externalId"},
		{RequirementInput{Title: "x", Provider: ProviderJira, ExternalID: "-bad"}, "externalId"},
		{RequirementInput{Title: "a\x00"}, "title"},
	} {
		_, err := svc.CreateRequirement(ctx, DefaultProjectID, c.in)
		assert.Equal(t, c.field, fieldOf(t, err), "%+v", c.in)
	}
	for _, c := range []struct {
		provider string
		items    []RequirementInput
		field    string
	}{
		{ProviderProvenly, []RequirementInput{{ExternalID: "1", Title: "x"}}, "provider"},
		{ProviderJira, nil, "items"},
		{ProviderJira, []RequirementInput{{ExternalID: "1", Title: "x"}, {ExternalID: "1", Title: "y"}}, "items[1].externalId"},
		{ProviderJira, []RequirementInput{{ExternalID: "", Title: "x"}}, "items[0].externalId"},
		{ProviderJira, []RequirementInput{{ExternalID: "1", Title: ""}}, "items[0].title"},
	} {
		_, err := svc.ImportRequirements(ctx, DefaultProjectID, c.provider, c.items)
		assert.Equal(t, c.field, fieldOf(t, err), "%+v", c.items)
	}
	big := make([]RequirementInput, MaxRequirementImport+1)
	for i := range big {
		big[i] = RequirementInput{ExternalID: strconv.Itoa(i + 1), Title: "x"}
	}
	_, err := svc.ImportRequirements(ctx, DefaultProjectID, ProviderJira, big)
	assert.Equal(t, "items", fieldOf(t, err))
	for _, in := range []UpdateRequirementInput{{}, {Title: ptr("")}, {URL: ptr("x")}} {
		_, err := svc.UpdateRequirement(ctx, DefaultProjectID, 1, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
}
