// Package postgres is the PostgreSQL adapter of the catalog Repository, built
// on the sqlc-generated catalogdb queries.
package postgres

import (
	"context"
	"errors"
	"sync"
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
	keys *keyCache
}

// keyCache remembers project keys by id. Keys never change and projects are
// never deleted (both enforced by the database), so the cache never goes stale.
type keyCache struct {
	mu   sync.RWMutex
	keys map[int64]string
}

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: catalogdb.New(pool), keys: &keyCache{keys: map[int64]string{}}}
}

// projectKey returns the key of a project, reading it once.
func (s *Store) projectKey(ctx context.Context, id int64) (string, error) {
	s.keys.mu.RLock()
	key, ok := s.keys.keys[id]
	s.keys.mu.RUnlock()
	if ok {
		return key, nil
	}
	p, err := s.q.GetProject(ctx, id)
	if err != nil {
		return "", err
	}
	s.keys.mu.Lock()
	s.keys.keys[id] = p.Key
	s.keys.mu.Unlock()
	return p.Key, nil
}

// testCase converts a row, filling in its project key.
func (s *Store) testCase(ctx context.Context, r catalogdb.TestCase) (catalog.TestCase, error) {
	key, err := s.projectKey(ctx, r.ProjectID)
	if err != nil {
		return catalog.TestCase{}, err
	}
	tc := toTestCase(r)
	tc.ProjectKey = key
	return tc, nil
}

func (s *Store) one(ctx context.Context, r catalogdb.TestCase, err error) (catalog.TestCase, error) {
	if err != nil {
		return catalog.TestCase{}, notFound(err)
	}
	return s.testCase(ctx, r)
}

var _ catalog.Repository = (*Store)(nil)

// InTx implements catalog.Repository.
func (s *Store) InTx(ctx context.Context, fn func(catalog.Repository) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: s.q.WithTx(tx), keys: s.keys})
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
		ID: r.ID, ProjectID: r.ProjectID, Number: r.Number, Title: r.Title, Description: r.Description, ExpectedResult: r.ExpectedResult,
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

func optInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func toProject(r catalogdb.Project) catalog.Project {
	return catalog.Project{
		ID: r.ID, Key: r.Key, Name: r.Name, Description: r.Description,
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}
}

// CreateTestCase implements catalog.Repository.
func (s *Store) CreateTestCase(ctx context.Context, in catalog.CreateInput) (catalog.TestCase, error) {
	r, err := s.q.CreateTestCase(ctx, catalogdb.CreateTestCaseParams{
		ProjectID: in.ProjectID, Title: in.Title, Description: in.Description, ExpectedResult: in.ExpectedResult, Automated: in.Automated,
	})
	return s.one(ctx, r, err)
}

// GetTestCase implements catalog.Repository.
func (s *Store) GetTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.GetTestCase(ctx, id)
	return s.one(ctx, r, err)
}

// LockTestCase implements catalog.Repository.
func (s *Store) LockTestCase(ctx context.Context, id int64) error {
	_, err := s.q.LockTestCase(ctx, id)
	return notFound(err)
}

// ListTestCases implements catalog.Repository.
func (s *Store) ListTestCases(ctx context.Context, f catalog.ListFilter, limit, offset int32) ([]catalog.TestCase, error) {
	rows, err := s.q.ListTestCases(ctx, catalogdb.ListTestCasesParams{
		Status: statusText(f.Status), ProjectID: optInt8(f.ProjectID), PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]catalog.TestCase, len(rows))
	for i, r := range rows {
		if out[i], err = s.testCase(ctx, r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CountTestCases implements catalog.Repository.
func (s *Store) CountTestCases(ctx context.Context, f catalog.ListFilter) (int64, error) {
	return s.q.CountTestCases(ctx, catalogdb.CountTestCasesParams{Status: statusText(f.Status), ProjectID: optInt8(f.ProjectID)})
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
	return s.one(ctx, r, err)
}

// DeprecateTestCase implements catalog.Repository.
func (s *Store) DeprecateTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.DeprecateTestCase(ctx, id)
	return s.one(ctx, r, err)
}

// ReactivateTestCase implements catalog.Repository.
func (s *Store) ReactivateTestCase(ctx context.Context, id int64) (catalog.TestCase, error) {
	r, err := s.q.ReactivateTestCase(ctx, id)
	return s.one(ctx, r, err)
}

// ListIngestionView implements catalog.Repository.
func (s *Store) ListIngestionView(ctx context.Context, projectID int64, numbers []int64) (catalog.IngestionView, error) {
	rows, err := s.q.ListIngestionView(ctx, catalogdb.ListIngestionViewParams{ProjectID: projectID, Numbers: numbers})
	if err != nil {
		return catalog.IngestionView{}, err
	}
	view := catalog.IngestionView{Expected: []int64{}, Entries: make(map[int64]catalog.IngestionEntry, len(numbers))}
	for _, r := range rows {
		if r.Expected {
			view.Expected = append(view.Expected, r.ID)
		}
		if r.Referenced {
			view.Entries[r.Number] = catalog.IngestionEntry{ID: r.ID, Status: catalog.Status(r.Status)}
		}
	}
	return view, nil
}

// ListTestCaseKeys implements catalog.Repository.
func (s *Store) ListTestCaseKeys(ctx context.Context, ids []int64) (map[int64]string, error) {
	rows, err := s.q.ListTestCaseKeys(ctx, ids)
	if err != nil {
		return nil, err
	}
	keys := make(map[int64]string, len(rows))
	for _, r := range rows {
		project, err := s.projectKey(ctx, r.ProjectID)
		if err != nil {
			return nil, err
		}
		keys[r.ID] = catalog.FormatKey(project, r.Number)
	}
	return keys, nil
}

// CreateProject implements catalog.Repository.
func (s *Store) CreateProject(ctx context.Context, in catalog.CreateProjectInput) (catalog.Project, error) {
	r, err := s.q.CreateProject(ctx, catalogdb.CreateProjectParams{Key: in.Key, Name: in.Name, Description: in.Description})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Project{}, catalog.ErrConflict
	}
	if err != nil {
		return catalog.Project{}, err
	}
	return toProject(r), nil
}

// GetProject implements catalog.Repository.
func (s *Store) GetProject(ctx context.Context, id int64) (catalog.Project, error) {
	return project(s.q.GetProject(ctx, id))
}

// GetProjectByKey implements catalog.Repository.
func (s *Store) GetProjectByKey(ctx context.Context, key string) (catalog.Project, error) {
	return project(s.q.GetProjectByKey(ctx, key))
}

func project(r catalogdb.Project, err error) (catalog.Project, error) {
	if err != nil {
		return catalog.Project{}, notFound(err)
	}
	return toProject(r), nil
}

// ListProjects implements catalog.Repository.
func (s *Store) ListProjects(ctx context.Context, limit, offset int32) ([]catalog.Project, error) {
	rows, err := s.q.ListProjects(ctx, catalogdb.ListProjectsParams{PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Project, len(rows))
	for i, r := range rows {
		out[i] = toProject(r)
	}
	return out, nil
}

// CountProjects implements catalog.Repository.
func (s *Store) CountProjects(ctx context.Context) (int64, error) {
	return s.q.CountProjects(ctx)
}

// UpdateProject implements catalog.Repository.
func (s *Store) UpdateProject(ctx context.Context, key string, in catalog.UpdateProjectInput) (catalog.Project, error) {
	return project(s.q.UpdateProject(ctx, catalogdb.UpdateProjectParams{Key: key, Name: text(in.Name), Description: text(in.Description)}))
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
