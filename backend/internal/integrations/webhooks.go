package integrations

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// Delivery limits and retry schedule.
const (
	// MaxDeliveryAttempts bounds the attempts of one delivery; it fails for good after the last.
	MaxDeliveryAttempts = 5
	// claimBatch bounds the deliveries one worker pass sends.
	claimBatch    = 20
	maxWebhookURL = 2000
)

// backoff is the wait after each failed attempt (before attempts 2 to 5).
var backoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute}

// WebhookEvents are the events a webhook can subscribe to.
var WebhookEvents = []string{EventRunCompleted}

// WebhookView is a webhook with its latest delivery (nil: none yet).
type WebhookView struct {
	Webhook
	LastDelivery *Delivery
}

func webhookNotFound(id int64) error { return apperr.NotFound("webhook %d not found", id) }

// project resolves a project the caller maintains (else 404 as if it did not exist, 403 for lower roles).
func (s *Service) project(ctx context.Context, key string) (catalog.Project, error) {
	return projectkey.Resolve(ctx, "projectKey", key, s.catalog.ProjectByKey, catalog.ProjectID, s.access, authz.RoleMaintainer)
}

// checkURL validates a webhook endpoint: http(s) without credentials; plain http and literal private addresses only
// when private targets are allowed (development).
func (s *Service) checkURL(v *apperr.Validator, raw string) {
	u, err := url.Parse(raw)
	ok := err == nil && (u.Scheme == "https" || (u.Scheme == "http" && s.cfg.AllowPrivate)) && u.Hostname() != "" && u.User == nil
	v.Check(ok, "url", "must be an https URL without credentials")
	v.Check(validLen(raw, maxWebhookURL), "url", fmt.Sprintf("must be at most %d characters", maxWebhookURL))
	v.CheckText("url", raw)
	if ok && !s.cfg.AllowPrivate {
		ip := net.ParseIP(u.Hostname())
		v.Check(ip == nil || !blocked(ip), "url", "must not target a private, loopback or link-local address")
	}
}

func validLen(s string, n int) bool { return len([]rune(s)) <= n }

func checkEvents(v *apperr.Validator, events []string) []string {
	out := slices.Clone(events)
	slices.Sort(out)
	out = slices.Compact(out)
	v.Check(len(out) >= 1, "events", "subscribe to at least one event")
	for _, e := range out {
		v.Check(slices.Contains(WebhookEvents, e), "events", "must be among: run.completed")
	}
	return out
}

func newSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

// Webhooks lists a project's webhooks with their latest delivery (maintainers).
func (s *Service) Webhooks(ctx context.Context, projectKey string) ([]WebhookView, error) {
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	hooks, err := s.repo.ListWebhooks(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, hooks)
}

func (s *Service) views(ctx context.Context, hooks []Webhook) ([]WebhookView, error) {
	ids := make([]int64, len(hooks))
	for i, h := range hooks {
		ids[i] = h.ID
	}
	last, err := s.repo.LastDeliveries(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]WebhookView, len(hooks))
	for i, h := range hooks {
		out[i] = WebhookView{Webhook: h}
		if d, ok := last[h.ID]; ok {
			out[i].LastDelivery = &d
		}
	}
	return out, nil
}

// CreateWebhook subscribes an endpoint to events of a project and returns its signing secret, shown only now.
func (s *Service) CreateWebhook(ctx context.Context, projectKey, rawURL string, events []string) (WebhookView, string, error) {
	var v apperr.Validator
	s.checkURL(&v, rawURL)
	events = checkEvents(&v, events)
	if err := v.Err(); err != nil {
		return WebhookView{}, "", err
	}
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return WebhookView{}, "", err
	}
	actor, err := s.access.Actor(ctx)
	if err != nil {
		return WebhookView{}, "", err
	}
	secret := newSecret()
	w, err := s.repo.CreateWebhook(ctx, Webhook{ProjectID: p.ID, URL: rawURL, Events: events, Secret: s.box.Seal(secret), CreatedBy: actor.Username})
	if err != nil {
		return WebhookView{}, "", err
	}
	return WebhookView{Webhook: w}, secret, nil
}

// webhook resolves a webhook of a project the caller maintains.
func (s *Service) webhook(ctx context.Context, projectKey string, id int64) (Webhook, error) {
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return Webhook{}, err
	}
	w, err := s.repo.GetWebhook(ctx, p.ID, id)
	if errors.Is(err, ErrNotFound) {
		return Webhook{}, webhookNotFound(id)
	}
	return w, err
}

// UpdateWebhook changes a webhook's URL or events, or pauses and resumes it.
func (s *Service) UpdateWebhook(ctx context.Context, projectKey string, id int64, in UpdateWebhookInput) (WebhookView, error) {
	var v apperr.Validator
	v.Check(in.URL != nil || in.Events != nil || in.Active != nil, "body", "at least one field is required")
	if in.URL != nil {
		s.checkURL(&v, *in.URL)
	}
	if in.Events != nil {
		in.Events = checkEvents(&v, in.Events)
	}
	if err := v.Err(); err != nil {
		return WebhookView{}, err
	}
	w, err := s.webhook(ctx, projectKey, id)
	if err != nil {
		return WebhookView{}, err
	}
	if err := s.repo.UpdateWebhook(ctx, w.ProjectID, id, in); err != nil {
		return WebhookView{}, err
	}
	if w, err = s.repo.GetWebhook(ctx, w.ProjectID, id); err != nil {
		return WebhookView{}, err
	}
	views, err := s.views(ctx, []Webhook{w})
	if err != nil {
		return WebhookView{}, err
	}
	return views[0], nil
}

// Ping queues a ping delivery to a webhook (to check the endpoint and its signature verification).
func (s *Service) Ping(ctx context.Context, projectKey string, id int64) (Delivery, error) {
	w, err := s.webhook(ctx, projectKey, id)
	if err != nil {
		return Delivery{}, err
	}
	payload, _ := json.Marshal(map[string]any{"event": EventPing, "webhook": map[string]any{"id": w.ID, "url": w.URL}, "sentAt": s.now().UTC()})
	did, err := s.repo.InsertDelivery(ctx, w.ID, EventPing, payload)
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{ID: did, WebhookID: w.ID, Event: EventPing, Payload: payload, Status: DeliveryPending, NextAttemptAt: s.now(), CreatedAt: s.now()}, nil
}

// Deliveries lists a webhook's deliveries, newest first.
func (s *Service) Deliveries(ctx context.Context, projectKey string, id int64, page pagination.Page) (Page, error) {
	w, err := s.webhook(ctx, projectKey, id)
	if err != nil {
		return Page{}, err
	}
	items, err := s.repo.ListDeliveries(ctx, w.ID, page.Limit(), page.Offset())
	if err != nil {
		return Page{}, err
	}
	total, err := s.repo.CountDeliveries(ctx, w.ID)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Page: page, Total: total}, nil
}

// RunCompleted queues a run.completed delivery to every active webhook of the run's project that subscribes to it.
// It never fails the recording of the run: problems are logged.
func (s *Service) RunCompleted(ctx context.Context, run execution.TestRun) {
	if err := s.runCompleted(ctx, run); err != nil {
		slog.ErrorContext(ctx, "webhook deliveries not queued", "run", run.ID, "error", err)
	}
}

func (s *Service) runCompleted(ctx context.Context, run execution.TestRun) error {
	hooks, err := s.repo.ListSubscribedWebhooks(ctx, run.ProjectID, EventRunCompleted)
	if err != nil || len(hooks) == 0 {
		return err
	}
	p, err := s.catalog.ProjectByID(ctx, run.ProjectID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"event": EventRunCompleted, "sentAt": s.now().UTC(),
		"project": map[string]string{"key": p.Key, "name": p.Name}, "run": execution.RunDTO(run),
	})
	for _, id := range hooks {
		if _, err := s.repo.InsertDelivery(ctx, id, EventRunCompleted, payload); err != nil {
			return err
		}
	}
	return nil
}

// Sign is the X-Provenly-Signature of a delivery: HMAC-SHA256 of "<timestamp>.<body>" with the webhook's secret.
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// DeliverDue sends the deliveries that are due (a batch) and records each attempt; it returns how many it sent.
func (s *Service) DeliverDue(ctx context.Context) (int, error) {
	due, err := s.repo.ClaimDueDeliveries(ctx, claimBatch)
	if err != nil {
		return 0, err
	}
	hooks := map[int64]Webhook{}
	for _, d := range due {
		w, ok := hooks[d.WebhookID]
		if !ok {
			if w, err = s.repo.GetWebhookByID(ctx, d.WebhookID); err != nil {
				return 0, err
			}
			hooks[d.WebhookID] = w
		}
		if err := s.repo.FinishAttempt(ctx, d.ID, s.attempt(ctx, w, d)); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}

// attempt sends one delivery and says what happened.
func (s *Service) attempt(ctx context.Context, w Webhook, d Delivery) Attempt {
	a := Attempt{Attempts: d.Attempts + 1, NextAttemptAt: s.now()}
	if !w.Active {
		a.Status, a.Error = DeliveryFailed, "the webhook is paused"
		return a
	}
	secret, err := s.box.Open(w.Secret)
	if err != nil {
		a.Status, a.Error = DeliveryFailed, err.Error()
		return a
	}
	ts := strconv.FormatInt(s.now().Unix(), 10)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(d.Payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Provenly-Webhooks/1")
	req.Header.Set("X-Provenly-Event", d.Event)
	req.Header.Set("X-Provenly-Delivery", strconv.FormatInt(d.ID, 10))
	req.Header.Set("X-Provenly-Timestamp", ts)
	req.Header.Set("X-Provenly-Signature", Sign(secret, ts, d.Payload))
	res, err := s.client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		_ = res.Body.Close()
		code := int32(res.StatusCode)
		a.StatusCode = &code
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			a.Status = DeliverySucceeded
			return a
		}
		err = fmt.Errorf("the endpoint answered %d", res.StatusCode)
	}
	a.Error = truncate(err.Error(), 1000)
	if a.Attempts >= MaxDeliveryAttempts {
		a.Status = DeliveryFailed
		return a
	}
	a.Status, a.NextAttemptAt = DeliveryPending, s.now().Add(backoff[a.Attempts-1])
	return a
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Run sends due deliveries every interval, and purges old ones every hour, until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var nextPurge time.Time
	for {
		if _, err := s.DeliverDue(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "webhook deliveries", "error", err)
		}
		if now := s.now(); !now.Before(nextPurge) {
			nextPurge = now.Add(purgeEvery)
			if _, err := s.PurgeDeliveries(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "webhook delivery purge", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// purgeBatch is how many deliveries one purge statement deletes; purgeEvery how often the worker purges.
const (
	purgeBatch = 1000
	purgeEvery = time.Hour
)

// PurgeDeliveries deletes the finished deliveries older than the retention, in batches, and logs how many; with no
// retention it keeps everything. Pending deliveries, audit events and run results are never purged.
func (s *Service) PurgeDeliveries(ctx context.Context) (int64, error) {
	if s.cfg.DeliveryRetention <= 0 {
		return 0, nil
	}
	before := s.now().Add(-s.cfg.DeliveryRetention)
	var total int64
	for {
		n, err := s.repo.PurgeDeliveries(ctx, before, purgeBatch)
		total += n
		if err != nil {
			return total, err
		}
		if n < purgeBatch {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "webhook deliveries purged", "count", total, "before", before)
	}
	return total, nil
}
