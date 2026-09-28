// Package catalog is the Test Catalog module: test cases (identified by an
// immutable, never-reused numeric TC-ID) and their optional ordered steps.
// Other modules use it only through Service; they never read its tables.
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

// ErrNotFound is returned by a Repository when a row does not exist.
var ErrNotFound = errors.New("not found")

// TestCase is the current definition of a test case. Editing content never
// changes its ID and never creates a version.
type TestCase struct {
	ID             int64
	Title          string
	Description    string
	ExpectedResult string
	Status         Status
	Automated      bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeprecatedAt   *time.Time
}

// Key is the display form of the TC-ID, e.g. TC-153.
func (tc TestCase) Key() string { return FormatKey(tc.ID) }

// FormatKey formats a numeric TC-ID as TC-<id>.
func FormatKey(id int64) string { return "TC-" + strconv.FormatInt(id, 10) }

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
	ListTestCases(ctx context.Context, status *Status, limit, offset int32) ([]TestCase, error)
	CountTestCases(ctx context.Context, status *Status) (int64, error)
	UpdateTestCase(ctx context.Context, id int64, in UpdateInput) (TestCase, error)
	DeprecateTestCase(ctx context.Context, id int64) (TestCase, error)
	ReactivateTestCase(ctx context.Context, id int64) (TestCase, error)
	ListExpectedUniverse(ctx context.Context) ([]int64, error)
	ListTestCaseStatuses(ctx context.Context, ids []int64) (map[int64]Status, error)

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
