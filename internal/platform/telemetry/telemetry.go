package telemetry

import (
	"context"
	"fmt"
	"github.com/felixge/httpsnoop"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type Runtime struct {
	Registry *prometheus.Registry
	Shutdown func(context.Context) error
}

func Init(ctx context.Context, service string) (*Runtime, error) {
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewWithAttributes("", attribute.String("service.name", service))), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1)))}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exp, e := otlptracehttp.New(ctx)
		if e != nil {
			return nil, e
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return &Runtime{reg, provider.Shutdown}, nil
}
func (t *Runtime) Metrics() http.Handler {
	return promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{})
}
func WrapHTTP(next http.Handler, logger *slog.Logger, registry *prometheus.Registry) http.Handler {
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "marketplace_http_duration_seconds", Help: "HTTP request duration", Buckets: prometheus.DefBuckets}, []string{"method", "route", "status"})
	registry.MustRegister(duration)
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := httpsnoop.CaptureMetrics(next, w, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		duration.WithLabelValues(r.Method, route, fmt.Sprint(m.Code)).Observe(m.Duration.Seconds())
		if strings.HasPrefix(r.URL.Path, "/health/") {
			return
		}
		sc := trace.SpanContextFromContext(r.Context())
		logger.InfoContext(r.Context(), "http_request", "method", r.Method, "route", route, "status", m.Code, "duration_ms", m.Duration.Milliseconds(), "request_id", r.Header.Get("X-Request-ID"), "trace_id", sc.TraceID().String())
	}), "http.server")
}

type SQLTracer struct{}

func (SQLTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := "query"
	if fields := strings.Fields(data.SQL); len(fields) > 0 {
		op = strings.ToUpper(fields[0])
	}
	ctx, _ = otel.Tracer("marketplace/postgres").Start(ctx, "postgres "+op, trace.WithSpanKind(trace.SpanKindClient))
	return ctx
}
func (SQLTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if data.Err != nil {
		span.SetAttributes(attribute.Bool("db.failed", true))
	}
	span.End()
}
