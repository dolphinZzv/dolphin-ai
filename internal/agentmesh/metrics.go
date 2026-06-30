package agentmesh

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the Prometheus collectors for AgentMesh. Registered once on
// the default registerer; AgentMesh records into them on each delegate.
type Metrics struct {
	delegateTotal    *prometheus.CounterVec
	delegateDuration *prometheus.HistogramVec
	delegateDepth    prometheus.Histogram
	childAgents      prometheus.Gauge
	queueDepth       *prometheus.GaugeVec
}

var globalMetrics *Metrics

// initMetrics registers the metrics once on the default registerer. Safe to
// call multiple times (returns the cached set).
func initMetrics() *Metrics {
	if globalMetrics != nil {
		return globalMetrics
	}
	m := &Metrics{
		delegateTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agentmesh_delegate_total",
			Help: "Total delegated tasks, by from/to/status.",
		}, []string{"from", "to", "status"}),
		delegateDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agentmesh_delegate_duration_seconds",
			Help:    "Delegate wall-clock duration in seconds.",
			Buckets: []float64{1, 5, 10, 30, 60, 300, 600},
		}, []string{"from", "to"}),
		delegateDepth: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "agentmesh_delegate_depth",
			Help:    "Delegation depth at which a delegate occurs.",
			Buckets: []float64{1, 2, 3, 4, 5},
		}),
		childAgents: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "agentmesh_child_agents",
			Help: "Number of currently-tracked child agents.",
		}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agentmesh_queue_depth",
			Help: "In-flight async delegations, by agent.",
		}, []string{"agent"}),
	}
	prometheus.MustRegister(m.delegateTotal, m.delegateDuration, m.delegateDepth, m.childAgents, m.queueDepth)
	globalMetrics = m
	return m
}

// recordDelegate records a completed delegation's outcome and duration.
func (m *Metrics) recordDelegate(from, to, status string, durationSeconds float64, depth int) {
	if m == nil {
		return
	}
	m.delegateTotal.WithLabelValues(from, to, status).Inc()
	m.delegateDuration.WithLabelValues(from, to).Observe(durationSeconds)
	m.delegateDepth.Observe(float64(depth))
}
