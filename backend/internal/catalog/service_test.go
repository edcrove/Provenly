package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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
	assert.Equal(t, "TC-153", FormatKey("TC", 153))
	assert.Equal(t, "CHK-7", TestCase{ID: 40, ProjectKey: "CHK", Number: 7}.Key())
}

func TestCreateAssignsIDAndTrims(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "  Login  ", ExpectedResult: "ok", Automated: true})
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
		{Title: "a\x00b"},
		{Title: "ok", Description: "\x00"},
		{Title: "ok", ExpectedResult: "\xff"},
	} {
		_, err := svc.Create(ctx, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}
}

// Unicode whitespace (tab, NBSP, ideographic space, line separator, ...) is blank too:
// the API rejects it before the database CHECK would (found validating Test Steps Management).
func TestUnicodeWhitespaceIsBlank(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "ok"})
	require.NoError(t, err)
	for _, ws := range []string{"\t\n", "\u00a0", "\u3000", "\u2028", "\u2003\u2009", "\u0085"} {
		_, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: ws})
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "title %q", ws)
		_, err = svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: ws})
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "action %q", ws)
	}
}

func TestCreateRepoError(t *testing.T) {
	svc, repo, ctx := setup(t)
	repo.errs["CreateTestCase"] = errBoom
	_, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetAndEnsureExists(t *testing.T) {
	svc, _, ctx := setup(t)
	created, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
	got, err := svc.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)
	require.NoError(t, svc.EnsureExists(ctx, created.ID))

	_, err = svc.Get(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	assert.Contains(t, err.Error(), "test case 99 not found")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, svc.EnsureExists(ctx, 99)))
}

func TestList(t *testing.T) {
	svc, repo, ctx := setup(t)
	for _, title := range []string{"a", "b", "c"} {
		_, _ = svc.Create(ctx, CreateInput{ProjectID: 1, Title: title})
	}
	_, _ = svc.Deprecate(ctx, 2)
	res, err := svc.List(ctx, ListFilter{}, pagination.Page{Number: 1, Size: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Total)
	assert.Equal(t, []int64{3, 2}, []int64{res.Items[0].ID, res.Items[1].ID})

	res, err = svc.List(ctx, ListFilter{Status: ptr(StatusDeprecated)}, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	repo.errs["CountTestCases"] = errBoom
	_, err = svc.List(ctx, ListFilter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListTestCases"] = errBoom
	_, err = svc.List(ctx, ListFilter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestUpdateKeepsIdentity(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "old"})
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
		{Title: ptr("a\x00")},
		{Description: ptr("\x00")},
		{ExpectedResult: ptr("\x00")},
	} {
		_, err := svc.Update(ctx, 1, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err))
	}
}

func TestDeprecate(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a", Automated: true})
	dep, err := svc.Deprecate(ctx, tc.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeprecated, dep.Status)
	_, err = svc.Deprecate(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestReactivate(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a", Automated: true})
	_, _ = svc.Deprecate(ctx, tc.ID)
	back, err := svc.Reactivate(ctx, tc.ID)
	require.NoError(t, err)
	assert.Equal(t, tc.ID, back.ID, "same TC-ID")
	assert.Equal(t, StatusActive, back.Status)
	view, _ := svc.IngestionView(ctx, 1, nil)
	assert.Equal(t, []int64{tc.ID}, view.Expected, "future runs include it again")
	_, err = svc.Reactivate(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
}

func TestExpectedUniverseAndStatuses(t *testing.T) {
	svc, _, ctx := setup(t)
	_, _ = svc.Create(ctx, CreateInput{ProjectID: 1, Title: "auto", Automated: true})
	_, _ = svc.Create(ctx, CreateInput{ProjectID: 1, Title: "manual"})
	_, _ = svc.Create(ctx, CreateInput{ProjectID: 1, Title: "auto-deprecated", Automated: true})
	_, _ = svc.Deprecate(ctx, 3)

	view, err := svc.IngestionView(ctx, 1, []int64{1, 3, 42})
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, view.Expected)
	assert.Equal(t, map[int64]IngestionEntry{1: {ID: 1, Status: StatusActive}, 3: {ID: 3, Status: StatusDeprecated}}, view.Entries)

	view, err = svc.IngestionView(ctx, 1, nil)
	require.NoError(t, err)
	assert.Empty(t, view.Entries)
}

func TestStepsLifecycle(t *testing.T) {
	svc, _, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
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
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
	for _, in := range []CreateStepInput{
		{Action: " "},
		{Action: strings.Repeat("a", 2001)},
		{Action: "a", ExpectedResult: strings.Repeat("e", 2001)},
		{Action: "a", Position: ptr(int32(0))},
		{Action: "a\x00"},
		{Action: "a", ExpectedResult: "\x00"},
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
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
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
		tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
		st, _ := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "s"})
		repo.errs[c.method] = errBoom
		assert.ErrorIs(t, c.op(svc, ctx, tc.ID, st.ID), errBoom, c.method)
	}
}

func TestReorderFinalListError(t *testing.T) {
	svc, repo, ctx := setup(t)
	tc, _ := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "a"})
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

// FuzzCreateText: any title/description either fails validation or is stored as
// valid UTF-8 without NUL characters (text PostgreSQL can always store).
func FuzzCreateText(f *testing.F) {
	for _, s := range []string{"Login", "a\x00b", "\xff", "ñandú 😀", "   ", ""} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, title, description string) {
		svc, _, ctx := setup(t)
		tc, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: title, Description: description})
		if err != nil {
			require.Equal(t, apperr.KindValidation, kindOf(t, err))
			return
		}
		for _, s := range []string{tc.Title, tc.Description} {
			require.True(t, utf8.ValidString(s) && !strings.ContainsRune(s, 0), "%q", s)
		}
	})
}

func TestProjectsLifecycle(t *testing.T) {
	svc, repo, ctx := setup(t)
	p, err := svc.CreateProject(ctx, CreateProjectInput{Key: " chk ", Name: "  Checkout ", Description: "web shop"})
	require.NoError(t, err)
	assert.Equal(t, "CHK", p.Key, "keys are upper-cased")
	assert.Equal(t, "Checkout", p.Name)

	_, err = svc.CreateProject(ctx, CreateProjectInput{Key: "CHK", Name: "again"})
	assert.Equal(t, apperr.KindConflict, kindOf(t, err))

	got, err := svc.ProjectByKey(ctx, "CHK")
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)
	id, err := svc.ProjectIDByKey(ctx, "CHK")
	require.NoError(t, err)
	assert.Equal(t, p.ID, id)
	_, err = svc.ProjectByKey(ctx, "NOPE")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	byID, err := svc.ProjectByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "CHK", byID.Key)
	_, err = svc.ProjectByID(ctx, 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	res, err := svc.ListProjects(ctx, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)
	assert.Equal(t, []string{"CHK", "TC"}, []string{res.Items[0].Key, res.Items[1].Key})

	up, err := svc.UpdateProject(ctx, "CHK", UpdateProjectInput{Name: ptr(" Shop "), Description: ptr("")})
	require.NoError(t, err)
	assert.Equal(t, "Shop", up.Name)
	assert.Equal(t, "CHK", up.Key, "the key never changes")
	_, err = svc.UpdateProject(ctx, "NOPE", UpdateProjectInput{Name: ptr("x")})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	for _, method := range []string{"CreateProject", "GetProjectByKey", "GetProject", "UpdateProject"} {
		repo.errs[method] = errBoom
	}
	_, err = svc.CreateProject(ctx, CreateProjectInput{Key: "WEB", Name: "w"})
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.ProjectByKey(ctx, "CHK")
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.ProjectByID(ctx, 1)
	assert.ErrorIs(t, err, errBoom)
	_, err = svc.UpdateProject(ctx, "CHK", UpdateProjectInput{Name: ptr("x")})
	assert.ErrorIs(t, err, errBoom)
	repo.errs["CountProjects"] = errBoom
	_, err = svc.ListProjects(ctx, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.errs["ListProjects"] = errBoom
	_, err = svc.ListProjects(ctx, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestProjectValidation(t *testing.T) {
	svc, _, ctx := setup(t)
	for _, in := range []CreateProjectInput{
		{Key: "C", Name: "n"},
		{Key: "1AB", Name: "n"},
		{Key: "TOOLONGKEY1", Name: "n"},
		{Key: "C-K", Name: "n"},
		{Key: "CK", Name: "  "},
		{Key: "CK", Name: strings.Repeat("n", 101)},
		{Key: "CK", Name: "n", Description: strings.Repeat("d", 2001)},
		{Key: "CK", Name: "a\x00"},
	} {
		_, err := svc.CreateProject(ctx, in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
	for _, in := range []UpdateProjectInput{{}, {Name: ptr(" ")}, {Description: ptr("\x00")}} {
		_, err := svc.UpdateProject(ctx, "TC", in)
		assert.Equal(t, apperr.KindValidation, kindOf(t, err), "%+v", in)
	}
}

// Numbers are assigned per project; the key is <PROJECT>-<number>.
func TestTestCaseNumbersPerProject(t *testing.T) {
	svc, _, ctx := setup(t)
	chk, err := svc.CreateProject(ctx, CreateProjectInput{Key: "CHK", Name: "Checkout"})
	require.NoError(t, err)
	a, _ := svc.Create(ctx, CreateInput{Title: "a", Automated: true})
	b, _ := svc.Create(ctx, CreateInput{ProjectID: chk.ID, Title: "b", Automated: true})
	c, _ := svc.Create(ctx, CreateInput{ProjectID: chk.ID, Title: "c"})
	assert.Equal(t, []string{"TC-1", "CHK-1", "CHK-2"}, []string{a.Key(), b.Key(), c.Key()}, "no project means the default one")

	res, err := svc.List(ctx, ListFilter{ProjectID: &chk.ID}, pagination.Default())
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)

	view, err := svc.IngestionView(ctx, chk.ID, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, view.Expected, "only the project's expected universe")
	assert.Equal(t, IngestionEntry{ID: c.ID, Status: StatusActive}, view.Entries[2])

	_, err = svc.Create(ctx, CreateInput{ProjectID: 99, Title: "x"})
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))

	keys, err := svc.Keys(ctx, []int64{a.ID, c.ID, 999})
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{a.ID: "TC-1", c.ID: "CHK-2"}, keys, "unknown ids have no key")
	keys, err = svc.Keys(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, keys)
}
