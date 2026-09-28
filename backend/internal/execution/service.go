package execution

import (
	"context"
	"errors"
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
func (s *Service) RecordRun(ctx context.Context, run NewRun, expected []int64, results []NewResult) (TestRun, bool, error) {
	externalID := ExternalRunID(run.Provider, run.ProviderRunID, run.RunAttempt)
	var (
		out     TestRun
		created bool
	)
	err := s.repo.InTx(ctx, func(r Repository) error {
		id, ok, err := r.InsertTestRun(ctx, InsertRunParams{
			NewRun: run, ExternalRunID: externalID, Status: RunCompleted, CompletedAt: s.now(),
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
		}
		out, err = r.GetTestRun(ctx, id)
		return err
	})
	return out, created, err
}

// GetRun returns a run.
func (s *Service) GetRun(ctx context.Context, id int64) (TestRun, error) {
	run, err := s.repo.GetTestRun(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return TestRun{}, runNotFound(id)
	}
	return run, err
}

// ListRuns returns a page of runs, newest first.
func (s *Service) ListRuns(ctx context.Context, page pagination.Page) (pagination.Result[TestRun], error) {
	items, err := s.repo.ListTestRuns(ctx, page.Limit(), page.Offset())
	if err != nil {
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
	if _, err := s.GetRun(ctx, runID); err != nil {
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

// Summary computes the summary of a run against its immutable snapshot.
func (s *Service) Summary(ctx context.Context, runID int64) (Summary, error) {
	if _, err := s.GetRun(ctx, runID); err != nil {
		return Summary{}, err
	}
	expected, err := s.repo.ListExpectedCaseIDs(ctx, runID)
	if err != nil {
		return Summary{}, err
	}
	valid, err := s.repo.ListValidResults(ctx, runID)
	if err != nil {
		return Summary{}, err
	}
	diagnostics, err := s.repo.ListDiagnostics(ctx, runID)
	if err != nil {
		return Summary{}, err
	}
	return ComputeSummary(runID, expected, valid, diagnostics), nil
}

// History returns the results of a TC-ID across runs, newest first.
func (s *Service) History(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[HistoryEntry], error) {
	items, err := s.repo.ListResultsForTestCase(ctx, testCaseID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[HistoryEntry]{}, err
	}
	total, err := s.repo.CountResultsForTestCase(ctx, testCaseID)
	if err != nil {
		return pagination.Result[HistoryEntry]{}, err
	}
	return pagination.Result[HistoryEntry]{Items: items, Page: page, Total: total}, nil
}
