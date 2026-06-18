package observability

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"

	"dolphin/internal/config"
	"dolphin/internal/hook"
)

func BuildObservability(cfg *config.Config, hr *hook.Registry, log ...*zap.Logger) (shutdown func()) {
	if !cfg.GetBool("otel.enabled") {
		return func() {}
	}

	var opts []otlptracehttp.Option

	if endpoint := cfg.GetString("otel.endpoint"); endpoint != "" {
		u, err := url.Parse(endpoint)
		if err == nil && u.Host != "" {
			host := u.Host
			if u.Port() == "" {
				switch u.Scheme {
				case "https":
					host = u.Host + ":443"
				case "http":
					host = u.Host + ":80"
				}
			}
			opts = append(opts, otlptracehttp.WithEndpoint(host))
			if u.Path != "" && u.Path != "/" {
				path := strings.TrimSuffix(u.Path, "/")
				if !strings.HasSuffix(path, "/v1/traces") {
					opts = append(opts, otlptracehttp.WithURLPath(path+"/v1/traces"))
				}
			}
		} else {
			opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		}
	}

	if headers := otelHeaders(cfg); len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}

	// Log export errors through the application logger.
	if len(log) > 0 {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			log[0].Error("otel export error", zap.Error(err))
		}))
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		if len(log) > 0 {
			log[0].Warn("otel exporter creation failed", zap.Error(err))
		}
		return func() {}
	}

	res := resource.NewWithAttributes(
		"https://opentelemetry.io/schemas/1.30.0",
		attribute.String("service.name", "dolphin"),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(1*time.Second),
		),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	hr.Register(NewOTelHook(tp))
	if mh, err := NewMetricsHook(otel.GetMeterProvider()); err == nil {
		hr.Register(mh)
	}

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = tp.ForceFlush(ctx)
		cancel()
		_ = tp.Shutdown(context.Background())
	}
}

// otelHeaders reads otel.headers.* from config and returns as a map.
func otelHeaders(cfg *config.Config) map[string]string {
	headers := make(map[string]string)
	prefix := "otel.headers."
	for _, key := range cfg.Keys() {
		if name, ok := strings.CutPrefix(key, prefix); ok {
			headers[name] = cfg.GetString(key)
		}
	}
	return headers
}

func NewMetricsHook(mp metric.MeterProvider) (*MetricsHook, error) {
	meter := mp.Meter("dolphin")
	return newMetricsHookFromMeter(meter)
}

// BuildPrometheus creates a PrometheusHook based on config and optionally starts
// a pull-mode HTTP server. Returns a shutdown function.
func BuildPrometheus(cfg *config.Config, hr *hook.Registry, log ...*zap.Logger) (shutdown func()) {
	return buildPrometheus(cfg, hr, prometheus.DefaultRegisterer, log...)
}

func buildPrometheus(cfg *config.Config, hr *hook.Registry, reg prometheus.Registerer, log ...*zap.Logger) (shutdown func()) {
	if !cfg.GetBool("prometheus.enabled") {
		return func() {}
	}

	mode := cfg.GetString("prometheus.mode")
	if mode == "" {
		mode = "pull"
	}

	var remoteWriteURL string
	if mode == "push" || mode == "both" {
		remoteWriteURL = cfg.GetString("prometheus.remote_url")
	}

	hook := newPrometheusHook(reg, remoteWriteURL, log...)
	hr.Register(hook)

	var pullShutdown func()
	if mode == "pull" || mode == "both" {
		addr := cfg.GetString("prometheus.addr")
		if addr == "" {
			addr = ":9090"
		}
		var err error
		pullShutdown, err = StartPrometheusServer(addr)
		if err != nil {
			return func() {}
		}
	}

	return func() {
		if pullShutdown != nil {
			pullShutdown()
		}
	}
}

func newMetricsHookFromMeter(meter metric.Meter) (*MetricsHook, error) {
	turnDuration, err := meter.Float64Histogram("dolphin.turn.duration")
	if err != nil {
		return nil, err
	}
	llmTokens, err := meter.Int64Counter("dolphin.llm.tokens")
	if err != nil {
		return nil, err
	}
	toolCalls, err := meter.Int64Counter("dolphin.tool.calls")
	if err != nil {
		return nil, err
	}
	turnTotal, err := meter.Int64Counter("dolphin.turn.total")
	if err != nil {
		return nil, err
	}
	cacheReadTokens, err := meter.Int64Counter("dolphin.llm.cache_read_tokens")
	if err != nil {
		return nil, err
	}
	cacheHitTokens, err := meter.Int64Counter("dolphin.llm.cache_hit_tokens")
	if err != nil {
		return nil, err
	}
	cacheMissTokens, err := meter.Int64Counter("dolphin.llm.cache_miss_tokens")
	if err != nil {
		return nil, err
	}

	return &MetricsHook{
		turnDuration:    turnDuration,
		llmTokens:       llmTokens,
		toolCalls:       toolCalls,
		turnTotal:       turnTotal,
		cacheReadTokens: cacheReadTokens,
		cacheHitTokens:  cacheHitTokens,
		cacheMissTokens: cacheMissTokens,
	}, nil
}
