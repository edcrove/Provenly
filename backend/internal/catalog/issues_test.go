package catalog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// The base matrix (DEC-8) per link and the worst-wins aggregate.
func TestVerify(t *testing.T) {
	for _, c := range []struct{ state, evidence, want string }{
		{IssueOpen, "", VerificationUnverified}, {IssueClosed, "", VerificationUnverified},
		{IssueOpen, "failed", VerificationKnownIssue}, {IssueOpen, "error", VerificationKnownIssue},
		{IssueClosed, "failed", VerificationReopen}, {IssueOpen, "passed", VerificationNotReproducible},
		{IssueClosed, "passed", VerificationValidatedFixed},
	} {
		assert.Equal(t, c.want, linkVerification(c.state, c.evidence), "%s %s", c.state, c.evidence)
	}
	assert.Equal(t, Verification{Status: VerificationUnlinked, Links: []LinkVerification{}}, verify(IssueOpen, nil, nil, nil, nil))
	run := int64(7)
	got := verify(IssueClosed, []int64{1, 2, 3}, map[int64]string{1: "passed", 2: "failed"}, map[int64]int64{1: 7, 2: 7}, map[int64]string{1: "skipped", 2: "failed"})
	assert.Equal(t, VerificationReopen, got.Status)
	assert.Equal(t, LinkVerification{TestCaseID: 1, Status: VerificationValidatedFixed, Evidence: "passed", EvidenceRunID: &run, LatestInconclusive: true}, got.Links[0])
	assert.Equal(t, LinkVerification{TestCaseID: 3, Status: VerificationUnverified}, got.Links[2])
	assert.Equal(t, VerificationUnverified, verify(IssueClosed, []int64{1, 3}, map[int64]string{1: "passed"}, nil, nil).Status)
	assert.Equal(t, VerificationValidatedFixed, verify(IssueClosed, []int64{1}, map[int64]string{1: "passed"}, nil, nil).Status)
	assert.Equal(t, VerificationKnownIssue, verify(IssueOpen, []int64{1, 2}, map[int64]string{1: "passed", 2: "error"}, nil, nil).Status)
}

func TestIssuesLifecycle(t *testing.T) {
	svc, repo, ctx := setup(t)
	results := &fakeResults{latest: map[int64]string{}, conclusive: map[int64]string{}, runs: map[int64]int64{}}
	a, _ := svc.Create(ctx, CreateInput{Title: "a"})
	b, _ := svc.Create(ctx, CreateInput{Title: "b"})

	// Without a results reader nothing has run.
	i1, err := svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Title: " Refund twice ", URL: " https://x.test/1 "})
	require.NoError(t, err)
	assert.Equal(t, "I-1", i1.ExternalID)
	assert.Equal(t, IssueOpen, i1.State)
	assert.Equal(t, "Refund twice", i1.Title)
	assert.Equal(t, VerificationUnlinked, i1.Verification.Status)
	i1, err = svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, []int64{b.ID, a.ID, a.ID})
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, b.ID}, i1.TestCaseIDs)
	assert.Equal(t, VerificationUnverified, i1.Verification.Status)

	svc.SetResults(results)
	results.conclusive = map[int64]string{a.ID: "failed", b.ID: "passed"}
	i1, err = svc.Issue(ctx, DefaultProjectID, i1.ID)
	require.NoError(t, err)
	assert.Equal(t, VerificationKnownIssue, i1.Verification.Status)
	assert.Equal(t, []int64{a.ID, b.ID}, results.asked)
	i1, err = svc.UpdateIssue(ctx, DefaultProjectID, i1.ID, UpdateIssueInput{State: ptr(IssueClosed)})
	require.NoError(t, err)
	assert.Equal(t, VerificationReopen, i1.Verification.Status)
	assert.NotNil(t, i1.ClosedAt)

	gh, err := svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Provider: ProviderGitHub, ExternalID: "12", Title: "Crash", State: IssueClosed})
	require.NoError(t, err)
	assert.Equal(t, IssueClosed, gh.State)
	_, err = svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Provider: ProviderGitHub, ExternalID: "12", Title: "again"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))
	i2, err := svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Title: "Second"})
	require.NoError(t, err)
	assert.Equal(t, "I-2", i2.ExternalID)

	list, err := svc.Issues(ctx, DefaultProjectID, IssueFilter{})
	require.NoError(t, err)
	assert.Equal(t, []int64{i2.ID, gh.ID, i1.ID}, []int64{list[0].ID, list[1].ID, list[2].ID})
	open, err := svc.Issues(ctx, DefaultProjectID, IssueFilter{State: ptr(IssueOpen)})
	require.NoError(t, err)
	assert.Len(t, open, 1)
	linked, err := svc.Issues(ctx, DefaultProjectID, IssueFilter{TestCaseID: &a.ID})
	require.NoError(t, err)
	require.Len(t, linked, 1)
	assert.Equal(t, i1.ID, linked[0].ID)
	_, err = svc.Issues(ctx, DefaultProjectID, IssueFilter{State: ptr("done")})
	assert.Equal(t, "state", fieldOf(t, err))

	// Import mirrors by external id with its state; links are kept.
	_, err = svc.SetIssueTestCases(ctx, DefaultProjectID, gh.ID, []int64{a.ID})
	require.NoError(t, err)
	res, err := svc.ImportIssues(ctx, DefaultProjectID, ProviderGitHub, []IssueInput{
		{ExternalID: "12", Title: "Crash v2", State: IssueOpen, ProviderStatus: "reopened"}, {ExternalID: "13", Title: "Slow", State: IssueOpen},
	})
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Created: 1, Updated: 1}, res)
	gh, _ = svc.Issue(ctx, DefaultProjectID, gh.ID)
	assert.Equal(t, "Crash v2", gh.Title)
	assert.Equal(t, IssueOpen, gh.State)
	assert.Nil(t, gh.ClosedAt)
	assert.NotNil(t, gh.LastSyncedAt)
	assert.Equal(t, []int64{a.ID}, gh.TestCaseIDs)

	gh, err = svc.UpdateIssue(ctx, DefaultProjectID, gh.ID, UpdateIssueInput{Title: ptr("t"), Description: ptr("d"), URL: ptr(""), ProviderStatus: ptr("s")})
	require.NoError(t, err)
	assert.Equal(t, "d", gh.Description)
	_, err = svc.UpdateIssue(ctx, DefaultProjectID, 999, UpdateIssueInput{Title: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.Issue(ctx, DefaultProjectID, 999)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.SetIssueTestCases(ctx, DefaultProjectID, 999, nil)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	chk, _ := svc.CreateProject(ctx, CreateProjectInput{Key: "CHK", Name: "Checkout"})
	other, _ := svc.Create(ctx, CreateInput{ProjectID: chk.ID, Title: "other"})
	_, err = svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, []int64{other.ID})
	assert.Equal(t, "testCaseIds", fieldOf(t, err))
	_, err = svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, make([]int64, 1001))
	assert.Equal(t, "testCaseIds", fieldOf(t, err))

	// Repository and reader failures surface.
	results.errConcl = errBoom
	_, err = svc.Issues(ctx, DefaultProjectID, IssueFilter{})
	assert.ErrorIs(t, err, errBoom)
	results.errConcl, results.err = nil, errBoom
	_, err = svc.Issue(ctx, DefaultProjectID, i1.ID)
	assert.ErrorIs(t, err, errBoom)
	results.err = nil
	for method, call := range map[string]func() error{
		"ListIssues":       func() error { _, err := svc.Issues(ctx, DefaultProjectID, IssueFilter{}); return err },
		"ListTestCaseKeys": func() error { _, err := svc.Issues(ctx, DefaultProjectID, IssueFilter{}); return err },
		"GetIssue":         func() error { _, err := svc.Issue(ctx, DefaultProjectID, i1.ID); return err },
		"NextNativeIssueNumber": func() error {
			_, err := svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Title: "x"})
			return err
		},
		"UpsertIssue": func() error {
			_, err := svc.ImportIssues(ctx, DefaultProjectID, ProviderJira, []IssueInput{{ExternalID: "J-1", Title: "x", State: IssueOpen}})
			return err
		},
		"UpdateIssue": func() error {
			_, err := svc.UpdateIssue(ctx, DefaultProjectID, i1.ID, UpdateIssueInput{Title: ptr("x")})
			return err
		},
		"SetIssueTestCases": func() error {
			_, err := svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, []int64{a.ID})
			return err
		},
		"ProjectCaseIDs": func() error {
			_, err := svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, []int64{a.ID})
			return err
		},
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
	repo.errs["GetIssue"] = errBoom
	_, err = svc.SetIssueTestCases(ctx, DefaultProjectID, i1.ID, nil)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.CreateIssue(ctx, DefaultProjectID, IssueInput{Title: "x"})
	assert.ErrorIs(t, err, errBoom, "reading the created issue")
}

func TestIssueValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	for _, c := range []struct {
		in    IssueInput
		field string
	}{
		{IssueInput{Title: " "}, "title"},
		{IssueInput{Title: strings.Repeat("t", 301)}, "title"},
		{IssueInput{Title: "x", URL: "ftp://x"}, "url"},
		{IssueInput{Title: "x", State: "done"}, "state"},
		{IssueInput{Title: "x", ExternalID: "I-9"}, "externalId"},
		{IssueInput{Title: "x", Provider: "trello", ExternalID: "1"}, "provider"},
		{IssueInput{Title: "x", Provider: ProviderJira}, "externalId"},
	} {
		_, err := svc.CreateIssue(ctx, DefaultProjectID, c.in)
		assert.Equal(t, c.field, fieldOf(t, err), "%+v", c.in)
	}
	for _, c := range []struct {
		provider string
		items    []IssueInput
		field    string
	}{
		{ProviderProvenly, []IssueInput{{ExternalID: "1", Title: "x", State: IssueOpen}}, "provider"},
		{ProviderJira, nil, "items"},
		{ProviderJira, []IssueInput{{ExternalID: "1", Title: "x", State: IssueOpen}, {ExternalID: "1", Title: "y", State: IssueOpen}}, "items[1].externalId"},
		{ProviderJira, []IssueInput{{ExternalID: "1", Title: "x"}}, "items[0].state"},
		{ProviderJira, []IssueInput{{ExternalID: "1", Title: "", State: IssueOpen}}, "items[0].title"},
	} {
		_, err := svc.ImportIssues(ctx, DefaultProjectID, c.provider, c.items)
		assert.Equal(t, c.field, fieldOf(t, err), "%+v", c.items)
	}
	big := make([]IssueInput, MaxIssueImport+1)
	for i := range big {
		big[i] = IssueInput{ExternalID: strconv.Itoa(i + 1), Title: "x", State: IssueOpen}
	}
	_, err := svc.ImportIssues(ctx, DefaultProjectID, ProviderJira, big)
	assert.Equal(t, "items", fieldOf(t, err))
	for _, in := range []UpdateIssueInput{{}, {Title: ptr("")}, {State: ptr("x")}} {
		_, err := svc.UpdateIssue(ctx, DefaultProjectID, 1, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
}
