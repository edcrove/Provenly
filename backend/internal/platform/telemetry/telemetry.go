// Package telemetry is Provenly's OpenTelemetry foundation (Trello "OpenTelemetry Basic Instrumentation", DEC-11):
// every API request, ingestion step and database query is a span; logs carry the trace and span ids; responses carry
// the trace id. Spans are always created so logs correlate; they are exported over OTLP/HTTP only when an endpoint
// is configured (OTEL_EXPORTER_OTLP_ENDPOINT). Test results are not linked to traces yet: the trace id of the
// ingestion that recorded them is in the logs and can be stored later without changing this package.
package telemetry

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ServiceName names the API in traces.
const ServiceName = "provenly-api"

// TraceHeader is the response header with the request's trace id (for support and bug reports).
const TraceHeader = "X-Trace-Id"

// Exporter builds the span exporter of an OTLP endpoint (otlptracehttp.New in production; reads the standard
// OTEL_EXPORTER_OTLP_* variables).
type Exporter func(ctx context.Context) (sdktrace.SpanExporter, error)

// OTLP is the production exporter.
func OTLP(ctx context.Context) (sdktrace.SpanExporter, error) { return otlptracehttp.New(ctx) }

// Setup installs the global tracer provider and W3C propagation for an environment. With export false spans are
// still created (logs correlate) but never leave the process. It returns the provider's shutdown, which flushes.
func Setup(ctx context.Context, env string, export bool, exporter Exporter, extra ...sdktrace.TracerProviderOption) (func(context.Context) error, error) {
	opts := append([]sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", ServiceName), attribute.String("deployment.environment.name", env))),
	}, extra...)
	if export {
		exp, err := exporter(ctx)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}

// Tracer returns the tracer of a module.
func Tracer(module string) trace.Tracer { return otel.Tracer("provenly/" + module) }

// Middleware traces each request (continuing an incoming W3C traceparent) and returns its trace id in TraceHeader.
// A routed request's span is named after its route pattern ("GET /api/v1/test-runs/{testRunId}", set by the mux on
// the request); an unrouted one after its method.
func Middleware(next http.Handler) http.Handler {
	tag := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sc := trace.SpanContextFromContext(r.Context()); sc.HasTraceID() {
			w.Header().Set(TraceHeader, sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
	return otelhttp.NewHandler(tag, "http", otelhttp.WithSpanNameFormatter(spanName))
}

func spanName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method
}

// LogHandler adds the trace_id and span_id of the record's context to every log record.
type LogHandler struct{ slog.Handler }

// Handle implements slog.Handler.
func (h LogHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return LogHandler{h.Handler.WithAttrs(attrs)}
}

// WithGroup implements slog.Handler.
func (h LogHandler) WithGroup(name string) slog.Handler { return LogHandler{h.Handler.WithGroup(name)} }
