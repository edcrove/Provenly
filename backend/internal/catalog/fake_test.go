package catalog

import (
	"context"
	"sort"
	"time"
)

// fakeRepo is an in-memory Repository with per-method error injection.
type fakeRepo struct {
	cases   map[int64]TestCase
	steps   map[int64][]TestStep
	nextID  int64
	nextSID int64
	errs    map[string]error
	now     time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		cases: map[int64]TestCase{}, steps: map[int64][]TestStep{}, errs: map[string]error{},
		now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func (f *fakeRepo) fail(method string) error { return f.errs[method] }

func (f *fakeRepo) CreateTestCase(_ context.Context, in CreateInput) (TestCase, error) {
	if err := f.fail("CreateTestCase"); err != nil {
		return TestCase{}, err
	}
	f.nextID++
	tc := TestCase{ID: f.nextID, Title: in.Title, Description: in.Description, ExpectedResult: in.ExpectedResult,
		Automated: in.Automated, Status: StatusActive, CreatedAt: f.now, UpdatedAt: f.now}
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

func (f *fakeRepo) LockTestCase(_ context.Context, id int64) error {
	if err := f.fail("LockTestCase"); err != nil {
		return err
	}
	if _, ok := f.cases[id]; !ok {
		return ErrNotFound
	}
	return nil
}

func (f *fakeRepo) filtered(status *Status) []TestCase {
	var out []TestCase
	for _, tc := range f.cases {
		if status == nil || tc.Status == *status {
			out = append(out, tc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (f *fakeRepo) ListTestCases(_ context.Context, status *Status, limit, offset int32) ([]TestCase, error) {
	if err := f.fail("ListTestCases"); err != nil {
		return nil, err
	}
	all := f.filtered(status)
	end := min(int(offset+limit), len(all))
	if int(offset) >= len(all) {
		return []TestCase{}, nil
	}
	return all[offset:end], nil
}

func (f *fakeRepo) CountTestCases(_ context.Context, status *Status) (int64, error) {
	if err := f.fail("CountTestCases"); err != nil {
		return 0, err
	}
	return int64(len(f.filtered(status))), nil
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
	f.cases[id] = tc
	return tc, nil
}

func (f *fakeRepo) ListExpectedUniverse(context.Context) ([]int64, error) {
	if err := f.fail("ListExpectedUniverse"); err != nil {
		return nil, err
	}
	var ids []int64
	for _, tc := range f.filtered(nil) {
		if tc.Status == StatusActive && tc.Automated {
			ids = append(ids, tc.ID)
		}
	}
	return ids, nil
}

func (f *fakeRepo) ListTestCaseStatuses(_ context.Context, ids []int64) (map[int64]Status, error) {
	if err := f.fail("ListTestCaseStatuses"); err != nil {
		return nil, err
	}
	out := map[int64]Status{}
	for _, id := range ids {
		if tc, ok := f.cases[id]; ok {
			out[id] = tc.Status
		}
	}
	return out, nil
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
	return nil
}

func (f *fakeRepo) InTx(_ context.Context, fn func(Repository) error) error {
	if err := f.fail("InTx"); err != nil {
		return err
	}
	return fn(f)
}
