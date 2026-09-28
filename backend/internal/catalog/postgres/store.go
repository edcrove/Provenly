// Package postgres is the PostgreSQL adapter of the catalog Repository, built
// on the sqlc-generated catalogdb queries.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/catalog/catalogdb"
)

// Store implements catalog.Repository.
type Store struct {
	pool *pgxpool.Pool
	q    *catalogdb.Queries
}

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: catalogdb.New(pool)}
}

var _ catalog.Repository = (*Store)(nil)

// InTx implements catalog.Repository.
func (s *Store) InTx(ctx context.Context, fn func(catalog.Repository) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: s.q.WithTx(tx)})
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.ErrNotFound
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

func toTestCase(r catalogdb.TestCase) catalog.TestCase {
	return catalog.TestCase{
		ID: r.ID, Title: r.Title, Description: r.Description, ExpectedResult: r.ExpectedResult,
		Status: catalog.Status(r.Status), Automated: r.Automated,
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, DeprecatedAt: timePtr(r.DeprecatedAt),
	}
}

func toStep(r catalogdb.TestStep) catalog.TestStep {
	return catalog.TestStep{
		ID: r.ID, TestCaseID: r.TestCaseID, Position: r.Position, Action: r.Action,
		ExpectedResult: r.ExpectedResult, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func statusText(s *catalog.Status) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*s), Valid: true}
}

// CreateTestCase implements catalog.Repository.
func (s *Store) CreateTestCase(ctx context.Context, in catalog.CreateInput) (catalog.TestCase, error) {
	r, err := s.q.CreateTestCase(ctx, catalogdb.CreateTestCaseParams{
		Title: in.Title, Description: in.Description, ExpectedResult: in.ExpectedResult, Automated: in.Automated,
	})
	if err != nil {
		return catalog.TestCase{}, err
	}
	return toTestCase(r), nil
}

// GetTestCase implements catalog.Repository.
func (s *Store) GetTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.GetTestCase(ctx, id)
	if err != nil {
		return catalog.TestCase{}, notFound(err)
	}
	return toTestCase(r), nil
}

// LockTestCase implements catalog.Repository.
func (s *Store) LockTestCase(ctx context.Context, id int64) error {
	_, err := s.q.LockTestCase(ctx, id)
	return notFound(err)
}

// ListTestCases implements catalog.Repository.
func (s *Store) ListTestCases(ctx context.Context, status *catalog.Status, limit, offset int32) ([]catalog.TestCase, error) {
	rows, err := s.q.ListTestCases(ctx, catalogdb.ListTestCasesParams{Status: statusText(status), PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]catalog.TestCase, len(rows))
	for i, r := range rows {
		out[i] = toTestCase(r)
	}
	return out, nil
}

// CountTestCases implements catalog.Repository.
func (s *Store) CountTestCases(ctx context.Context, status *catalog.Status) (int64, error) {
	return s.q.CountTestCases(ctx, statusText(status))
}

// UpdateTestCase implements catalog.Repository.
func (s *Store) UpdateTestCase(ctx context.Context, id int64, in catalog.UpdateInput) (catalog.TestCase, error) {
	params := catalogdb.UpdateTestCaseParams{
		ID: id, Title: text(in.Title), Description: text(in.Description), ExpectedResult: text(in.ExpectedResult),
	}
	if in.Automated != nil {
		params.Automated = pgtype.Bool{Bool: *in.Automated, Valid: true}
	}
	r, err := s.q.UpdateTestCase(ctx, params)
	if err != nil {
		return catalog.TestCase{}, notFound(err)
	}
	return toTestCase(r), nil
}

// DeprecateTestCase implements catalog.Repository.
func (s *Store) DeprecateTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.DeprecateTestCase(ctx, id)
	if err != nil {
		return catalog.TestCase{}, notFound(err)
	}
	return toTestCase(r), nil
}

// ReactivateTestCase implements catalog.Repository.
func (s *Store) ReactivateTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.ReactivateTestCase(ctx, id)
	if err != nil {
		return catalog.TestCase{}, notFound(err)
	}
	return toTestCase(r), nil
}

// ListExpectedUniverse implements catalog.Repository.
func (s *Store) ListExpectedUniverse(ctx context.Context) ([]int64, error) {
	ids, err := s.q.ListExpectedUniverse(ctx)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids, nil
}

// ListTestCaseStatuses implements catalog.Repository.
func (s *Store) ListTestCaseStatuses(ctx context.Context, ids []int64) (map[int64]catalog.Status, error) {
	rows, err := s.q.ListTestCaseStatuses(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]catalog.Status, len(rows))
	for _, r := range rows {
		out[r.ID] = catalog.Status(r.Status)
	}
	return out, nil
}

// ListTestSteps implements catalog.Repository.
func (s *Store) ListTestSteps(ctx context.Context, testCaseID int64, limit, offset int32) ([]catalog.TestStep, error) {
	rows, err := s.q.ListTestSteps(ctx, catalogdb.ListTestStepsParams{TestCaseID: testCaseID, PageLimit: limit, PageOffset: offset})
	return steps(rows, err)
}

// ListAllTestSteps implements catalog.Repository.
func (s *Store) ListAllTestSteps(ctx context.Context, testCaseID int64) ([]catalog.TestStep, error) {
	return steps(s.q.ListAllTestSteps(ctx, testCaseID))
}

func steps(rows []catalogdb.TestStep, err error) ([]catalog.TestStep, error) {
	if err != nil {
		return nil, err
	}
	out := make([]catalog.TestStep, len(rows))
	for i, r := range rows {
		out[i] = toStep(r)
	}
	return out, nil
}

// CountTestSteps implements catalog.Repository.
func (s *Store) CountTestSteps(ctx context.Context, testCaseID int64) (int64, error) {
	return s.q.CountTestSteps(ctx, testCaseID)
}

// ShiftTestStepsDown implements catalog.Repository.
func (s *Store) ShiftTestStepsDown(ctx context.Context, testCaseID int64, fromPosition int32) error {
	return s.q.ShiftTestStepsDown(ctx, catalogdb.ShiftTestStepsDownParams{TestCaseID: testCaseID, FromPosition: fromPosition})
}

// CreateTestStep implements catalog.Repository.
func (s *Store) CreateTestStep(ctx context.Context, testCaseID int64, position int32, action, expectedResult string) (catalog.TestStep, error) {
	r, err := s.q.CreateTestStep(ctx, catalogdb.CreateTestStepParams{
		TestCaseID: testCaseID, Position: position, Action: action, ExpectedResult: expectedResult,
	})
	if err != nil {
		return catalog.TestStep{}, err
	}
	return toStep(r), nil
}

// UpdateTestStep implements catalog.Repository.
func (s *Store) UpdateTestStep(ctx context.Context, testCaseID, stepID int64, in catalog.UpdateStepInput) (catalog.TestStep, error) {
	r, err := s.q.UpdateTestStep(ctx, catalogdb.UpdateTestStepParams{
		TestCaseID: testCaseID, ID: stepID, Action: text(in.Action), ExpectedResult: text(in.ExpectedResult),
	})
	if err != nil {
		return catalog.TestStep{}, notFound(err)
	}
	return toStep(r), nil
}

// DeleteTestStep implements catalog.Repository.
func (s *Store) DeleteTestStep(ctx context.Context, testCaseID, stepID int64) (int32, error) {
	pos, err := s.q.DeleteTestStep(ctx, catalogdb.DeleteTestStepParams{TestCaseID: testCaseID, ID: stepID})
	return pos, notFound(err)
}

// CloseTestStepGap implements catalog.Repository.
func (s *Store) CloseTestStepGap(ctx context.Context, testCaseID int64, afterPosition int32) error {
	return s.q.CloseTestStepGap(ctx, catalogdb.CloseTestStepGapParams{TestCaseID: testCaseID, AfterPosition: afterPosition})
}

// SetTestStepPosition implements catalog.Repository.
func (s *Store) SetTestStepPosition(ctx context.Context, testCaseID, stepID int64, position int32) error {
	return s.q.SetTestStepPosition(ctx, catalogdb.SetTestStepPositionParams{TestCaseID: testCaseID, ID: stepID, Position: position})
}
