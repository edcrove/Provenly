package catalog

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"
)

// fakeRepo is an in-memory Repository with per-method error injection.
type fakeRepo struct {
	projects map[int64]Project
	nextNum  map[int64]int64
	nextPID  int64
	cases    map[int64]TestCase
	steps    map[int64][]TestStep
	nextID   int64
	nextSID  int64
	errs     map[string]error
	now      time.Time
	// locks counts LockTestCase calls; errs["LockTestCase#n"] fails the n-th one.
	locks int
}

func newFakeRepo() *fakeRepo {
	f := &fakeRepo{
		projects: map[int64]Project{}, nextNum: map[int64]int64{},
		cases: map[int64]TestCase{}, steps: map[int64][]TestStep{}, errs: map[string]error{},
		now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	_, _ = f.CreateProject(context.Background(), CreateProjectInput{Key: DefaultProjectKey, Name: "Default"})
	return f
}

func (f *fakeRepo) fail(method string) error { return f.errs[method] }

func (f *fakeRepo) CreateTestCase(_ context.Context, in CreateInput) (TestCase, error) {
	if err := f.fail("CreateTestCase"); err != nil {
		return TestCase{}, err
	}
	p, ok := f.projects[in.ProjectID]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	f.nextID++
	f.nextNum[p.ID]++
	tc := TestCase{ID: f.nextID, ProjectID: p.ID, ProjectKey: p.Key, Number: f.nextNum[p.ID], Title: in.Title, Description: in.Description, ExpectedResult: in.ExpectedResult,
		Automated: in.Automated, Status: StatusActive, CreatedAt: f.now, UpdatedAt: f.now, Version: 1}
	f.cases[tc.ID] = tc
	return tc, nil
}

func (f *fakeRepo) GetTestCase(_ context.Context, id int64) (TestCase, error) {
	if err := f.fail("GetTestCase"); err != nil {
		return TestCase{}, err
	}
	tc, ok := f.cases[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	return tc, nil
}

func (f *fakeRepo) LockTestCase(_ context.Context, id int64) (int64, error) {
	f.locks++
	if err := f.fail("LockTestCase"); err != nil {
		return 0, err
	}
	if err := f.fail(fmt.Sprintf("LockTestCase#%d", f.locks)); err != nil {
		return 0, err
	}
	tc, ok := f.cases[id]
	if !ok {
		return 0, ErrNotFound
	}
	return tc.Version, nil
}

// advance moves a test case to its next version, like the database triggers on test cases and steps.
func (f *fakeRepo) advance(id int64) {
	tc := f.cases[id]
	tc.Version++
	f.cases[id] = tc
}

func (f *fakeRepo) filtered(lf ListFilter) []TestCase {
	var out []TestCase
	for _, tc := range f.cases {
		if (lf.Status == nil || tc.Status == *lf.Status) && (lf.ProjectIDs == nil || slices.Contains(lf.ProjectIDs, tc.ProjectID)) {
			out = append(out, tc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (f *fakeRepo) ListTestCases(_ context.Context, lf ListFilter, limit, offset int32) ([]TestCase, error) {
	if err := f.fail("ListTestCases"); err != nil {
		return nil, err
	}
	all := f.filtered(lf)
	end := min(int(offset+limit), len(all))
	if int(offset) >= len(all) {
		return []TestCase{}, nil
	}
	return all[offset:end], nil
}

func (f *fakeRepo) CountTestCases(_ context.Context, lf ListFilter) (int64, error) {
	if err := f.fail("CountTestCases"); err != nil {
		return 0, err
	}
	return int64(len(f.filtered(lf))), nil
}

func (f *fakeRepo) UpdateTestCase(_ context.Context, id int64, in UpdateInput) (TestCase, error) {
	if err := f.fail("UpdateTestCase"); err != nil {
		return TestCase{}, err
	}
	tc, ok := f.cases[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	if in.Title != nil {
		tc.Title = *in.Title
	}
	if in.Description != nil {
		tc.Description = *in.Description
	}
	if in.ExpectedResult != nil {
		tc.ExpectedResult = *in.ExpectedResult
	}
	if in.Automated != nil {
		tc.Automated = *in.Automated
	}
	tc.Version++
	f.cases[id] = tc
	return tc, nil
}

func (f *fakeRepo) DeprecateTestCase(_ context.Context, id int64) (TestCase, error) {
	if err := f.fail("DeprecateTestCase"); err != nil {
		return TestCase{}, err
	}
	tc, ok := f.cases[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	tc.Status = StatusDeprecated
	tc.Version++
	f.cases[id] = tc
	return tc, nil
}

func (f *fakeRepo) ReactivateTestCase(_ context.Context, id int64) (TestCase, error) {
	if err := f.fail("ReactivateTestCase"); err != nil {
		return TestCase{}, err
	}
	tc, ok := f.cases[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	tc.Status, tc.DeprecatedAt = StatusActive, nil
	tc.Version++
	f.cases[id] = tc
	return tc, nil
}

func (f *fakeRepo) ListIngestionView(_ context.Context, projectID int64, numbers []int64) (IngestionView, error) {
	if err := f.fail("ListIngestionView"); err != nil {
		return IngestionView{}, err
	}
	view := IngestionView{Entries: map[int64]IngestionEntry{}}
	for _, tc := range f.filtered(ListFilter{ProjectIDs: []int64{projectID}}) {
		if tc.Status == StatusActive && tc.Automated {
			view.Expected = append(view.Expected, tc.ID)
		}
		for _, n := range numbers {
			if n == tc.Number {
				view.Entries[n] = IngestionEntry{ID: tc.ID, Status: tc.Status}
			}
		}
	}
	return view, nil
}

func (f *fakeRepo) ListTestCaseKeys(_ context.Context, ids []int64) (map[int64]string, error) {
	if err := f.fail("ListTestCaseKeys"); err != nil {
		return nil, err
	}
	keys := map[int64]string{}
	for _, id := range ids {
		if tc, ok := f.cases[id]; ok {
			keys[id] = tc.Key()
		}
	}
	return keys, nil
}

func (f *fakeRepo) CreateProject(_ context.Context, in CreateProjectInput) (Project, error) {
	if err := f.fail("CreateProject"); err != nil {
		return Project{}, err
	}
	for _, p := range f.projects {
		if p.Key == in.Key {
			return Project{}, ErrConflict
		}
	}
	f.nextPID++
	p := Project{ID: f.nextPID, Key: in.Key, Name: in.Name, Description: in.Description, CreatedAt: f.now, UpdatedAt: f.now}
	f.projects[p.ID] = p
	return p, nil
}

func (f *fakeRepo) GetProject(_ context.Context, id int64) (Project, error) {
	if err := f.fail("GetProject"); err != nil {
		return Project{}, err
	}
	p, ok := f.projects[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) GetProjectByKey(_ context.Context, key string) (Project, error) {
	if err := f.fail("GetProjectByKey"); err != nil {
		return Project{}, err
	}
	for _, p := range f.projects {
		if p.Key == key {
			return p, nil
		}
	}
	return Project{}, ErrNotFound
}

func (f *fakeRepo) ListProjects(_ context.Context, ids []int64, limit, offset int32) ([]Project, error) {
	if err := f.fail("ListProjects"); err != nil {
		return nil, err
	}
	var all []Project
	for _, p := range f.projects {
		if ids == nil || slices.Contains(ids, p.ID) {
			all = append(all, p)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	if int(offset) >= len(all) {
		return []Project{}, nil
	}
	return all[offset:min(int(offset+limit), len(all))], nil
}

func (f *fakeRepo) CountProjects(ctx context.Context, ids []int64) (int64, error) {
	if err := f.fail("CountProjects"); err != nil {
		return 0, err
	}
	all, _ := f.ListProjects(ctx, ids, 1<<30, 0)
	return int64(len(all)), nil
}

func (f *fakeRepo) UpdateProject(_ context.Context, key string, in UpdateProjectInput) (Project, error) {
	if err := f.fail("UpdateProject"); err != nil {
		return Project{}, err
	}
	for id, p := range f.projects {
		if p.Key == key {
			if in.Name != nil {
				p.Name = *in.Name
			}
			if in.Description != nil {
				p.Description = *in.Description
			}
			f.projects[id] = p
			return p, nil
		}
	}
	return Project{}, ErrNotFound
}

func (f *fakeRepo) sorted(tcID int64) []TestStep {
	st := append([]TestStep(nil), f.steps[tcID]...)
	sort.Slice(st, func(i, j int) bool { return st[i].Position < st[j].Position })
	return st
}

func (f *fakeRepo) ListTestSteps(_ context.Context, tcID int64, limit, offset int32) ([]TestStep, error) {
	if err := f.fail("ListTestSteps"); err != nil {
		return nil, err
	}
	all := f.sorted(tcID)
	if int(offset) >= len(all) {
		return []TestStep{}, nil
	}
	return all[offset:min(int(offset+limit), len(all))], nil
}

func (f *fakeRepo) ListAllTestSteps(_ context.Context, tcID int64) ([]TestStep, error) {
	if err := f.fail("ListAllTestSteps"); err != nil {
		return nil, err
	}
	return f.sorted(tcID), nil
}

func (f *fakeRepo) CountTestSteps(_ context.Context, tcID int64) (int64, error) {
	if err := f.fail("CountTestSteps"); err != nil {
		return 0, err
	}
	return int64(len(f.steps[tcID])), nil
}

func (f *fakeRepo) ShiftTestStepsDown(_ context.Context, tcID int64, from int32) error {
	if err := f.fail("ShiftTestStepsDown"); err != nil {
		return err
	}
	for i := range f.steps[tcID] {
		if f.steps[tcID][i].Position >= from {
			f.steps[tcID][i].Position++
		}
	}
	return nil
}

func (f *fakeRepo) CreateTestStep(_ context.Context, tcID int64, position int32, action, expected string) (TestStep, error) {
	if err := f.fail("CreateTestStep"); err != nil {
		return TestStep{}, err
	}
	f.nextSID++
	st := TestStep{ID: f.nextSID, TestCaseID: tcID, Position: position, Action: action, ExpectedResult: expected}
	f.steps[tcID] = append(f.steps[tcID], st)
	f.advance(tcID)
	return st, nil
}

func (f *fakeRepo) UpdateTestStep(_ context.Context, tcID, stepID int64, in UpdateStepInput) (TestStep, error) {
	if err := f.fail("UpdateTestStep"); err != nil {
		return TestStep{}, err
	}
	for i, st := range f.steps[tcID] {
		if st.ID == stepID {
			if in.Action != nil {
				st.Action = *in.Action
			}
			if in.ExpectedResult != nil {
				st.ExpectedResult = *in.ExpectedResult
			}
			f.steps[tcID][i] = st
			f.advance(tcID)
			return st, nil
		}
	}
	return TestStep{}, ErrNotFound
}

func (f *fakeRepo) DeleteTestStep(_ context.Context, tcID, stepID int64) (int32, error) {
	if err := f.fail("DeleteTestStep"); err != nil {
		return 0, err
	}
	for i, st := range f.steps[tcID] {
		if st.ID == stepID {
			f.steps[tcID] = append(f.steps[tcID][:i], f.steps[tcID][i+1:]...)
			f.advance(tcID)
			return st.Position, nil
		}
	}
	return 0, ErrNotFound
}

func (f *fakeRepo) CloseTestStepGap(_ context.Context, tcID int64, after int32) error {
	if err := f.fail("CloseTestStepGap"); err != nil {
		return err
	}
	for i := range f.steps[tcID] {
		if f.steps[tcID][i].Position > after {
			f.steps[tcID][i].Position--
		}
	}
	return nil
}

func (f *fakeRepo) SetTestStepPosition(_ context.Context, tcID, stepID int64, position int32) error {
	if err := f.fail("SetTestStepPosition"); err != nil {
		return err
	}
	for i := range f.steps[tcID] {
		if f.steps[tcID][i].ID == stepID {
			f.steps[tcID][i].Position = position
		}
	}
	f.advance(tcID)
	return nil
}

func (f *fakeRepo) InTx(_ context.Context, fn func(Repository) error) error {
	if err := f.fail("InTx"); err != nil {
		return err
	}
	return fn(f)
}
