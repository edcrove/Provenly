package insights

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

var (
	now     = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type fakeCatalog struct {
	automated, manual []int64
	selErr            map[bool]error
	keysErr           error
	asked             []int64
}

func (c *fakeCatalog) ProjectByKey(_ context.Context, key string) (catalog.Project, error) {
	if key == "NOPE" {
		return catalog.Project{}, apperr.NotFound("project NOPE not found")
	}
	return catalog.Project{ID: 2, Key: key}, nil
}

func (c *fakeCatalog) Selection(_ context.Context, _ int64, _ string, automated *bool) (catalog.Suite, []int64, error) {
	if *automated {
		return catalog.Suite{}, c.automated, c.selErr[true]
	}
	return catalog.Suite{}, c.manual, c.selErr[false]
}

func (c *fakeCatalog) Keys(_ context.Context, ids []int64) (map[int64]string, error) {
	c.asked = ids
	out := map[int64]string{}
	for _, id := range ids {
		out[id] = "CHK-" + string(rune('0'+id))
	}
	return out, c.keysErr
}

type fakeHistory struct {
	last       map[int64]time.Time
	flaky      []execution.FlakyCount
	lastErr    error
	flakyErr   error
	gotWindow  int32
	gotProject int64
}

func (h *fakeHistory) LastExecuted(context.Context, []int64) (map[int64]time.Time, error) {
	return h.last, h.lastErr
}

func (h *fakeHistory) FlakyCounts(_ context.Context, projectID int64, window, _ int32) ([]execution.FlakyCount, error) {
	h.gotProject, h.gotWindow = projectID, window
	return h.flaky, h.flakyErr
}

type fakeAccess struct{ role authz.Role }

func (a fakeAccess) Require(_ context.Context, _ int64, minRole authz.Role, notFound error) error {
	if a.role < minRole {
		return notFound
	}
	return nil
}

func TestQuality(t *testing.T) {
	cat := &fakeCatalog{automated: []int64{1, 2, 3}, manual: []int64{4}}
	hist := &fakeHistory{
		last:  map[int64]time.Time{1: now.Add(-time.Hour), 2: now.AddDate(0, 0, -30), 3: now.AddDate(0, 0, -20)},
		flaky: []execution.FlakyCount{{TestCaseID: 1, Runs: 2}},
	}
	svc := NewService(cat, hist, fakeAccess{role: authz.RoleViewer}, func() time.Time { return now })
	ctx := context.Background()
	q, err := svc.Quality(ctx, Query{ProjectKey: "CHK"})
	require.NoError(t, err)
	at2, at3 := now.AddDate(0, 0, -30), now.AddDate(0, 0, -20)
	assert.Equal(t, Quality{Active: 4, Automated: 3, Manual: 1, AutomationRate: 75, StaleDays: 14, NeverExecuted: 1, Stale: 2,
		StaleCases: []StaleCase{{TestCaseID: 4, Key: "CHK-4"}, {TestCaseID: 2, Key: "CHK-2", LastExecutedAt: &at2}, {TestCaseID: 3, Key: "CHK-3", LastExecutedAt: &at3}},
		Window:     20, Flaky: []FlakyCase{{TestCaseID: 1, Key: "CHK-1", Runs: 2}}}, q)
	assert.Equal(t, int64(2), hist.gotProject)

	q, err = svc.Quality(ctx, Query{ProjectKey: "CHK", StaleDays: 25, Window: 5})
	require.NoError(t, err)
	assert.Equal(t, int32(1), q.Stale)
	assert.Equal(t, int32(5), hist.gotWindow)

	// An empty project has no rate; many stale test cases are listed up to the bound.
	empty := NewService(&fakeCatalog{}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, func() time.Time { return now })
	q, err = empty.Quality(ctx, Query{ProjectKey: "CHK"})
	require.NoError(t, err)
	assert.Zero(t, q.AutomationRate)
	assert.Empty(t, q.StaleCases)
	many := make([]int64, 30)
	for i := range many {
		many[i] = int64(i + 1)
	}
	big := NewService(&fakeCatalog{automated: many}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, func() time.Time { return now })
	q, err = big.Quality(ctx, Query{ProjectKey: "CHK"})
	require.NoError(t, err)
	assert.Equal(t, int32(30), q.NeverExecuted)
	assert.Len(t, q.StaleCases, maxListed)
}

func TestQualityRefusals(t *testing.T) {
	ctx := context.Background()
	field := func(err error) string {
		e, ok := apperr.As(err)
		require.True(t, ok, "%v", err)
		require.NotEmpty(t, e.Fields)
		return e.Fields[0].Field
	}
	kindOf := func(err error) apperr.Kind {
		e, ok := apperr.As(err)
		require.True(t, ok, "%v", err)
		return e.Kind
	}
	svc := NewService(&fakeCatalog{}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, func() time.Time { return now })
	for _, c := range []struct {
		q     Query
		field string
	}{
		{Query{ProjectKey: "bad"}, "projectKey"},
		{Query{ProjectKey: "CHK", StaleDays: 366}, "staleDays"},
		{Query{ProjectKey: "CHK", StaleDays: -1}, "staleDays"},
		{Query{ProjectKey: "CHK", Window: 201}, "window"},
	} {
		_, err := svc.Quality(ctx, c.q)
		assert.Equal(t, c.field, field(err), "%+v", c.q)
	}
	_, err := svc.Quality(ctx, Query{ProjectKey: "NOPE"})
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	hidden := NewService(&fakeCatalog{}, &fakeHistory{}, fakeAccess{role: authz.RoleNone}, func() time.Time { return now })
	_, err = hidden.Quality(ctx, Query{ProjectKey: "CHK"})
	assert.Equal(t, apperr.KindNotFound, kindOf(err))

	for name, s := range map[string]*Service{
		"automated": NewService(&fakeCatalog{selErr: map[bool]error{true: errBoom}}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, time.Now),
		"manual":    NewService(&fakeCatalog{selErr: map[bool]error{false: errBoom}}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, time.Now),
		"keys":      NewService(&fakeCatalog{keysErr: errBoom}, &fakeHistory{}, fakeAccess{role: authz.RoleViewer}, time.Now),
		"last":      NewService(&fakeCatalog{}, &fakeHistory{lastErr: errBoom}, fakeAccess{role: authz.RoleViewer}, time.Now),
		"flaky":     NewService(&fakeCatalog{}, &fakeHistory{flakyErr: errBoom}, fakeAccess{role: authz.RoleViewer}, time.Now),
	} {
		_, err := s.Quality(ctx, Query{ProjectKey: "CHK"})
		assert.ErrorIs(t, err, errBoom, name)
	}
}

type stubAPI struct {
	got Query
	err error
}

func (s *stubAPI) Quality(_ context.Context, q Query) (Quality, error) {
	s.got = q
	at := now
	return Quality{Active: 2, Automated: 1, Manual: 1, AutomationRate: 50, StaleDays: q.StaleDays, Stale: 1,
		StaleCases: []StaleCase{{TestCaseID: 3, Key: "CHK-3", LastExecutedAt: &at}}, Window: 20,
		Flaky: []FlakyCase{{TestCaseID: 4, Key: "CHK-4", Runs: 2}}}, s.err
}

func TestHandler(t *testing.T) {
	serve := func(api API, target string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		NewHandler(api).Register(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	api := &stubAPI{}
	rec := serve(api, "/api/v1/projects/CHK/quality?staleDays=7&window=10")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"testCases":{"active":2,"automated":1,"manual":1,"automationRate":50},
		"execution":{"staleDays":7,"neverExecuted":0,"stale":1,"testCases":[{"testCaseId":3,"testCaseKey":"CHK-3","lastExecutedAt":"2026-10-05T12:00:00Z"}]},
		"flaky":{"window":20,"testCases":[{"testCaseId":4,"testCaseKey":"CHK-4","runs":2}]}}`, rec.Body.String())
	assert.Equal(t, Query{ProjectKey: "CHK", StaleDays: 7, Window: 10}, api.got)
	for _, target := range []string{"/api/v1/projects/CHK/quality?staleDays=", "/api/v1/projects/CHK/quality?staleDays=x",
		"/api/v1/projects/CHK/quality?window=0", "/api/v1/projects/CHK/quality?window=99999999999"} {
		assert.Equal(t, http.StatusBadRequest, serve(&stubAPI{}, target).Code, target)
	}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{err: apperr.NotFound("x")}, "/api/v1/projects/CHK/quality").Code)
}
