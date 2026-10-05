package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// MaxIssueImport bounds the issues of one import.
const MaxIssueImport = 500

// Verification statuses (Notion 05 Issue Verification State Machine, DEC-8): QA's view of an issue from its state and
// the latest conclusive result of each linked test case. They never replace the tracker's state or the test result.
const (
	VerificationUnlinked        = "unlinked"         // no test case reproduces the issue
	VerificationUnverified      = "unverified"       // linked, but no conclusive result yet
	VerificationKnownIssue      = "known_issue"      // open and the test fails: the failure is known
	VerificationReopen          = "reopen"           // closed but the test fails again: candidate to reopen
	VerificationNotReproducible = "not_reproducible" // open but the test passes
	VerificationValidatedFixed  = "validated_fixed"  // closed and the test passes
)

// verificationRank orders link verifications for the issue aggregate: the worst one wins.
var verificationRank = map[string]int{
	VerificationValidatedFixed: 0, VerificationNotReproducible: 1, VerificationUnverified: 2,
	VerificationKnownIssue: 3, VerificationReopen: 4,
}

// LinkVerification is the verification of one test case linked to an issue.
type LinkVerification struct {
	TestCaseID int64
	Status     string
	// Evidence is the latest conclusive status of the test case ("" when it has none) and EvidenceRunID its run.
	Evidence      string
	EvidenceRunID *int64
	// LatestInconclusive tells that the latest result of the test case was skipped: the verification kept the previous
	// conclusive evidence.
	LatestInconclusive bool
}

// Verification is an issue's aggregate verification and the verification of each linked test case.
type Verification struct {
	Status string
	Links  []LinkVerification
}

// IssueView is an issue with its verification.
type IssueView struct {
	Issue
	Verification Verification
}

func issueNotFound(id int64) error { return apperr.NotFound("issue %d not found", id) }

// linkVerification applies the base matrix to one linked test case.
func linkVerification(state, evidence string) string {
	switch {
	case evidence == "":
		return VerificationUnverified
	case evidence == "passed" && state == IssueOpen:
		return VerificationNotReproducible
	case evidence == "passed":
		return VerificationValidatedFixed
	case state == IssueOpen:
		return VerificationKnownIssue
	default:
		return VerificationReopen
	}
}

// verify computes an issue's verification from its state and the evidence of its linked test cases.
func verify(state string, linked []int64, conclusive map[int64]string, runs map[int64]int64, latest map[int64]string) Verification {
	v := Verification{Status: VerificationUnlinked, Links: make([]LinkVerification, len(linked))}
	for i, id := range linked {
		l := LinkVerification{TestCaseID: id, Evidence: conclusive[id], LatestInconclusive: latest[id] == "skipped"}
		l.Status = linkVerification(state, l.Evidence)
		if run, ok := runs[id]; ok {
			l.EvidenceRunID = &run
		}
		if i == 0 || verificationRank[l.Status] > verificationRank[v.Status] {
			v.Status = l.Status
		}
		v.Links[i] = l
	}
	return v
}

// withVerification adds verification to issues with one read of the evidence.
func (s *Service) withVerification(ctx context.Context, issues []Issue) ([]IssueView, error) {
	var ids []int64
	for _, is := range issues {
		ids = append(ids, is.TestCaseIDs...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	conclusive, runs, latest := map[int64]string{}, map[int64]int64{}, map[int64]string{}
	if len(ids) > 0 {
		var err error
		if conclusive, runs, err = s.resultReader().LatestConclusive(ctx, ids); err != nil {
			return nil, err
		}
		if latest, err = s.resultReader().LatestStatuses(ctx, ids); err != nil {
			return nil, err
		}
	}
	out := make([]IssueView, len(issues))
	for i, is := range issues {
		out[i] = IssueView{Issue: is, Verification: verify(is.State, is.TestCaseIDs, conclusive, runs, latest)}
	}
	return out, nil
}

// Issues returns a project's issues with their verification, newest first, narrowed by f.
func (s *Service) Issues(ctx context.Context, projectID int64, f IssueFilter) ([]IssueView, error) {
	if f.State != nil && *f.State != IssueOpen && *f.State != IssueClosed {
		return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "state", Message: "must be one of open, closed"})
	}
	issues, err := s.repo.ListIssues(ctx, projectID, f)
	if err != nil {
		return nil, err
	}
	return s.withVerification(ctx, issues)
}

// Issue returns one issue of a project with its verification.
func (s *Service) Issue(ctx context.Context, projectID, id int64) (IssueView, error) {
	is, err := s.repo.GetIssue(ctx, projectID, id)
	if errors.Is(err, ErrNotFound) {
		return IssueView{}, issueNotFound(id)
	}
	if err != nil {
		return IssueView{}, err
	}
	views, err := s.withVerification(ctx, []Issue{is})
	if err != nil {
		return IssueView{}, err
	}
	return views[0], nil
}

func checkState(v *apperr.Validator, field string, state *string) {
	if state != nil {
		v.Check(*state == IssueOpen || *state == IssueClosed, field+"state", "must be one of open, closed")
	}
}

// CreateIssue reports a native issue (provider provenly, numbered I-<n>) or registers one of an external tracker by
// its external id (409 when it is already there). A new issue is open unless said otherwise.
func (s *Service) CreateIssue(ctx context.Context, projectID int64, in IssueInput) (IssueView, error) {
	if in.Provider == "" {
		in.Provider = ProviderProvenly
	}
	if in.State == "" {
		in.State = IssueOpen
	}
	var v apperr.Validator
	native := in.Provider == ProviderProvenly
	v.Check(native || slices.Contains(externalProviders, in.Provider), "provider", "must be one of provenly, jira, github, azure_devops")
	if native {
		v.Check(in.ExternalID == "", "externalId", "is assigned by Provenly for native issues")
	} else {
		v.Check(ExternalIDPattern.MatchString(in.ExternalID), "externalId", "must be the issue's id in its tracker (e.g. PAY-123)")
	}
	checkState(&v, "", &in.State)
	validateRequirementText(&v, "", &in.Title, &in.Description, &in.URL, &in.ProviderStatus)
	if err := v.Err(); err != nil {
		return IssueView{}, err
	}
	var id int64
	err := s.repo.InTx(ctx, func(r Repository) error {
		if native {
			n, err := r.NextNativeIssueNumber(ctx, projectID)
			if err != nil {
				return err
			}
			in.ExternalID = "I-" + strconv.FormatInt(n, 10)
		}
		var ok bool
		var err error
		id, _, ok, err = r.UpsertIssue(ctx, projectID, in, false, nil)
		if err == nil && !ok {
			return apperr.Conflict("issue %s of %s already exists", in.ExternalID, in.Provider)
		}
		return err
	})
	if err != nil {
		return IssueView{}, err
	}
	return s.Issue(ctx, projectID, id)
}

// ImportIssues mirrors issues of an external tracker (read-only sync): each one is created or updated by its external
// id, with its state, and marked synced now. Links to test cases are kept.
func (s *Service) ImportIssues(ctx context.Context, projectID int64, provider string, items []IssueInput) (ImportResult, error) {
	now := time.Now().UTC()
	var v apperr.Validator
	v.Check(slices.Contains(externalProviders, provider), "provider", "must be one of jira, github, azure_devops")
	v.Check(len(items) >= 1 && len(items) <= MaxIssueImport, "items", fmt.Sprintf("must hold 1 to %d issues", MaxIssueImport))
	seen := map[string]bool{}
	for i := range items {
		field := fmt.Sprintf("items[%d].", i)
		in := &items[i]
		in.Provider = provider
		v.Check(ExternalIDPattern.MatchString(in.ExternalID), field+"externalId", "must be the issue's id in its tracker")
		v.Check(!seen[in.ExternalID], field+"externalId", "is repeated")
		seen[in.ExternalID] = true
		checkState(&v, field, &in.State)
		validateRequirementText(&v, field, &in.Title, &in.Description, &in.URL, &in.ProviderStatus)
	}
	if err := v.Err(); err != nil {
		return ImportResult{}, err
	}
	var res ImportResult
	err := s.repo.InTx(ctx, func(r Repository) error {
		for _, in := range items {
			_, created, _, err := r.UpsertIssue(ctx, projectID, in, true, &now)
			if err != nil {
				return err
			}
			if created {
				res.Created++
			} else {
				res.Updated++
			}
		}
		return nil
	})
	return res, err
}

// UpdateIssue edits an issue, closes or reopens it.
func (s *Service) UpdateIssue(ctx context.Context, projectID, id int64, in UpdateIssueInput) (IssueView, error) {
	var v apperr.Validator
	v.Check(in.Title != nil || in.Description != nil || in.URL != nil || in.ProviderStatus != nil || in.State != nil, "body", "at least one field is required")
	checkState(&v, "", in.State)
	validateRequirementText(&v, "", in.Title, in.Description, in.URL, in.ProviderStatus)
	if err := v.Err(); err != nil {
		return IssueView{}, err
	}
	if err := s.repo.UpdateIssue(ctx, projectID, id, in); err != nil {
		if errors.Is(err, ErrNotFound) {
			return IssueView{}, issueNotFound(id)
		}
		return IssueView{}, err
	}
	return s.Issue(ctx, projectID, id)
}

// SetIssueTestCases replaces the test cases (of the same project) linked to an issue.
func (s *Service) SetIssueTestCases(ctx context.Context, projectID, id int64, ids []int64) (IssueView, error) {
	err := s.repo.InTx(ctx, func(r Repository) error {
		if _, err := r.GetIssue(ctx, projectID, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return issueNotFound(id)
			}
			return err
		}
		if len(ids) > maxRequirementLinks {
			return apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: fmt.Sprintf("at most %d test cases", maxRequirementLinks)})
		}
		checked, err := checkCases(ctx, r, projectID, ids)
		if err != nil {
			return err
		}
		return r.SetIssueTestCases(ctx, id, projectID, checked)
	})
	if err != nil {
		return IssueView{}, err
	}
	return s.Issue(ctx, projectID, id)
}
