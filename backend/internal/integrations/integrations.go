// Package integrations connects projects with the outside (Notion 16 Connectors, 17 Security; Planning #3, #21):
// webhooks that export run events, signed and retried, and the GitHub Issues connector. Secrets it must read back
// (webhook signing secrets, connector tokens) are stored encrypted (platform/secrets, MVP D4) and never returned
// after creation. A failing external system never blocks recording tests.
package integrations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/secrets"
)

// ErrNotFound is returned by the repository for a missing row.
var ErrNotFound = errors.New("not found")

// Events a webhook can subscribe to; ping is sent on request only.
const (
	EventRunCompleted = "run.completed"
	EventPing         = "ping"
)

// Delivery statuses.
const (
	DeliveryPending   = "pending"
	DeliverySucceeded = "succeeded"
	DeliveryFailed    = "failed"
)

// Webhook is a project's subscription of an endpoint to events. Secret holds the sealed signing secret.
type Webhook struct {
	ID        int64
	ProjectID int64
	URL       string
	Events    []string
	Secret    string
	Active    bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Delivery is one event sent (or to send) to a webhook.
type Delivery struct {
	ID             int64
	WebhookID      int64
	Event          string
	Payload        []byte
	Status         string
	Attempts       int32
	NextAttemptAt  time.Time
	LastStatusCode *int32
	LastError      string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

// Attempt is the outcome of one delivery attempt.
type Attempt struct {
	Status        string
	Attempts      int32
	StatusCode    *int32
	Error         string
	NextAttemptAt time.Time
}

// GitHubConnection links a project to a GitHub repository whose issues it mirrors. Token holds the sealed token.
type GitHubConnection struct {
	ProjectID    int64
	Repository   string
	Token        string
	Labels       string
	LastSyncedAt *time.Time
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UpdateWebhookInput holds the webhook fields to change; nil means unchanged.
type UpdateWebhookInput struct {
	URL    *string
	Events []string
	Active *bool
}

// Repository is the persistence port of the integrations module.
type Repository interface {
	CreateWebhook(ctx context.Context, w Webhook) (Webhook, error)
	ListWebhooks(ctx context.Context, projectID int64) ([]Webhook, error)
	GetWebhook(ctx context.Context, projectID, id int64) (Webhook, error)
	GetWebhookByID(ctx context.Context, id int64) (Webhook, error)
	UpdateWebhook(ctx context.Context, projectID, id int64, in UpdateWebhookInput) error
	ListSubscribedWebhooks(ctx context.Context, projectID int64, event string) ([]int64, error)
	InsertDelivery(ctx context.Context, webhookID int64, event string, payload []byte) (int64, error)
	ClaimDueDeliveries(ctx context.Context, limit int32) ([]Delivery, error)
	FinishAttempt(ctx context.Context, id int64, a Attempt) error
	ListDeliveries(ctx context.Context, webhookID int64, limit, offset int32) ([]Delivery, error)
	CountDeliveries(ctx context.Context, webhookID int64) (int64, error)
	LastDeliveries(ctx context.Context, webhookIDs []int64) (map[int64]Delivery, error)
	UpsertGitHubConnection(ctx context.Context, c GitHubConnection) error
	GetGitHubConnection(ctx context.Context, projectID int64) (GitHubConnection, error)
	DeleteGitHubConnection(ctx context.Context, projectID int64) (bool, error)
	RecordGitHubSync(ctx context.Context, projectID int64, syncedAt *time.Time, lastError string) error
	// PurgeDeliveries deletes up to limit finished deliveries completed before `before`, unless another server holds
	// the purge lock; it returns how many it deleted.
	PurgeDeliveries(ctx context.Context, before time.Time, limit int32) (int64, error)
}

// Catalog is what integrations need from the catalog module.
type Catalog interface {
	ProjectByKey(ctx context.Context, key string) (catalog.Project, error)
	ProjectByID(ctx context.Context, id int64) (catalog.Project, error)
	ImportIssues(ctx context.Context, projectID int64, provider string, items []catalog.IssueInput) (catalog.ImportResult, error)
}

// Access authorizes integration management (maintainers of the project) and names the actor.
type Access interface {
	Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error
	Actor(ctx context.Context) (authz.Actor, error)
}

// Config tunes the module.
type Config struct {
	// AllowPrivate lets webhooks target private and loopback addresses (development and tests).
	AllowPrivate bool
	// GitHubAPIURL is the GitHub REST API base URL.
	GitHubAPIURL string
	// DeliveryRetention is how long finished webhook deliveries are kept; 0 keeps them forever.
	DeliveryRetention time.Duration
}

// Service is the integrations use cases.
type Service struct {
	repo    Repository
	catalog Catalog
	access  Access
	box     *secrets.Box
	cfg     Config
	client  *http.Client
	now     func() time.Time
}

// NewService builds a Service. Outbound requests (webhooks, GitHub) use a client that refuses private addresses
// unless cfg.AllowPrivate.
func NewService(repo Repository, cat Catalog, access Access, box *secrets.Box, cfg Config, now func() time.Time) *Service {
	return &Service{repo: repo, catalog: cat, access: access, box: box, cfg: cfg, client: outboundClient(cfg.AllowPrivate), now: now}
}

// Page lists one page of deliveries.
type Page = pagination.Result[Delivery]
