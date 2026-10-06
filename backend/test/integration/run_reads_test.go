//go:build integration

package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/ingestion"
)

// queryCounter counts the statements a pool sends.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}
func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// A live poll (authorizing the run, then its live state) costs a fixed, small number of statements, whatever the size
// of the run (card #44: it read the run three times and its summary inputs twice).
func TestRunReads(t *testing.T) {
	t.Run("BE-INT-066_a_live_poll_costs_a_fixed_small_number_of_statements", func(t *testing.T) {
		s, ctx := fresh(t)
		counter := &queryCounter{}
		cfg, err := pgxpool.ParseConfig(db.URL)
		require.NoError(t, err)
		cfg.ConnConfig.Tracer = counter
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		counted := app.NewServicesWith(pool, time.Now, testIdentity())
		ctx = identity.WithUser(ctx, identity.User{Username: "integration", IsAdmin: true})

		poll := func(cases int) int64 {
			for i := range cases {
				_, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: catalog.DefaultProjectID, Title: "t" + itoa(int64(i)), Automated: true})
				require.NoError(t, err)
			}
			run, err := s.Live.Start(ctx, ingestion.RunMeta{Provider: "github", ProviderRunID: "poll" + itoa(int64(cases)), RunAttempt: 1})
			require.NoError(t, err)
			counter.n.Store(0)
			_, err = counted.Execution.RunProject(ctx, run.ID)
			require.NoError(t, err)
			_, err = counted.Execution.Live(ctx, run.ID)
			require.NoError(t, err)
			return counter.n.Load()
		}
		small, large := poll(1), poll(300)
		assert.LessOrEqual(t, small, int64(5), "authorize + run + summary inputs + diagnostics + events")
		assert.Equal(t, small, large, "the statements do not grow with the run")
	})
}
