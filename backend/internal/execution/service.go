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
			NewRun: run, ExternalRunID: externalID, Status: status, CompletedAt: &now,
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

// StartRun creates a running manual run with its expected universe and no results yet.
func (s *Service) StartRun(ctx context.Context, run NewRun, expected []int64) (TestRun, error) {
	run.Mode = ModeManual
	var out TestRun
	err := s.repo.InTx(ctx, func(r Repository) error {
		id, ok, err := r.InsertTestRun(ctx, InsertRunParams{
			NewRun: run, ExternalRunID: ExternalRunID(run.Provider, run.ProviderRunID, run.RunAttempt), Status: RunRunning,
		})
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Conflict("a run with this id already exists")
		}
		if err := r.InsertExpectedCases(ctx, id, expected); err != nil {
			return err
		}
		if out, err = r.GetTestRun(ctx, id); err != nil {
			return err
		}
		return attachOutcomes(ctx, r, []TestRun{out}, func(_ int, o RunOutcome) { out.Outcome = o })
	})
	return out, err
}

// lockRunning locks a manual run that is still running (409 otherwise).
func lockRunning(ctx context.Context, r Repository, runID int64) error {
	status, mode, err := r.LockTestRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return runNotFound(runID)
	}
	if err != nil {
		return err
	}
	if mode != ModeManual {
		return apperr.Conflict("run %d was reported by CI: its results come from its report", runID)
	}
	if status != RunRunning {
		return apperr.Conflict("run %d is %s: it takes no more results", runID, status)
	}
	return nil
}

// RecordResult appends a manually recorded result of a test case in the run's universe; recording it again is a
// re-test (its next attempt; the last one counts).
func (s *Service) RecordResult(ctx context.Context, runID int64, res NewResult) (TestResult, error) {
	var out TestResult
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := lockRunning(ctx, r, runID); err != nil {
			return err
		}
		in, err := r.IsInUniverse(ctx, runID, *res.TestCaseID)
		if err != nil {
			return err
		}
		if !in {
			return apperr.Conflict("test case %d is not expected in run %d", *res.TestCaseID, runID)
		}
		if out, err = r.InsertManualResult(ctx, runID, res); err != nil {
			return err
		}
		if out.Attempt > MaxAttempts {
			return apperr.Conflict("test case %d was already recorded %d times in run %d", *res.TestCaseID, MaxAttempts, runID)
		}
		return nil
	})
	return out, err
}

// FinishRun ends a running manual run as completed or cancelled.
func (s *Service) FinishRun(ctx context.Context, runID int64, status RunStatus) (TestRun, error) {
	if status != RunCompleted && status != RunCancelled {
		return TestRun{}, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "status", Message: "must be one of completed, cancelled"})
	}
	var out TestRun
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := lockRunning(ctx, r, runID); err != nil {
			return err
		}
		if err := r.FinishTestRun(ctx, runID, status); err != nil {
			return err
		}
		var err error
		if out, err = r.GetTestRun(ctx, runID); err != nil {
			return err
		}
		return attachOutcomes(ctx, r, []TestRun{out}, func(_ int, o RunOutcome) { out.Outcome = o })
	})
	return out, err
}

// LatestStatuses returns the status each test case had in the latest run with a result for it (its logical status
// there: last attempts, variants aggregated failed > error > skipped > passed); test cases without results are absent.
func (s *Service) LatestStatuses(ctx context.Context, testCaseIDs []int64) (map[int64]string, error) {
	rows, err := s.repo.ListLatestResults(ctx, testCaseIDs)
	if err != nil {
		return nil, err
	}
	byCase := map[int64][]ValidResult{}
	for _, r := range rows {
		byCase[r.TestCaseID] = append(byCase[r.TestCaseID], r)
	}
	out := make(map[int64]string, len(byCase))
	for id, results := range byCase {
		statuses, _ := logical(results)
		out[id] = string(Aggregate(statuses))
	}
	return out, nil
}

// LatestConclusive returns, for each test case with conclusive evidence, its logical status (passed, failed or error)
// in the latest run where it was conclusive, and that run's id; skipped runs are inconclusive and ignored.
func (s *Service) LatestConclusive(ctx context.Context, testCaseIDs []int64) (map[int64]string, map[int64]int64, error) {
	rows, err := s.repo.ListLatestConclusive(ctx, testCaseIDs)
	if err != nil {
		return nil, nil, err
	}
	statuses, runs := make(map[int64]string, len(rows)), make(map[int64]int64, len(rows))
	for _, r := range rows {
		statuses[r.TestCaseID], runs[r.TestCaseID] = string(r.Status), r.RunID
	}
	return statuses, runs, nil
}

// LastExecuted returns when each test case last had a valid result; test cases never executed are absent.
func (s *Service) LastExecuted(ctx context.Context, testCaseIDs []int64) (map[int64]time.Time, error) {
	return s.repo.ListLastExecuted(ctx, testCaseIDs)
}

// FlakyCounts returns the test cases that were flaky in a project's latest window runs, most flaky first, at most
// limit of them.
func (s *Service) FlakyCounts(ctx context.Context, projectID int64, window, limit int32) ([]FlakyCount, error) {
	return s.repo.ListFlakyCounts(ctx, projectID, window, limit)
}
