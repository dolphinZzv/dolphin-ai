package metrics

import (
	"strings"
	"testing"
)

func TestCounter(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("test_counter", "test help", map[string]string{"env": "test"})
	if c.Value() != 0 {
		t.Errorf("expected 0, got %d", c.Value())
	}
	c.Inc()
	if c.Value() != 1 {
		t.Errorf("expected 1, got %d", c.Value())
	}
	c.Add(5)
	if c.Value() != 6 {
		t.Errorf("expected 6, got %d", c.Value())
	}
}

func TestCounterViaDefaultRegistry(t *testing.T) {
	_ = NewCounter("test_default_counter", "default help", nil)
}

func TestGauge(t *testing.T) {
	r := NewRegistry()
	g := r.NewGauge("test_gauge", "test help", nil)
	g.Set(42)
	if g.Value() != 42 {
		t.Errorf("expected 42, got %d", g.Value())
	}
	g.Add(-10)
	if g.Value() != 32 {
		t.Errorf("expected 32, got %d", g.Value())
	}
}

func TestHistogram(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("test_histogram", "test help", map[string]string{"service": "test"}, nil)
	h.Observe(0.1)
	h.Observe(0.5)
	h.Observe(1.0)
}

func TestRenderCounter(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("http_requests_total", "Total HTTP requests", map[string]string{"method": "GET"}).Inc()

	output := r.Render()
	if !strings.Contains(output, "http_requests_total") {
		t.Errorf("expected metric name in output, got: %s", output)
	}
	if !strings.Contains(output, "counter") {
		t.Errorf("expected TYPE counter in output")
	}
	if !strings.Contains(output, `method="GET"`) {
		t.Errorf("expected labels in output")
	}
}

func TestRenderGauge(t *testing.T) {
	r := NewRegistry()
	r.NewGauge("pool_size", "Pool size", nil).Set(3)

	output := r.Render()
	if !strings.Contains(output, "pool_size") {
		t.Errorf("expected metric name in output")
	}
	if !strings.Contains(output, "gauge") {
		t.Errorf("expected TYPE gauge in output")
	}
}

func TestRenderHistogram(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("request_duration", "Request duration", nil, []float64{0.1, 0.5, 1.0})
	h.Observe(0.2)
	h.Observe(0.8)

	output := r.Render()
	if !strings.Contains(output, "request_duration_bucket") {
		t.Errorf("expected histogram buckets in output, got: %s", output)
	}
	if !strings.Contains(output, `le="+Inf"`) {
		t.Errorf("expected +Inf bucket in output")
	}
	if !strings.Contains(output, "request_duration_sum") {
		t.Errorf("expected histogram sum in output")
	}
	if !strings.Contains(output, "request_duration_count") {
		t.Errorf("expected histogram count in output")
	}
}

func TestRenderEmpty(t *testing.T) {
	r := NewRegistry()
	output := r.Render()
	if output != "" {
		t.Errorf("expected empty output, got: %s", output)
	}
}

func TestTimer(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("timer_test", "test", nil, nil)
	timer := StartTimer(h)
	timer.Stop()
	// Should not panic, value should be > 0
	if h.count.Load() != 1 {
		t.Errorf("expected 1 observation, got %d", h.count.Load())
	}
}

func TestRenderHTTP(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("test", "test", nil).Inc()
	body, ctype := r.RenderHTTP()
	if ctype != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("unexpected content type: %s", ctype)
	}
	if !strings.Contains(body, "test") {
		t.Errorf("expected test metric in body")
	}
}

func TestHistogramDefaultBuckets(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("default_buckets", "test", nil, nil)
	if len(h.bounds) != 11 {
		t.Errorf("expected 11 default buckets, got %d", len(h.bounds))
	}
}

func TestMultipleLabels(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("multi_label", "test", map[string]string{"app": "test", "env": "prod"})
	c.Inc()

	output := r.Render()
	if !strings.Contains(output, `app="test"`) {
		t.Errorf("expected app label in output")
	}
	if !strings.Contains(output, `env="prod"`) {
		t.Errorf("expected env label in output")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("concurrent", "test", nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			c.Inc()
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			c.Inc()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	if c.Value() != 200 {
		t.Errorf("expected 200, got %d", c.Value())
	}
}
