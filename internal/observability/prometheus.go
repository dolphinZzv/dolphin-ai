package observability

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/prompb"
	"go.uber.org/zap"

	"dolphin/internal/event"
	"dolphin/internal/hook"
)

// PrometheusHook updates Prometheus metrics from lifecycle events.
// Supports both pull mode (/metrics endpoint) and push mode (Remote Write).
type PrometheusHook struct {
	turnTotal          *prometheus.CounterVec
	turnDuration       prometheus.Histogram
	systemContextChars prometheus.Gauge
	toolCalls          *prometheus.CounterVec
	inputTokens        *prometheus.CounterVec
	outputTokens       *prometheus.CounterVec
	cacheReadTokens    *prometheus.CounterVec
	cacheHitTokens     *prometheus.CounterVec
	cacheMissTokens    *prometheus.CounterVec

	remoteWriteURL string
	log            *zap.Logger
}

func NewPrometheusHook(remoteWriteURL string, log ...*zap.Logger) *PrometheusHook {
	return newPrometheusHook(prometheus.DefaultRegisterer, remoteWriteURL, log...)
}

func newPrometheusHook(reg prometheus.Registerer, remoteWriteURL string, log ...*zap.Logger) *PrometheusHook {
	h := &PrometheusHook{
		turnTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_turn_total",
			Help: "Total number of completed turns.",
		}, []string{"session_id"}),
		turnDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "dolphin_turn_duration_seconds",
			Help:    "Turn duration in seconds.",
			Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		}),
		systemContextChars: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dolphin_system_context_chars",
			Help: "Length of the system prompt in characters for the current turn.",
		}),
		toolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_tool_calls_total",
			Help: "Total number of tool calls executed.",
		}, []string{"session_id"}),
		inputTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_input_tokens_total",
			Help: "Total number of input tokens consumed.",
		}, []string{"session_id"}),
		outputTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_output_tokens_total",
			Help: "Total number of output tokens produced.",
		}, []string{"session_id"}),
		cacheReadTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_cache_read_tokens_total",
			Help: "Total number of cache read tokens (prompt cached / cache read input).",
		}, []string{"session_id"}),
		cacheHitTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_prompt_cache_hit_tokens_total",
			Help: "Total number of prompt tokens served from cache (DeepSeek).",
		}, []string{"session_id"}),
		cacheMissTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dolphin_prompt_cache_miss_tokens_total",
			Help: "Total number of prompt tokens not found in cache (DeepSeek).",
		}, []string{"session_id"}),
		remoteWriteURL: remoteWriteURL,
	}

	if len(log) > 0 {
		h.log = log[0]
	}

	reg.MustRegister(
		h.turnTotal,
		h.turnDuration,
		h.systemContextChars,
		h.toolCalls,
		h.inputTokens,
		h.outputTokens,
		h.cacheReadTokens,
		h.cacheHitTokens,
		h.cacheMissTokens,
	)

	return h
}

func (h *PrometheusHook) Name() string { return "prometheus" }

func (h *PrometheusHook) Handle(ctx context.Context, e event.Event) error {
	sid := e.SessionID

	switch e.Type { //nolint:exhaustive // exports only turn/tool/token metrics
	case event.EventTurnComplete:
		h.turnTotal.WithLabelValues(sid).Inc()

		if v, ok := e.Payload["duration_ms"].(float64); ok {
			h.turnDuration.Observe(v / 1000.0)
		}

		if v, ok := e.Payload["system_context_length"].(float64); ok {
			h.systemContextChars.Set(v)
		} else if v, ok := e.Payload["system_context_length"].(int); ok {
			h.systemContextChars.Set(float64(v))
		}

		if v, ok := e.Payload["tool_call_count"].(float64); ok {
			h.toolCalls.WithLabelValues(sid).Add(v)
		} else if v, ok := e.Payload["tool_call_count"].(int); ok {
			h.toolCalls.WithLabelValues(sid).Add(float64(v))
		}

		// Push via Remote Write after turn completes, if configured.
		// pushMetrics runs detached from this request-scoped handler and
		// applies its own bounded timeout, so it must not borrow `ctx`.
		if h.remoteWriteURL != "" {
			go h.pushMetrics() //nolint:gosec // G118: detached push needs an independent context
		}

	case event.EventLLMComplete:
		if v, ok := e.Payload["input_tokens"].(int); ok && v > 0 {
			h.inputTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["input_tokens"].(float64); ok && v > 0 {
			h.inputTokens.WithLabelValues(sid).Add(v)
		}

		if v, ok := e.Payload["output_tokens"].(int); ok && v > 0 {
			h.outputTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["output_tokens"].(float64); ok && v > 0 {
			h.outputTokens.WithLabelValues(sid).Add(v)
		}

		if v, ok := e.Payload["cache_read_input_tokens"].(int); ok && v > 0 {
			h.cacheReadTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["cache_read_input_tokens"].(float64); ok && v > 0 {
			h.cacheReadTokens.WithLabelValues(sid).Add(v)
		}

		if v, ok := e.Payload["prompt_cached_tokens"].(int); ok && v > 0 {
			h.cacheReadTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["prompt_cached_tokens"].(float64); ok && v > 0 {
			h.cacheReadTokens.WithLabelValues(sid).Add(v)
		}

		if v, ok := e.Payload["prompt_cache_hit_tokens"].(int); ok && v > 0 {
			h.cacheHitTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["prompt_cache_hit_tokens"].(float64); ok && v > 0 {
			h.cacheHitTokens.WithLabelValues(sid).Add(v)
		}

		if v, ok := e.Payload["prompt_cache_miss_tokens"].(int); ok && v > 0 {
			h.cacheMissTokens.WithLabelValues(sid).Add(float64(v))
		} else if v, ok := e.Payload["prompt_cache_miss_tokens"].(float64); ok && v > 0 {
			h.cacheMissTokens.WithLabelValues(sid).Add(v)
		}
	}

	return nil
}

// pushMetrics gathers all registered metrics and pushes them to the configured
// Prometheus server via Remote Write (protobuf + snappy).
func (h *PrometheusHook) pushMetrics() {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		if h.log != nil {
			h.log.Error("prometheus gather failed", zap.Error(err))
		}
		return
	}

	ts := make([]prompb.TimeSeries, 0)
	now := time.Now().UnixMilli()

	for _, mf := range mfs {
		ts = append(ts, metricToTimeSeries(mf, now)...)
	}

	if len(ts) == 0 {
		if h.log != nil {
			h.log.Warn("prometheus push: no time series to push")
		}
		return
	}

	wr := &prompb.WriteRequest{
		Timeseries: ts,
	}

	data, err := proto.Marshal(wr)
	if err != nil {
		if h.log != nil {
			h.log.Error("prometheus proto marshal failed", zap.Error(err))
		}
		return
	}

	compressed := snappy.Encode(nil, data)

	// pushMetrics runs in a detached goroutine outliving the event handler,
	// so it cannot borrow the request-scoped context. A bounded timeout
	// prevents a hanging remote-write from leaking the goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.remoteWriteURL+"/api/v1/write", bytes.NewReader(compressed))
	if err != nil {
		if h.log != nil {
			h.log.Error("prometheus http request creation failed", zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if h.log != nil {
			h.log.Error("prometheus remote write failed", zap.Error(err))
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if h.log != nil {
			h.log.Error("prometheus remote write rejected",
				zap.Int("status", resp.StatusCode),
				zap.String("body", string(body)),
			)
		}
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if h.log != nil {
		h.log.Debug(fmt.Sprintf("prometheus push: %d series", len(ts)))
	}
}

// metricToTimeSeries converts a Prometheus metric family to remote write time series.
func metricToTimeSeries(mf *dto.MetricFamily, timestamp int64) []prompb.TimeSeries {
	name := mf.GetName()
	metricType := mf.GetType()

	var series []prompb.TimeSeries
	for _, m := range mf.GetMetric() {
		labels := make([]prompb.Label, 0, len(m.GetLabel())+1)
		labels = append(labels, prompb.Label{Name: "__name__", Value: name})
		for _, l := range m.GetLabel() {
			labels = append(labels, prompb.Label{Name: l.GetName(), Value: l.GetValue()})
		}

		var samples []prompb.Sample
		switch metricType { //nolint:exhaustive // SUMMARY/UNTYPED/GAUGE_HISTOGRAM fall through to default (skipped)
		case dto.MetricType_COUNTER:
			if m.Counter != nil {
				samples = append(samples, prompb.Sample{
					Value:     m.Counter.GetValue(),
					Timestamp: timestamp,
				})
			}
		case dto.MetricType_GAUGE:
			if m.Gauge != nil {
				samples = append(samples, prompb.Sample{
					Value:     m.Gauge.GetValue(),
					Timestamp: timestamp,
				})
			}
		case dto.MetricType_HISTOGRAM:
			if m.Histogram != nil {
				samples = append(samples, prompb.Sample{
					Value:     m.Histogram.GetSampleSum(),
					Timestamp: timestamp,
				})
			}
		default:
			if m.Gauge != nil {
				samples = append(samples, prompb.Sample{
					Value:     m.Gauge.GetValue(),
					Timestamp: timestamp,
				})
			}
		}

		if len(samples) > 0 {
			series = append(series, prompb.TimeSeries{
				Labels:  labels,
				Samples: samples,
			})
		}
	}
	return series
}

// StartPrometheusServer starts an HTTP server exposing /metrics and returns a
// shutdown function.
func StartPrometheusServer(addr string) (func(), error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() { _ = srv.ListenAndServe() }()

	return func() { _ = srv.Close() }, nil
}

// Ensure PrometheusHook implements hook.Handler.
var _ hook.Handler = (*PrometheusHook)(nil)
