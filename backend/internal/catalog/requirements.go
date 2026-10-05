package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Requirement limits and formats (mirrored in the OpenAPI contract and the database).
const (
	maxRequirementTitle  = 300
	maxRequirementURL    = 2000
	maxProviderStatus    = 50
	maxRequirementLinks  = 1000
	MaxRequirementImport = 500
)

// ExternalIDPattern is the format of a requirement's id in its provider (e.g. PAY-123, #42, R-7).
var ExternalIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._#/-]{0,99}$`)

var externalProviders = []string{ProviderJira, ProviderGitHub, ProviderAzureDevOps}

// Coverage statuses of a requirement, from the latest result of each test case that covers it.
const (
	CoverageUncovered = "uncovered" // no test case covers it
	CoverageNotRun    = "not_run"   // covered, but no covering test case has a result yet
	CoverageFailing   = "failing"   // a covering test case failed or errored in its latest result
	CoveragePartial   = "partial"   // nothing failing, but not every covering test case passed
	CoveragePassing   = "passing"   // every covering test case passed in its latest result
)

// Coverage summarizes the latest results of the test cases that cover a requirement.
type Coverage struct {
	Status string
	Linked int32
	Passed int32
	Failed int32
	NotRun int32
	// Latest is the latest status of each covering test case ("" when it has no result).
	Latest map[int64]string
}

// RequirementView is a requirement with its coverage.
type RequirementView struct {
	Requirement
	Coverage Coverage
}

// ResultReader reads the latest result of test cases (the execution module, injected to keep the catalog
// independent of it).
type ResultReader interface {
	// LatestStatuses returns the status of each test case in the latest run that has a result for it (passed,
	// failed, error or skipped); test cases without results are absent.
	LatestStatuses(ctx context.Context, testCaseIDs []int64) (map[int64]string, error)
}

// noResults is the reader of a catalog without an execution module: nothing has run.
type noResults struct{}

func (noResults) LatestStatuses(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{}, nil
}

// SetResults gives the service the reader of latest results used for requirement coverage.
func (s *Service) SetResults(r ResultReader) { s.results = r }

func (s *Service) resultReader() ResultReader {
	if s.results == nil {
		return noResults{}
	}
	return s.results
}

func requirementNotFound(id int64) error { return apperr.NotFound("requirement %d not found", id) }

// coverage computes the coverage of a requirement from the latest statuses.
func coverage(linked []int64, latest map[int64]string) Coverage {
	c := Coverage{Linked: int32(len(linked)), Latest: make(map[int64]string, len(linked))}
	for _, id := range linked {
		st := latest[id]
		c.Latest[id] = st
		switch st {
		case "passed":
			c.Passed++
		case "failed", "error":
			c.Failed++
		default:
			c.NotRun++
		}
	}
	switch {
	case c.Linked == 0:
		c.Status = CoverageUncovered
	case c.Failed > 0:
		c.Status = CoverageFailing
	case c.Passed == c.Linked:
		c.Status = CoveragePassing
	case !anyResult(latest, linked):
		c.Status = CoverageNotRun
	default:
		c.Status = CoveragePartial
	}
	return c
}

func anyResult(latest map[int64]string, ids []int64) bool {
	for _, id := range ids {
		if latest[id] != "" {
			return true
		}
	}
	return false
}

// withCoverage adds coverage to requirements with one read of latest results.
func (s *Service) withCoverage(ctx context.Context, reqs []Requirement) ([]RequirementView, error) {
	var ids []int64
	for _, r := range reqs {
		ids = append(ids, r.TestCaseIDs...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	latest := map[int64]string{}
	if len(ids) > 0 {
		var err error
		if latest, err = s.resultReader().LatestStatuses(ctx, ids); err != nil {
			return nil, err
		}
	}
	out := make([]RequirementView, len(reqs))
	for i, r := range reqs {
		out[i] = RequirementView{Requirement: r, Coverage: coverage(r.TestCaseIDs, latest)}
	}
	return out, nil
}

// Requirements returns a project's requirements with their coverage, newest first (only those a test case covers
// when testCaseID is set).
func (s *Service) Requirements(ctx context.Context, projectID int64, testCaseID *int64) ([]RequirementView, error) {
	reqs, err := s.repo.ListRequirements(ctx, projectID, testCaseID)
	if err != nil {
		return nil, err
	}
	return s.withCoverage(ctx, reqs)
}

// Requirement returns one requirement of a project with its coverage.
func (s *Service) Requirement(ctx context.Context, projectID, id int64) (RequirementView, error) {
	r, err := s.repo.GetRequirement(ctx, projectID, id)
	if errors.Is(err, ErrNotFound) {
		return RequirementView{}, requirementNotFound(id)
	}
	if err != nil {
		return RequirementView{}, err
	}
	views, err := s.withCoverage(ctx, []Requirement{r})
	if err != nil {
		return RequirementView{}, err
	}
	return views[0], nil
}

func validateRequirementText(v *apperr.Validator, field string, title, description, url, providerStatus *string) {
	if title != nil {
		*title = strings.TrimSpace(*title)
		v.Check(*title != "", field+"title", "must not be empty")
		v.Check(validLen(*title, maxRequirementTitle), field+"title", fmt.Sprintf("must be at most %d characters", maxRequirementTitle))
		v.CheckText(field+"title", *title)
	}
	if description != nil {
		v.Check(validLen(*description, maxLongText), field+"description", fmt.Sprintf("must be at most %d characters", maxLongText))
		v.CheckText(field+"description", *description)
	}
	if url != nil {
		*url = strings.TrimSpace(*url)
		v.Check(*url == "" || strings.HasPrefix(*url, "https://") || strings.HasPrefix(*url, "http://"), field+"url", "must be an http(s) URL")
		v.Check(validLen(*url, maxRequirementURL), field+"url", fmt.Sprintf("must be at most %d characters", maxRequirementURL))
		v.CheckText(field+"url", *url)
	}
	if providerStatus != nil {
		*providerStatus = strings.TrimSpace(*providerStatus)
		v.Check(validLen(*providerStatus, maxProviderStatus), field+"providerStatus", fmt.Sprintf("must be at most %d characters", maxProviderStatus))
		v.CheckText(field+"providerStatus", *providerStatus)
	}
}

// CreateRequirement adds a native requirement (provider provenly, numbered R-<n>) or registers one of an external
// tool by its external id (409 when it is already there).
func (s *Service) CreateRequirement(ctx context.Context, projectID int64, in RequirementInput) (RequirementView, error) {
	if in.Provider == "" {
		in.Provider = ProviderProvenly
	}
	var v apperr.Validator
	native := in.Provider == ProviderProvenly
	v.Check(native || slices.Contains(externalProviders, in.Provider), "provider", "must be one of provenly, jira, github, azure_devops")
	if native {
		v.Check(in.ExternalID == "", "externalId", "is assigned by Provenly for native requirements")
	} else {
		v.Check(ExternalIDPattern.MatchString(in.ExternalID), "externalId", "must be the requirement's id in its provider (e.g. PAY-123)")
	}
	validateRequirementText(&v, "", &in.Title, &in.Description, &in.URL, &in.ProviderStatus)
	if err := v.Err(); err != nil {
		return RequirementView{}, err
	}
	var id int64
	err := s.repo.InTx(ctx, func(r Repository) error {
		if native {
			n, err := r.NextNativeRequirementNumber(ctx, projectID)
			if err != nil {
				return err
			}
			in.ExternalID = "R-" + strconv.FormatInt(n, 10)
		}
		var ok bool
		var err error
		id, _, ok, err = r.UpsertRequirement(ctx, projectID, in, false, nil)
		if err == nil && !ok {
			return apperr.Conflict("requirement %s of %s already exists", in.ExternalID, in.Provider)
		}
		return err
	})
	if err != nil {
		return RequirementView{}, err
	}
	return s.Requirement(ctx, projectID, id)
}

// ImportResult counts what an import did.
type ImportResult struct {
	Created int
	Updated int
}

// ImportRequirements mirrors requirements of an external tool (read-only sync): each one is created or updated by
// its external id and marked synced now. Links to test cases and archiving are kept.
func (s *Service) ImportRequirements(ctx context.Context, projectID int64, provider string, items []RequirementInput) (ImportResult, error) {
	now := time.Now().UTC()
	var v apperr.Validator
	v.Check(slices.Contains(externalProviders, provider), "provider", "must be one of jira, github, azure_devops")
	v.Check(len(items) >= 1 && len(items) <= MaxRequirementImport, "items", fmt.Sprintf("must hold 1 to %d requirements", MaxRequirementImport))
	seen := map[string]bool{}
	for i := range items {
		field := fmt.Sprintf("items[%d].", i)
		in := &items[i]
		in.Provider = provider
		v.Check(ExternalIDPattern.MatchString(in.ExternalID), field+"externalId", "must be the requirement's id in its provider")
		v.Check(!seen[in.ExternalID], field+"externalId", "is repeated")
		seen[in.ExternalID] = true
		validateRequirementText(&v, field, &in.Title, &in.Description, &in.URL, &in.ProviderStatus)
	}
	if err := v.Err(); err != nil {
		return ImportResult{}, err
	}
	var res ImportResult
	err := s.repo.InTx(ctx, func(r Repository) error {
		for _, in := range items {
			_, created, _, err := r.UpsertRequirement(ctx, projectID, in, true, &now)
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

// UpdateRequirement edits, archives or restores a requirement.
func (s *Service) UpdateRequirement(ctx context.Context, projectID, id int64, in UpdateRequirementInput) (RequirementView, error) {
	var v apperr.Validator
	v.Check(in.Title != nil || in.Description != nil || in.URL != nil || in.ProviderStatus != nil || in.Archived != nil, "body", "at least one field is required")
	validateRequirementText(&v, "", in.Title, in.Description, in.URL, in.ProviderStatus)
	if err := v.Err(); err != nil {
		return RequirementView{}, err
	}
	if err := s.repo.UpdateRequirement(ctx, projectID, id, in); err != nil {
		if errors.Is(err, ErrNotFound) {
			return RequirementView{}, requirementNotFound(id)
		}
		return RequirementView{}, err
	}
	return s.Requirement(ctx, projectID, id)
}

// SetRequirementTestCases replaces the test cases (of the same project) that cover a requirement.
func (s *Service) SetRequirementTestCases(ctx context.Context, projectID, id int64, ids []int64) (RequirementView, error) {
	err := s.repo.InTx(ctx, func(r Repository) error {
		if _, err := r.GetRequirement(ctx, projectID, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return requirementNotFound(id)
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
		return r.SetRequirementTestCases(ctx, id, projectID, checked)
	})
	if err != nil {
		return RequirementView{}, err
	}
	return s.Requirement(ctx, projectID, id)
}
