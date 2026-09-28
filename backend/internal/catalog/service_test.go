package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

var errBoom = errors.New("boom")

func ptr[T any](v T) *T { return &v }

func kindOf(t *testing.T, err error) apperr.Kind {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "expected apperr, got %v", err)
	return e.Kind
}

func setup(t *testing.T) (*Service, *fakeRepo, context.Context) {
	t.Helper()
	repo := newFakeRepo()
	return NewService(repo), repo, context.Background()
}

func TestFormatKey(t *testing.T) {
	assert.Equal(t, "TC-153", FormatKey(153))
	assert.Equal(t, "TC-7", TestCase{ID: 7}.Key())
}

func TestCreateAssignsIDAndTrims(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{Title: "  Login  ", ExpectedResult: "ok", Automated: true})
	require.NoError(t, err)
	assert.Equal(t, int64(1), tc.ID)
	assert.Equal(t, "Login", tc.Title)
	assert.Equal(t, StatusActive, tc.Status)
	assert.True(t, tc.Automated)
}

func TestCreateValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	long := strings.Repeat("x", 10001)
	for _, in := range []CreateInput{
		{Title: "   "},
		{Title: strings.Repeat("t", 201)},
		{Title: "ok", Description: long},
		{Title: "ok", ExpectedResult: long},
	} {
		_, err := svc.Create(ctx, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}
}

func TestCreateRepoError(t *testing.T) {
	svc, repo, ctx := setup(t)
	repo.errs["CreateTestCase"] = errBoom
	_, err := svc.Create(ctx, CreateInput{Title: "a"})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetAndEnsureExists(t *testing.T) {
	svc, _, ctx := setup(t)
	created, _ := svc.Create(ctx, CreateInput{Title: "a"})
	got, err := svc.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)
	require.NoError(t, svc.EnsureExists(ctx, created.ID))

	_, err = svc.Get(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	assert.Contains(t, err.Error(), "TC-99")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, svc.EnsureExists(ctx, 99)))
}

func TestList(t *testing.T) {
	svc, repo, ctx := setup(t)
	for _, title := range []string{"a", "b", "c"} {
		_, _ = svc.Create(ctx, CreateInput{Title: title})
	}
	_, _ = svc.Deprecate(ctx, 2)
	res, err := svc.List(ctx, nil, pagination.Page{Number: 1, Size: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Total)
	assert.Equal(t, []int64{3, 2}, []int64{res.Items[0].ID, res.Items[1].ID})

	res, err = svc.List(ctx, ptr(StatusDeprecated), pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	repo.errs["CountTestCases"] = errBoom
	_, err = svc.List(ctx, nil, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListTestCases"] = errBoom
	_, err = svc.List(ctx, nil, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestUpdateKeepsIdentity(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "old"})
	up, err := svc.Update(ctx, tc.ID, UpdateInput{Title: ptr(" new "), Description: ptr("d"), ExpectedResult: ptr("e"), Automated: ptr(true)})
	require.NoError(t, err)
	assert.Equal(t, tc.ID, up.ID)
	assert.Equal(t, "new", up.Title)
	assert.Equal(t, "d", up.Description)
	assert.Equal(t, "e", up.ExpectedResult)
	assert.True(t, up.Automated)

	_, err = svc.Update(ctx, 99, UpdateInput{Automated: ptr(false)})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestUpdateValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	long := strings.Repeat("x", 10001)
	for _, in := range []UpdateInput{
		{},
		{Title: ptr(" ")},
		{Title: ptr(strings.Repeat("t", 201))},
		{Description: ptr(long)},
		{ExpectedResult: ptr(long)},
	} {
		_, err := svc.Update(ctx, 1, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}
}

func TestDeprecate(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a", Automated: true})
	dep, err := svc.Deprecate(ctx, tc.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeprecated, dep.Status)
	_, err = svc.Deprecate(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestReactivate(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a", Automated: true})
	_, _ = svc.Deprecate(ctx, tc.ID)
	back, err := svc.Reactivate(ctx, tc.ID)
	require.NoError(t, err)
	assert.Equal(t, tc.ID, back.ID, "same TC-ID")
	assert.Equal(t, StatusActive, back.Status)
	ids, _ := svc.ExpectedUniverse(ctx)
	assert.Equal(t, []int64{tc.ID}, ids, "future runs include it again")
	_, err = svc.Reactivate(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestExpectedUniverseAndStatuses(t *testing.T) {
	svc, _, ctx := setup(t)
	_, _ = svc.Create(ctx, CreateInput{Title: "auto", Automated: true})
	_, _ = svc.Create(ctx, CreateInput{Title: "manual"})
	_, _ = svc.Create(ctx, CreateInput{Title: "auto-deprecated", Automated: true})
	_, _ = svc.Deprecate(ctx, 3)

	ids, err := svc.ExpectedUniverse(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids)

	st, err := svc.Statuses(ctx, []int64{1, 3, 42})
	require.NoError(t, err)
	assert.Equal(t, map[int64]Status{1: StatusActive, 3: StatusDeprecated}, st)

	st, err = svc.Statuses(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, st)
}

func TestStepsLifecycle(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a"})
	s1, err := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "open", ExpectedResult: "page"})
	require.NoError(t, err)
	s2, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "click"})
	s0, err := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "prepare", Position: ptr(int32(1))})
	require.NoError(t, err)
	assert.Equal(t, int32(1), s0.Position)
	s3, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "last", Position: ptr(int32(50))})
	assert.Equal(t, int32(4), s3.Position)

	page, err := svc.ListSteps(ctx, tc.ID, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(4), page.Total)
	assert.Equal(t, []int64{s0.ID, s1.ID, s2.ID, s3.ID}, stepIDs(page.Items))

	up, err := svc.UpdateStep(ctx, tc.ID, s1.ID, UpdateStepInput{Action: ptr("open app"), ExpectedResult: ptr("home")})
	require.NoError(t, err)
	assert.Equal(t, "open app", up.Action)

	require.NoError(t, svc.DeleteStep(ctx, tc.ID, s0.ID))
	page, _ = svc.ListSteps(ctx, tc.ID, pagination.Default())
	assert.Equal(t, []int32{1, 2, 3}, positions(page.Items))

	steps, err := svc.ReorderSteps(ctx, tc.ID, []int64{s3.ID, s1.ID, s2.ID})
	require.NoError(t, err)
	assert.Equal(t, []int64{s3.ID, s1.ID, s2.ID}, stepIDs(steps))
	assert.Equal(t, []int32{1, 2, 3}, positions(steps))

	after, _ := svc.Get(ctx, tc.ID)
	assert.Equal(t, tc.ID, after.ID, "editing steps never changes the TC-ID")
}

func stepIDs(st []TestStep) []int64 {
	out := make([]int64, len(st))
	for i, s := range st {
		out[i] = s.ID
	}
	return out
}

func positions(st []TestStep) []int32 {
	out := make([]int32, len(st))
	for i, s := range st {
		out[i] = s.Position
	}
	return out
}

func TestStepValidationAndNotFound(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a"})
	for _, in := range []CreateStepInput{
		{Action: " "},
		{Action: strings.Repeat("a", 2001)},
		{Action: "a", ExpectedResult: strings.Repeat("e", 2001)},
		{Action: "a", Position: ptr(int32(0))},
	} {
		_, err := svc.CreateStep(ctx, tc.ID, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}
	_, err := svc.CreateStep(ctx, 99, CreateStepInput{Action: "a"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	_, err = svc.UpdateStep(ctx, tc.ID, 1, UpdateStepInput{})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	_, err = svc.UpdateStep(ctx, 99, 1, UpdateStepInput{Action: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = svc.UpdateStep(ctx, tc.ID, 1, UpdateStepInput{Action: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	assert.Equal(t, apperr.KindNotFound, kindOf(t, svc.DeleteStep(ctx, 99, 1)))
	assert.Equal(t, apperr.KindNotFound, kindOf(t, svc.DeleteStep(ctx, tc.ID, 1)))

	_, err = svc.ListSteps(ctx, 99, pagination.Default())
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	_, err = svc.ReorderSteps(ctx, 99, nil)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	s1, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "a"})
	s2, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "b"})
	for _, ids := range [][]int64{{s1.ID}, {s1.ID, s1.ID}, {s1.ID, 999}, {s2.ID, s1.ID, 5}} {
		_, err = svc.ReorderSteps(ctx, tc.ID, ids)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%v", ids)
	}
}

func TestStepLimit(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a"})
	for i := 0; i < MaxSteps; i++ {
		_, err := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "s"})
		require.NoError(t, err)
	}
	_, err := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "one too many"})
	assert.Equal(t, apperr.KindValidation, kindOf(t, err))
}

func TestStepRepositoryErrors(t *testing.T) {
	type op func(*Service, context.Context, int64, int64) error
	createAt1 := func(s *Service, ctx context.Context, tc, _ int64) error {
		_, err := s.CreateStep(ctx, tc, CreateStepInput{Action: "x", Position: ptr(int32(1))})
		return err
	}
	update := func(s *Service, ctx context.Context, tc, st int64) error {
		_, err := s.UpdateStep(ctx, tc, st, UpdateStepInput{Action: ptr("y")})
		return err
	}
	del := func(s *Service, ctx context.Context, tc, st int64) error { return s.DeleteStep(ctx, tc, st) }
	reorder := func(s *Service, ctx context.Context, tc, st int64) error {
		_, err := s.ReorderSteps(ctx, tc, []int64{st})
		return err
	}
	list := func(s *Service, ctx context.Context, tc, _ int64) error {
		_, err := s.ListSteps(ctx, tc, pagination.Default())
		return err
	}
	cases := []struct {
		method string
		op     op
	}{
		{"InTx", createAt1}, {"LockTestCase", createAt1}, {"CountTestSteps", createAt1},
		{"ShiftTestStepsDown", createAt1}, {"CreateTestStep", createAt1},
		{"UpdateTestStep", update},
		{"DeleteTestStep", del}, {"CloseTestStepGap", del},
		{"ListAllTestSteps", reorder}, {"SetTestStepPosition", reorder},
		{"ListTestSteps", list}, {"CountTestSteps", list},
	}
	for _, c := range cases {
		svc, repo, ctx := setup(t)
		tc, _ := svc.Create(ctx, CreateInput{Title: "a"})
		st, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "s"})
		repo.errs[c.method] = errBoom
		assert.ErrorIs(t, c.op(svc, ctx, tc.ID, st.ID), errBoom, c.method)
	}
}

func TestReorderFinalListError(t *testing.T) {
	svc, repo, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{Title: "a"})
	st, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "s"})
	failing := &failSecondListRepo{fakeRepo: repo}
	svc = NewService(failing)
	_, err := svc.ReorderSteps(ctx, tc.ID, []int64{st.ID})
	assert.ErrorIs(t, err, errBoom)
}

// failSecondListRepo fails the second ListAllTestSteps call (the re-read after reordering).
type failSecondListRepo struct {
	*fakeRepo
	calls int
}

func (f *failSecondListRepo) ListAllTestSteps(ctx context.Context, tcID int64) ([]TestStep, error) {
	f.calls++
	if f.calls == 2 {
		return nil, errBoom
	}
	return f.fakeRepo.ListAllTestSteps(ctx, tcID)
}

func (f *failSecondListRepo) InTx(_ context.Context, fn func(Repository) error) error { return fn(f) }
