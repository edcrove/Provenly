package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// fakeGitHub serves /repos/acme/shop/issues: total issues (every third a pull request), paginated like GitHub.
type fakeGitHub struct {
	total   int
	status  int
	body    string
	queries []string
	auth    string
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.queries = append(g.queries, r.URL.RawQuery)
	g.auth = r.Header.Get("Authorization")
	if r.URL.Path != "/repos/acme/shop/issues" {
		http.NotFound(w, r)
		return
	}
	if g.status != 0 {
		w.WriteHeader(g.status)
		_, _ = w.Write([]byte(g.body))
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	items := []map[string]any{}
	for n := (page-1)*per + 1; n <= min(page*per, g.total); n++ {
		it := map[string]any{"number": n, "title": fmt.Sprintf("Bug %d", n), "body": "steps", "html_url": fmt.Sprintf("https://github.com/acme/shop/issues/%d", n), "state": "open"}
		switch {
		case n%3 == 0:
			it["pull_request"] = map[string]any{"url": "x"}
		case n%5 == 0:
			it["state"], it["state_reason"] = "closed", "completed"
		case n == 1:
			it["title"] = "  "
		}
		items = append(items, it)
	}
	_ = json.NewEncoder(w).Encode(items)
}

func connected(t *testing.T, gh http.Handler) (fixture, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	f := newFixture(t, Config{AllowPrivate: true, GitHubAPIURL: srv.URL}, maintainer())
	_, err := f.svc.ConnectGitHub(context.Background(), "SHOP", GitHubInput{Repository: " acme/shop ", Token: ptr("ghp_secret1234"), Labels: " qa "})
	require.NoError(t, err)
	return f, srv
}

func TestConnectGitHub(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	_, err := f.svc.GitHub(ctx, "SHOP")
	e, _ := apperr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, "project SHOP has no GitHub connection", e.Message)

	for name, in := range map[string]GitHubInput{
		"repository":    {Repository: "acme"},
		"labels":        {Repository: "acme/shop", Labels: strings.Repeat("l", 201), Token: ptr("t")},
		"labels text":   {Repository: "acme/shop", Labels: "a\x00", Token: ptr("t")},
		"empty token":   {Repository: "acme/shop", Token: ptr("")},
		"long token":    {Repository: "acme/shop", Token: ptr(strings.Repeat("t", 501))},
		"token text":    {Repository: "acme/shop", Token: ptr("t\x00")},
		"needs a token": {Repository: "acme/shop"},
	} {
		_, err := f.svc.ConnectGitHub(ctx, "SHOP", in)
		assert.Equal(t, apperr.KindValidation, kindOf(err), name)
	}
	_, err = f.svc.ConnectGitHub(ctx, "NOPE", GitHubInput{Repository: "acme/shop", Token: ptr("t")})
	assert.Equal(t, apperr.KindNotFound, kindOf(err))

	v, err := f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/shop", Token: ptr("ghp_secret1234"), Labels: "qa,bug"})
	require.NoError(t, err)
	assert.Equal(t, GitHubView{Repository: "acme/shop", Labels: "qa,bug", TokenHint: "…1234", UpdatedAt: now}, v)
	assert.NotContains(t, f.repo.github[2].Token, "ghp_secret1234", "the token is stored sealed")

	// Without a token the stored one is kept (change repository or labels only); a new one rotates it.
	v, err = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/web"})
	require.NoError(t, err)
	assert.Equal(t, "acme/web", v.Repository)
	assert.Equal(t, "…1234", v.TokenHint)
	v, err = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/web", Token: ptr("xyz")})
	require.NoError(t, err)
	assert.Equal(t, "…xyz", v.TokenHint)

	// A token sealed with another key shows no hint (connect again with a token).
	c := f.repo.github[2]
	c.Token = "v1:AAAA"
	f.repo.github[2] = c
	v, err = f.svc.GitHub(ctx, "SHOP")
	require.NoError(t, err)
	assert.Empty(t, v.TokenHint)

	f.repo.errs["UpsertGitHubConnection"] = errBoom
	_, err = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/web", Token: ptr("t")})
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["GetGitHubConnection"] = errBoom
	_, err = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/web"})
	assert.ErrorIs(t, err, errBoom)
	_, err = f.svc.GitHub(ctx, "SHOP")
	assert.ErrorIs(t, err, errBoom)
	_, err = f.svc.GitHub(ctx, "NOPE")
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
}

func TestDisconnectGitHub(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	_, _ = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/shop", Token: ptr("t")})
	require.NoError(t, f.svc.DisconnectGitHub(ctx, "SHOP"))
	assert.Equal(t, apperr.KindNotFound, kindOf(f.svc.DisconnectGitHub(ctx, "SHOP")))
	assert.Equal(t, apperr.KindNotFound, kindOf(f.svc.DisconnectGitHub(ctx, "NOPE")))
	f.repo.errs["DeleteGitHubConnection"] = errBoom
	assert.ErrorIs(t, f.svc.DisconnectGitHub(ctx, "SHOP"), errBoom)
}

func TestSyncGitHub(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{total: 150}
	f, _ := connected(t, gh)
	res, err := f.svc.SyncGitHub(ctx, "SHOP")
	require.NoError(t, err)
	assert.Equal(t, catalog.ImportResult{Created: 100}, res, "150 items, 50 of them pull requests")
	assert.Equal(t, catalog.ProviderGitHub, f.cat.provider)
	assert.Equal(t, "Bearer ghp_secret1234", gh.auth)
	require.Len(t, gh.queries, 2, "a short page ends the sync")
	assert.Equal(t, "labels=qa&page=1&per_page=100&state=all", gh.queries[0])
	byID := map[string]catalog.IssueInput{}
	for _, it := range f.cat.imported {
		byID[it.ExternalID] = it
	}
	assert.Equal(t, catalog.IssueInput{ExternalID: "1", Title: "#1", Description: "steps", URL: "https://github.com/acme/shop/issues/1", State: catalog.IssueOpen, ProviderStatus: "open"}, byID["1"])
	assert.Equal(t, catalog.IssueClosed, byID["5"].State)
	assert.Equal(t, "completed", byID["5"].ProviderStatus)
	assert.NotContains(t, byID, "3", "pull requests are skipped")
	assert.Equal(t, &now, f.repo.synced)
	assert.Empty(t, f.repo.syncError)

	// At most five pages (500 issues, the import bound) per sync.
	gh.total, gh.queries = 1000, nil
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	require.NoError(t, err)
	assert.Len(t, gh.queries, githubMaxPages)

	// Nothing to mirror: nothing imported.
	gh.total = 0
	f.cat.imported = nil
	res, err = f.svc.SyncGitHub(ctx, "SHOP")
	require.NoError(t, err)
	assert.Zero(t, res)
	assert.Nil(t, f.cat.imported)

	// Without labels every issue is asked for.
	_, _ = f.svc.ConnectGitHub(ctx, "SHOP", GitHubInput{Repository: "acme/shop"})
	gh.queries = nil
	_, _ = f.svc.SyncGitHub(ctx, "SHOP")
	assert.NotContains(t, gh.queries[0], "labels")
}

func TestSyncGitHubFailures(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGitHub{status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}
	f, srv := connected(t, gh)

	_, err := f.svc.SyncGitHub(ctx, "SHOP")
	e, _ := apperr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, apperr.KindUpstream, e.Kind)
	assert.Equal(t, "GitHub answered 401 for acme/shop (check the repository and the token's access)", e.Message)
	assert.Equal(t, e.Message, f.repo.syncError, "the failure is recorded on the connection")
	assert.Nil(t, f.repo.synced)

	gh.status, gh.body = http.StatusOK, `{"message":"not a list"}`
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.Equal(t, apperr.KindUpstream, kindOf(err))
	assert.Contains(t, err.Error(), "not a list of issues")

	srv.Close()
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.Equal(t, apperr.KindUpstream, kindOf(err))
	assert.Contains(t, err.Error(), "GitHub is unreachable")

	// A token sealed with another key cannot be used: 409, connect again.
	c := f.repo.github[2]
	c.Token = "v1:AAAA"
	f.repo.github[2] = c
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.Equal(t, apperr.KindConflict, kindOf(err))

	f.repo.errs["RecordGitHubSync"] = errBoom
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["GetGitHubConnection"] = errBoom
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.ErrorIs(t, err, errBoom)
	_, err = f.svc.SyncGitHub(ctx, "NOPE")
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	delete(f.repo.errs, "GetGitHubConnection")
	delete(f.repo.github, 2)
	_, err = f.svc.SyncGitHub(ctx, "SHOP")
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
}

func TestSyncGitHubImportFailure(t *testing.T) {
	gh := &fakeGitHub{total: 2}
	f, _ := connected(t, gh)
	f.cat.importErr = errBoom
	_, err := f.svc.SyncGitHub(context.Background(), "SHOP")
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, "boom", f.repo.syncError)
}

// GitHub on a private address is refused like webhooks unless private targets are allowed.
func TestSyncGitHubPrivateAddress(t *testing.T) {
	srv := httptest.NewServer(&fakeGitHub{})
	defer srv.Close()
	f := newFixture(t, Config{GitHubAPIURL: srv.URL}, maintainer())
	_, _ = f.svc.ConnectGitHub(context.Background(), "SHOP", GitHubInput{Repository: "acme/shop", Token: ptr("t")})
	_, err := f.svc.SyncGitHub(context.Background(), "SHOP")
	assert.Equal(t, apperr.KindUpstream, kindOf(err))
	assert.Contains(t, err.Error(), errBlockedAddress.Error())
}
