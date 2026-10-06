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
	tc, err := s.testCase(ctx, r)
	if err != nil {
		return catalog.TestCase{}, err
	}
	list := []catalog.TestCase{tc}
	if err := s.withTaxonomy(ctx, list); err != nil {
		return catalog.TestCase{}, err
	}
	return list[0], nil
}

// withTaxonomy fills in the tags and classification of test cases, with one query each.
func (s *Store) withTaxonomy(ctx context.Context, list []catalog.TestCase) error {
	ids := make([]int64, len(list))
	at := make(map[int64]int, len(list))
	for i := range list {
		ids[i] = list[i].ID
		at[list[i].ID] = i
		list[i].Tags = []string{}
		list[i].Classification = map[string]string{}
	}
	tags, err := s.q.ListTestCaseTags(ctx, ids)
	if err != nil {
		return err
	}
	for _, t := range tags {
		tc := &list[at[t.TestCaseID]]
		tc.Tags = append(tc.Tags, t.Tag)
	}
	classes, err := s.q.ListTestCaseClassifications(ctx, ids)
	if err != nil {
		return err
	}
	for _, c := range classes {
		list[at[c.TestCaseID]].Classification[c.Dimension] = c.Value
	}
	return nil
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
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, DeprecatedAt: timePtr(r.DeprecatedAt), Version: r.Version,
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
func (s *Store) LockTestCase(ctx context.Context, id int64) (int64, error) {
	v, err := s.q.LockTestCase(ctx, id)
	return v, notFound(err)
}

// ListTestCases implements catalog.Repository.
func (s *Store) ListTestCases(ctx context.Context, f catalog.ListFilter, limit, offset int32) ([]catalog.TestCase, error) {
	rows, err := s.q.ListTestCases(ctx, catalogdb.ListTestCasesParams{
		Status: statusText(f.Status), ProjectIds: f.ProjectIDs, Tag: text(f.Tag), Classified: f.Classified,
		SuiteID: int8Arg(f.SuiteID), Automated: boolArg(f.Automated), Number: int8Arg(f.Number), PageLimit: limit, PageOffset: offset,
		Search: text(f.Search), SearchNumber: int8Arg(f.SearchNumber),
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
	return out, s.withTaxonomy(ctx, out)
}

// CountTestCases implements catalog.Repository.
func (s *Store) CountTestCases(ctx context.Context, f catalog.ListFilter) (int64, error) {
	return s.q.CountTestCases(ctx, catalogdb.CountTestCasesParams{
		Status: statusText(f.Status), ProjectIds: f.ProjectIDs, Tag: text(f.Tag), Classified: f.Classified,
		SuiteID: int8Arg(f.SuiteID), Automated: boolArg(f.Automated), Number: int8Arg(f.Number),
		Search: text(f.Search), SearchNumber: int8Arg(f.SearchNumber),
	})
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
func (s *Store) ListProjects(ctx context.Context, projectIDs []int64, limit, offset int32) ([]catalog.Project, error) {
	rows, err := s.q.ListProjects(ctx, catalogdb.ListProjectsParams{ProjectIds: projectIDs, PageLimit: limit, PageOffset: offset})
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
func (s *Store) CountProjects(ctx context.Context, projectIDs []int64) (int64, error) {
	return s.q.CountProjects(ctx, projectIDs)
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

func toDimension(r catalogdb.ClassificationDimension) catalog.Dimension {
	return catalog.Dimension{
		ID: r.ID, ProjectID: r.ProjectID, Key: r.Key, Name: r.Name, BuiltIn: r.BuiltIn,
		ArchivedAt: timePtr(r.ArchivedAt), CreatedAt: r.CreatedAt.Time, Values: []catalog.DimensionValue{},
	}
}

func toValue(r catalogdb.ClassificationValue) catalog.DimensionValue {
	return catalog.DimensionValue{
		ID: r.ID, DimensionID: r.DimensionID, Key: r.Key, Name: r.Name, Position: r.Position,
		ArchivedAt: timePtr(r.ArchivedAt), CreatedAt: r.CreatedAt.Time,
	}
}

func boolArg(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

// ListDimensions implements catalog.Repository.
func (s *Store) ListDimensions(ctx context.Context, projectID int64) ([]catalog.Dimension, error) {
	dims, err := s.q.ListDimensions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	values, err := s.q.ListDimensionValues(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Dimension, len(dims))
	at := make(map[int64]int, len(dims))
	for i, d := range dims {
		out[i] = toDimension(d)
		at[d.ID] = i
	}
	for _, v := range values {
		d := &out[at[v.DimensionID]]
		d.Values = append(d.Values, toValue(v))
	}
	return out, nil
}

// LockScope implements catalog.Repository.
func (s *Store) LockScope(ctx context.Context, scope string, id int64) error {
	return s.q.LockLinkParent(ctx, catalogdb.LockLinkParentParams{Kind: scope, ID: id})
}

// CreateDimension implements catalog.Repository.
func (s *Store) CreateDimension(ctx context.Context, projectID int64, in catalog.DimensionInput) (catalog.Dimension, error) {
	r, err := s.q.CreateDimension(ctx, catalogdb.CreateDimensionParams{ProjectID: projectID, Key: in.Key, Name: in.Name})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Dimension{}, catalog.ErrConflict
	}
	return toDimension(r), err
}

// UpdateDimension implements catalog.Repository.
func (s *Store) UpdateDimension(ctx context.Context, projectID int64, key string, in catalog.UpdateDimensionInput) (catalog.Dimension, error) {
	r, err := s.q.UpdateDimension(ctx, catalogdb.UpdateDimensionParams{ProjectID: projectID, Key: key, Name: text(in.Name), Archived: boolArg(in.Archived)})
	return toDimension(r), notFound(err)
}

// CreateDimensionValue implements catalog.Repository.
func (s *Store) CreateDimensionValue(ctx context.Context, dimensionID int64, in catalog.DimensionInput) (catalog.DimensionValue, error) {
	r, err := s.q.CreateDimensionValue(ctx, catalogdb.CreateDimensionValueParams{DimensionID: dimensionID, Key: in.Key, Name: in.Name})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.DimensionValue{}, catalog.ErrConflict
	}
	return toValue(r), err
}

// UpdateDimensionValue implements catalog.Repository.
func (s *Store) UpdateDimensionValue(ctx context.Context, dimensionID int64, key string, in catalog.UpdateDimensionInput) (catalog.DimensionValue, error) {
	r, err := s.q.UpdateDimensionValue(ctx, catalogdb.UpdateDimensionValueParams{DimensionID: dimensionID, Key: key, Name: text(in.Name), Archived: boolArg(in.Archived)})
	return toValue(r), notFound(err)
}

// SetTags implements catalog.Repository. Unchanged tags are neither deleted nor inserted, so a no-op
// does not advance the version.
func (s *Store) SetTags(ctx context.Context, testCaseID int64, tags []string) error {
	if err := s.q.DeleteTestCaseTags(ctx, catalogdb.DeleteTestCaseTagsParams{TestCaseID: testCaseID, Keep: tags}); err != nil {
		return err
	}
	return s.q.AddTestCaseTags(ctx, catalogdb.AddTestCaseTagsParams{TestCaseID: testCaseID, Tags: tags})
}

// SetClassification implements catalog.Repository.
func (s *Store) SetClassification(ctx context.Context, testCaseID, projectID, dimensionID, valueID int64) error {
	if valueID == 0 {
		return s.q.ClearTestCaseClassification(ctx, catalogdb.ClearTestCaseClassificationParams{TestCaseID: testCaseID, DimensionID: dimensionID})
	}
	return s.q.SetTestCaseClassification(ctx, catalogdb.SetTestCaseClassificationParams{
		TestCaseID: testCaseID, ProjectID: projectID, DimensionID: dimensionID, ValueID: valueID,
	})
}

func int8Arg(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ListTestCaseIDs implements catalog.Repository.
func (s *Store) ListTestCaseIDs(ctx context.Context, f catalog.ListFilter) ([]int64, error) {
	ids, err := s.q.ListTestCaseIDs(ctx, catalogdb.ListTestCaseIDsParams{
		Status: statusText(f.Status), ProjectIds: f.ProjectIDs, Tag: text(f.Tag), Classified: f.Classified,
		SuiteID: int8Arg(f.SuiteID), Automated: boolArg(f.Automated),
	})
	return nonNil(ids), err
}

func toSuite(r catalogdb.ListSuitesRow) catalog.Suite {
	su := catalog.Suite{
		ID: r.ID, ProjectID: r.ProjectID, Key: r.Key, Name: r.Name, Description: r.Description, Kind: catalog.SuiteKind(r.Kind),
		Query:      catalog.SuiteQuery{Classified: nonNil(r.QueryClassified)},
		ArchivedAt: timePtr(r.ArchivedAt), CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, CaseCount: r.CaseCount,
	}
	if r.QueryTag.Valid {
		su.Query.Tag = &r.QueryTag.String
	}
	return su
}

// ListSuites implements catalog.Repository.
func (s *Store) ListSuites(ctx context.Context, projectID int64) ([]catalog.Suite, error) {
	rows, err := s.q.ListSuites(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Suite, len(rows))
	for i, r := range rows {
		out[i] = toSuite(r)
	}
	return out, nil
}

// GetSuite implements catalog.Repository.
func (s *Store) GetSuite(ctx context.Context, projectID int64, key string) (catalog.Suite, error) {
	r, err := s.q.GetSuite(ctx, catalogdb.GetSuiteParams{ProjectID: projectID, Key: key})
	if err != nil {
		return catalog.Suite{}, notFound(err)
	}
	su := toSuite(catalogdb.ListSuitesRow(r))
	ids, err := s.q.ListSuiteCaseIDs(ctx, su.ID)
	su.CaseIDs = nonNil(ids)
	return su, err
}

// CreateSuite implements catalog.Repository.
func (s *Store) CreateSuite(ctx context.Context, projectID int64, in catalog.SuiteInput) (int64, error) {
	id, err := s.q.CreateSuite(ctx, catalogdb.CreateSuiteParams{
		ProjectID: projectID, Key: in.Key, Name: in.Name, Description: in.Description, Kind: string(in.Kind),
		QueryTag: text(in.Query.Tag), QueryClassified: nonNil(in.Query.Classified),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, catalog.ErrConflict
	}
	return id, err
}

// UpdateSuite implements catalog.Repository.
func (s *Store) UpdateSuite(ctx context.Context, projectID int64, key string, in catalog.UpdateSuiteInput) (int64, error) {
	params := catalogdb.UpdateSuiteParams{
		ProjectID: projectID, Key: key, Name: text(in.Name), Description: text(in.Description), Archived: boolArg(in.Archived),
		QueryClassified: []string{},
	}
	if in.Query != nil {
		params.SetQuery, params.QueryTag, params.QueryClassified = true, text(in.Query.Tag), nonNil(in.Query.Classified)
	}
	id, err := s.q.UpdateSuite(ctx, params)
	return id, notFound(err)
}

// SetSuiteCases implements catalog.Repository.
func (s *Store) SetSuiteCases(ctx context.Context, suiteID, projectID int64, ids []int64) error {
	// One error path: the lock and the delete fail the same way (a broken connection).
	err := s.q.LockLinkParent(ctx, catalogdb.LockLinkParentParams{Kind: "suite", ID: suiteID})
	if err == nil {
		err = s.q.DeleteSuiteCases(ctx, catalogdb.DeleteSuiteCasesParams{SuiteID: suiteID, Keep: ids})
	}
	if err != nil {
		return err
	}
	return s.q.AddSuiteCases(ctx, catalogdb.AddSuiteCasesParams{SuiteID: suiteID, ProjectID: projectID, TestCaseIds: nonNil(ids)})
}

// ProjectCaseIDs implements catalog.Repository.
func (s *Store) ProjectCaseIDs(ctx context.Context, projectID int64, ids []int64) ([]int64, error) {
	found, err := s.q.ListProjectCaseIDs(ctx, catalogdb.ListProjectCaseIDsParams{ProjectID: projectID, Ids: ids})
	return nonNil(found), err
}

func toRequirement(r catalogdb.ListRequirementsRow) catalog.Requirement {
	return catalog.Requirement{
		ID: r.ID, ProjectID: r.ProjectID, Provider: r.Provider, ExternalID: r.ExternalID, Title: r.Title, Description: r.Description,
		URL: r.Url, ProviderStatus: r.ProviderStatus, ArchivedAt: timePtr(r.ArchivedAt), LastSyncedAt: timePtr(r.LastSyncedAt),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, TestCaseIDs: nonNil(r.TestCaseIds),
	}
}

// ListRequirements implements catalog.Repository.
func (s *Store) ListRequirements(ctx context.Context, projectID int64, testCaseID *int64) ([]catalog.Requirement, error) {
	rows, err := s.q.ListRequirements(ctx, catalogdb.ListRequirementsParams{ProjectID: projectID, TestCaseID: int8Arg(testCaseID)})
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Requirement, len(rows))
	for i, r := range rows {
		out[i] = toRequirement(r)
	}
	return out, nil
}

// GetRequirement implements catalog.Repository.
func (s *Store) GetRequirement(ctx context.Context, projectID, id int64) (catalog.Requirement, error) {
	r, err := s.q.GetRequirement(ctx, catalogdb.GetRequirementParams{ProjectID: projectID, ID: id})
	return toRequirement(catalogdb.ListRequirementsRow(r)), notFound(err)
}

// NextNativeRequirementNumber implements catalog.Repository.
func (s *Store) NextNativeRequirementNumber(ctx context.Context, projectID int64) (int64, error) {
	return s.q.NextNativeRequirementNumber(ctx, projectID)
}

// UpsertRequirement implements catalog.Repository.
func (s *Store) UpsertRequirement(ctx context.Context, projectID int64, in catalog.RequirementInput, sync bool, syncedAt *time.Time) (int64, bool, bool, error) {
	r, err := s.q.UpsertRequirement(ctx, catalogdb.UpsertRequirementParams{
		ProjectID: projectID, Provider: in.Provider, ExternalID: in.ExternalID, Title: in.Title, Description: in.Description,
		Url: in.URL, ProviderStatus: in.ProviderStatus, LastSyncedAt: timestamptzArg(syncedAt), Sync: sync,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, nil
	}
	return r.ID, r.Created, err == nil, err
}

// UpdateRequirement implements catalog.Repository.
func (s *Store) UpdateRequirement(ctx context.Context, projectID, id int64, in catalog.UpdateRequirementInput) error {
	_, err := s.q.UpdateRequirement(ctx, catalogdb.UpdateRequirementParams{
		ProjectID: projectID, ID: id, Title: text(in.Title), Description: text(in.Description), Url: text(in.URL),
		ProviderStatus: text(in.ProviderStatus), Archived: boolArg(in.Archived),
	})
	return notFound(err)
}

// SetRequirementTestCases implements catalog.Repository.
func (s *Store) SetRequirementTestCases(ctx context.Context, requirementID, projectID int64, ids []int64) error {
	// One error path: the lock and the delete fail the same way (a broken connection).
	err := s.q.LockLinkParent(ctx, catalogdb.LockLinkParentParams{Kind: "requirement", ID: requirementID})
	if err == nil {
		err = s.q.DeleteRequirementLinks(ctx, catalogdb.DeleteRequirementLinksParams{RequirementID: requirementID, Keep: ids})
	}
	if err != nil {
		return err
	}
	return s.q.AddRequirementLinks(ctx, catalogdb.AddRequirementLinksParams{RequirementID: requirementID, ProjectID: projectID, TestCaseIds: nonNil(ids)})
}

func timestamptzArg(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func toIssue(r catalogdb.ListIssuesRow) catalog.Issue {
	return catalog.Issue{
		ID: r.ID, ProjectID: r.ProjectID, Provider: r.Provider, ExternalID: r.ExternalID, Title: r.Title, Description: r.Description,
		URL: r.Url, State: r.State, ProviderStatus: r.ProviderStatus, ClosedAt: timePtr(r.ClosedAt), LastSyncedAt: timePtr(r.LastSyncedAt),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, TestCaseIDs: nonNil(r.TestCaseIds),
	}
}

// ListIssues implements catalog.Repository.
func (s *Store) ListIssues(ctx context.Context, projectID int64, f catalog.IssueFilter) ([]catalog.Issue, error) {
	rows, err := s.q.ListIssues(ctx, catalogdb.ListIssuesParams{ProjectID: projectID, State: text(f.State), TestCaseID: int8Arg(f.TestCaseID)})
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Issue, len(rows))
	for i, r := range rows {
		out[i] = toIssue(r)
	}
	return out, nil
}

// GetIssue implements catalog.Repository.
func (s *Store) GetIssue(ctx context.Context, projectID, id int64) (catalog.Issue, error) {
	r, err := s.q.GetIssue(ctx, catalogdb.GetIssueParams{ProjectID: projectID, ID: id})
	return toIssue(catalogdb.ListIssuesRow(r)), notFound(err)
}

// NextNativeIssueNumber implements catalog.Repository.
func (s *Store) NextNativeIssueNumber(ctx context.Context, projectID int64) (int64, error) {
	return s.q.NextNativeIssueNumber(ctx, projectID)
}

// UpsertIssue implements catalog.Repository.
func (s *Store) UpsertIssue(ctx context.Context, projectID int64, in catalog.IssueInput, sync bool, syncedAt *time.Time) (int64, bool, bool, error) {
	r, err := s.q.UpsertIssue(ctx, catalogdb.UpsertIssueParams{
		ProjectID: projectID, Provider: in.Provider, ExternalID: in.ExternalID, Title: in.Title, Description: in.Description,
		Url: in.URL, State: in.State, ProviderStatus: in.ProviderStatus, LastSyncedAt: timestamptzArg(syncedAt), Sync: sync,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, nil
	}
	return r.ID, r.Created, err == nil, err
}

// UpdateIssue implements catalog.Repository.
func (s *Store) UpdateIssue(ctx context.Context, projectID, id int64, in catalog.UpdateIssueInput) error {
	_, err := s.q.UpdateIssue(ctx, catalogdb.UpdateIssueParams{
		ProjectID: projectID, ID: id, Title: text(in.Title), Description: text(in.Description), Url: text(in.URL),
		ProviderStatus: text(in.ProviderStatus), State: text(in.State),
	})
	return notFound(err)
}

// SetIssueTestCases implements catalog.Repository.
func (s *Store) SetIssueTestCases(ctx context.Context, issueID, projectID int64, ids []int64) error {
	// One error path: the lock and the delete fail the same way (a broken connection).
	err := s.q.LockLinkParent(ctx, catalogdb.LockLinkParentParams{Kind: "issue", ID: issueID})
	if err == nil {
		err = s.q.DeleteIssueLinks(ctx, catalogdb.DeleteIssueLinksParams{IssueID: issueID, Keep: ids})
	}
	if err != nil {
		return err
	}
	return s.q.AddIssueLinks(ctx, catalogdb.AddIssueLinksParams{IssueID: issueID, ProjectID: projectID, TestCaseIds: nonNil(ids)})
}
