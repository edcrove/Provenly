package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Suite limits (mirrored in the OpenAPI contract and the database).
const (
	maxSuiteName        = 100
	maxSuiteDescription = 2000
	// MaxSuiteCases bounds the test cases a static suite lists.
	MaxSuiteCases = 1000
	// MaxClassified bounds the dimension:value pairs of a query (suite or list filter).
	MaxClassified = 10
)

// SuiteKeyPattern is the format of a suite key (the dimension key format).
var SuiteKeyPattern = DimensionKeyPattern

// SuiteKeyMessage is the validation message of a malformed suite key.
const SuiteKeyMessage = "must be 1 to 30 lower-case letters, digits or '-', starting with a letter"

func suiteNotFound(key string) error { return apperr.NotFound("suite %s not found", key) }

// validateQuery checks and normalizes a suite query (pairs sorted, without duplicates); it needs a criterion.
func validateQuery(v *apperr.Validator, q *SuiteQuery) {
	if q.Tag != nil {
		t := strings.ToLower(strings.TrimSpace(*q.Tag))
		q.Tag = &t
		v.Check(TagPattern.MatchString(t), "query.tag", TagMessage)
	}
	pairs := make([]string, 0, len(q.Classified))
	for _, p := range q.Classified {
		dim, value, ok := strings.Cut(p, ":")
		if !ok || !DimensionKeyPattern.MatchString(dim) || !ValueKeyPattern.MatchString(value) {
			v.Check(false, "query.classification", fmt.Sprintf("%q must be a dimension:value pair (e.g. risk:critical)", p))
			continue
		}
		pairs = append(pairs, p)
	}
	slices.Sort(pairs)
	q.Classified = slices.Compact(pairs)
	v.Check(len(q.Classified) <= MaxClassified, "query.classification", fmt.Sprintf("at most %d dimension:value pairs", MaxClassified))
	v.Check(q.Tag != nil || len(q.Classified) > 0, "query", "a query suite needs a tag or a dimension:value pair")
}

func validateSuiteText(v *apperr.Validator, name, description *string) {
	if name != nil {
		*name = strings.TrimSpace(*name)
		v.Check(*name != "", "name", "must not be empty")
		v.Check(validLen(*name, maxSuiteName), "name", fmt.Sprintf("must be at most %d characters", maxSuiteName))
		v.CheckText("name", *name)
	}
	if description != nil {
		v.Check(validLen(*description, maxSuiteDescription), "description", fmt.Sprintf("must be at most %d characters", maxSuiteDescription))
		v.CheckText("description", *description)
	}
}

// checkCases validates a static suite's test cases: unique, bounded and all of the project.
func checkCases(ctx context.Context, r Repository, projectID int64, ids []int64) ([]int64, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > MaxSuiteCases {
		return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: fmt.Sprintf("a suite can list at most %d test cases", MaxSuiteCases)})
	}
	found, err := r.ProjectCaseIDs(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	if len(found) != len(ids) {
		var missing []string
		for _, id := range ids {
			if !slices.Contains(found, id) {
				missing = append(missing, fmt.Sprint(id))
			}
		}
		return nil, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: "not test cases of the project: " + strings.Join(missing, ", ")})
	}
	return ids, nil
}

// Suites returns a project's suites by key.
func (s *Service) Suites(ctx context.Context, projectID int64) ([]Suite, error) {
	return s.repo.ListSuites(ctx, projectID)
}

// Suite returns one suite of a project, with the test cases a static suite lists.
func (s *Service) Suite(ctx context.Context, projectID int64, key string) (Suite, error) {
	su, err := s.repo.GetSuite(ctx, projectID, key)
	if errors.Is(err, ErrNotFound) {
		return Suite{}, suiteNotFound(key)
	}
	return su, err
}

// CreateSuite adds a static suite (with its test cases) or a query suite.
func (s *Service) CreateSuite(ctx context.Context, projectID int64, in SuiteInput) (Suite, error) {
	var v apperr.Validator
	v.Check(SuiteKeyPattern.MatchString(in.Key), "key", SuiteKeyMessage)
	validateSuiteText(&v, &in.Name, &in.Description)
	switch in.Kind {
	case SuiteKindQuery:
		validateQuery(&v, &in.Query)
		v.Check(len(in.TestCaseIDs) == 0, "testCaseIds", "a query suite selects its test cases with its query")
	case SuiteKindStatic:
		v.Check(in.Query.Tag == nil && len(in.Query.Classified) == 0, "query", "a static suite lists its test cases instead")
	default:
		v.Check(false, "kind", "must be one of static, query")
	}
	if err := v.Err(); err != nil {
		return Suite{}, err
	}
	var su Suite
	err := s.repo.InTx(ctx, func(r Repository) error {
		ids, err := checkCases(ctx, r, projectID, in.TestCaseIDs)
		if err != nil {
			return err
		}
		id, err := r.CreateSuite(ctx, projectID, in)
		if errors.Is(err, ErrConflict) {
			return apperr.Conflict("suite %s already exists", in.Key)
		}
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := r.SetSuiteCases(ctx, id, projectID, ids); err != nil {
				return err
			}
		}
		su, err = r.GetSuite(ctx, projectID, in.Key)
		return err
	})
	return su, err
}

// UpdateSuite renames, re-describes, re-queries (query suites), archives or restores a suite.
func (s *Service) UpdateSuite(ctx context.Context, projectID int64, key string, in UpdateSuiteInput) (Suite, error) {
	var v apperr.Validator
	v.Check(in.Name != nil || in.Description != nil || in.Query != nil || in.Archived != nil, "body", "at least one field is required")
	validateSuiteText(&v, in.Name, in.Description)
	if in.Query != nil {
		validateQuery(&v, in.Query)
	}
	if err := v.Err(); err != nil {
		return Suite{}, err
	}
	var su Suite
	err := s.repo.InTx(ctx, func(r Repository) error {
		current, err := r.GetSuite(ctx, projectID, key)
		if errors.Is(err, ErrNotFound) {
			return suiteNotFound(key)
		}
		if err != nil {
			return err
		}
		if in.Query != nil && current.Kind != SuiteKindQuery {
			return apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "query", Message: "a static suite lists its test cases instead"})
		}
		if _, err := r.UpdateSuite(ctx, projectID, key, in); err != nil {
			return err
		}
		su, err = r.GetSuite(ctx, projectID, key)
		return err
	})
	return su, err
}

// SetSuiteCases replaces the test cases a static suite lists.
func (s *Service) SetSuiteCases(ctx context.Context, projectID int64, key string, ids []int64) (Suite, error) {
	var su Suite
	err := s.repo.InTx(ctx, func(r Repository) error {
		current, err := r.GetSuite(ctx, projectID, key)
		if errors.Is(err, ErrNotFound) {
			return suiteNotFound(key)
		}
		if err != nil {
			return err
		}
		if current.Kind != SuiteKindStatic {
			return apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: "a query suite selects its test cases with its query"})
		}
		if ids, err = checkCases(ctx, r, projectID, ids); err != nil {
			return err
		}
		if err := r.SetSuiteCases(ctx, current.ID, projectID, ids); err != nil {
			return err
		}
		su, err = r.GetSuite(ctx, projectID, key)
		return err
	})
	return su, err
}

// SuiteFilter narrows f to a suite's test cases (the members of a static suite, the matches of a query suite).
func SuiteFilter(f ListFilter, su Suite) ListFilter {
	if su.Kind == SuiteKindStatic {
		f.SuiteID = &su.ID
		return f
	}
	f.Tag, f.Classified = su.Query.Tag, su.Query.Classified
	return f
}

// SuiteSelection resolves a suite for a run: the active automated test cases it selects now, ascending. An archived
// suite receives no runs (409).
func (s *Service) SuiteSelection(ctx context.Context, projectID int64, key string) (Suite, []int64, error) {
	su, err := s.Suite(ctx, projectID, key)
	if err != nil {
		return Suite{}, nil, err
	}
	if su.ArchivedAt != nil {
		return Suite{}, nil, apperr.Conflict("suite %s is archived: restore it to report runs for it", key)
	}
	active, automated := StatusActive, true
	ids, err := s.repo.ListTestCaseIDs(ctx, SuiteFilter(ListFilter{Status: &active, Automated: &automated, ProjectIDs: []int64{projectID}}, su))
	return su, ids, err
}
