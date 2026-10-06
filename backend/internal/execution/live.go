package execution

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// EventType is the kind of a live event (Trello "Live Test Run Streaming & Reconciliation").
type EventType string

// Live event types.
const (
	EventTestStarted   EventType = "test.started"
	EventTestFinished  EventType = "test.finished"
	EventStepStarted   EventType = "step.started"
	EventStepCompleted EventType = "step.completed"
	EventRunFinished   EventType = "run.finished"
)

// EventTypes are the accepted live event types.
var EventTypes = []EventType{EventTestStarted, EventTestFinished, EventStepStarted, EventStepCompleted, EventRunFinished}

// MaxRunEvents bounds the live events one run keeps.
const MaxRunEvents = 10000

// NewEvent is a live event sent by a runner while a live run is running.
type NewEvent struct {
	// EventID identifies the event: a repeated delivery of the same id is a duplicate and is skipped.
	EventID  string
	Sequence int64
	Type     EventType
	TestName string
	// RequestedTestCaseID is the TC-ID the runner declared; TestCaseID is set when it resolved to a test case.
	RequestedTestCaseID *string
	TestCaseID          *int64
	// Status is the outcome of a test.finished event.
	Status     *ResultStatus
	OccurredAt time.Time
	// Attempt is the test's attempt (1 = first; a retry is the next one).
	Attempt int32
}

// Event is a stored live event.
type Event struct {
	NewEvent
	ID         int64
	ReceivedAt time.Time
}

// EventsResult counts what a batch of events did.
type EventsResult struct {
	Accepted, Duplicates int
}

// Live states of an expected test case while a live run runs (provisional: the final report decides).
const (
	LiveWaiting = "waiting"
	LiveRunning = "running"
)

// LiveCase is the provisional state of an expected test case: waiting, running or the status of its last
// test.finished event.
type LiveCase struct {
	TestCaseID int64
	State      string
}

// Reconciliation statuses and mismatch kinds.
const (
	ReconciliationPending    = "pending"
	ReconciliationConsistent = "consistent"
	ReconciliationMismatch   = "mismatch"

	MismatchStatus             = "status_mismatch"
	MismatchLiveOnly           = "live_only"
	MismatchFinalOnly          = "final_only"
	MismatchStartedNotFinished = "started_without_finished"
	MismatchDuplicate          = "duplicate"
	MismatchInvalidCorrelation = "invalid_correlation"
)

// Mismatch is one disagreement between the live events and the final report.
type Mismatch struct {
	Kind string
	// TestCaseID is the test case it is about (nil for an invalid correlation, which names Requested).
	TestCaseID  *int64
	Requested   string
	LiveStatus  string
	FinalStatus string
}

// Live is the provisional live state of a run and, once its report arrived, the reconciliation of the live events
// with the final results.
type Live struct {
	Cases                      []LiveCase
	Waiting, Running, Finished int32
	Events                     int32
	LastSequence               *int64
	RunFinished                bool
	Reconciliation             string
	Mismatches                 []Mismatch
}

// liveState derives the provisional state of the expected test cases from the events (ordered by sequence).
func liveState(expected []int64, events []Event) (Live, map[int64][]Event) {
	byCase := map[int64][]Event{}
	var live Live
	for _, e := range events {
		live.Events++
		seq := e.Sequence
		live.LastSequence = &seq
		if e.Type == EventRunFinished {
			live.RunFinished = true
		}
		if e.TestCaseID != nil && (e.Type == EventTestStarted || e.Type == EventTestFinished) {
			byCase[*e.TestCaseID] = append(byCase[*e.TestCaseID], e)
		}
	}
	live.Cases = make([]LiveCase, len(expected))
	for i, id := range expected {
		c := LiveCase{TestCaseID: id, State: LiveWaiting}
		if evs := byCase[id]; len(evs) > 0 {
			last := evs[len(evs)-1]
			c.State = LiveRunning
			if last.Type == EventTestFinished {
				c.State = string(*last.Status)
			}
		}
		switch c.State {
		case LiveWaiting:
			live.Waiting++
		case LiveRunning:
			live.Running++
		default:
			live.Finished++
		}
		live.Cases[i] = c
	}
	return live, byCase
}

// liveStatus aggregates the live outcome of a test case's tests: each test (by name) counts with its last finished
// attempt, and the worst wins (failed > error > skipped > passed), as in run summaries. duplicate tells that one
// attempt of one test finished more than once; unfinished that a test started without finishing.
func liveStatus(events []Event) (status string, duplicate, unfinished bool) {
	type test struct {
		started  bool
		finished map[int32]int
		last     int32
		status   ResultStatus
	}
	tests := map[string]*test{}
	var names []string
	for _, e := range events {
		t := tests[e.TestName]
		if t == nil {
			t = &test{finished: map[int32]int{}}
			tests[e.TestName] = t
			names = append(names, e.TestName)
		}
		if e.Type == EventTestStarted {
			t.started = true
			continue
		}
		t.finished[e.Attempt]++
		if t.finished[e.Attempt] > 1 {
			duplicate = true
		}
		if e.Attempt >= t.last {
			t.last, t.status = e.Attempt, *e.Status
		}
	}
	var statuses []ResultStatus
	for _, n := range names {
		t := tests[n]
		if len(t.finished) == 0 {
			unfinished = true
			continue
		}
		statuses = append(statuses, t.status)
	}
	if len(statuses) > 0 {
		status = string(Aggregate(statuses))
	}
	return status, duplicate, unfinished
}

// reconcile compares the live events of each test case with its final aggregated status ("" when the report has
// no result for it) and lists the disagreements, by test case id.
func reconcile(byCase map[int64][]Event, final map[int64]string, events []Event) []Mismatch {
	ids := make([]int64, 0, len(byCase)+len(final))
	for id := range byCase {
		ids = append(ids, id)
	}
	for id := range final {
		if _, ok := byCase[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	out := []Mismatch{}
	for _, id := range ids {
		status, duplicate, unfinished := liveStatus(byCase[id])
		tc := id
		m := Mismatch{TestCaseID: &tc, FinalStatus: final[id], LiveStatus: status}
		if duplicate {
			d := m
			d.Kind = MismatchDuplicate
			out = append(out, d)
		}
		switch {
		case len(byCase[id]) == 0:
			m.Kind = MismatchFinalOnly
		case unfinished:
			m.Kind = MismatchStartedNotFinished
		case m.FinalStatus == "":
			m.Kind = MismatchLiveOnly
		case m.LiveStatus != m.FinalStatus:
			m.Kind = MismatchStatus
		default:
			continue
		}
		out = append(out, m)
	}
	for _, e := range events {
		if e.TestCaseID == nil && e.RequestedTestCaseID != nil {
			out = append(out, Mismatch{Kind: MismatchInvalidCorrelation, Requested: *e.RequestedTestCaseID})
		}
	}
	return out
}

// RecordEvents appends a batch of live events to a running live run; repeated event ids are skipped.
func (s *Service) RecordEvents(ctx context.Context, runID int64, events []NewEvent) (EventsResult, error) {
	var res EventsResult
	err := s.repo.InTx(ctx, func(r Repository) error {
		status, mode, err := r.LockTestRun(ctx, runID)
		if errors.Is(err, ErrNotFound) {
			return runNotFound(runID)
		}
		if err != nil {
			return err
		}
		if mode != ModeLive {
			return apperr.Conflict("run %d is not a live run: it takes no live events", runID)
		}
		if status != RunRunning {
			return apperr.Conflict("run %d is %s: it takes no more live events", runID, status)
		}
		count, err := r.CountRunEvents(ctx, runID)
		if err != nil {
			return err
		}
		if count+len(events) > MaxRunEvents {
			return apperr.Conflict("run %d already holds %d live events (at most %d)", runID, count, MaxRunEvents)
		}
		for _, e := range events {
			inserted, err := r.InsertRunEvent(ctx, runID, e)
			if err != nil {
				return err
			}
			if inserted {
				res.Accepted++
			} else {
				res.Duplicates++
			}
		}
		return nil
	})
	return res, err
}

// Live returns the provisional live state of a live run and, once its final report arrived, the reconciliation of
// its events with the final results (pending while it runs).
func (s *Service) Live(ctx context.Context, runID int64) (Live, error) {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return Live{}, err
	}
	if run.Mode != ModeLive {
		return Live{}, apperr.Conflict("run %d is not a live run", runID)
	}
	summary, err := s.Summary(ctx, runID)
	if err != nil {
		return Live{}, err
	}
	events, err := s.repo.ListRunEvents(ctx, runID)
	if err != nil {
		return Live{}, err
	}
	expected := make([]int64, len(summary.TestCases))
	final := map[int64]string{}
	for i, c := range summary.TestCases {
		expected[i] = c.TestCaseID
		if c.Status != Untested {
			final[c.TestCaseID] = string(c.Status)
		}
	}
	// A test the final report has outside the universe (a manual or deprecated test case CI ran) is still a final
	// result: without it, its live events read as live_only.
	for id, st := range summary.OutsideStatuses {
		final[id] = string(st)
	}
	live, byCase := liveState(expected, events)
	live.Mismatches, live.Reconciliation = []Mismatch{}, ReconciliationPending
	if run.Status != RunRunning {
		live.Mismatches = reconcile(byCase, final, events)
		live.Reconciliation = ReconciliationConsistent
		if len(live.Mismatches) > 0 {
			live.Reconciliation = ReconciliationMismatch
		}
	}
	return live, nil
}
