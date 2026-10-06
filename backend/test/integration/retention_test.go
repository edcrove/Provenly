//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/integrations"
	integrationspg "github.com/edcrove/provenly/backend/internal/integrations/postgres"
)

func TestRetention(t *testing.T) {
	t.Run("BE-INT-070_finished_webhook_deliveries_past_the_retention_are_purged_in_batches_under_a_lock", func(t *testing.T) {
		ctx := context.Background()
		require.NoError(t, db.Reset(ctx))
		at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
		s := app.NewServicesConfig(db.Pool, func() time.Time { return at }, app.Config{
			Identity: testIdentity(), SecretsKey: []byte(strings.Repeat("s", 32)),
			Integrations: integrations.Config{AllowPrivate: true, DeliveryRetention: 90 * 24 * time.Hour},
		})
		admin := identity.WithUser(ctx, identity.User{Username: "integration", IsAdmin: true})
		hook, _, err := s.Integrations.CreateWebhook(admin, "TC", "https://hooks.example.com/x", []string{integrations.EventRunCompleted})
		require.NoError(t, err)
		// 2,005 old finished deliveries (more than two batches), and what must stay: an old pending one, a finished one
		// inside the retention, one finished exactly at the cutoff.
		add := func(n int, status string, completed *time.Time) {
			_, err := db.Pool.Exec(ctx, `INSERT INTO webhook_deliveries (webhook_id, event, payload, status, completed_at, created_at)
				SELECT $1, 'run.completed', '{}', $2, $3, $4 FROM generate_series(1, $5)`, hook.ID, status, completed, at.AddDate(0, -6, 0), n)
			require.NoError(t, err)
		}
		old, recent, cutoff := at.AddDate(0, 0, -91), at.AddDate(0, 0, -10), at.AddDate(0, 0, -90)
		add(2000, "succeeded", &old)
		add(5, "failed", &old)
		add(1, "pending", nil)
		add(1, "succeeded", &recent)
		add(1, "failed", &cutoff)
		_, err = db.Pool.Exec(ctx, `INSERT INTO audit_events (occurred_at, actor, action, path, status) VALUES ($1, 'ana', 'POST /x', '/x', 201)`, at.AddDate(-2, 0, 0))
		require.NoError(t, err)
		count := func(q string) (n int) {
			require.NoError(t, db.Pool.QueryRow(ctx, q).Scan(&n))
			return n
		}

		// Another server purging holds the lock: this one deletes nothing.
		tx, err := db.Pool.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x70726f76_70757267))
		require.NoError(t, err)
		n, err := s.Integrations.PurgeDeliveries(ctx)
		require.NoError(t, err)
		assert.Zero(t, n, "the lock is held elsewhere")
		require.NoError(t, tx.Rollback(ctx))

		n, err = s.Integrations.PurgeDeliveries(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2005), n)
		assert.Equal(t, 3, count(`SELECT count(*) FROM webhook_deliveries`), "pending, recent and the one at the cutoff stay")
		assert.Equal(t, 1, count(`SELECT count(*) FROM webhook_deliveries WHERE status = 'pending'`))
		assert.Equal(t, 1, count(`SELECT count(*) FROM audit_events`), "audit events are never purged")

		// No retention keeps everything.
		add(3, "succeeded", &old)
		keep := integrationspg.NewStore(db.Pool)
		n, err = integrations.NewService(keep, s.Catalog, s.Identity, nil, integrations.Config{}, func() time.Time { return at }).PurgeDeliveries(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)
		assert.Equal(t, 6, count(`SELECT count(*) FROM webhook_deliveries`))
	})
}
