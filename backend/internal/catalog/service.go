package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Field limits (mirrored in the OpenAPI contract).
const (
	maxTitle       = 200
	maxLongText    = 10000
	maxStepText    = 2000
	maxProjectName = 100
	maxProjectDesc = 2000
)

// ProjectKeyPattern is the format of a project key (mirrored in the OpenAPI contract and the database).
var ProjectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// Service holds the catalog use cases. It is the only entry point of the
// module for REST handlers, other modules and future interfaces (MCP).
type Service struct {
	repo Repository
}

// NewService builds a Service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

func notFound(id int64) error { return apperr.NotFound("test case %d not found", id) }

func projectNotFound(key string) error { return apperr.NotFound("project %s not found", key) }

func mapNotFound(err error, id int64) error {
	if errors.Is(err, ErrNotFound) {
		return notFound(id)
	}
	return err
}

func validLen(s string, limit int) bool { return utf8.RuneCountInString(s) <= limit }

// Create validates and stores a new test case; storage assigns its number in the project.
func (s *Service) Create(ctx context.Context, in CreateInput) (TestCase, error) {
	in.Title = strings.TrimSpace(in.Title)
	var v apperr.Validator
	v.Check(in.Title != "", "title", "is required")
	v.Check(validLen(in.Title, maxTitle), "title", fmt.Sprintf("must be at most %d characters", maxTitle))
	v.Check(validLen(in.Description, maxLongText), "description", fmt.Sprintf("must be at most %d characters", maxLongText))
	v.Check(validLen(in.ExpectedResult, maxLongText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxLongText))
	v.CheckText("title", in.Title)
	v.CheckText("description", in.Description)
	v.CheckText("expectedResult", in.ExpectedResult)
	if err := v.Err(); err != nil {
		return TestCase{}, err
	}
	if in.ProjectID == 0 {
		in.ProjectID = DefaultProjectID
	}
	tc, err := s.repo.CreateTestCase(ctx, in)
	if errors.Is(err, ErrNotFound) {
		return TestCase{}, apperr.NotFound("project %d not found", in.ProjectID)
	}
	return tc, err
}

// Get returns a test case by TC-ID.
func (s *Service) Get(ctx context.Context, id int64) (TestCase, error) {
	tc, err := s.repo.GetTestCase(ctx, id)
	return tc, mapNotFound(err, id)
}

// ProjectOf returns the project of a test case.
func (s *Service) ProjectOf(ctx context.Context, id int64) (int64, error) {
	tc, err := s.Get(ctx, id)
	return tc.ProjectID, err
}

// EnsureExists returns a not-found error when the TC-ID does not exist.
func (s *Service) EnsureExists(ctx context.Context, id int64) error {
	_, err := s.Get(ctx, id)
	return err
}

// List returns a page of test cases, newest first.
func (s *Service) List(ctx context.Context, f ListFilter, page pagination.Page) (pagination.Result[TestCase], error) {
	items, err := s.repo.ListTestCases(ctx, f, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestCase]{}, err
	}
	total, err := s.repo.CountTestCases(ctx, f)
	if err != nil {
		return pagination.Result[TestCase]{}, err
	}
	return pagination.Result[TestCase]{Items: items, Page: page, Total: total}, nil
}

// changed is the error of a write whose If-Match no longer matches the test case.
func changed(id, version int64) error {
	return apperr.PreconditionFailed("test case %d changed since you read it (it is now at version %d): reload it and apply your change again", id, version)
}

// guarded runs a write on a test case in a transaction that locks it and checks the client's If-Match first
// (optimistic locking, MVP D7). It returns the test case version after the write.
func (s *Service) guarded(ctx context.Context, id int64, m etag.Match, write func(Repository) error) (int64, error) {
	var version int64
	err := s.repo.InTx(ctx, func(r Repository) error {
		v, err := r.LockTestCase(ctx, id)
		if err != nil {
			return mapNotFound(err, id)
		}
		if !m.Matches(v) {
			return changed(id, v)
		}
		if err := write(r); err != nil {
			return err
		}
		version, err = r.LockTestCase(ctx, id)
		return err
	})
	return version, err
}

// Update edits content fields. The TC-ID never changes; the version advances (If-Match is checked first).
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput, m etag.Match) (TestCase, error) {
	var v apperr.Validator
	v.Check(in.Title != nil || in.Description != nil || in.ExpectedResult != nil || in.Automated != nil, "body", "at least one field is required")
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
		v.Check(t != "", "title", "must not be empty")
		v.Check(validLen(t, maxTitle), "title", fmt.Sprintf("must be at most %d characters", maxTitle))
		v.CheckText("title", t)
	}
	if in.Description != nil {
		v.Check(validLen(*in.Description, maxLongText), "description", fmt.Sprintf("must be at most %d characters", maxLongText))
		v.CheckText("description", *in.Description)
	}
	if in.ExpectedResult != nil {
		v.Check(validLen(*in.ExpectedResult, maxLongText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxLongText))
		v.CheckText("expectedResult", *in.ExpectedResult)
	}
	if err := v.Err(); err != nil {
		return TestCase{}, err
	}
	var tc TestCase
	_, err := s.guarded(ctx, id, m, func(r Repository) (err error) {
		tc, err = r.UpdateTestCase(ctx, id, in)
		return err
	})
	return tc, err
}

// Deprecate marks a test case deprecated (idempotent). Its history is kept and
// it leaves the expected universe of runs created afterwards.
func (s *Service) Deprecate(ctx context.Context, id int64, m etag.Match) (TestCase, error) {
	var tc TestCase
	_, err := s.guarded(ctx, id, m, func(r Repository) (err error) {
		tc, err = r.DeprecateTestCase(ctx, id)
		return err
	})
	return tc, err
}

// Reactivate brings a deprecated test case back to active (idempotent), keeping
// its TC-ID. Existing run snapshots are immutable; future runs include it again
// when it is automated.
func (s *Service) Reactivate(ctx context.Context, id int64, m etag.Match) (TestCase, error) {
	var tc TestCase
	_, err := s.guarded(ctx, id, m, func(r Repository) (err error) {
		tc, err = r.ReactivateTestCase(ctx, id)
		return err
	})
	return tc, err
}

// IngestionView returns, in one snapshot, the project's test cases that are
// active and automated right now and each existing test case among numbers.
func (s *Service) IngestionView(ctx context.Context, projectID int64, numbers []int64) (IngestionView, error) {
	return s.repo.ListIngestionView(ctx, projectID, numbers)
}

// Keys returns the display key of each known test case id (for other modules' read models).
func (s *Service) Keys(ctx context.Context, ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	return s.repo.ListTestCaseKeys(ctx, ids)
}

func validateProjectText(v *apperr.Validator, name, description *string) {
	if name != nil {
		v.Check(*name != "", "name", "must not be empty")
		v.Check(validLen(*name, maxProjectName), "name", fmt.Sprintf("must be at most %d characters", maxProjectName))
		v.CheckText("name", *name)
	}
	if description != nil {
		v.Check(validLen(*description, maxProjectDesc), "description", fmt.Sprintf("must be at most %d characters", maxProjectDesc))
		v.CheckText("description", *description)
	}
}

// CreateProject validates and stores a new project. The key is upper-cased and never changes.
func (s *Service) CreateProject(ctx context.Context, in CreateProjectInput) (Project, error) {
	in.Key = strings.ToUpper(strings.TrimSpace(in.Key))
	in.Name = strings.TrimSpace(in.Name)
	var v apperr.Validator
	v.Check(ProjectKeyPattern.MatchString(in.Key), "key", "must be 2 to 10 letters or digits, starting with a letter (e.g. CHK)")
	validateProjectText(&v, &in.Name, &in.Description)
	if err := v.Err(); err != nil {
		return Project{}, err
	}
	p, err := s.repo.CreateProject(ctx, in)
	if errors.Is(err, ErrConflict) {
		return Project{}, apperr.Conflict("project %s already exists", in.Key)
	}
	return p, err
}

// ProjectByKey returns a project by its key.
func (s *Service) ProjectByKey(ctx context.Context, key string) (Project, error) {
	p, err := s.repo.GetProjectByKey(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return Project{}, projectNotFound(key)
	}
	return p, err
}

// ProjectIDByKey returns the id of a project by its key (other modules' project filter).
func (s *Service) ProjectIDByKey(ctx context.Context, key string) (int64, error) {
	p, err := s.ProjectByKey(ctx, key)
	return p.ID, err
}

// ProjectByID returns a project by its id.
func (s *Service) ProjectByID(ctx context.Context, id int64) (Project, error) {
	p, err := s.repo.GetProject(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Project{}, apperr.NotFound("project %d not found", id)
	}
	return p, err
}

// ListProjects returns a page of projects ordered by key.
func (s *Service) ListProjects(ctx context.Context, projectIDs []int64, page pagination.Page) (pagination.Result[Project], error) {
	items, err := s.repo.ListProjects(ctx, projectIDs, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[Project]{}, err
	}
	total, err := s.repo.CountProjects(ctx, projectIDs)
	if err != nil {
		return pagination.Result[Project]{}, err
	}
	return pagination.Result[Project]{Items: items, Page: page, Total: total}, nil
}

// UpdateProject edits a project's name or description; its key never changes.
func (s *Service) UpdateProject(ctx context.Context, key string, in UpdateProjectInput) (Project, error) {
	var v apperr.Validator
	v.Check(in.Name != nil || in.Description != nil, "body", "at least one field is required")
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
	}
	validateProjectText(&v, in.Name, in.Description)
	if err := v.Err(); err != nil {
		return Project{}, err
	}
	p, err := s.repo.UpdateProject(ctx, key, in)
	if errors.Is(err, ErrNotFound) {
		return Project{}, projectNotFound(key)
	}
	return p, err
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
		v.CheckText("action", *action)
	}
	if expected != nil {
		v.Check(validLen(*expected, maxStepText), "expectedResult", fmt.Sprintf("must be at most %d characters", maxStepText))
		v.CheckText("expectedResult", *expected)
	}
}

// CreateStep inserts a step at the requested position (clamped to the end) or appends it; it returns the
// test case's new version.
func (s *Service) CreateStep(ctx context.Context, testCaseID int64, in CreateStepInput, m etag.Match) (TestStep, int64, error) {
	var v apperr.Validator
	validateStepText(&v, &in.Action, &in.ExpectedResult)
	if in.Position != nil {
		v.Check(*in.Position >= 1, "position", "must be >= 1")
	}
	if err := v.Err(); err != nil {
		return TestStep{}, 0, err
	}
	var step TestStep
	version, err := s.guarded(ctx, testCaseID, m, func(r Repository) error {
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
	return step, version, err
}

func stepNotFound(testCaseID, stepID int64) error {
	return apperr.NotFound("step %d of test case %d not found", stepID, testCaseID)
}

// UpdateStep edits a step's content. It never affects the TC-ID or historical results; it returns the test
// case's new version.
func (s *Service) UpdateStep(ctx context.Context, testCaseID, stepID int64, in UpdateStepInput, m etag.Match) (TestStep, int64, error) {
	var v apperr.Validator
	v.Check(in.Action != nil || in.ExpectedResult != nil, "body", "at least one field is required")
	validateStepText(&v, in.Action, in.ExpectedResult)
	if err := v.Err(); err != nil {
		return TestStep{}, 0, err
	}
	var step TestStep
	version, err := s.guarded(ctx, testCaseID, m, func(r Repository) (err error) {
		step, err = r.UpdateTestStep(ctx, testCaseID, stepID, in)
		if errors.Is(err, ErrNotFound) {
			return stepNotFound(testCaseID, stepID)
		}
		return err
	})
	return step, version, err
}

// DeleteStep removes a step and renumbers the remaining ones contiguously; it returns the test case's new version.
func (s *Service) DeleteStep(ctx context.Context, testCaseID, stepID int64, m etag.Match) (int64, error) {
	return s.guarded(ctx, testCaseID, m, func(r Repository) error {
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

// ReorderSteps applies a new order; stepIDs must be a permutation of all current steps. It returns the steps
// and the test case's new version.
func (s *Service) ReorderSteps(ctx context.Context, testCaseID int64, stepIDs []int64, m etag.Match) ([]TestStep, int64, error) {
	var steps []TestStep
	version, err := s.guarded(ctx, testCaseID, m, func(r Repository) error {
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
	return steps, version, err
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
