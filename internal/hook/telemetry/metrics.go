package telemetry

import (
	"context"
	"time"

	"dolphin/internal/agent"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	meter        metric.Meter
	llmReqCount  metric.Int64Counter
	llmErrCount  metric.Int64Counter
	llmLatency   metric.Float64Histogram
	llmInTokens  metric.Int64Counter
	llmOutTokens metric.Int64Counter

	toolCallCount metric.Int64Counter
	toolErrCount  metric.Int64Counter
	toolLatency   metric.Float64Histogram

	poolSizeGauge   metric.Int64Gauge
	activeAgGauge   metric.Int64Gauge
	taskDispatchCnt metric.Int64Counter
	taskCompleteCnt metric.Int64Counter
	taskFailCnt     metric.Int64Counter

	sessionGauge metric.Int64Gauge
	turnCounter  metric.Int64Counter

	compressCount metric.Int64Counter
	failoverCount metric.Int64Counter

	schedulerTaskCount  metric.Int64Counter
	schedulerTaskErrCnt metric.Int64Counter
	schedulerTaskLat    metric.Float64Histogram

	transportRxCount metric.Int64Counter
	transportTxCount metric.Int64Counter
	transportConn    metric.Int64Gauge
)

func initMetrics() {
	meter = otel.Meter("dolphin")

	llmReqCount, _ = meter.Int64Counter(
		"gen_ai.client.requests",
		metric.WithDescription("Number of LLM requests"),
		metric.WithUnit("{request}"),
	)
	llmErrCount, _ = meter.Int64Counter(
		"gen_ai.client.errors",
		metric.WithDescription("Number of LLM errors"),
		metric.WithUnit("{error}"),
	)
	llmLatency, _ = meter.Float64Histogram(
		"gen_ai.client.operation.duration",
		metric.WithDescription("LLM request duration"),
		metric.WithUnit("s"),
	)
	llmInTokens, _ = meter.Int64Counter(
		"gen_ai.client.token.usage.input",
		metric.WithDescription("Input tokens consumed"),
		metric.WithUnit("{token}"),
	)
	llmOutTokens, _ = meter.Int64Counter(
		"gen_ai.client.token.usage.output",
		metric.WithDescription("Output tokens produced"),
		metric.WithUnit("{token}"),
	)

	toolCallCount, _ = meter.Int64Counter(
		"mcp.tool.calls",
		metric.WithDescription("Number of MCP tool calls"),
		metric.WithUnit("{call}"),
	)
	toolErrCount, _ = meter.Int64Counter(
		"mcp.tool.errors",
		metric.WithDescription("Number of MCP tool errors"),
		metric.WithUnit("{error}"),
	)
	toolLatency, _ = meter.Float64Histogram(
		"mcp.tool.duration",
		metric.WithDescription("MCP tool execution duration"),
		metric.WithUnit("s"),
	)

	poolSizeGauge, _ = meter.Int64Gauge(
		"agent.pool.size",
		metric.WithDescription("Current agent pool size"),
		metric.WithUnit("{agent}"),
	)
	activeAgGauge, _ = meter.Int64Gauge(
		"agent.pool.active",
		metric.WithDescription("Currently busy agents"),
		metric.WithUnit("{agent}"),
	)
	taskDispatchCnt, _ = meter.Int64Counter(
		"agent.task.dispatched",
		metric.WithDescription("Tasks dispatched to agents"),
		metric.WithUnit("{task}"),
	)
	taskCompleteCnt, _ = meter.Int64Counter(
		"agent.task.completed",
		metric.WithDescription("Tasks completed by agents"),
		metric.WithUnit("{task}"),
	)
	taskFailCnt, _ = meter.Int64Counter(
		"agent.task.failed",
		metric.WithDescription("Tasks failed by agents"),
		metric.WithUnit("{task}"),
	)

	sessionGauge, _ = meter.Int64Gauge(
		"session.active",
		metric.WithDescription("Active sessions"),
		metric.WithUnit("{session}"),
	)
	turnCounter, _ = meter.Int64Counter(
		"session.turns",
		metric.WithDescription("Total turns processed"),
		metric.WithUnit("{turn}"),
	)

	compressCount, _ = meter.Int64Counter(
		"context.compression.count",
		metric.WithDescription("Context compression events"),
		metric.WithUnit("{compression}"),
	)
	failoverCount, _ = meter.Int64Counter(
		"llm.failover.count",
		metric.WithDescription("Provider failover events"),
		metric.WithUnit("{failover}"),
	)

	schedulerTaskCount, _ = meter.Int64Counter(
		"scheduler.task.count",
		metric.WithDescription("Number of scheduled tasks executed"),
		metric.WithUnit("{task}"),
	)
	schedulerTaskErrCnt, _ = meter.Int64Counter(
		"scheduler.task.errors",
		metric.WithDescription("Number of scheduled task errors"),
		metric.WithUnit("{error}"),
	)
	schedulerTaskLat, _ = meter.Float64Histogram(
		"scheduler.task.duration",
		metric.WithDescription("Scheduled task execution duration"),
		metric.WithUnit("s"),
	)

	transportRxCount, _ = meter.Int64Counter(
		"transport.messages.received",
		metric.WithDescription("Number of messages received via transport"),
		metric.WithUnit("{message}"),
	)
	transportTxCount, _ = meter.Int64Counter(
		"transport.messages.sent",
		metric.WithDescription("Number of messages sent via transport"),
		metric.WithUnit("{message}"),
	)
	transportConn, _ = meter.Int64Gauge(
		"transport.connections.active",
		metric.WithDescription("Active transport connections"),
		metric.WithUnit("{connection}"),
	)

	registerCallbacks()
}

// registerCallbacks wires OTel metric recorders and span callbacks into the
// agent package's TelemetryCallbacks to avoid circular imports.
func registerCallbacks() {
	agent.TelemetryCallbacks.OnCompression = func() { RecordCompression(context.Background()) }
	agent.TelemetryCallbacks.OnFailover = func(from, to string) { RecordFailover(context.Background(), from, to) }
	agent.TelemetryCallbacks.OnPoolSize = func(n int64) { RecordPoolSize(n) }
	agent.TelemetryCallbacks.OnActiveAgents = func(n int64) { RecordActiveAgents(n) }
	agent.TelemetryCallbacks.OnTaskDispatched = func(name string) { RecordTaskDispatched(context.Background(), name) }
	agent.TelemetryCallbacks.OnTaskCompleted = func(name string, success bool) {
		RecordTaskCompleted(context.Background(), name, success)
	}

	// Span callbacks
	agent.TelemetryCallbacks.OnCompressionSpan = func(ctx context.Context, sessionID string, turn int) func() {
		var parentCtx context.Context
		if v, ok := spanStore.Load(turnKey(sessionID, turn)); ok {
			parentCtx = childContext(v.(trace.Span))
		} else {
			parentCtx = ctx
		}
		tr := Tracer(tracerName)
		_, span := tr.Start(parentCtx, "context.compression",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		span.SetAttributes(
			attribute.String("session.id", sessionID),
			attribute.Int("turn.number", turn),
		)
		return func() {
			span.SetStatus(codes.Ok, "")
			span.End()
		}
	}

	agent.TelemetryCallbacks.OnDispatchSpan = func(ctx context.Context, agentName string) func() {
		tr := Tracer(tracerName)
		_, span := tr.Start(ctx, "agent.dispatch",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		span.SetAttributes(attribute.String("agent.name", agentName))
		return func() {
			span.SetStatus(codes.Ok, "")
			span.End()
		}
	}

	agent.TelemetryCallbacks.OnTaskSpan = func(ctx context.Context, agentName, taskID string) func() {
		tr := Tracer(tracerName)
		_, span := tr.Start(ctx, "agent.task",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		span.SetAttributes(
			attribute.String("agent.name", agentName),
			attribute.String("task.id", taskID),
		)
		return func() {
			span.SetStatus(codes.Ok, "")
			span.End()
		}
	}

	agent.TelemetryCallbacks.OnBuildinSpan = func(ctx context.Context, agentName, triggerEvent string) func() {
		tr := Tracer(tracerName)
		_, span := tr.Start(ctx, "buildin.action",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		span.SetAttributes(
			attribute.String("agent.name", agentName),
			attribute.String("trigger.event", triggerEvent),
		)
		return func() {
			span.SetStatus(codes.Ok, "")
			span.End()
		}
	}
}

// ---- recording helpers (called from agent / provider code) ----

func RecordLLMRequest(ctx context.Context, provider string) {
	if llmReqCount == nil {
		return
	}
	llmReqCount.Add(ctx, 1, metric.WithAttributes(attribute.String("gen_ai.system", provider)))
}

func RecordLLMError(ctx context.Context, provider string) {
	if llmErrCount == nil {
		return
	}
	llmErrCount.Add(ctx, 1, metric.WithAttributes(attribute.String("gen_ai.system", provider)))
}

func RecordLLMLatency(ctx context.Context, provider string, d time.Duration) {
	if llmLatency == nil {
		return
	}
	llmLatency.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("gen_ai.system", provider)))
}

func RecordLLMTokens(ctx context.Context, provider string, input, output int64) {
	if llmInTokens != nil {
		llmInTokens.Add(ctx, input, metric.WithAttributes(attribute.String("gen_ai.system", provider)))
	}
	if llmOutTokens != nil {
		llmOutTokens.Add(ctx, output, metric.WithAttributes(attribute.String("gen_ai.system", provider)))
	}
}

func RecordToolCall(ctx context.Context, name string) {
	if toolCallCount == nil {
		return
	}
	toolCallCount.Add(ctx, 1, metric.WithAttributes(attribute.String("tool.name", name)))
}

func RecordToolError(ctx context.Context, name string) {
	if toolErrCount == nil {
		return
	}
	toolErrCount.Add(ctx, 1, metric.WithAttributes(attribute.String("tool.name", name)))
}

func RecordToolLatency(ctx context.Context, name string, d time.Duration) {
	if toolLatency == nil {
		return
	}
	toolLatency.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("tool.name", name)))
}

func RecordPoolSize(n int64) {
	if poolSizeGauge == nil {
		return
	}
	poolSizeGauge.Record(context.Background(), n)
}

func RecordActiveAgents(n int64) {
	if activeAgGauge == nil {
		return
	}
	activeAgGauge.Record(context.Background(), n)
}

func RecordTaskDispatched(ctx context.Context, agentName string) {
	if taskDispatchCnt == nil {
		return
	}
	taskDispatchCnt.Add(ctx, 1, metric.WithAttributes(attribute.String("agent.name", agentName)))
}

func RecordTaskCompleted(ctx context.Context, agentName string, success bool) {
	if taskCompleteCnt != nil {
		taskCompleteCnt.Add(ctx, 1, metric.WithAttributes(attribute.String("agent.name", agentName)))
	}
	if !success && taskFailCnt != nil {
		taskFailCnt.Add(ctx, 1, metric.WithAttributes(attribute.String("agent.name", agentName)))
	}
}

func RecordSessionStart() {
	if sessionGauge == nil {
		return
	}
	sessionGauge.Record(context.Background(), 1)
}

func RecordSessionEnd() {
	if sessionGauge == nil {
		return
	}
	sessionGauge.Record(context.Background(), -1)
}

func RecordTurn(ctx context.Context) {
	if turnCounter == nil {
		return
	}
	turnCounter.Add(ctx, 1)
}

func RecordCompression(ctx context.Context) {
	if compressCount == nil {
		return
	}
	compressCount.Add(ctx, 1)
}

func RecordFailover(ctx context.Context, from, to string) {
	if failoverCount == nil {
		return
	}
	failoverCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("from", from),
		attribute.String("to", to),
	))
}

func RecordSchedulerTask(ctx context.Context, name string) {
	if schedulerTaskCount == nil {
		return
	}
	schedulerTaskCount.Add(ctx, 1, metric.WithAttributes(attribute.String("task.name", name)))
}

func RecordSchedulerTaskError(ctx context.Context, name string) {
	if schedulerTaskErrCnt == nil {
		return
	}
	schedulerTaskErrCnt.Add(ctx, 1, metric.WithAttributes(attribute.String("task.name", name)))
}

func RecordSchedulerTaskLatency(ctx context.Context, name string, d time.Duration) {
	if schedulerTaskLat == nil {
		return
	}
	schedulerTaskLat.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("task.name", name)))
}

func RecordTransportRx(ctx context.Context, transportName string) {
	if transportRxCount == nil {
		return
	}
	transportRxCount.Add(ctx, 1, metric.WithAttributes(attribute.String("transport.name", transportName)))
}

func RecordTransportTx(ctx context.Context, transportName string) {
	if transportTxCount == nil {
		return
	}
	transportTxCount.Add(ctx, 1, metric.WithAttributes(attribute.String("transport.name", transportName)))
}

func RecordTransportConnect(ctx context.Context) {
	if transportConn == nil {
		return
	}
	transportConn.Record(ctx, 1)
}

func RecordTransportDisconnect(ctx context.Context) {
	if transportConn == nil {
		return
	}
	transportConn.Record(ctx, -1)
}
