// Package postgres is the PostgreSQL adapter of the integrations Repository, built on the sqlc-generated
// integrationsdb queries.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/integrations"
	"github.com/edcrove/provenly/backend/internal/integrations/integrationsdb"
)

// Store implements integrations.Repository.
type Store struct{ q *integrationsdb.Queries }

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{q: integrationsdb.New(pool)} }

var _ integrations.Repository = (*Store)(nil)

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return integrations.ErrNotFound
	}
	return err
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func timestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func webhook(w integrationsdb.Webhook) integrations.Webhook {
	return integrations.Webhook{
		ID: w.ID, ProjectID: w.ProjectID, URL: w.Url, Events: w.Events, Secret: w.Secret, Active: w.Active,
		CreatedBy: w.CreatedBy, CreatedAt: w.CreatedAt.Time, UpdatedAt: w.UpdatedAt.Time,
	}
}

func webhooks(rows []integrationsdb.Webhook) []integrations.Webhook {
	out := make([]integrations.Webhook, len(rows))
	for i, w := range rows {
		out[i] = webhook(w)
	}
	return out
}

func delivery(d integrationsdb.WebhookDelivery) integrations.Delivery {
	out := integrations.Delivery{
		ID: d.ID, WebhookID: d.WebhookID, Event: d.Event, Payload: d.Payload, Status: d.Status, Attempts: d.Attempts,
		NextAttemptAt: d.NextAttemptAt.Time, LastError: d.LastError, CreatedAt: d.CreatedAt.Time, CompletedAt: timePtr(d.CompletedAt),
	}
	if d.LastStatusCode.Valid {
		code := d.LastStatusCode.Int32
		out.LastStatusCode = &code
	}
	return out
}

func deliveries(rows []integrationsdb.WebhookDelivery) []integrations.Delivery {
	out := make([]integrations.Delivery, len(rows))
	for i, d := range rows {
		out[i] = delivery(d)
	}
	return out
}

// CreateWebhook implements integrations.Repository.
func (s *Store) CreateWebhook(ctx context.Context, w integrations.Webhook) (integrations.Webhook, error) {
	row, err := s.q.CreateWebhook(ctx, integrationsdb.CreateWebhookParams{ProjectID: w.ProjectID, Url: w.URL, Events: w.Events, Secret: w.Secret, CreatedBy: w.CreatedBy})
	return webhook(row), err
}

// ListWebhooks implements integrations.Repository.
func (s *Store) ListWebhooks(ctx context.Context, projectID int64) ([]integrations.Webhook, error) {
	rows, err := s.q.ListWebhooks(ctx, projectID)
	return webhooks(rows), err
}

// GetWebhook implements integrations.Repository.
func (s *Store) GetWebhook(ctx context.Context, projectID, id int64) (integrations.Webhook, error) {
	row, err := s.q.GetWebhook(ctx, integrationsdb.GetWebhookParams{ProjectID: projectID, ID: id})
	return webhook(row), notFound(err)
}

// GetWebhookByID implements integrations.Repository.
func (s *Store) GetWebhookByID(ctx context.Context, id int64) (integrations.Webhook, error) {
	row, err := s.q.GetWebhookByID(ctx, id)
	return webhook(row), notFound(err)
}

// UpdateWebhook implements integrations.Repository.
func (s *Store) UpdateWebhook(ctx context.Context, projectID, id int64, in integrations.UpdateWebhookInput) error {
	p := integrationsdb.UpdateWebhookParams{ProjectID: projectID, ID: id, Events: in.Events}
	if in.URL != nil {
		p.Url = pgtype.Text{String: *in.URL, Valid: true}
	}
	if in.Active != nil {
		p.Active = pgtype.Bool{Bool: *in.Active, Valid: true}
	}
	_, err := s.q.UpdateWebhook(ctx, p)
	return notFound(err)
}

// ListSubscribedWebhooks implements integrations.Repository.
func (s *Store) ListSubscribedWebhooks(ctx context.Context, projectID int64, event string) ([]int64, error) {
	return s.q.ListSubscribedWebhooks(ctx, integrationsdb.ListSubscribedWebhooksParams{ProjectID: projectID, Event: event})
}

// InsertDelivery implements integrations.Repository.
func (s *Store) InsertDelivery(ctx context.Context, webhookID int64, event string, payload []byte) (int64, error) {
	return s.q.InsertDelivery(ctx, integrationsdb.InsertDeliveryParams{WebhookID: webhookID, Event: event, Payload: payload})
}

// ClaimDueDeliveries implements integrations.Repository.
func (s *Store) ClaimDueDeliveries(ctx context.Context, limit int32) ([]integrations.Delivery, error) {
	rows, err := s.q.ClaimDueDeliveries(ctx, limit)
	return deliveries(rows), err
}

// FinishAttempt implements integrations.Repository.
func (s *Store) FinishAttempt(ctx context.Context, id int64, a integrations.Attempt) error {
	p := integrationsdb.FinishAttemptParams{
		ID: id, Status: a.Status, Attempts: a.Attempts, LastError: a.Error, ClaimedAttempts: a.Attempts - 1,
		NextAttemptAt: pgtype.Timestamptz{Time: a.NextAttemptAt, Valid: true},
	}
	if a.StatusCode != nil {
		p.LastStatusCode = pgtype.Int4{Int32: *a.StatusCode, Valid: true}
	}
	_, err := s.q.FinishAttempt(ctx, p) // zero rows: a late worker's stale attempt, dropped
	return err
}

// ListDeliveries implements integrations.Repository.
func (s *Store) ListDeliveries(ctx context.Context, webhookID int64, limit, offset int32) ([]integrations.Delivery, error) {
	rows, err := s.q.ListDeliveries(ctx, integrationsdb.ListDeliveriesParams{WebhookID: webhookID, PageLimit: limit, PageOffset: offset})
	return deliveries(rows), err
}

// CountDeliveries implements integrations.Repository.
func (s *Store) CountDeliveries(ctx context.Context, webhookID int64) (int64, error) {
	return s.q.CountDeliveries(ctx, webhookID)
}

// LastDeliveries implements integrations.Repository.
func (s *Store) LastDeliveries(ctx context.Context, webhookIDs []int64) (map[int64]integrations.Delivery, error) {
	rows, err := s.q.LastDeliveries(ctx, webhookIDs)
	out := make(map[int64]integrations.Delivery, len(rows))
	for _, d := range rows {
		out[d.WebhookID] = delivery(d)
	}
	return out, err
}

// UpsertGitHubConnection implements integrations.Repository.
func (s *Store) UpsertGitHubConnection(ctx context.Context, c integrations.GitHubConnection) error {
	return s.q.UpsertGitHubConnection(ctx, integrationsdb.UpsertGitHubConnectionParams{ProjectID: c.ProjectID, Repository: c.Repository, Token: c.Token, Labels: c.Labels})
}

// GetGitHubConnection implements integrations.Repository.
func (s *Store) GetGitHubConnection(ctx context.Context, projectID int64) (integrations.GitHubConnection, error) {
	row, err := s.q.GetGitHubConnection(ctx, projectID)
	return integrations.GitHubConnection{
		ProjectID: row.ProjectID, Repository: row.Repository, Token: row.Token, Labels: row.Labels,
		LastSyncedAt: timePtr(row.LastSyncedAt), LastError: row.LastError, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, notFound(err)
}

// DeleteGitHubConnection implements integrations.Repository.
func (s *Store) DeleteGitHubConnection(ctx context.Context, projectID int64) (bool, error) {
	n, err := s.q.DeleteGitHubConnection(ctx, projectID)
	return n > 0, err
}

// RecordGitHubSync implements integrations.Repository.
func (s *Store) RecordGitHubSync(ctx context.Context, projectID int64, syncedAt *time.Time, lastError string) error {
	return s.q.RecordGitHubSync(ctx, integrationsdb.RecordGitHubSyncParams{ProjectID: projectID, SyncedAt: timestamptz(syncedAt), LastError: lastError})
}
