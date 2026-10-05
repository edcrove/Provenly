//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/telemetry"
)

func TestTelemetry(t *testing.T) {
	t.Run("BE-INT-054_ingestion_and_its_database_queries_are_one_trace", func(t *testing.T) {
		_, _ = fresh(t)
		ctx := context.Background()
		rec := tracetest.NewSpanRecorder()
		shutdown, err := telemetry.Setup(ctx, "test", false, nil, sdktrace.WithSpanProcessor(rec))
		require.NoError(t, err)
		defer func() { _ = shutdown(ctx) }()
		pool, err := postgres.Open(ctx, db.URL)
		require.NoError(t, err)
		defer pool.Close()
		s := app.NewServicesWith(pool, time.Now, testIdentity())
		uctx := identity.WithUser(ctx, identity.User{Username: "integration", IsAdmin: true})
		tc, err := s.Catalog.Create(uctx, catalog.CreateInput{Title: "traced", Automated: true})
		require.NoError(t, err)

		out, err := s.Ingestion.IngestJUnit(uctx, meta("700", 1), strings.NewReader(junitFor(tcProp("traced", tc.Key(), ""))))
		require.NoError(t, err)

		var root sdktrace.ReadOnlySpan
		byName := map[string]int{}
		for _, sp := range rec.Ended() {
			byName[sp.Name()]++
			if sp.Name() == "ingestion.junit" {
				root = sp
			}
		}
		require.NotNil(t, root)
		attrs := map[string]any{}
		for _, a := range root.Attributes() {
			attrs[string(a.Key)] = a.Value.AsInterface()
		}
		assert.Equal(t, out.Run.ID, attrs["provenly.test_run.id"])
		assert.Equal(t, true, attrs["provenly.created"])
		assert.Equal(t, int64(1), attrs["provenly.results"])
		assert.Equal(t, 1, byName["ingestion.junit.parse"])
		queries := 0
		for _, sp := range rec.Ended() {
			if sp.SpanContext().TraceID() == root.SpanContext().TraceID() && sp.Name() != "ingestion.junit" && sp.Name() != "ingestion.junit.parse" {
				queries++
			}
		}
		assert.Greater(t, queries, 3, "the ingestion's database queries are spans of its trace: %v", byName)

		_, err = s.Ingestion.IngestJUnit(uctx, meta("701", 1), strings.NewReader("<not-junit"))
		require.Error(t, err)
		last := rec.Ended()[len(rec.Ended())-1]
		assert.Equal(t, "ingestion.junit", last.Name())
		assert.Equal(t, "Error", last.Status().Code.String())
	})
}
