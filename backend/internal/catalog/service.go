package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Field limits (mirrored in the OpenAPI contract).
const (
	maxTitle    = 200
	maxLongText = 10000
	maxStepText = 2000
)

// Service holds the catalog use cases. It is the only entry point of the
// module for REST handlers, other modules and future interfaces (MCP).
type Service struct {
	repo Repository
}

// NewService builds a Service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

func notFound(id int64) error { return apperr.NotFound("test case %s not found", FormatKey(id)) }

func mapNotFound(err error, id int64) error {
	if errors.Is(err, ErrNotFound) {
		return notFound(id)
	}
	return err
}

func validLen(s string, limit int) bool { return utf8.RuneCountInString(s) <= limit }

// Create validates and stores a new test case; storage assigns the TC-ID.
func (s *Service) Create(ctx context.Context, in CreateInput) (TestCase, error) {
	in.Title = strings.TrimSpace(in.Title)
	var v apperr.Validator
	v.Check(in.Title != "", "title", "is required")
	v.Check(validLen(in.Title, maxTitle), "title", fmt.Sprintf("must be at most %d characters", maxTitle))
	v.Check(validLen(in.Description, maxLongText), "description", fmt.Sprintf("must be at most %d characters", maxLongText))
	v.Check(validLen(in.ExpectedResult, maxLongText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxLongText))
	if err := v.Err(); err != nil {
		return TestCase{}, err
	}
	return s.repo.CreateTestCase(ctx, in)
}

// Get returns a test case by TC-ID.
func (s *Service) Get(ctx context.Context, id int64) (TestCase, error) {
	tc, err := s.repo.GetTestCase(ctx, id)
	return tc, mapNotFound(err, id)
}

// EnsureExists returns a not-found error when the TC-ID does not exist.
func (s *Service) EnsureExists(ctx context.Context, id int64) error {
	_, err := s.Get(ctx, id)
	return err
}

// List returns a page of test cases, newest TC-ID first.
func (s *Service) List(ctx context.Context, status *Status, page pagination.Page) (pagination.Result[TestCase], error) {
	items, err := s.repo.ListTestCases(ctx, status, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestCase]{}, err
	}
	total, err := s.repo.CountTestCases(ctx, status)
	if err != nil {
		return pagination.Result[TestCase]{}, err
	}
	return pagination.Result[TestCase]{Items: items, Page: page, Total: total}, nil
}

// Update edits content fields. The TC-ID never changes and no version is created.
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (TestCase, error) {
	var v apperr.Validator
	v.Check(in.Title != nil || in.Description != nil || in.ExpectedResult != nil || in.Automated != nil, "body", "at least one field is required")
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
		v.Check(t != "", "title", "must not be empty")
		v.Check(validLen(t, maxTitle), "title", fmt.Sprintf("must be at most %d characters", maxTitle))
	}
	if in.Description != nil {
		v.Check(validLen(*in.Description, maxLongText), "description", fmt.Sprintf("must be at most %d characters", maxLongText))
	}
	if in.ExpectedResult != nil {
		v.Check(validLen(*in.ExpectedResult, maxLongText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxLongText))
	}
	if err := v.Err(); err != nil {
		return TestCase{}, err
	}
	tc, err := s.repo.UpdateTestCase(ctx, id, in)
	return tc, mapNotFound(err, id)
}

// Deprecate marks a test case deprecated (idempotent). Its history is kept and
// it leaves the expected universe of runs created afterwards.
func (s *Service) Deprecate(ctx context.Context, id int64) (TestCase, error) {
	tc, err := s.repo.DeprecateTestCase(ctx, id)
	return tc, mapNotFound(err, id)
}

// Reactivate brings a deprecated test case back to active (idempotent), keeping
// its TC-ID. Existing run snapshots are immutable; future runs include it again
// when it is automated.
func (s *Service) Reactivate(ctx context.Context, id int64) (TestCase, error) {
	tc, err := s.repo.ReactivateTestCase(ctx, id)
	return tc, mapNotFound(err, id)
}

// ExpectedUniverse returns the TC-IDs that are active and automated right now.
func (s *Service) ExpectedUniverse(ctx context.Context) ([]int64, error) {
	return s.repo.ListExpectedUniverse(ctx)
}

// Statuses returns the status of each existing TC-ID among ids; unknown ids are absent.
func (s *Service) Statuses(ctx context.Context, ids []int64) (map[int64]Status, error) {
	if len(ids) == 0 {
		return map[int64]Status{}, nil
	}
	return s.repo.ListTestCaseStatuses(ctx, ids)
}

// ListSteps returns a page of steps ordered by position.
func (s *Service) ListSteps(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[TestStep], error) {
	if err := s.EnsureExists(ctx, testCaseID); err != nil {
		return pagination.Result[TestStep]{}, err
	}
	items, err := s.repo.ListTestSteps(ctx, testCaseID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestStep]{}, err
	}
	total, err := s.repo.CountTestSteps(ctx, testCaseID)
	if err != nil {
		return pagination.Result[TestStep]{}, err
	}
	return pagination.Result[TestStep]{Items: items, Page: page, Total: total}, nil
}

func validateStepText(v *apperr.Validator, action, expected *string) {
	if action != nil {
		v.Check(strings.TrimSpace(*action) != "", "action", "must not be empty")
		v.Check(validLen(*action, maxStepText), "action", fmt.Sprintf("must be at most %d characters", maxStepText))
	}
	if expected != nil {
		v.Check(validLen(*expected, maxStepText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxStepText))
	}
}

// CreateStep inserts a step at the requested position (clamped to the end) or appends it.
func (s *Service) CreateStep(ctx context.Context, testCaseID int64, in CreateStepInput) (TestStep, error) {
	var v apperr.Validator
	validateStepText(&v, &in.Action, &in.ExpectedResult)
	if in.Position != nil {
		v.Check(*in.Position >= 1, "position", "must be >= 1")
	}
	if err := v.Err(); err != nil {
		return TestStep{}, err
	}
	var step TestStep
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := r.LockTestCase(ctx, testCaseID); err != nil {
			return mapNotFound(err, testCaseID)
		}
		count, err := r.CountTestSteps(ctx, testCaseID)
		if err != nil {
			return err
		}
		if count >= MaxSteps {
			return apperr.Validation("too many steps", apperr.FieldError{Field: "action", Message: fmt.Sprintf("a test case can have at most %d steps", MaxSteps)})
		}
		position := int32(count) + 1
		if in.Position != nil && *in.Position < position {
			position = *in.Position
			if err := r.ShiftTestStepsDown(ctx, testCaseID, position); err != nil {
				return err
			}
		}
		step, err = r.CreateTestStep(ctx, testCaseID, position, in.Action, in.ExpectedResult)
		return err
	})
	return step, err
}

func stepNotFound(testCaseID, stepID int64) error {
	return apperr.NotFound("step %d of test case %s not found", stepID, FormatKey(testCaseID))
}

// UpdateStep edits a step's content. It never affects the TC-ID or historical results.
func (s *Service) UpdateStep(ctx context.Context, testCaseID, stepID int64, in UpdateStepInput) (TestStep, error) {
	var v apperr.Validator
	v.Check(in.Action != nil || in.ExpectedResult != nil, "body", "at least one field is required")
	validateStepText(&v, in.Action, in.ExpectedResult)
	if err := v.Err(); err != nil {
		return TestStep{}, err
	}
	if err := s.EnsureExists(ctx, testCaseID); err != nil {
		return TestStep{}, err
	}
	step, err := s.repo.UpdateTestStep(ctx, testCaseID, stepID, in)
	if errors.Is(err, ErrNotFound) {
		return TestStep{}, stepNotFound(testCaseID, stepID)
	}
	return step, err
}

// DeleteStep removes a step and renumbers the remaining ones contiguously.
func (s *Service) DeleteStep(ctx context.Context, testCaseID, stepID int64) error {
	return s.repo.InTx(ctx, func(r Repository) error {
		if err := r.LockTestCase(ctx, testCaseID); err != nil {
			return mapNotFound(err, testCaseID)
		}
		position, err := r.DeleteTestStep(ctx, testCaseID, stepID)
		if errors.Is(err, ErrNotFound) {
			return stepNotFound(testCaseID, stepID)
		}
		if err != nil {
			return err
		}
		return r.CloseTestStepGap(ctx, testCaseID, position)
	})
}

// ReorderSteps applies a new order; stepIDs must be a permutation of all current steps.
func (s *Service) ReorderSteps(ctx context.Context, testCaseID int64, stepIDs []int64) ([]TestStep, error) {
	var steps []TestStep
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := r.LockTestCase(ctx, testCaseID); err != nil {
			return mapNotFound(err, testCaseID)
		}
		current, err := r.ListAllTestSteps(ctx, testCaseID)
		if err != nil {
			return err
		}
		if !isPermutation(current, stepIDs) {
			return apperr.Validation("invalid step order", apperr.FieldError{Field: "stepIds", Message: "must list every step of the test case exactly once"})
		}
		for i, id := range stepIDs {
			if err := r.SetTestStepPosition(ctx, testCaseID, id, int32(i+1)); err != nil {
				return err
			}
		}
		steps, err = r.ListAllTestSteps(ctx, testCaseID)
		return err
	})
	return steps, err
}

func isPermutation(current []TestStep, ids []int64) bool {
	if len(current) != len(ids) {
		return false
	}
	want := make(map[int64]bool, len(current))
	for _, st := range current {
		want[st.ID] = true
	}
	for _, id := range ids {
		if !want[id] {
			return false
		}
		delete(want, id)
	}
	return true
}
