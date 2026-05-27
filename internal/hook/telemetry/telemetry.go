// Package telemetry provides OpenTelemetry hooks and metrics for agent lifecycle events.
package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"dolphin/internal/config"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	tracerProvider *sdktrace.TracerProvider
	logProvider    *sdklog.LoggerProvider
	meterProvider  *sdkmetric.MeterProvider

	// Span truncation limits (configurable via telemetry.input_max_len / output_max_len)
	// 0 = unlimited.
	spanInputMaxLen  = 2048
	spanOutputMaxLen = 2048
)

// otlpTarget holds parsed OTLP endpoint parts shared by all three signals.
type otlpTarget struct {
	host     string
	path     string // base path from endpoint URL (e.g. /api/org-id)
	insecure bool
}

func parseOTLPTarget(raw string) (otlpTarget, error) {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return otlpTarget{host: raw, insecure: true}, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return otlpTarget{}, err
	}
	return otlpTarget{
		host:     u.Host,
		path:     strings.TrimRight(u.Path, "/"),
		insecure: u.Scheme == "http",
	}, nil
}

// Init initializes the global OTel providers for traces, logs, and metrics.
func Init(ctx context.Context, cfg config.TelemetryConfig) error {
	if !cfg.Enabled {
		return nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
		),
	)
	if err != nil {
		return fmt.Errorf("telemetry: create resource: %w", err)
	}

	// Route OTel SDK internal errors (export failures, timeouts, etc.) through zap
	// instead of the default stderr logging.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		zap.S().Errorw("otel error", "error", err)
	}))

	// ---- traces ----
	traceExp, err := newTraceExporter(ctx, cfg)
	if err != nil {
		return fmt.Errorf("telemetry: create trace exporter: %w", err)
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tracerProvider)

	// ---- logs (optional) ----
	if cfg.LogsEnabled && cfg.Exporter != "stdout" {
		logExp, lerr := newLogExporter(ctx, cfg)
		if lerr != nil {
			return fmt.Errorf("telemetry: create log exporter: %w", lerr)
		}
		logProvider = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
	}

	// ---- metrics (optional) ----
	if cfg.MetricsEnabled && cfg.Exporter != "stdout" {
		metricExp, merr := newMetricExporter(ctx, cfg)
		if merr != nil {
			return fmt.Errorf("telemetry: create metric exporter: %w", merr)
		}
		meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
				sdkmetric.WithInterval(30*time.Second),
			)),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(meterProvider)
	}
	initMetrics()

	spanInputMaxLen = cfg.InputMaxLen
	spanOutputMaxLen = cfg.OutputMaxLen

	// ---- startup banner ----
	endpoint := cfg.OTLPEndpoint
	if tgt, e := parseOTLPTarget(cfg.OTLPEndpoint); e == nil {
		endpoint = tgt.host
	}
	signals := []string{"traces"}
	if cfg.LogsEnabled {
		signals = append(signals, "logs")
	}
	if cfg.MetricsEnabled {
		signals = append(signals, "metrics")
	}
	fmt.Fprintf(os.Stderr, "\n=== Telemetry active ===\nExporter: %s  Endpoint: %s  Signals: %s  SampleRate: %.0f%%\n\n",
		cfg.Exporter, endpoint, strings.Join(signals, "+"), cfg.SampleRate*100)

	zap.S().Infow("telemetry initialized",
		"exporter", cfg.Exporter,
		"endpoint", cfg.OTLPEndpoint,
		"service", cfg.ServiceName,
		"signals", signals,
		"sample_rate", cfg.SampleRate,
	)
	return nil
}

// BridgeZap replaces the global zap logger with one that also writes to OTel.
func BridgeZap() {
	if logProvider == nil {
		return
	}
	otelCore := otelzap.NewCore("dolphin", otelzap.WithLoggerProvider(logProvider))
	merged := zap.New(zapcore.NewTee(zap.L().Core(), otelCore))
	zap.ReplaceGlobals(merged)
}

// Shutdown flushes and shuts down all providers.
func Shutdown(ctx context.Context) error {
	var errs []string
	if tracerProvider != nil {
		if err := tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, "traces: "+err.Error())
		}
	}
	if logProvider != nil {
		if err := logProvider.Shutdown(ctx); err != nil {
			errs = append(errs, "logs: "+err.Error())
		}
	}
	if meterProvider != nil {
		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, "metrics: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("telemetry shutdown errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Tracer returns a named tracer.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// ---- trace exporter ----

func newTraceExporter(ctx context.Context, cfg config.TelemetryConfig) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "otlp-grpc":
		grpcOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.OTLPEndpoint == "" || !strings.HasPrefix(cfg.OTLPEndpoint, "https://") {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.OTLPHeaders) > 0 {
			grpcOpts = append(grpcOpts, otlptracegrpc.WithHeaders(cfg.OTLPHeaders))
		}
		return otlptracegrpc.New(ctx, grpcOpts...)
	case "otlp-http":
		tgt, err := parseOTLPTarget(cfg.OTLPEndpoint)
		if err != nil {
			return nil, err
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(tgt.host),
			otlptracehttp.WithURLPath(tgt.path + "/v1/traces"),
		}
		if len(cfg.OTLPHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.OTLPHeaders))
		}
		if tgt.insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	default:
		return nil, fmt.Errorf("unknown exporter: %q", cfg.Exporter)
	}
}

// ---- log exporter ----

func newLogExporter(ctx context.Context, cfg config.TelemetryConfig) (sdklog.Exporter, error) {
	tgt, err := parseOTLPTarget(cfg.OTLPEndpoint)
	if err != nil {
		return nil, err
	}
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(tgt.host),
		otlploghttp.WithURLPath(tgt.path + "/v1/logs"),
	}
	if len(cfg.OTLPHeaders) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(cfg.OTLPHeaders))
	}
	if tgt.insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return otlploghttp.New(ctx, opts...)
}

// ---- metric exporter ----

func newMetricExporter(ctx context.Context, cfg config.TelemetryConfig) (sdkmetric.Exporter, error) {
	tgt, err := parseOTLPTarget(cfg.OTLPEndpoint)
	if err != nil {
		return nil, err
	}
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(tgt.host),
		otlpmetrichttp.WithURLPath(tgt.path + "/v1/metrics"),
	}
	if len(cfg.OTLPHeaders) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(cfg.OTLPHeaders))
	}
	if tgt.insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return otlpmetrichttp.New(ctx, opts...)
}
