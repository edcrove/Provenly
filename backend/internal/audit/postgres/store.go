// Package postgres is the PostgreSQL adapter of the audit Repository, built on the sqlc-generated auditdb queries.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/audit"
	"github.com/edcrove/provenly/backend/internal/audit/auditdb"
)

// Store implements audit.Repository.
type Store struct{ q *auditdb.Queries }

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{q: auditdb.New(pool)} }

var _ audit.Repository = (*Store)(nil)

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// Insert implements audit.Repository.
func (s *Store) Insert(ctx context.Context, e audit.Event) error {
	return s.q.InsertAuditEvent(ctx, auditdb.InsertAuditEventParams{Actor: e.Actor, Action: e.Action, Path: e.Path, ProjectKey: text(e.ProjectKey), Status: e.Status,
		Summary: text(e.Summary), TestCaseKey: text(e.TestCaseKey), Ip: text(e.IP), UserAgent: text(e.UserAgent)})
}

// List implements audit.Repository.
func (s *Store) List(ctx context.Context, f audit.Filter, limit, offset int32) ([]audit.Event, error) {
	rows, err := s.q.ListAuditEvents(ctx, auditdb.ListAuditEventsParams{ProjectKey: text(f.ProjectKey), Actor: text(f.Actor), TestCaseKey: text(f.TestCaseKey), ProjectKeys: f.ProjectKeys, PageLimit: limit, PageOffset: offset})
	out := make([]audit.Event, len(rows))
	for i, r := range rows {
		out[i] = audit.Event{ID: r.ID, OccurredAt: r.OccurredAt.Time, Actor: r.Actor, Action: r.Action, Path: r.Path, ProjectKey: r.ProjectKey.String, Status: r.Status,
			Summary: r.Summary.String, TestCaseKey: r.TestCaseKey.String, IP: r.Ip.String, UserAgent: r.UserAgent.String}
	}
	return out, err
}

// Count implements audit.Repository.
func (s *Store) Count(ctx context.Context, f audit.Filter) (int64, error) {
	return s.q.CountAuditEvents(ctx, auditdb.CountAuditEventsParams{ProjectKey: text(f.ProjectKey), Actor: text(f.Actor), TestCaseKey: text(f.TestCaseKey), ProjectKeys: f.ProjectKeys})
}
