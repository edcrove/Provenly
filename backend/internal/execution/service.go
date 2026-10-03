package execution

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
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
			if id, err = r.GetTestRunIDByExternalID(ctx, externalID); err != nil {
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
		outcomes[id] = ComputeSummary(id, in.Expected, in.Valid, nil).Outcome()
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

// ListRuns returns a page of runs with their outcomes, newest first.
func (s *Service) ListRuns(ctx context.Context, page pagination.Page) (pagination.Result[TestRun], error) {
	items, err := s.repo.ListTestRuns(ctx, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[TestRun]{}, err
	}
	if err := attachOutcomes(ctx, s.repo, items, func(i int, o RunOutcome) { items[i].Outcome = o }); err != nil {
		return pagination.Result[TestRun]{}, err
	}
	total, err := s.repo.CountTestRuns(ctx)
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
	in := inputs[runID]
	return ComputeSummary(runID, in.Expected, in.Valid, diagnostics), nil
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
