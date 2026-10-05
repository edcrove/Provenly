// Package catalog is the Test Catalog module: projects, test cases (identified
// by an immutable, never-reused key <PROJECT>-<number>) and their optional
// ordered steps. Other modules use it only through Service; they never read its tables.
package catalog

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// Status is the lifecycle status of a test case.
type Status string

// Test case statuses.
const (
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
)

// MaxSteps bounds the number of steps per test case.
const MaxSteps = 100

// DefaultProjectKey is the project that holds the test cases created before
// projects existed (TC-153 stays TC-153) and the default for clients that do
// not name a project.
const DefaultProjectKey = "TC"

// DefaultProjectID is the id of the default project (the first row the projects migration inserts);
// a CreateInput without a project goes there.
const DefaultProjectID int64 = 1

// Project groups a catalog and its runs. Its key prefixes every test case key
// and never changes.
type Project struct {
	ID          int64
	Key         string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateProjectInput is the content of a new project.
type CreateProjectInput struct {
	Key         string
	Name        string
	Description string
}

// UpdateProjectInput holds the project fields to change; nil means unchanged.
type UpdateProjectInput struct {
	Name        *string
	Description *string
}

// IngestionEntry is an existing test case referenced by a report.
type IngestionEntry struct {
	ID     int64
	Status Status
}

// IngestionView is what an ingestion reads from the catalog, in one snapshot.
type IngestionView struct {
	// Expected are the ids of the project's test cases that are active and automated right now, ascending.
	Expected []int64
	// Entries holds each existing referenced test case by its number in the project; unknown numbers are absent.
	Entries map[int64]IngestionEntry
}

// ErrNotFound is returned by a Repository when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned by a Repository when a unique key already exists.
var ErrConflict = errors.New("conflict")

// TestCase is the current definition of a test case. Editing content never
// changes its ID and never creates a version.
type TestCase struct {
	ID int64
	// ProjectID, ProjectKey and Number form the key, e.g. CHK-12.
	ProjectID      int64
	ProjectKey     string
	Number         int64
	Title          string
	Description    string
	ExpectedResult string
	Status         Status
	Automated      bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeprecatedAt   *time.Time
}

// Key is the test case key, e.g. CHK-12 (TC-153 in the default project).
func (tc TestCase) Key() string { return FormatKey(tc.ProjectKey, tc.Number) }

// FormatKey formats a test case key as <PROJECT>-<number>.
func FormatKey(projectKey string, number int64) string {
	return projectKey + "-" + strconv.FormatInt(number, 10)
}

// TestStep is an optional ordered step of a test case.
type TestStep struct {
	ID             int64
	TestCaseID     int64
	Position       int32
	Action         string
	ExpectedResult string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateInput is the content of a new test case. The ID is always assigned by storage.
type CreateInput struct {
	ProjectID      int64
	Title          string
	Description    string
	ExpectedResult string
	Automated      bool
}

// UpdateInput holds the fields to change; nil means unchanged.
type UpdateInput struct {
	Title          *string
	Description    *string
	ExpectedResult *string
	Automated      *bool
}

// ListFilter narrows a test case list; nil fields do not filter.
type ListFilter struct {
	Status     *Status
	ProjectIDs []int64
}

// CreateStepInput is the content of a new step. Position nil appends at the end.
type CreateStepInput struct {
	Action         string
	ExpectedResult string
	Position       *int32
}

// UpdateStepInput holds the step fields to change; nil means unchanged.
type UpdateStepInput struct {
	Action         *string
	ExpectedResult *string
}

// Repository is the persistence port of the catalog module.
type Repository interface {
	CreateTestCase(ctx context.Context, in CreateInput) (TestCase, error)
	GetTestCase(ctx context.Context, id int64) (TestCase, error)
	LockTestCase(ctx context.Context, id int64) error
	ListTestCases(ctx context.Context, f ListFilter, limit, offset int32) ([]TestCase, error)
	CountTestCases(ctx context.Context, f ListFilter) (int64, error)
	UpdateTestCase(ctx context.Context, id int64, in UpdateInput) (TestCase, error)
	DeprecateTestCase(ctx context.Context, id int64) (TestCase, error)
	ReactivateTestCase(ctx context.Context, id int64) (TestCase, error)
	ListIngestionView(ctx context.Context, projectID int64, numbers []int64) (IngestionView, error)

	// ListTestCaseKeys returns the display key (<KEY>-<n>) of the given test cases; unknown ids are absent.
	ListTestCaseKeys(ctx context.Context, ids []int64) (map[int64]string, error)
	CreateProject(ctx context.Context, in CreateProjectInput) (Project, error)
	GetProject(ctx context.Context, id int64) (Project, error)
	GetProjectByKey(ctx context.Context, key string) (Project, error)
	ListProjects(ctx context.Context, projectIDs []int64, limit, offset int32) ([]Project, error)
	CountProjects(ctx context.Context, projectIDs []int64) (int64, error)
	UpdateProject(ctx context.Context, key string, in UpdateProjectInput) (Project, error)

	ListTestSteps(ctx context.Context, testCaseID int64, limit, offset int32) ([]TestStep, error)
	ListAllTestSteps(ctx context.Context, testCaseID int64) ([]TestStep, error)
	CountTestSteps(ctx context.Context, testCaseID int64) (int64, error)
	ShiftTestStepsDown(ctx context.Context, testCaseID int64, fromPosition int32) error
	CreateTestStep(ctx context.Context, testCaseID int64, position int32, action, expectedResult string) (TestStep, error)
	UpdateTestStep(ctx context.Context, testCaseID, stepID int64, in UpdateStepInput) (TestStep, error)
	DeleteTestStep(ctx context.Context, testCaseID, stepID int64) (int32, error)
	CloseTestStepGap(ctx context.Context, testCaseID int64, afterPosition int32) error
	SetTestStepPosition(ctx context.Context, testCaseID, stepID int64, position int32) error

	// InTx runs fn inside one database transaction.
	InTx(ctx context.Context, fn func(Repository) error) error
}
