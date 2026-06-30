package agentmesh

import (
	"context"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "agentmesh"

// delegateSpan wraps an OTel span for a delegation. We use a small wrapper so
// callers can `defer span.End()` without importing OTel in agentmesh.go.
type delegateSpan struct {
	span trace.Span
}

func (s *delegateSpan) End() {
	if s != nil && s.span != nil {
		s.span.End()
	}
}

// startDelegateSpan opens a span tagged with from/to/depth/capabilities.
func (m *AgentMesh) startDelegateSpan(ctx context.Context, payload DelegatePayload, target AgentCard) (context.Context, *delegateSpan) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "agent.delegate",
		trace.WithAttributes(
			attribute.String("agentmesh.from", m.card.Addr),
			attribute.String("agentmesh.to", target.Addr),
			attribute.String("agentmesh.to_name", target.Name),
			attribute.String("agentmesh.parent_session", payload.ParentSessionID),
			attribute.String("agentmesh.capabilities", strings.Join(payload.RequiredCapabilities, ",")),
			attribute.String("agentmesh.depth", strconv.Itoa(payload.DelegationDepth)),
		),
	)
	return ctx, &delegateSpan{span: span}
}

// recordSpanError marks the span as failed.
func recordSpanError(span *delegateSpan, err error) {
	if span == nil || span.span == nil || err == nil {
		return
	}
	span.span.RecordError(err)
	span.span.SetStatus(codes.Error, err.Error())
}

// recordSpanResult tags the span with the outcome.
func recordSpanResult(span *delegateSpan, result *DelegateResult) {
	if span == nil || span.span == nil || result == nil {
		return
	}
	span.span.SetAttributes(
		attribute.String("agentmesh.result.status", string(result.Status)),
	)
}

// injectTraceContext injects the current span's trace context into HTTP
// headers using the global OTel propagator (W3C TraceContext). The peer
// extracts it with extractTraceContext.
func injectTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// extractTraceContext extracts a parent span from HTTP headers. Used by the
// A2A server-side handler to continue the trace across agents.
func extractTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
