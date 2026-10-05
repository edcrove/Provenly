package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupExportsOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	exported := keep{tracetest.NewInMemoryExporter()}
	calls := 0
	exporter := func(context.Context) (sdktrace.SpanExporter, error) { calls++; return exported, nil }

	shutdown, err := Setup(ctx, "test", false, exporter)
	require.NoError(t, err)
	_, span := Tracer("x").Start(ctx, "quiet")
	assert.True(t, span.SpanContext().IsValid(), "spans exist without an exporter, so logs correlate")
	span.End()
	require.NoError(t, shutdown(ctx))
	assert.Zero(t, calls)

	shutdown, err = Setup(ctx, "test", true, exporter)
	require.NoError(t, err)
	_, span = Tracer("x").Start(ctx, "exported")
	span.End()
	require.NoError(t, shutdown(ctx))
	require.Len(t, exported.GetSpans(), 1)
	assert.Equal(t, "exported", exported.GetSpans()[0].Name)
	assert.Contains(t, exported.GetSpans()[0].Resource.String(), "service.name=provenly-api")

	_, err = Setup(ctx, "test", true, func(context.Context) (sdktrace.SpanExporter, error) { return nil, errors.New("no endpoint") })
	assert.EqualError(t, err, "no endpoint")

	otlp, err := OTLP(ctx)
	require.NoError(t, err, "the OTLP exporter connects lazily")
	require.NoError(t, otlp.Shutdown(ctx))
}

func TestMiddlewareAndRouter(t *testing.T) {
	ctx := context.Background()
	rec := tracetest.NewSpanRecorder()
	shutdown, err := Setup(ctx, "test", false, nil, sdktrace.WithSpanProcessor(rec))
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := Middleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/things/7", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assert.Equal(t, http.StatusNoContent, res.Code)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", res.Header().Get(TraceHeader), "an incoming trace continues")

	res = httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	assert.Len(t, res.Header().Get(TraceHeader), 32)

	spans := rec.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "GET /things/{id}", spans[0].Name())
	assert.Equal(t, "GET", spans[1].Name(), "an unrouted request keeps the method name")
}

func TestLogHandlerAddsTraceIDs(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Setup(ctx, "test", false, nil)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()
	var buf bytes.Buffer
	log := slog.New(LogHandler{slog.NewJSONHandler(&buf, nil)}).With("module", "m").WithGroup("g")
	log.InfoContext(ctx, "no span")
	assert.NotContains(t, buf.String(), "trace_id")
	spanCtx, span := Tracer("x").Start(ctx, "s")
	log.InfoContext(spanCtx, "in span", "k", "v")
	span.End()
	_, line, found := strings.Cut(buf.String(), "in span")
	require.True(t, found)
	assert.Contains(t, line, `"trace_id":"`+span.SpanContext().TraceID().String()+`"`)
	assert.Contains(t, line, `"span_id":"`+span.SpanContext().SpanID().String()+`"`)
	assert.Contains(t, buf.String(), `"module":"m"`)
}

// keep is an in-memory exporter whose spans survive the provider's shutdown.
type keep struct{ *tracetest.InMemoryExporter }

func (keep) Shutdown(context.Context) error { return nil }
