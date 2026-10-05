// Package postgres is the PostgreSQL adapter of the execution Repository,
// built on the sqlc-generated executiondb queries.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/execution/executiondb"
)

// Store implements execution.Repository.
type Store struct {
	pool *pgxpool.Pool
	q    *executiondb.Queries
}

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: executiondb.New(pool)}
}

var _ execution.Repository = (*Store)(nil)

// InTx implements execution.Repository.
func (s *Store) InTx(ctx context.Context, fn func(execution.Repository) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: s.q.WithTx(tx)})
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return execution.ErrNotFound
	}
	return err
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func timestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func int8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func filterText[T ~string](v *T) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*v), Valid: true}
}

type runRow struct {
	executiondb.TestRun
	ExpectedCount  int32
	ResultCount    int32
	AmendmentCount int32
}

func toRun(r runRow) execution.TestRun {
	return execution.TestRun{
		ID: r.ID, ProjectID: r.ProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
		RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, Commit: r.CommitSha,
		Status: execution.RunStatus(r.Status), ExpectedCount: r.ExpectedCount + r.AmendmentCount, ResultCount: r.ResultCount,
		AmendmentCount: r.AmendmentCount, CreatedAt: r.CreatedAt.Time, StartedAt: timePtr(r.StartedAt), CompletedAt: timePtr(r.CompletedAt),
		ReportSHA256: r.ReportSha256,
	}
}

func toResult(r executiondb.TestResult) execution.TestResult {
	return execution.TestResult{
		ID: r.ID, TestRunID: r.TestRunID, TestCaseID: int8Ptr(r.TestCaseID), RequestedTestCaseID: textPtr(r.RequestedTestCaseID),
		Correlation: execution.Correlation(r.Correlation), TestName: r.TestName, ClassName: r.ClassName, SuiteName: r.SuiteName,
		Status: execution.ResultStatus(r.Status), DurationMs: int8Ptr(r.DurationMs), ErrorMessage: r.ErrorMessage,
		ErrorDetails: r.ErrorDetails, CreatedAt: r.CreatedAt.Time, Attempt: r.Attempt,
	}
}

// InsertTestRun implements execution.Repository.
func (s *Store) InsertTestRun(ctx context.Context, p execution.InsertRunParams) (int64, bool, error) {
	id, err := s.q.InsertTestRun(ctx, executiondb.InsertTestRunParams{
		ProjectID: p.ProjectID, ExternalRunID: p.ExternalRunID, Provider: p.Provider, ProviderRunID: p.ProviderRunID, RunAttempt: p.RunAttempt,
		Pipeline: p.Pipeline, Branch: p.Branch, CommitSha: p.Commit, Status: string(p.Status),
		StartedAt: timestamptz(p.StartedAt), CompletedAt: timestamptz(&p.CompletedAt), ReportSha256: p.ReportSHA256,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// GetTestRunIDByExternalID implements execution.Repository.
func (s *Store) GetTestRunIDByExternalID(ctx context.Context, projectID int64, externalRunID string) (int64, error) {
	id, err := s.q.GetTestRunIDByExternalID(ctx, executiondb.GetTestRunIDByExternalIDParams{ProjectID: projectID, ExternalRunID: externalRunID})
	return id, notFound(err)
}

// InsertExpectedCases implements execution.Repository.
func (s *Store) InsertExpectedCases(ctx context.Context, runID int64, testCaseIDs []int64) error {
	return s.q.InsertExpectedCases(ctx, executiondb.InsertExpectedCasesParams{TestRunID: runID, TestCaseIds: testCaseIDs})
}

// InsertTestResults implements execution.Repository.
func (s *Store) InsertTestResults(ctx context.Context, runID int64, results []execution.NewResult) error {
	rows := make([]executiondb.InsertTestResultsParams, len(results))
	for i, r := range results {
		row := executiondb.InsertTestResultsParams{
			TestRunID: runID, Correlation: string(r.Correlation), TestName: r.TestName, ClassName: r.ClassName,
			SuiteName: r.SuiteName, Status: string(r.Status),
			ErrorMessage: r.ErrorMessage, ErrorDetails: r.ErrorDetails, Attempt: max(r.Attempt, 1),
		}
		if r.TestCaseID != nil {
			row.TestCaseID = pgtype.Int8{Int64: *r.TestCaseID, Valid: true}
		}
		if r.RequestedTestCaseID != nil {
			row.RequestedTestCaseID = pgtype.Text{String: *r.RequestedTestCaseID, Valid: true}
		}
		if r.DurationMs != nil {
			row.DurationMs = pgtype.Int8{Int64: *r.DurationMs, Valid: true}
		}
		rows[i] = row
	}
	_, err := s.q.InsertTestResults(ctx, rows)
	return err
}

// InsertParseErrors implements execution.Repository.
func (s *Store) InsertParseErrors(ctx context.Context, runID int64, errs []execution.ParseError) error {
	rows := make([]executiondb.InsertParseErrorsParams, len(errs))
	for i, e := range errs {
		rows[i] = executiondb.InsertParseErrorsParams{TestRunID: runID, CaseIndex: e.Index, TestName: e.TestName, Message: e.Message, Persisted: e.Persisted, Severity: e.Severity}
	}
	_, err := s.q.InsertParseErrors(ctx, rows)
	return err
}

// ListParseErrors implements execution.Repository.
func (s *Store) ListParseErrors(ctx context.Context, runID int64, limit, offset int32) ([]execution.ParseError, error) {
	rows, err := s.q.ListParseErrors(ctx, executiondb.ListParseErrorsParams{TestRunID: runID, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]execution.ParseError, len(rows))
	for i, r := range rows {
		out[i] = execution.ParseError{Index: r.CaseIndex, TestName: r.TestName, Message: r.Message, Persisted: r.Persisted, Severity: r.Severity}
	}
	return out, nil
}

// CountParseErrors implements execution.Repository.
func (s *Store) CountParseErrors(ctx context.Context, runID int64) (int64, error) {
	return s.q.CountParseErrors(ctx, runID)
}

// GetTestRun implements execution.Repository.
func (s *Store) GetTestRun(ctx context.Context, id int64) (execution.TestRun, error) {
	r, err := s.q.GetTestRun(ctx, id)
	if err != nil {
		return execution.TestRun{}, notFound(err)
	}
	return toRun(runRow{
		TestRun: executiondb.TestRun{
			ID: r.ID, ProjectID: r.ProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
			RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, CommitSha: r.CommitSha, Status: r.Status,
			CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt, ReportSha256: r.ReportSha256,
		},
		ExpectedCount: r.ExpectedCount, ResultCount: r.ResultCount, AmendmentCount: r.AmendmentCount,
	}), nil
}

// ListTestRuns implements execution.Repository.
func (s *Store) ListTestRuns(ctx context.Context, projectIDs []int64, limit, offset int32) ([]execution.TestRun, error) {
	rows, err := s.q.ListTestRuns(ctx, executiondb.ListTestRunsParams{ProjectIds: projectIDs, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]execution.TestRun, len(rows))
	for i, r := range rows {
		out[i] = toRun(runRow{
			TestRun: executiondb.TestRun{
				ID: r.ID, ProjectID: r.ProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
				RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, CommitSha: r.CommitSha, Status: r.Status,
				CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt,
			},
			ExpectedCount: r.ExpectedCount, ResultCount: r.ResultCount, AmendmentCount: r.AmendmentCount,
		})
	}
	return out, nil
}

// CountTestRuns implements execution.Repository.
func (s *Store) CountTestRuns(ctx context.Context, projectIDs []int64) (int64, error) {
	return s.q.CountTestRuns(ctx, projectIDs)
}

// ListRunResults implements execution.Repository.
func (s *Store) ListRunResults(ctx context.Context, runID int64, f execution.ResultFilter, limit, offset int32) ([]execution.TestResult, error) {
	rows, err := s.q.ListRunResults(ctx, executiondb.ListRunResultsParams{
		TestRunID: runID, Status: filterText(f.Status), Correlation: filterText(f.Correlation), PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]execution.TestResult, len(rows))
	for i, r := range rows {
		out[i] = toResult(r.TestResult)
		out[i].Retried = r.Retried
	}
	return out, nil
}

// CountRunResults implements execution.Repository.
func (s *Store) CountRunResults(ctx context.Context, runID int64, f execution.ResultFilter) (int64, error) {
	return s.q.CountRunResults(ctx, executiondb.CountRunResultsParams{
		TestRunID: runID, Status: filterText(f.Status), Correlation: filterText(f.Correlation),
	})
}

// ListSummaryInputs implements execution.Repository.
func (s *Store) ListSummaryInputs(ctx context.Context, runIDs []int64) (map[int64]execution.SummaryInputs, error) {
	rows, err := s.q.ListSummaryInputs(ctx, runIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]execution.SummaryInputs, len(runIDs))
	for _, r := range rows {
		in := out[r.TestRunID]
		switch r.Kind {
		case "result":
			in.Valid = append(in.Valid, execution.ValidResult{
				TestCaseID: r.TestCaseID, Status: execution.ResultStatus(r.Status.String), Execution: r.Execution, Attempt: r.Attempt,
			})
		case "amended":
			in.Amended = append(in.Amended, r.TestCaseID)
		default:
			in.Expected = append(in.Expected, r.TestCaseID)
		}
		out[r.TestRunID] = in
	}
	return out, nil
}

// ListDiagnostics implements execution.Repository.
func (s *Store) ListDiagnostics(ctx context.Context, runID int64) ([]execution.Diagnostic, error) {
	rows, err := s.q.ListDiagnosticResults(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]execution.Diagnostic, len(rows))
	for i, r := range rows {
		out[i] = execution.Diagnostic{TestName: r.TestName, Correlation: execution.Correlation(r.Correlation), RequestedTestCaseID: textPtr(r.RequestedTestCaseID)}
	}
	return out, nil
}

// ListResultsForTestCase implements execution.Repository.
func (s *Store) ListResultsForTestCase(ctx context.Context, testCaseID int64, limit, offset int32) ([]execution.HistoryEntry, error) {
	rows, err := s.q.ListResultsForTestCase(ctx, executiondb.ListResultsForTestCaseParams{
		TestCaseID: pgtype.Int8{Int64: testCaseID, Valid: true}, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]execution.HistoryEntry, len(rows))
	for i, r := range rows {
		out[i] = execution.HistoryEntry{
			Result: func() execution.TestResult { res := toResult(r.TestResult); res.Retried = r.Retried; return res }(),
			Run: toRun(runRow{
				TestRun: executiondb.TestRun{
					ID: r.TestResult.TestRunID, ProjectID: r.RunProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
					RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, CommitSha: r.CommitSha, Status: r.RunStatus,
					CreatedAt: r.RunCreatedAt, StartedAt: r.RunStartedAt, CompletedAt: r.RunCompletedAt,
				},
				ExpectedCount: r.RunExpectedCount, ResultCount: r.RunResultCount, AmendmentCount: r.RunAmendmentCount,
			}),
		}
	}
	return out, nil
}

// CountResultsForTestCase implements execution.Repository.
func (s *Store) CountResultsForTestCase(ctx context.Context, testCaseID int64) (int64, error) {
	return s.q.CountResultsForTestCase(ctx, pgtype.Int8{Int64: testCaseID, Valid: true})
}

func toAmendment(r executiondb.TestRunAmendment) execution.Amendment {
	return execution.Amendment{
		ID: r.ID, TestRunID: r.TestRunID, TestCaseID: r.TestCaseID, AmendedBy: r.AmendedBy, AmendedByUsername: r.AmendedByUsername,
		Reason: r.Reason, CreatedAt: r.CreatedAt.Time,
	}
}

// InsertAmendment implements execution.Repository.
func (s *Store) InsertAmendment(ctx context.Context, a execution.NewAmendment) (execution.Amendment, error) {
	r, err := s.q.InsertAmendment(ctx, executiondb.InsertAmendmentParams{
		TestRunID: a.TestRunID, TestCaseID: a.TestCaseID, AmendedBy: a.AmendedBy, AmendedByUsername: a.AmendedByUsername, Reason: a.Reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return execution.Amendment{}, execution.ErrConflict
	}
	return toAmendment(r), err
}

// ListAmendments implements execution.Repository.
func (s *Store) ListAmendments(ctx context.Context, runID int64, limit, offset int32) ([]execution.Amendment, error) {
	rows, err := s.q.ListAmendments(ctx, executiondb.ListAmendmentsParams{TestRunID: runID, PageLimit: limit, PageOffset: offset})
	out := make([]execution.Amendment, len(rows))
	for i, r := range rows {
		out[i] = toAmendment(r)
	}
	return out, err
}

// CountAmendments implements execution.Repository.
func (s *Store) CountAmendments(ctx context.Context, runID int64) (int64, error) {
	return s.q.CountAmendments(ctx, runID)
}
