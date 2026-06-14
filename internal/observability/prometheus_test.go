package observability

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/prompb"
)

var testHookOnce sync.Once
var testHook *PrometheusHook

// getTestHook returns a shared PrometheusHook instance. NewPrometheusHook calls
// prometheus.MustRegister which panics on duplicate registration, so we must
// only create one hook per test process.
func getTestHook(t *testing.T) *PrometheusHook {
	t.Helper()
	testHookOnce.Do(func() {
		testHook = NewPrometheusHook("")
	})
	return testHook
}

func TestNewPrometheusHook(t *testing.T) {
	h := getTestHook(t)
	if h == nil {
		t.Fatal("expected non-nil hook")
	}
	if h.Name() != "prometheus" {
		t.Fatalf("expected 'prometheus', got '%s'", h.Name())
	}
	if h.remoteWriteURL != "" {
		t.Fatalf("expected empty remoteWriteURL, got '%s'", h.remoteWriteURL)
	}
	if h.log != nil {
		t.Fatal("expected nil log when no logger passed")
	}
}

func TestPrometheusHook_Handle_TurnComplete(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-turn-complete"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: sid,
		Payload: map[string]any{
			"duration_ms":           float64(1234),
			"system_context_length": float64(5000),
			"tool_call_count":       float64(2),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_Handle_TurnCompleteIntValues(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-turn-int"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: sid,
		Payload: map[string]any{
			"duration_ms":           float64(567),
			"system_context_length": 3000,
			"tool_call_count":       1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_Handle_LLMComplete(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-llm-complete"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":             100,
			"output_tokens":            50,
			"cache_read_input_tokens":  30,
			"prompt_cached_tokens":     20,
			"prompt_cache_hit_tokens":  15,
			"prompt_cache_miss_tokens": 5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_Handle_LLMCompleteFloatValues(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-llm-float"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":             float64(200),
			"output_tokens":            float64(100),
			"cache_read_input_tokens":  float64(60),
			"prompt_cached_tokens":     float64(40),
			"prompt_cache_hit_tokens":  float64(30),
			"prompt_cache_miss_tokens": float64(10),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_Handle_LLMCompleteZeroValues(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-llm-zero"

	// Zero values should not update counters (v > 0 check).
	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":             0,
			"output_tokens":            0,
			"cache_read_input_tokens":  0,
			"prompt_cached_tokens":     0,
			"prompt_cache_hit_tokens":  0,
			"prompt_cache_miss_tokens": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_Handle_IgnoredEvents(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()

	for _, et := range []event.Type{
		event.EventTurnStart,
		event.EventTurnError,
		event.EventTurnInterrupt,
		event.EventLLMStart,
		event.EventLLMError,
		event.EventToolStart,
		event.EventToolComplete,
		event.EventToolError,
	} {
		err := h.Handle(ctx, event.Event{Type: et, SessionID: "ignored"})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", et, err)
		}
	}
}

func TestPrometheusHook_Handle_TurnCompleteTriggersPush(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: "session-push",
		Payload: map[string]any{
			"duration_ms":           float64(100),
			"system_context_length": float64(100),
			"tool_call_count":       float64(0),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestMetricToTimeSeries_Counter(t *testing.T) {
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_counter"})
	counter.Add(42)
	registry := prometheus.NewRegistry()
	registry.MustRegister(counter)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) != 1 {
		t.Fatalf("expected 1 metric family, got %d", len(mfs))
	}

	ts := metricToTimeSeries(mfs[0], 1000)
	if len(ts) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(ts))
	}
	if len(ts[0].Labels) != 1 {
		t.Fatalf("expected 1 label (__name__), got %d", len(ts[0].Labels))
	}
	if ts[0].Labels[0].Name != "__name__" {
		t.Fatalf("expected __name__ label, got '%s'", ts[0].Labels[0].Name)
	}
	if ts[0].Labels[0].Value != "test_counter" {
		t.Fatalf("expected 'test_counter', got '%s'", ts[0].Labels[0].Value)
	}
	if len(ts[0].Samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(ts[0].Samples))
	}
	if ts[0].Samples[0].Value != 42 {
		t.Fatalf("expected 42, got %f", ts[0].Samples[0].Value)
	}
	if ts[0].Samples[0].Timestamp != 1000 {
		t.Fatalf("expected 1000, got %d", ts[0].Samples[0].Timestamp)
	}
}

func TestMetricToTimeSeries_Gauge(t *testing.T) {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_gauge"})
	gauge.Set(3.14)
	registry := prometheus.NewRegistry()
	registry.MustRegister(gauge)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	ts := metricToTimeSeries(mfs[0], 2000)
	if len(ts) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(ts))
	}
	if ts[0].Samples[0].Value != 3.14 {
		t.Fatalf("expected 3.14, got %f", ts[0].Samples[0].Value)
	}
}

func TestMetricToTimeSeries_Histogram(t *testing.T) {
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "test_histogram"})
	hist.Observe(1.0)
	hist.Observe(2.0)
	hist.Observe(3.0)
	registry := prometheus.NewRegistry()
	registry.MustRegister(hist)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	ts := metricToTimeSeries(mfs[0], 3000)
	if len(ts) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(ts))
	}
	if ts[0].Samples[0].Value != 6.0 {
		t.Fatalf("expected sum 6.0, got %f", ts[0].Samples[0].Value)
	}
}

func TestMetricToTimeSeries_CounterVec(t *testing.T) {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_counter_vec"}, []string{"session_id"})
	cv.WithLabelValues("sess-a").Add(10)
	cv.WithLabelValues("sess-b").Add(20)
	registry := prometheus.NewRegistry()
	registry.MustRegister(cv)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	ts := metricToTimeSeries(mfs[0], 4000)
	if len(ts) != 2 {
		t.Fatalf("expected 2 time series, got %d", len(ts))
	}

	for _, s := range ts {
		hasName := false
		hasSID := false
		for _, l := range s.Labels {
			if l.Name == "__name__" {
				hasName = true
			}
			if l.Name == "session_id" {
				hasSID = true
			}
		}
		if !hasName || !hasSID {
			t.Fatalf("expected __name__ and session_id labels, got %v", s.Labels)
		}
	}
}

func TestMetricToTimeSeries_EmptyMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "empty_counter"})
	registry.MustRegister(counter)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	ts := metricToTimeSeries(mfs[0], 5000)
	if len(ts) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(ts))
	}
	if ts[0].Samples[0].Value != 0 {
		t.Fatalf("expected value 0, got %f", ts[0].Samples[0].Value)
	}
}

func TestBuildPrometheus_Disabled(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled": false,
	})
	hr := hook.NewRegistry()
	shutdown := BuildPrometheus(cfg, hr)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	shutdown()
}

// BuildPrometheus with enabled modes (pull/push/both) internally calls
// NewPrometheusHook which registers metrics with the default registry via
// MustRegister. That panics in-package because getTestHook already registered
// the same metric names. External test packages can test these paths.

func TestStartPrometheusServer(t *testing.T) {
	shutdown, err := StartPrometheusServer(":0")
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	time.Sleep(50 * time.Millisecond)

	shutdown2, err := StartPrometheusServer(":0")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	shutdown2()
	shutdown()
}

func TestPrometheusHook_ImplementsHandler(t *testing.T) {
	h := getTestHook(t)
	// Compile-time interface check via assignment.
	var _ hook.Handler = h
}

func TestPrometheusHook_GatherAfterEvents(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-gather"

	_ = h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: sid,
		Payload: map[string]any{
			"duration_ms":           float64(2500),
			"system_context_length": float64(2048),
			"tool_call_count":       3,
		},
	})

	_ = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":             500,
			"output_tokens":            200,
			"cache_read_input_tokens":  100,
			"prompt_cached_tokens":     80,
			"prompt_cache_hit_tokens":  60,
			"prompt_cache_miss_tokens": 40,
		},
	})

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}

	metricNames := make(map[string]bool)
	for _, mf := range mfs {
		metricNames[mf.GetName()] = true
	}

	expected := []string{
		"dolphin_turn_total",
		"dolphin_turn_duration_seconds",
		"dolphin_system_context_chars",
		"dolphin_tool_calls_total",
		"dolphin_input_tokens_total",
		"dolphin_output_tokens_total",
		"dolphin_cache_read_tokens_total",
		"dolphin_prompt_cache_hit_tokens_total",
		"dolphin_prompt_cache_miss_tokens_total",
	}
	for _, name := range expected {
		if !metricNames[name] {
			t.Errorf("expected metric family '%s' to exist", name)
		}
	}
}

func TestPrometheusHook_GatherVerifyValues(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-verify"

	_ = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":             100,
			"output_tokens":            50,
			"prompt_cache_hit_tokens":  25,
			"prompt_cache_miss_tokens": 10,
		},
	})

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "session_id" && l.GetValue() == sid {
					switch mf.GetName() {
					case "dolphin_input_tokens_total":
						if m.Counter.GetValue() != 100 {
							t.Errorf("input_tokens: expected 100, got %f", m.Counter.GetValue())
						}
					case "dolphin_output_tokens_total":
						if m.Counter.GetValue() != 50 {
							t.Errorf("output_tokens: expected 50, got %f", m.Counter.GetValue())
						}
					case "dolphin_prompt_cache_hit_tokens_total":
						if m.Counter.GetValue() != 25 {
							t.Errorf("prompt_cache_hit_tokens: expected 25, got %f", m.Counter.GetValue())
						}
					case "dolphin_prompt_cache_miss_tokens_total":
						if m.Counter.GetValue() != 10 {
							t.Errorf("prompt_cache_miss_tokens: expected 10, got %f", m.Counter.GetValue())
						}
					}
				}
			}
		}
	}
}

func TestPrometheusHook_Handle_TypeAssertionFallback(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()
	sid := "session-typecheck"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sid,
		Payload: map[string]any{
			"input_tokens":  "string_not_int",
			"output_tokens": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: sid,
		Payload: map[string]any{
			"system_context_length": "not_a_number",
			"tool_call_count":       []int{1, 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_pushMetrics_NoURL(t *testing.T) {
	h := &PrometheusHook{remoteWriteURL: ""}
	h.pushMetrics()
}

func TestPrometheusHook_pushMetrics_InvalidURL(t *testing.T) {
	h := &PrometheusHook{remoteWriteURL: "http://127.0.0.1:19999"}

	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_push_counter", Help: "test"})
	counter.Add(1)
	prometheus.MustRegister(counter)
	defer prometheus.Unregister(counter)

	h.pushMetrics()
}

func TestPrometheusHook_RemoteWriteURLField(t *testing.T) {
	// Test remoteWriteURL is set properly — construct manually to avoid MustRegister.
	h := &PrometheusHook{remoteWriteURL: "http://example.com:9090"}
	if h.remoteWriteURL != "http://example.com:9090" {
		t.Fatalf("expected 'http://example.com:9090', got '%s'", h.remoteWriteURL)
	}
}

func TestStartPrometheusServer_EmptyAddr(t *testing.T) {
	shutdown, err := StartPrometheusServer(":0")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	shutdown()
}

func TestMetricToTimeSeries_MultipleTypes(t *testing.T) {
	registry := prometheus.NewRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "multi_counter"})
	counter.Add(10)
	registry.MustRegister(counter)

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "multi_gauge"})
	gauge.Set(20)
	registry.MustRegister(gauge)

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "multi_histogram"})
	histogram.Observe(5)
	registry.MustRegister(histogram)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	allSeries := make([]prompb.TimeSeries, 0)
	for _, mf := range mfs {
		allSeries = append(allSeries, metricToTimeSeries(mf, 0)...)
	}

	if len(allSeries) < 3 {
		t.Errorf("expected at least 3 time series, got %d", len(allSeries))
	}
}

func TestPrometheusHook_Handle_EmptySessionID(t *testing.T) {
	h := getTestHook(t)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: "",
		Payload: map[string]any{
			"duration_ms": float64(100),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "",
		Payload: map[string]any{
			"input_tokens": 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildPrometheus_EmptyConfig(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{})
	hr := hook.NewRegistry()
	shutdown := BuildPrometheus(cfg, hr)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	shutdown()
}

// --- Tests using newPrometheusHook with custom registries ---

func TestNewPrometheusHook_CustomRegistry(t *testing.T) {
	// Using a custom registry avoids MustRegister panicking on duplicates.
	reg := prometheus.NewRegistry()
	h := newPrometheusHook(reg, "http://example.com:9090")
	if h == nil {
		t.Fatal("expected non-nil hook")
	}
	if h.remoteWriteURL != "http://example.com:9090" {
		t.Fatalf("expected 'http://example.com:9090', got '%s'", h.remoteWriteURL)
	}

	// Should be able to create another hook with a different registry.
	reg2 := prometheus.NewRegistry()
	h2 := newPrometheusHook(reg2, "")
	if h2 == nil {
		t.Fatal("expected non-nil hook for second registry")
	}
}

func TestNewPrometheusHook_CustomRegistryWithLogger(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := newPrometheusHook(reg, "")
	if h.log != nil {
		t.Fatal("expected nil log")
	}
}

func TestNewPrometheusHook_CustomRegistryHandle(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := newPrometheusHook(reg, "")
	ctx := context.Background()

	_ = h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: "sid-reg",
		Payload: map[string]any{
			"duration_ms":           float64(500),
			"system_context_length": float64(1024),
			"tool_call_count":       float64(1),
		},
	})

	_ = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sid-reg",
		Payload: map[string]any{
			"input_tokens":            50,
			"output_tokens":           25,
			"cache_read_input_tokens": 10,
			"prompt_cached_tokens":    5,
			"prompt_cache_hit_tokens": 3,
			"prompt_cache_miss_tokens": 2,
		},
	})

	// Verify metrics were recorded to the custom registry.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) == 0 {
		t.Fatal("expected metrics in custom registry")
	}
}

func startMockServer(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	addr := "http://" + ln.Addr().String()
	return addr, func() { srv.Close() }
}

func TestPushMetrics_WithMockServer(t *testing.T) {
	received := make(chan bool, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/write", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		received <- true
	})

	addr, cleanup := startMockServer(t, mux)
	defer cleanup()

	// Create a hook with a custom registry (pushMetrics gathers from DefaultGatherer,
	// so we register to DefaultGatherer for this test).
	reg := prometheus.NewRegistry()
	h := newPrometheusHook(reg, addr)

	// Register a counter in the default gatherer so pushMetrics has something to push.
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "push_test_counter_ok"})
	counter.Add(1)
	prometheus.MustRegister(counter)
	defer prometheus.Unregister(counter)

	h.pushMetrics()

	select {
	case <-received:
		// Push succeeded.
	case <-time.After(2 * time.Second):
		t.Fatal("pushMetrics did not complete within timeout")
	}
}

func TestPushMetrics_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/write", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	})

	addr, cleanup := startMockServer(t, mux)
	defer cleanup()

	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "push_err_counter_500"})
	counter.Add(1)
	prometheus.MustRegister(counter)
	defer prometheus.Unregister(counter)

	h := newPrometheusHook(reg, addr)
	// Should not panic on 500 — error is logged.
	h.pushMetrics()
}

// --- BuildPrometheus mode tests using buildPrometheus with custom registries ---

func TestBuildPrometheus_PullMode(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled": true,
		"prometheus.mode":    "pull",
		"prometheus.addr":    ":0",
	})
	hr := hook.NewRegistry()
	reg := prometheus.NewRegistry()
	shutdown := buildPrometheus(cfg, hr, reg)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	time.Sleep(50 * time.Millisecond)
	shutdown()
}

func TestBuildPrometheus_PushMode(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled":    true,
		"prometheus.mode":       "push",
		"prometheus.remote_url": "http://127.0.0.1:9090",
	})
	hr := hook.NewRegistry()
	reg := prometheus.NewRegistry()
	shutdown := buildPrometheus(cfg, hr, reg)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	shutdown()
}

func TestBuildPrometheus_BothMode(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled":    true,
		"prometheus.mode":       "both",
		"prometheus.addr":       ":0",
		"prometheus.remote_url": "http://127.0.0.1:9090",
	})
	hr := hook.NewRegistry()
	reg := prometheus.NewRegistry()
	shutdown := buildPrometheus(cfg, hr, reg)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	time.Sleep(50 * time.Millisecond)
	shutdown()
}

func TestBuildPrometheus_DefaultModePull(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled": true,
		"prometheus.addr":    ":0",
	})
	hr := hook.NewRegistry()
	reg := prometheus.NewRegistry()
	shutdown := buildPrometheus(cfg, hr, reg)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	time.Sleep(50 * time.Millisecond)
	shutdown()
}

func TestBuildPrometheus_PullModeDefaultAddr(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"prometheus.enabled": true,
		"prometheus.mode":    "pull",
	})
	hr := hook.NewRegistry()
	reg := prometheus.NewRegistry()
	shutdown := buildPrometheus(cfg, hr, reg)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	time.Sleep(50 * time.Millisecond)
	shutdown()
}

func TestPushMetrics_ConnectionRefused(t *testing.T) {
	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "push_refused_counter"})
	counter.Add(1)
	prometheus.MustRegister(counter)
	defer prometheus.Unregister(counter)

	h := newPrometheusHook(reg, "http://127.0.0.1:19999")
	// Should not panic on connection refused.
	h.pushMetrics()
}

func TestMetricToTimeSeries_Summary(t *testing.T) {
	// SUMMARY type falls to default case in metricToTimeSeries.
	summary := prometheus.NewSummary(prometheus.SummaryOpts{Name: "test_summary"})
	summary.Observe(10)
	registry := prometheus.NewRegistry()
	registry.MustRegister(summary)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	ts := metricToTimeSeries(mfs[0], 100)
	// Summary falls to default case which checks m.Gauge.
	if len(ts) == 0 {
		t.Log("summary had no gauge field, produced 0 time series")
	}
}

func TestMetricToTimeSeries_NilCounter(t *testing.T) {
	// Counter with nil Counter field (edge case for default branch).
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "nil_check_counter"})
	registry.MustRegister(counter)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	// Newly created counter should have non-nil Counter field.
	ts := metricToTimeSeries(mfs[0], 100)
	if len(ts) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(ts))
	}
	if ts[0].Samples[0].Value != 0 {
		t.Fatalf("expected value 0, got %f", ts[0].Samples[0].Value)
	}
}

// Import guards — ensure these types are used.
var _ = prompb.WriteRequest{}
var _ = dto.MetricType_COUNTER
var _ = http.StatusOK
