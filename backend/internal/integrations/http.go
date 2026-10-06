package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// API is the integrations use cases exposed over REST.
type API interface {
	Webhooks(ctx context.Context, projectKey string) ([]WebhookView, error)
	CreateWebhook(ctx context.Context, projectKey, rawURL string, events []string) (WebhookView, string, error)
	UpdateWebhook(ctx context.Context, projectKey string, id int64, in UpdateWebhookInput) (WebhookView, error)
	Ping(ctx context.Context, projectKey string, id int64) (Delivery, error)
	Deliveries(ctx context.Context, projectKey string, id int64, page pagination.Page) (Page, error)
	GitHub(ctx context.Context, projectKey string) (GitHubView, error)
	ConnectGitHub(ctx context.Context, projectKey string, in GitHubInput) (GitHubView, error)
	DisconnectGitHub(ctx context.Context, projectKey string) error
	SyncGitHub(ctx context.Context, projectKey string) (catalog.ImportResult, error)
}

// Handler is the REST adapter of integrations (session routes, project maintainers).
type Handler struct{ api API }

// NewHandler builds a Handler.
func NewHandler(api API) *Handler { return &Handler{api: api} }

// Register mounts the integrations routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/webhooks", keyed(h.listWebhooks))
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/webhooks", keyed(h.createWebhook))
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}", keyed(h.updateWebhook))
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping", keyed(h.ping))
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries", keyed(h.deliveries))
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/github", keyed(h.getGitHub))
	mux.HandleFunc("PUT /api/v1/projects/{projectKey}/github", keyed(h.connectGitHub))
	mux.HandleFunc("DELETE /api/v1/projects/{projectKey}/github", keyed(h.disconnectGitHub))
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/github/sync", keyed(h.syncGitHub))
}

// DeliveryDTO is the wire form of Delivery.
type DeliveryDTO struct {
	ID             int64           `json:"id"`
	WebhookID      int64           `json:"webhookId"`
	Event          string          `json:"event"`
	Status         string          `json:"status"`
	Attempts       int32           `json:"attempts"`
	NextAttemptAt  *time.Time      `json:"nextAttemptAt"`
	LastStatusCode *int32          `json:"lastStatusCode"`
	LastError      *string         `json:"lastError"`
	CreatedAt      time.Time       `json:"createdAt"`
	CompletedAt    *time.Time      `json:"completedAt"`
	Payload        json.RawMessage `json:"payload"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deliveryDTO(d Delivery) DeliveryDTO {
	dto := DeliveryDTO{
		ID: d.ID, WebhookID: d.WebhookID, Event: d.Event, Status: d.Status, Attempts: d.Attempts,
		LastStatusCode: d.LastStatusCode, LastError: optional(d.LastError), CreatedAt: d.CreatedAt, CompletedAt: d.CompletedAt,
		Payload: json.RawMessage(d.Payload),
	}
	if d.Status == DeliveryPending {
		next := d.NextAttemptAt
		dto.NextAttemptAt = &next
	}
	return dto
}

// WebhookDTO is the wire form of WebhookView; the signing secret is never returned after creation.
type WebhookDTO struct {
	ID           int64        `json:"id"`
	URL          string       `json:"url"`
	Events       []string     `json:"events"`
	Active       bool         `json:"active"`
	CreatedBy    string       `json:"createdBy"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	LastDelivery *DeliveryDTO `json:"lastDelivery,omitempty"`
}

func webhookDTO(w WebhookView) WebhookDTO {
	dto := WebhookDTO{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active, CreatedBy: w.CreatedBy, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	if w.LastDelivery != nil {
		d := deliveryDTO(*w.LastDelivery)
		dto.LastDelivery = &d
	}
	return dto
}

type webhookList struct {
	Items []WebhookDTO `json:"items"`
}

type createdWebhook struct {
	Webhook WebhookDTO `json:"webhook"`
	Secret  string     `json:"secret"`
}

type webhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

type updateWebhookRequest struct {
	URL    *string  `json:"url"`
	Events []string `json:"events"`
	Active *bool    `json:"active"`
}

// GitHubDTO is the wire form of GitHubView.
type GitHubDTO struct {
	Repository   string     `json:"repository"`
	Labels       string     `json:"labels"`
	TokenHint    string     `json:"tokenHint"`
	LastSyncedAt *time.Time `json:"lastSyncedAt"`
	LastError    *string    `json:"lastError"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func githubDTO(v GitHubView) GitHubDTO {
	return GitHubDTO{Repository: v.Repository, Labels: v.Labels, TokenHint: v.TokenHint, LastSyncedAt: v.LastSyncedAt, LastError: optional(v.LastError), UpdatedAt: v.UpdatedAt}
}

type githubRequest struct {
	Repository string  `json:"repository"`
	Token      *string `json:"token"`
	Labels     string  `json:"labels"`
}

// keyed validates the {projectKey} path segment before the handler runs: a malformed key is a 400 and never
// reaches the database.
func keyed(handle func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, err := projectkey.Path(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		handle(w, r, key)
	}
}

type syncResponse struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request, projectKey string) {
	hooks, err := h.api.Webhooks(r.Context(), projectKey)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := webhookList{Items: make([]WebhookDTO, len(hooks))}
	for i, hk := range hooks {
		out.Items[i] = webhookDTO(hk)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createWebhook(w http.ResponseWriter, r *http.Request, projectKey string) {
	var req webhookRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hook, secret, err := h.api.CreateWebhook(r.Context(), projectKey, req.URL, req.Events)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdWebhook{Webhook: webhookDTO(hook), Secret: secret})
}

func (h *Handler) updateWebhook(w http.ResponseWriter, r *http.Request, projectKey string) {
	id, err := httpx.PathID(r, "webhookId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req updateWebhookRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hook, err := h.api.UpdateWebhook(r.Context(), projectKey, id, UpdateWebhookInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, webhookDTO(hook))
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request, projectKey string) {
	id, err := httpx.PathID(r, "webhookId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := h.api.Ping(r.Context(), projectKey, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, deliveryDTO(d))
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request, projectKey string) {
	id, err := httpx.PathID(r, "webhookId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.Deliveries(r.Context(), projectKey, id, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, deliveryDTO))
}

func (h *Handler) getGitHub(w http.ResponseWriter, r *http.Request, projectKey string) {
	v, err := h.api.GitHub(r.Context(), projectKey)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, githubDTO(v))
}

func (h *Handler) connectGitHub(w http.ResponseWriter, r *http.Request, projectKey string) {
	var req githubRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	v, err := h.api.ConnectGitHub(r.Context(), projectKey, GitHubInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, githubDTO(v))
}

func (h *Handler) disconnectGitHub(w http.ResponseWriter, r *http.Request, projectKey string) {
	if err := h.api.DisconnectGitHub(r.Context(), projectKey); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) syncGitHub(w http.ResponseWriter, r *http.Request, projectKey string) {
	res, err := h.api.SyncGitHub(r.Context(), projectKey)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, syncResponse(res))
}
