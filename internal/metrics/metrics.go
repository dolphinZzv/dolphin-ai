// Package metrics provides lightweight instrumentation with Prometheus exposition format.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Type constants for Prometheus metric family type labels.
const (
	TypeCounter   = "counter"
	TypeGauge     = "gauge"
	TypeHistogram = "histogram"
)

// Registry holds all registered metrics and can render them in Prometheus format.
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// NewRegistry creates a new metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

// ---- Counter ----

// Counter is a monotonically increasing counter with optional labels.
type Counter struct {
	name   string
	help   string
	labels map[string]string // static labels (e.g. provider, model)
	value  atomic.Int64
}

// Add increments the counter by n.
func (c *Counter) Add(n int64) {
	c.value.Add(n)
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Value returns the current count.
func (c *Counter) Value() int64 {
	return c.value.Load()
}

// NewCounter registers and returns a counter.
func (r *Registry) NewCounter(name, help string, labels map[string]string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &Counter{name: name, help: help, labels: labels}
	r.counters[name] = c
	return c
}

// Counter retrieves an existing counter by name.
func (r *Registry) Counter(name string) *Counter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.counters[name]
}

// ---- Gauge ----

// Gauge is a point-in-time value with optional labels.
type Gauge struct {
	name   string
	help   string
	labels map[string]string
	value  atomic.Int64
}

// Set sets the gauge to n.
func (g *Gauge) Set(n int64) {
	g.value.Store(n)
}

// Add adds n to the gauge (can be negative).
func (g *Gauge) Add(n int64) {
	g.value.Add(n)
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return g.value.Load()
}

// NewGauge registers and returns a gauge.
func (r *Registry) NewGauge(name, help string, labels map[string]string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := &Gauge{name: name, help: help, labels: labels}
	r.gauges[name] = g
	return g
}

// Gauge retrieves an existing gauge by name.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gauges[name]
}

// ---- Histogram ----

// Histogram tracks value distributions using Prometheus-style buckets.
type Histogram struct {
	name    string
	help    string
	labels  map[string]string
	bounds  []float64 // bucket upper bounds
	buckets []atomic.Int64
	count   atomic.Int64
	sum     atomic.Int64 // nanoseconds
	mu      sync.Mutex
}

// Observe records a value (in seconds, converted to float64).
func (h *Histogram) Observe(seconds float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count.Add(1)
	h.sum.Add(int64(seconds * 1e9))
	for i, bound := range h.bounds {
		if seconds <= bound {
			h.buckets[i].Add(1)
		}
	}
}

// NewHistogram registers and returns a histogram with the given bucket bounds.
// Default buckets (seconds) are used if bounds is nil: {.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
func (r *Registry) NewHistogram(name, help string, labels map[string]string, bounds []float64) *Histogram {
	if bounds == nil {
		bounds = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	buckets := make([]atomic.Int64, len(bounds))
	h := &Histogram{name: name, help: help, labels: labels, bounds: bounds, buckets: buckets}
	r.histograms[name] = h
	return h
}

// Histogram retrieves an existing histogram by name.
func (r *Registry) Histogram(name string) *Histogram {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.histograms[name]
}

// ---- Prometheus Exposition ----

// Render returns all metrics in Prometheus text format (content-type: text/plain; version=0.0.4).
func (r *Registry) Render() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var b strings.Builder

	// Collect names and sort for deterministic output
	cNames := mapKeys(r.counters)
	gNames := mapKeys(r.gauges)
	hNames := mapKeys(r.histograms)

	for _, name := range cNames {
		c := r.counters[name]
		writeMetric(&b, TypeCounter, c.name, c.help, c.labels, float64(c.Value()), 0, 0)
	}

	for _, name := range gNames {
		g := r.gauges[name]
		writeMetric(&b, TypeGauge, g.name, g.help, g.labels, float64(g.Value()), 0, 0)
	}

	for _, name := range hNames {
		h := r.histograms[name]
		total := h.count.Load()
		writeMetric(&b, TypeHistogram, h.name, h.help, h.labels, 0, 0, 0)
		for i, bound := range h.bounds {
			le := fmt.Sprintf("%g", bound)
			if bound == float64(int64(bound)) {
				le = fmt.Sprintf("%d", int64(bound))
			}
			writeMetric(&b, TypeHistogram, h.name+"_bucket", "", mergeLabels(h.labels, "le", le), float64(h.buckets[i].Load()), 0, 0)
		}
		writeMetric(&b, TypeHistogram, h.name+"_bucket", "", mergeLabels(h.labels, "le", "+Inf"), float64(total), 0, 0)
		writeMetric(&b, TypeHistogram, h.name+"_sum", "", h.labels, float64(h.sum.Load())/1e9, 0, 0)
		writeMetric(&b, TypeHistogram, h.name+"_count", "", h.labels, float64(total), 0, 0)
	}

	return b.String()
}

// RenderHTTP is a convenience wrapper that returns the Prometheus text and an
// HTTP content-type. Useful for http.HandlerFunc.
func (r *Registry) RenderHTTP() (string, string) {
	return r.Render(), "text/plain; version=0.0.4; charset=utf-8"
}

// ---- Helpers ----

func writeMetric(b *strings.Builder, mtype, name, help string, labels map[string]string, value float64, _ int64, _ int64) {
	switch mtype {
	case TypeCounter:
		b.WriteString("# HELP ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(help)
		b.WriteString("\n# TYPE ")
		b.WriteString(name)
		b.WriteString(" counter\n")
	case TypeGauge:
		b.WriteString("# HELP ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(help)
		b.WriteString("\n# TYPE ")
		b.WriteString(name)
		b.WriteString(" gauge\n")
	case TypeHistogram:
		// HELP/TYPE only on the base name, not _bucket/_sum/_count
		if !strings.HasSuffix(name, "_bucket") && !strings.HasSuffix(name, "_sum") && !strings.HasSuffix(name, "_count") {
			b.WriteString("# HELP ")
			b.WriteString(name)
			b.WriteString(" ")
			b.WriteString(help)
			b.WriteString("\n# TYPE ")
			b.WriteString(name)
			b.WriteString(" histogram\n")
		}
	}

	if name, ok := trimName(name); ok {
		b.WriteString(name)
	} else {
		b.WriteString(sanitizeName(name))
	}

	if len(labels) > 0 {
		b.WriteString("{")
		first := true
		for _, k := range sortedKeysMap(labels) {
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString(k)
			b.WriteString("=\"")
			b.WriteString(labels[k])
			b.WriteString("\"")
		}
		b.WriteString("}")
	}

	if isHistogramBucket(name) {
		b.WriteString(" ")
	}
	b.WriteString(formatFloat(value))
	b.WriteString("\n")
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func isHistogramBucket(name string) bool {
	return strings.HasSuffix(name, "_bucket") || strings.HasSuffix(name, "_sum") || strings.HasSuffix(name, "_count")
}

func trimName(name string) (string, bool) {
	return name, true
}

func mergeLabels(base map[string]string, key, value string) map[string]string {
	if base == nil {
		return map[string]string{key: value}
	}
	m := make(map[string]string, len(base)+1)
	for k, v := range base {
		m[k] = v
	}
	m[key] = value
	return m
}

func sanitizeName(name string) string {
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(name)
}

func mapKeys[T any](m map[string]T) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysMap(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- Global Registry ----

var defaultRegistry = NewRegistry()

// Default returns the global metrics registry.
func Default() *Registry { return defaultRegistry }

// Convenience wrappers for the default registry.
func NewCounter(name, help string, labels map[string]string) *Counter {
	return defaultRegistry.NewCounter(name, help, labels)
}
func NewGauge(name, help string, labels map[string]string) *Gauge {
	return defaultRegistry.NewGauge(name, help, labels)
}
func NewHistogram(name, help string, labels map[string]string, bounds []float64) *Histogram {
	return defaultRegistry.NewHistogram(name, help, labels, bounds)
}
func Render() string { return defaultRegistry.Render() }

// ---- Timer helper ----

// Timer is a simple helper that records a duration to a Histogram when stopped.
type Timer struct {
	start time.Time
	h     *Histogram
}

// StartTimer starts a timer for the given histogram.
func StartTimer(h *Histogram) *Timer {
	return &Timer{start: time.Now(), h: h}
}

// Stop records the elapsed time in the histogram.
func (t *Timer) Stop() {
	t.h.Observe(time.Since(t.start).Seconds())
}
