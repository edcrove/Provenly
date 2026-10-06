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
	// Version advances with every change to the test case, its steps, tags or classification (optimistic locking, ETag).
	Version int64
	// Tags are free labels, sorted; Classification maps a dimension key to the value key of the test case.
	Tags           []string
	Classification map[string]string
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
	Tags           []string
	// Classification maps dimension keys to value keys.
	Classification map[string]string
}

// UpdateInput holds the fields to change; nil means unchanged.
type UpdateInput struct {
	Title          *string
	Description    *string
	ExpectedResult *string
	Automated      *bool
	// Tags, when not nil, replaces every tag.
	Tags *[]string
	// Classification merges into the current one: a dimension mapped to nil loses its value, absent ones keep theirs.
	Classification map[string]*string
}

// ListFilter narrows a test case list; nil fields do not filter.
type ListFilter struct {
	Status     *Status
	ProjectIDs []int64
	Tag        *string
	// Classified holds dimension:value pairs that must all hold.
	Classified []string
	// SuiteID narrows to the test cases a static suite lists.
	SuiteID   *int64
	Automated *bool
	// Number narrows to the test case with this number (with ProjectIDs: one key).
	Number *int64
}

// Requirement providers: native requirements are written in Provenly; the others mirror an external tool.
const (
	ProviderProvenly    = "provenly"
	ProviderJira        = "jira"
	ProviderGitHub      = "github"
	ProviderAzureDevOps = "azure_devops"
)

// Requirement is something a project must satisfy, native or mirrored from an external tool, and the test cases
// that cover it. Provider and external id never change; requirements are archived, never deleted.
type Requirement struct {
	ID             int64
	ProjectID      int64
	Provider       string
	ExternalID     string
	Title          string
	Description    string
	URL            string
	ProviderStatus string
	ArchivedAt     *time.Time
	LastSyncedAt   *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	TestCaseIDs    []int64
}

// RequirementInput is a new (or, when imported, mirrored) requirement.
type RequirementInput struct {
	Provider       string
	ExternalID     string
	Title          string
	Description    string
	URL            string
	ProviderStatus string
}

// UpdateRequirementInput holds the requirement fields to change; nil means unchanged.
type UpdateRequirementInput struct {
	Title          *string
	Description    *string
	URL            *string
	ProviderStatus *string
	Archived       *bool
}

// Issue states: an issue is open until it is fixed (closed) in its tracker.
const (
	IssueOpen   = "open"
	IssueClosed = "closed"
)

// Issue is a defect tracked in Provenly or mirrored from an external tracker, and the test cases that reproduce it.
// Provider and external id never change; issues are closed, never deleted.
type Issue struct {
	ID             int64
	ProjectID      int64
	Provider       string
	ExternalID     string
	Title          string
	Description    string
	URL            string
	State          string
	ProviderStatus string
	ClosedAt       *time.Time
	LastSyncedAt   *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	TestCaseIDs    []int64
}

// IssueInput is a new (or, when imported, mirrored) issue.
type IssueInput struct {
	Provider       string
	ExternalID     string
	Title          string
	Description    string
	URL            string
	State          string
	ProviderStatus string
}

// UpdateIssueInput holds the issue fields to change; nil means unchanged.
type UpdateIssueInput struct {
	Title          *string
	Description    *string
	URL            *string
	State          *string
	ProviderStatus *string
}

// IssueFilter narrows a project's issues.
type IssueFilter struct {
	State      *string
	TestCaseID *int64
}

// SuiteKind tells how a suite selects its test cases.
type SuiteKind string

// Suite kinds.
const (
	// SuiteKindStatic lists its test cases explicitly.
	SuiteKindStatic SuiteKind = "static"
	// SuiteKindQuery selects the test cases matching a tag and classification query ("smart" suite).
	SuiteKindQuery SuiteKind = "query"
)

// SuiteQuery is the selection of a query suite: a tag and dimension:value pairs, all must hold.
type SuiteQuery struct {
	Tag        *string
	Classified []string
}

// Suite is a named selection of a project's test cases. A run reported for a suite expects its active automated
// test cases. Suites are archived, never deleted; key, project and kind never change.
type Suite struct {
	ID          int64
	ProjectID   int64
	Key         string
	Name        string
	Description string
	Kind        SuiteKind
	Query       SuiteQuery
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// CaseCount is the number of test cases a static suite lists (0 for query suites).
	CaseCount int32
	// CaseIDs are the test cases a static suite lists, ascending (filled when reading one suite).
	CaseIDs []int64
}

// SuiteInput is a new suite.
type SuiteInput struct {
	Key         string
	Name        string
	Description string
	Kind        SuiteKind
	Query       SuiteQuery
	TestCaseIDs []int64
}

// UpdateSuiteInput holds the suite fields to change; nil means unchanged.
type UpdateSuiteInput struct {
	Name        *string
	Description *string
	Query       *SuiteQuery
	Archived    *bool
}

// Dimension is a project's classification axis (feature, risk, ...) with its controlled values.
// Dimensions and values are archived, never deleted, and their keys never change.
type Dimension struct {
	ID         int64
	ProjectID  int64
	Key        string
	Name       string
	BuiltIn    bool
	ArchivedAt *time.Time
	CreatedAt  time.Time
	Values     []DimensionValue
}

// DimensionValue is one controlled value of a dimension.
type DimensionValue struct {
	ID          int64
	DimensionID int64
	Key         string
	Name        string
	Position    int32
	ArchivedAt  *time.Time
	CreatedAt   time.Time
}

// value returns the dimension's value with the given key.
func (d Dimension) value(key string) (DimensionValue, bool) {
	for _, v := range d.Values {
		if v.Key == key {
			return v, true
		}
	}
	return DimensionValue{}, false
}

// DimensionInput is a new dimension or value.
type DimensionInput struct {
	Key  string
	Name string
}

// UpdateDimensionInput holds the dimension or value fields to change; nil means unchanged.
type UpdateDimensionInput struct {
	Name     *string
	Archived *bool
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
	// LockTestCase locks the test case until the transaction ends and returns its version.
	LockTestCase(ctx context.Context, id int64) (int64, error)
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

	// ListDimensions returns a project's dimensions with their values.
	ListDimensions(ctx context.Context, projectID int64) ([]Dimension, error)
	CreateDimension(ctx context.Context, projectID int64, in DimensionInput) (Dimension, error)
	UpdateDimension(ctx context.Context, projectID int64, key string, in UpdateDimensionInput) (Dimension, error)
	CreateDimensionValue(ctx context.Context, dimensionID int64, in DimensionInput) (DimensionValue, error)
	UpdateDimensionValue(ctx context.Context, dimensionID int64, key string, in UpdateDimensionInput) (DimensionValue, error)
	// SetTags replaces the tags of a test case.
	SetTags(ctx context.Context, testCaseID int64, tags []string) error
	// SetClassification sets (valueID > 0) or clears (valueID 0) the value of one dimension of a test case.
	SetClassification(ctx context.Context, testCaseID, projectID, dimensionID, valueID int64) error

	// ListTestCaseIDs returns the ids of every test case the filter selects, ascending.
	ListTestCaseIDs(ctx context.Context, f ListFilter) ([]int64, error)
	ListSuites(ctx context.Context, projectID int64) ([]Suite, error)
	// GetSuite returns a suite with its listed test cases (static suites).
	GetSuite(ctx context.Context, projectID int64, key string) (Suite, error)
	CreateSuite(ctx context.Context, projectID int64, in SuiteInput) (int64, error)
	UpdateSuite(ctx context.Context, projectID int64, key string, in UpdateSuiteInput) (int64, error)
	// SetSuiteCases replaces the test cases a static suite lists.
	SetSuiteCases(ctx context.Context, suiteID, projectID int64, ids []int64) error
	// ProjectCaseIDs returns which of ids are test cases of the project.
	ProjectCaseIDs(ctx context.Context, projectID int64, ids []int64) ([]int64, error)

	// ListRequirements returns a project's requirements, newest first (only those testCaseID covers when set).
	ListRequirements(ctx context.Context, projectID int64, testCaseID *int64) ([]Requirement, error)
	GetRequirement(ctx context.Context, projectID, id int64) (Requirement, error)
	// NextNativeRequirementNumber returns the n of the next native R-<n>.
	NextNativeRequirementNumber(ctx context.Context, projectID int64) (int64, error)
	// UpsertRequirement creates a requirement or, with sync, updates the mirrored one; ok=false when it existed
	// and sync is false. created tells an insert from an update.
	UpsertRequirement(ctx context.Context, projectID int64, in RequirementInput, sync bool, syncedAt *time.Time) (id int64, created, ok bool, err error)
	UpdateRequirement(ctx context.Context, projectID, id int64, in UpdateRequirementInput) error
	// SetRequirementTestCases replaces the test cases that cover a requirement.
	SetRequirementTestCases(ctx context.Context, requirementID, projectID int64, ids []int64) error
	// ListIssues returns a project's issues, newest first, narrowed by f.
	ListIssues(ctx context.Context, projectID int64, f IssueFilter) ([]Issue, error)
	GetIssue(ctx context.Context, projectID, id int64) (Issue, error)
	// NextNativeIssueNumber returns the n of the next native I-<n>.
	NextNativeIssueNumber(ctx context.Context, projectID int64) (int64, error)
	// UpsertIssue creates an issue or, with sync, updates the mirrored one; ok=false when it existed without sync.
	UpsertIssue(ctx context.Context, projectID int64, in IssueInput, sync bool, syncedAt *time.Time) (id int64, created, ok bool, err error)
	UpdateIssue(ctx context.Context, projectID, id int64, in UpdateIssueInput) error
	// SetIssueTestCases replaces the test cases linked to an issue.
	SetIssueTestCases(ctx context.Context, issueID, projectID int64, ids []int64) error

	// InTx runs fn inside one database transaction.
	InTx(ctx context.Context, fn func(Repository) error) error
	// LockScope serializes, until the transaction ends, the writes that count or number rows of one scope (a
	// project's dimensions, a dimension's values): the count and the insert see each other's effects.
	LockScope(ctx context.Context, scope string, id int64) error
}
