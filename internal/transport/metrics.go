package transport

import "dolphin/internal/metrics"

// Transport-level metrics (shared across all transport implementations).
var (
	msgsReceived      = metrics.NewCounter("transport_messages_received_total", "Total messages received across all transports", map[string]string{})
	msgsSent          = metrics.NewCounter("transport_messages_sent_total", "Total messages sent across all transports", map[string]string{})
	activeConnections = metrics.NewGauge("transport_connections_active", "Current number of active transport connections", map[string]string{})
)
