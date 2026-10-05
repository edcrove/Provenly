package ingestion

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// MaxEventBatch bounds the live events of one request.
const MaxEventBatch = 500

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,100}$`)

// EventInput is a live event as a runner sends it.
type EventInput struct {
	EventID  string
	Sequence int64
	Type     execution.EventType
	TestName string
	// TestCase is the TC-ID the test declares (CHK-12, TC-12 or 12), optional.
	TestCase   string
	Status     execution.ResultStatus
	OccurredAt *time.Time
}

// LiveRecorder is what live runs need from the execution module.
type LiveRecorder interface {
	StartRun(ctx context.Context, run execution.NewRun, expected []int64) (execution.TestRun, error)
	GetRun(ctx context.Context, id int64) (execution.TestRun, error)
	RecordEvents(ctx context.Context, runID int64, events []execution.NewEvent) (execution.EventsResult, error)
}

// Live runs live executions (Trello "Live Test Run Streaming & Reconciliation"): CI starts the run, streams events
// and finally sends its JUnit report through the normal ingestion, which completes the run.
type Live struct {
	ingest   *Service
	recorder LiveRecorder
	now      func() time.Time
}

// NewLive builds a Live sharing the ingestion's catalog and access.
func NewLive(ingest *Service, r LiveRecorder, now func() time.Time) *Live {
	return &Live{ingest: ingest, recorder: r, now: now}
}

// Start starts a live run expecting the project's active automated test cases (a suite's when named); starting it
// again returns the same run.
func (l *Live) Start(ctx context.Context, meta RunMeta) (execution.TestRun, error) {
	if err := ValidateMeta(meta); err != nil {
		return execution.TestRun{}, err
	}
	if meta.Status != "" {
		return execution.TestRun{}, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "status", Message: "is sent with the final report"})
	}
	project, err := l.ingest.project(ctx, meta.ProjectKey)
	if err != nil {
		return execution.TestRun{}, err
	}
	var suite catalog.Suite
	var selection []int64
	if meta.SuiteKey != "" {
		if suite, selection, err = l.ingest.catalog.SuiteSelection(ctx, project.ID, meta.SuiteKey); err != nil {
			return execution.TestRun{}, err
		}
	}
	view, err := l.ingest.catalog.IngestionView(ctx, project.ID, nil)
	if err != nil {
		return execution.TestRun{}, err
	}
	expected := view.Expected
	if meta.SuiteKey != "" {
		expected = intersect(view.Expected, selection)
	}
	return l.recorder.StartRun(ctx, execution.NewRun{
		ProjectID: project.ID, Provider: meta.Provider, ProviderRunID: meta.ProviderRunID, RunAttempt: meta.RunAttempt,
		Pipeline: meta.Pipeline, Branch: meta.Branch, Commit: meta.Commit, SuiteKey: suite.Key, SuiteName: suite.Name,
		Mode: execution.ModeLive,
	}, expected)
}

func validateEvents(events []EventInput) error {
	var v apperr.Validator
	v.Check(len(events) >= 1 && len(events) <= MaxEventBatch, "events", fmt.Sprintf("must hold 1 to %d events", MaxEventBatch))
	for i, e := range events {
		f := fmt.Sprintf("events[%d].", i)
		v.Check(eventIDPattern.MatchString(e.EventID), f+"eventId", "must match "+eventIDPattern.String())
		v.Check(e.Sequence >= 0, f+"sequence", "must be >= 0")
		v.Check(slices.Contains(execution.EventTypes, e.Type), f+"type", "must be one of test.started, test.finished, step.started, step.completed, run.finished")
		v.Check(utf8.RuneCountInString(e.TestName) <= 1000, f+"testName", "must be at most 1000 characters")
		v.CheckText(f+"testName", e.TestName)
		v.Check(utf8.RuneCountInString(e.TestCase) <= 100, f+"testCase", "must be at most 100 characters")
		v.CheckText(f+"testCase", e.TestCase)
		if e.Type == execution.EventTestFinished {
			v.Check(slices.Contains(execution.ResultStatuses, e.Status), f+"status", "a finished test needs one of passed, failed, error, skipped")
		} else {
			v.Check(e.Status == "", f+"status", "only a test.finished event has a status")
		}
	}
	return v.Err()
}

// RecordEvents appends a batch of live events to a running live run of a project the caller reports into. Declared
// TC-IDs are resolved in the run's project; one that does not resolve is kept as an invalid correlation.
func (l *Live) RecordEvents(ctx context.Context, runID int64, events []EventInput) (execution.EventsResult, error) {
	if err := validateEvents(events); err != nil {
		return execution.EventsResult{}, err
	}
	run, err := l.recorder.GetRun(ctx, runID)
	if err == nil {
		err = l.ingest.access.Require(ctx, run.ProjectID, authz.RoleMember, apperr.NotFound("test run %d not found", runID))
	}
	if err != nil {
		return execution.EventsResult{}, err
	}
	project, err := l.ingest.catalog.ProjectByID(ctx, run.ProjectID)
	if err != nil {
		return execution.EventsResult{}, err
	}
	refs := make([]junit.TCRef, len(events))
	var numbers []int64
	for i, e := range events {
		refs[i] = junit.ParseRef(e.TestCase)
		if refs[i].Kind == junit.RefFound && ownProject(refs[i], project.Key) {
			numbers = append(numbers, refs[i].ID)
		}
	}
	view, err := l.ingest.catalog.IngestionView(ctx, project.ID, numbers)
	if err != nil {
		return execution.EventsResult{}, err
	}
	out := make([]execution.NewEvent, len(events))
	for i, e := range events {
		ne := execution.NewEvent{EventID: e.EventID, Sequence: e.Sequence, Type: e.Type, TestName: e.TestName, OccurredAt: l.now()}
		if e.OccurredAt != nil {
			ne.OccurredAt = *e.OccurredAt
		}
		if e.Status != "" {
			st := e.Status
			ne.Status = &st
		}
		if refs[i].Kind != junit.RefMissing {
			raw := refs[i].Raw
			ne.RequestedTestCaseID = &raw
			if entry, ok := view.Entries[refs[i].ID]; ok && refs[i].Kind == junit.RefFound && ownProject(refs[i], project.Key) {
				id := entry.ID
				ne.TestCaseID = &id
			}
		}
		out[i] = ne
	}
	return l.recorder.RecordEvents(ctx, runID, out)
}
