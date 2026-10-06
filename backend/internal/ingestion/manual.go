package ingestion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// ManualScope tells which test cases a manual run expects.
type ManualScope string

// Manual scopes.
const (
	// ScopeManual expects the active manual (not automated) test cases: what CI does not run.
	ScopeManual ManualScope = "manual"
	// ScopeAll expects every active test case (e.g. a release sign-off).
	ScopeAll ManualScope = "all"
)

// Manual result limits.
const (
	maxManualName = 200
	maxManualNote = 10000
)

// ManualRunInput starts a manual run.
type ManualRunInput struct {
	ProjectKey string
	// SuiteKey narrows the run to a suite (optional).
	SuiteKey string
	// Name says what is being tested (e.g. "Release 2.4 sign-off"); it is the run's pipeline.
	Name  string
	Scope ManualScope
	// Branch and Commit tell what was tested (optional).
	Branch string
	Commit string
}

// ManualResultInput is one recorded result.
type ManualResultInput struct {
	TestCaseID int64
	Status     execution.ResultStatus
	// Note explains the result (shown like a failure message).
	Note string
	// FailedStep is the step where the test failed (optional).
	FailedStep *int32
	DurationMs *int64
}

// ManualCatalog is what manual execution needs from the catalog module.
type ManualCatalog interface {
	ProjectByKey(ctx context.Context, key string) (catalog.Project, error)
	Selection(ctx context.Context, projectID int64, key string, automated *bool) (catalog.Suite, []int64, error)
	Keys(ctx context.Context, ids []int64) (map[int64]string, error)
}

// ManualRecorder is what manual execution needs from the execution module.
type ManualRecorder interface {
	StartRun(ctx context.Context, run execution.NewRun, expected []int64) (execution.TestRun, error)
	GetRun(ctx context.Context, id int64) (execution.TestRun, error)
	RecordResult(ctx context.Context, runID int64, r execution.NewResult) (execution.TestResult, error)
	FinishRun(ctx context.Context, runID int64, status execution.RunStatus) (execution.TestRun, error)
}

// ManualAccess authorizes manual execution: a member of the project, with a session.
type ManualAccess interface {
	Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error
	Actor(ctx context.Context) (authz.Actor, error)
}

// Manual runs manual executions (MVP D3): a person starts a run, records results and finishes it.
type Manual struct {
	catalog  ManualCatalog
	recorder ManualRecorder
	access   ManualAccess
	notify   RunNotifier
}

// NewManual builds a Manual.
func NewManual(c ManualCatalog, r ManualRecorder, a ManualAccess) *Manual {
	return &Manual{catalog: c, recorder: r, access: a, notify: noNotifier{}}
}

// SetNotifier sets who hears of the manual runs that finish.
func (m *Manual) SetNotifier(n RunNotifier) { m.notify = n }

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start starts a manual run of a project (optionally of a suite) expecting its active manual test cases, or all
// its active test cases with ScopeAll.
func (m *Manual) Start(ctx context.Context, in ManualRunInput) (execution.TestRun, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Scope = ManualScope(strings.TrimSpace(string(in.Scope)))
	if in.Scope == "" {
		in.Scope = ScopeManual
	}
	var v apperr.Validator
	projectkey.Check(&v, "project", in.ProjectKey)
	v.Check(in.SuiteKey == "" || catalog.SuiteKeyPattern.MatchString(in.SuiteKey), "suite", catalog.SuiteKeyMessage)
	v.Check(in.Name != "", "name", "is required")
	v.Check(utf8.RuneCountInString(in.Name) <= maxManualName, "name", fmt.Sprintf("must be at most %d characters", maxManualName))
	v.Check(in.Scope == ScopeManual || in.Scope == ScopeAll, "scope", "must be one of manual, all")
	v.Check(utf8.RuneCountInString(in.Branch) <= 255, "branch", "must be at most 255 characters")
	v.Check(utf8.RuneCountInString(in.Commit) <= 64, "commit", "must be at most 64 characters")
	v.CheckText("name", in.Name)
	v.CheckText("branch", in.Branch)
	v.CheckText("commit", in.Commit)
	if err := v.Err(); err != nil {
		return execution.TestRun{}, err
	}
	p, err := projectkey.Resolve(ctx, "project", in.ProjectKey, m.catalog.ProjectByKey, catalog.ProjectID, m.access, authz.RoleMember)
	if err != nil {
		return execution.TestRun{}, err
	}
	actor, err := m.access.Actor(ctx)
	if err != nil {
		return execution.TestRun{}, err
	}
	var automated *bool
	if in.Scope == ScopeManual {
		manual := false
		automated = &manual
	}
	suite, expected, err := m.catalog.Selection(ctx, p.ID, in.SuiteKey, automated)
	if err != nil {
		return execution.TestRun{}, err
	}
	return m.recorder.StartRun(ctx, execution.NewRun{
		ProjectID: p.ID, Provider: "manual", ProviderRunID: newRunID(), RunAttempt: 1, Pipeline: in.Name,
		Branch: in.Branch, Commit: in.Commit, SuiteKey: suite.Key, SuiteName: suite.Name, StartedBy: actor.Username,
	}, expected)
}

// authorizeRun checks the caller may record results in the run (a member of its project).
func (m *Manual) authorizeRun(ctx context.Context, runID int64) error {
	run, err := m.recorder.GetRun(ctx, runID)
	if err == nil {
		err = m.access.Require(ctx, run.ProjectID, authz.RoleMember, apperr.NotFound("test run %d not found", runID))
	}
	return err
}

// Record records the result of a test case of a running manual run; recording it again is a re-test.
func (m *Manual) Record(ctx context.Context, runID int64, in ManualResultInput) (execution.TestResult, error) {
	var v apperr.Validator
	v.Check(in.TestCaseID >= 1, "testCaseId", "must be a positive integer")
	v.Check(slices.Contains(execution.ResultStatuses, in.Status), "status", "must be one of passed, failed, error, skipped")
	v.Check(utf8.RuneCountInString(in.Note) <= maxManualNote, "note", fmt.Sprintf("must be at most %d characters", maxManualNote))
	v.CheckText("note", in.Note)
	v.Check(in.FailedStep == nil || *in.FailedStep >= 1, "failedStep", "must be >= 1")
	v.Check(in.FailedStep == nil || in.Status == execution.Failed || in.Status == execution.Error, "failedStep", "only for a failed or error result")
	v.Check(in.DurationMs == nil || *in.DurationMs >= 0, "durationMs", "must be >= 0")
	if err := v.Err(); err != nil {
		return execution.TestResult{}, err
	}
	if err := m.authorizeRun(ctx, runID); err != nil {
		return execution.TestResult{}, err
	}
	actor, err := m.access.Actor(ctx)
	if err != nil {
		return execution.TestResult{}, err
	}
	keys, err := m.catalog.Keys(ctx, []int64{in.TestCaseID})
	if err != nil {
		return execution.TestResult{}, err
	}
	key, ok := keys[in.TestCaseID]
	if !ok {
		return execution.TestResult{}, apperr.Conflict("test case %d is not expected in run %d", in.TestCaseID, runID)
	}
	id := in.TestCaseID
	return m.recorder.RecordResult(ctx, runID, execution.NewResult{
		TestCaseID: &id, RequestedTestCaseID: &key, Correlation: execution.CorrelationValid, TestName: key,
		ClassName: execution.ManualClass, Status: in.Status, DurationMs: in.DurationMs, ErrorMessage: in.Note,
		Attempt: 1, RecordedBy: actor.Username, FailedStep: in.FailedStep,
	})
}

// Finish ends a running manual run as completed or cancelled.
func (m *Manual) Finish(ctx context.Context, runID int64, status execution.RunStatus) (execution.TestRun, error) {
	if err := m.authorizeRun(ctx, runID); err != nil {
		return execution.TestRun{}, err
	}
	run, err := m.recorder.FinishRun(ctx, runID, status)
	if err == nil {
		m.notify.RunCompleted(ctx, run)
	}
	return run, err
}
