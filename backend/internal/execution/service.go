package execution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Service holds the execution use cases.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService builds a Service.
func NewService(repo Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}

func runNotFound(id int64) error { return apperr.NotFound("test run %d not found", id) }

// RecordRun creates the run identified by {provider}:{run_id}:{run_attempt}
// with its expected-universe snapshot and results, atomically. When the run
// already exists the call is an idempotent replay: nothing is written, the
// snapshot is not recomputed, and the existing run is returned with created=false.
func (s *Service) RecordRun(ctx context.Context, run NewRun, expected []int64, results []NewResult, parseErrors []ParseError) (TestRun, bool, error) {
	externalID := ExternalRunID(run.Provider, run.ProviderRunID, run.RunAttempt)
	status := run.Status
	if status == "" {
		status = RunCompleted
	}
	now := s.now()
	if run.StartedAt != nil && run.StartedAt.After(now) {
		run.StartedAt = nil // a run cannot start after it is recorded: clock skew or a local time without a zone
	}
	var (
		out     TestRun
		created bool
	)
	err := s.repo.InTx(ctx, func(r Repository) error {
		id, ok, err := r.InsertTestRun(ctx, InsertRunParams{
			NewRun: run, ExternalRunID: externalID, Status: status, CompletedAt: now,
		})
		if err != nil {
			return err
		}
		if !ok {
			if id, err = r.GetTestRunIDByExternalID(ctx, run.ProjectID, externalID); err != nil {
				return err
			}
		} else {
			created = true
			if err := r.InsertExpectedCases(ctx, id, expected); err != nil {
				return err
			}
			if err := r.InsertTestResults(ctx, id, results); err != nil {
				return err
			}
			if err := r.InsertParseErrors(ctx, id, parseErrors); err != nil {
				return err
			}
		}
		if out, err = r.GetTestRun(ctx, id); err != nil {
			return err
		}
		return attachOutcomes(ctx, r, []TestRun{out}, func(_ int, o RunOutcome) { out.Outcome = o })
	})
	return out, created, err
}

// attachOutcomes computes the outcome of each run (one batch read) and hands
// it to set with the run's index.
func attachOutcomes(ctx context.Context, repo Repository, runs []TestRun, set func(int, RunOutcome)) error {
	// A history page often repeats a run (one row per result): each run's outcome
	// is read and computed once.
	outcomes := make(map[int64]RunOutcome, len(runs))
	var ids []int64
	for _, r := range runs {
		if _, seen := outcomes[r.ID]; !seen {
			outcomes[r.ID] = RunOutcome{}
			ids = append(ids, r.ID)
		}
	}
	inputs, err := repo.ListSummaryInputs(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		in := inputs[id]
		outcomes[id] = Summarize(id, in, nil).Outcome()
	}
	for i, r := range runs {
		set(i, outcomes[r.ID])
	}
	return nil
}

// GetRun returns a run with its outcome.
func (s *Service) GetRun(ctx context.Context, id int64) (TestRun, error) {
	run, err := s.getRun(ctx, id)
	if err != nil {
		return TestRun{}, err
	}
	err = attachOutcomes(ctx, s.repo, []TestRun{run}, func(_ int, o RunOutcome) { run.Outcome = o })
	return run, err
}

// getRun returns a run without its outcome (existence checks).
func (s *Service) getRun(ctx context.Context, id int64) (TestRun, error) {
	run, err := s.repo.GetTestRun(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return TestRun{}, runNotFound(id)
	}
	return run, err
}

// ListRuns returns a page of runs with their outcomes, newest first, narrowed by projects and suite.
func (s *Service) ListRuns(ctx context.Context, f RunFilter, page pagination.Page) (pagination.Result[TestRun], error) {
	items, err := s.repo.ListTestRuns(ctx, f, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestRun]{}, err
	}
	if err := attachOutcomes(ctx, s.repo, items, func(i int, o RunOutcome) { items[i].Outcome = o }); err != nil {
		return pagination.Result[TestRun]{}, err
	}
	total, err := s.repo.CountTestRuns(ctx, f)
	if err != nil {
		return pagination.Result[TestRun]{}, err
	}
	return pagination.Result[TestRun]{Items: items, Page: page, Total: total}, nil
}

// ListRunResults returns a page of the individual results of a run.
func (s *Service) ListRunResults(ctx context.Context, runID int64, f ResultFilter, page pagination.Page) (pagination.Result[TestResult], error) {
	if _, err := s.getRun(ctx, runID); err != nil {
		return pagination.Result[TestResult]{}, err
	}
	items, err := s.repo.ListRunResults(ctx, runID, f, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestResult]{}, err
	}
	total, err := s.repo.CountRunResults(ctx, runID, f)
	if err != nil {
		return pagination.Result[TestResult]{}, err
	}
	return pagination.Result[TestResult]{Items: items, Page: page, Total: total}, nil
}

// Diagnostics returns the stored results of a run whose TC-ID is not valid.
func (s *Service) Diagnostics(ctx context.Context, runID int64) ([]Diagnostic, error) {
	return s.repo.ListDiagnostics(ctx, runID)
}

// ParseErrors returns every stored parse error of a run, in document order.
func (s *Service) ParseErrors(ctx context.Context, runID int64) ([]ParseError, error) {
	return s.repo.ListParseErrors(ctx, runID, math.MaxInt32, 0)
}

// ListParseErrors returns a page of the stored parse errors of a run.
func (s *Service) ListParseErrors(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[ParseError], error) {
	if _, err := s.getRun(ctx, runID); err != nil {
		return pagination.Result[ParseError]{}, err
	}
	items, err := s.repo.ListParseErrors(ctx, runID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[ParseError]{}, err
	}
	total, err := s.repo.CountParseErrors(ctx, runID)
	if err != nil {
		return pagination.Result[ParseError]{}, err
	}
	return pagination.Result[ParseError]{Items: items, Page: page, Total: total}, nil
}

// Summary computes the summary of a run against its immutable snapshot.
func (s *Service) Summary(ctx context.Context, runID int64) (Summary, error) {
	if _, err := s.getRun(ctx, runID); err != nil {
		return Summary{}, err
	}
	inputs, err := s.repo.ListSummaryInputs(ctx, []int64{runID})
	if err != nil {
		return Summary{}, err
	}
	diagnostics, err := s.repo.ListDiagnostics(ctx, runID)
	if err != nil {
		return Summary{}, err
	}
	return Summarize(runID, inputs[runID], diagnostics), nil
}

const maxReason = 500

// Amend includes in a run's universe a TC-ID that has valid results in the run but was outside its snapshot
// (DEC-42). The snapshot never changes; the amendment is recorded with who made it and why.
func (s *Service) Amend(ctx context.Context, runID, testCaseID int64, reason string, by authz.Actor) (Amendment, error) {
	reason = strings.TrimSpace(reason)
	var v apperr.Validator
	v.Check(reason != "" && utf8.RuneCountInString(reason) <= maxReason, "reason", fmt.Sprintf("must be 1 to %d characters", maxReason))
	v.CheckText("reason", reason)
	v.Check(testCaseID >= 1, "testCaseId", "must be a positive integer")
	if err := v.Err(); err != nil {
		return Amendment{}, err
	}
	if _, err := s.getRun(ctx, runID); err != nil {
		return Amendment{}, err
	}
	inputs, err := s.repo.ListSummaryInputs(ctx, []int64{runID})
	if err != nil {
		return Amendment{}, err
	}
	in := inputs[runID]
	if slices.Contains(in.Universe(), testCaseID) {
		return Amendment{}, apperr.Conflict("TC-ID %d is already in the universe of run %d", testCaseID, runID)
	}
	if !slices.Contains(Summarize(runID, in, nil).OutsideUniverseIDs, testCaseID) {
		return Amendment{}, apperr.Validation(apperr.ValidationFailed,
			apperr.FieldError{Field: "testCaseId", Message: "has no valid result in this run: only TC-IDs reported by the run can be included"})
	}
	a, err := s.repo.InsertAmendment(ctx, NewAmendment{
		TestRunID: runID, TestCaseID: testCaseID, AmendedBy: by.ID, AmendedByUsername: by.Username, Reason: reason,
	})
	if errors.Is(err, ErrConflict) {
		return Amendment{}, apperr.Conflict("TC-ID %d is already in the universe of run %d", testCaseID, runID)
	}
	return a, err
}

// ListAmendments returns a page of a run's amendments, oldest first.
func (s *Service) ListAmendments(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[Amendment], error) {
	if _, err := s.getRun(ctx, runID); err != nil {
		return pagination.Result[Amendment]{}, err
	}
	items, err := s.repo.ListAmendments(ctx, runID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[Amendment]{}, err
	}
	total, err := s.repo.CountAmendments(ctx, runID)
	if err != nil {
		return pagination.Result[Amendment]{}, err
	}
	return pagination.Result[Amendment]{Items: items, Page: page, Total: total}, nil
}

// History returns the results of a TC-ID across runs, newest first.
func (s *Service) History(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[HistoryEntry], error) {
	items, err := s.repo.ListResultsForTestCase(ctx, testCaseID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[HistoryEntry]{}, err
	}
	runs := make([]TestRun, len(items))
	for i, h := range items {
		runs[i] = h.Run
	}
	if err := attachOutcomes(ctx, s.repo, runs, func(i int, o RunOutcome) { items[i].Run.Outcome = o }); err != nil {
		return pagination.Result[HistoryEntry]{}, err
	}
	total, err := s.repo.CountResultsForTestCase(ctx, testCaseID)
	if err != nil {
		return pagination.Result[HistoryEntry]{}, err
	}
	return pagination.Result[HistoryEntry]{Items: items, Page: page, Total: total}, nil
}
