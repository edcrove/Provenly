package integrations

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/secrets"
)

var (
	now     = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

// fakeRepo is an in-memory Repository; errs[method] makes that method fail.
type fakeRepo struct {
	errs       map[string]error
	hooks      map[int64]Webhook
	deliveries []Delivery
	github     map[int64]GitHubConnection
	claims     int
	onClaim    func(n int)
	synced     *time.Time
	syncError  string
	// purges records each purge call's cutoff; purgeable is how many old deliveries are left to purge.
	purges    []time.Time
	purgeable int64
}

func newRepo() *fakeRepo {
	return &fakeRepo{errs: map[string]error{}, hooks: map[int64]Webhook{}, github: map[int64]GitHubConnection{}}
}

func (r *fakeRepo) CreateWebhook(_ context.Context, w Webhook) (Webhook, error) {
	if err := r.errs["CreateWebhook"]; err != nil {
		return Webhook{}, err
	}
	w.ID, w.Active, w.CreatedAt, w.UpdatedAt = int64(len(r.hooks)+1), true, now, now
	r.hooks[w.ID] = w
	return w, nil
}

func (r *fakeRepo) ListWebhooks(_ context.Context, projectID int64) ([]Webhook, error) {
	var out []Webhook
	for _, w := range r.hooks {
		if w.ProjectID == projectID {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b Webhook) int { return int(a.ID - b.ID) })
	return out, r.errs["ListWebhooks"]
}

func (r *fakeRepo) GetWebhook(_ context.Context, projectID, id int64) (Webhook, error) {
	if err := r.errs["GetWebhook"]; err != nil {
		return Webhook{}, err
	}
	w, ok := r.hooks[id]
	if !ok || w.ProjectID != projectID {
		return Webhook{}, ErrNotFound
	}
	return w, nil
}

func (r *fakeRepo) GetWebhookByID(_ context.Context, id int64) (Webhook, error) {
	if err := r.errs["GetWebhookByID"]; err != nil {
		return Webhook{}, err
	}
	return r.hooks[id], nil
}

func (r *fakeRepo) UpdateWebhook(_ context.Context, _, id int64, in UpdateWebhookInput) error {
	if err := r.errs["UpdateWebhook"]; err != nil {
		return err
	}
	w := r.hooks[id]
	if in.URL != nil {
		w.URL = *in.URL
	}
	if in.Events != nil {
		w.Events = in.Events
	}
	if in.Active != nil {
		w.Active = *in.Active
	}
	r.hooks[id] = w
	if r.errs["GetWebhookAfterUpdate"] != nil {
		r.errs["GetWebhook"] = r.errs["GetWebhookAfterUpdate"]
	}
	return nil
}

func (r *fakeRepo) ListSubscribedWebhooks(_ context.Context, projectID int64, event string) ([]int64, error) {
	var out []int64
	for _, w := range r.hooks {
		if w.ProjectID == projectID && w.Active && slices.Contains(w.Events, event) {
			out = append(out, w.ID)
		}
	}
	slices.Sort(out)
	return out, r.errs["ListSubscribedWebhooks"]
}

func (r *fakeRepo) InsertDelivery(_ context.Context, webhookID int64, event string, payload []byte) (int64, error) {
	if err := r.errs["InsertDelivery"]; err != nil {
		return 0, err
	}
	d := Delivery{ID: int64(len(r.deliveries) + 1), WebhookID: webhookID, Event: event, Payload: payload, Status: DeliveryPending, NextAttemptAt: now, CreatedAt: now}
	r.deliveries = append(r.deliveries, d)
	return d.ID, nil
}

func (r *fakeRepo) PurgeDeliveries(_ context.Context, before time.Time, limit int32) (int64, error) {
	r.purges = append(r.purges, before)
	n := min(r.purgeable, int64(limit))
	r.purgeable -= n
	return n, r.errs["PurgeDeliveries"]
}

func (r *fakeRepo) ClaimDueDeliveries(_ context.Context, limit int32) ([]Delivery, error) {
	r.claims++
	if r.onClaim != nil {
		r.onClaim(r.claims)
	}
	if err := r.errs["ClaimDueDeliveries"]; err != nil {
		return nil, err
	}
	var out []Delivery
	for _, d := range r.deliveries {
		if d.Status == DeliveryPending && !d.NextAttemptAt.After(now) && len(out) < int(limit) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *fakeRepo) FinishAttempt(_ context.Context, id int64, a Attempt) error {
	if err := r.errs["FinishAttempt"]; err != nil {
		return err
	}
	d := &r.deliveries[id-1]
	d.Status, d.Attempts, d.LastStatusCode, d.LastError, d.NextAttemptAt = a.Status, a.Attempts, a.StatusCode, a.Error, a.NextAttemptAt
	return nil
}

func (r *fakeRepo) ListDeliveries(_ context.Context, webhookID int64, limit, offset int32) ([]Delivery, error) {
	var out []Delivery
	for i := len(r.deliveries) - 1; i >= 0; i-- {
		if r.deliveries[i].WebhookID == webhookID {
			out = append(out, r.deliveries[i])
		}
	}
	out = out[min(int(offset), len(out)):]
	return out[:min(int(limit), len(out))], r.errs["ListDeliveries"]
}

func (r *fakeRepo) CountDeliveries(_ context.Context, webhookID int64) (int64, error) {
	var n int64
	for _, d := range r.deliveries {
		if d.WebhookID == webhookID {
			n++
		}
	}
	return n, r.errs["CountDeliveries"]
}

func (r *fakeRepo) LastDeliveries(_ context.Context, ids []int64) (map[int64]Delivery, error) {
	out := map[int64]Delivery{}
	for _, d := range r.deliveries {
		if slices.Contains(ids, d.WebhookID) {
			out[d.WebhookID] = d
		}
	}
	return out, r.errs["LastDeliveries"]
}

func (r *fakeRepo) UpsertGitHubConnection(_ context.Context, c GitHubConnection) error {
	if err := r.errs["UpsertGitHubConnection"]; err != nil {
		return err
	}
	c.UpdatedAt = now
	r.github[c.ProjectID] = c
	return nil
}

func (r *fakeRepo) GetGitHubConnection(_ context.Context, projectID int64) (GitHubConnection, error) {
	if err := r.errs["GetGitHubConnection"]; err != nil {
		return GitHubConnection{}, err
	}
	c, ok := r.github[projectID]
	if !ok {
		return GitHubConnection{}, ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) DeleteGitHubConnection(_ context.Context, projectID int64) (bool, error) {
	if err := r.errs["DeleteGitHubConnection"]; err != nil {
		return false, err
	}
	_, ok := r.github[projectID]
	delete(r.github, projectID)
	return ok, nil
}

func (r *fakeRepo) RecordGitHubSync(_ context.Context, projectID int64, syncedAt *time.Time, lastError string) error {
	if err := r.errs["RecordGitHubSync"]; err != nil {
		return err
	}
	c := r.github[projectID]
	if syncedAt != nil {
		c.LastSyncedAt = syncedAt
	}
	c.LastError = lastError
	r.github[projectID] = c
	r.synced, r.syncError = syncedAt, lastError
	return nil
}

// fakeCatalog knows the projects SHOP (id 2) and OTHER (id 3); NOPE does not exist.
type fakeCatalog struct {
	byIDErr   error
	importErr error
	imported  []catalog.IssueInput
	provider  string
}

func (c *fakeCatalog) ProjectByKey(_ context.Context, key string) (catalog.Project, error) {
	switch key {
	case "SHOP":
		return catalog.Project{ID: 2, Key: "SHOP", Name: "Shop"}, nil
	case "OTHER":
		return catalog.Project{ID: 3, Key: "OTHER", Name: "Other"}, nil
	}
	return catalog.Project{}, apperr.NotFound("project %s not found", key)
}

func (c *fakeCatalog) ProjectByID(_ context.Context, id int64) (catalog.Project, error) {
	return catalog.Project{ID: id, Key: "SHOP", Name: "Shop"}, c.byIDErr
}

func (c *fakeCatalog) ImportIssues(_ context.Context, _ int64, provider string, items []catalog.IssueInput) (catalog.ImportResult, error) {
	c.provider, c.imported = provider, items
	return catalog.ImportResult{Created: len(items)}, c.importErr
}

// fakeAccess grants role on every project: without a role it hides the project, below minRole it forbids.
type fakeAccess struct {
	role     authz.Role
	actorErr error
}

func (a fakeAccess) Require(_ context.Context, _ int64, minRole authz.Role, notFound error) error {
	switch {
	case a.role < authz.RoleViewer:
		return notFound
	case a.role < minRole:
		return apperr.Forbidden("requires %s", minRole)
	}
	return nil
}

func (a fakeAccess) Actor(context.Context) (authz.Actor, error) {
	return authz.Actor{Username: "maria"}, a.actorErr
}

type fixture struct {
	svc  *Service
	repo *fakeRepo
	cat  *fakeCatalog
	box  *secrets.Box
}

func newFixture(t *testing.T, cfg Config, access fakeAccess) fixture {
	t.Helper()
	box, err := secrets.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, cat := newRepo(), &fakeCatalog{}
	return fixture{svc: NewService(repo, cat, access, box, cfg, func() time.Time { return now }), repo: repo, cat: cat, box: box}
}

func maintainer() fakeAccess { return fakeAccess{role: authz.RoleMaintainer} }

func kindOf(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return 0
}
