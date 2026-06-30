package agentmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/propagation"

	"dolphin/internal/signal"
	"dolphin/internal/tool"
	"dolphin/internal/transport/a2a"
	"dolphin/internal/types"
)

// AttachServer wires the AgentMesh into the local A2A server: it registers
// the self card (for agents/discover/ping) and the server-side extension
// handlers (tasks/cancel, tasks/get, tools/list, tools/call). The signal bus
// is used by tasks/cancel to interrupt a running turn; the tool registry is
// used by tools/list and tools/call for tool federation.
//
// This is what makes a Dolphin process a full mesh citizen: it can both
// delegate (client) and be delegated-to (server, with cancel/get/tool-fed).
func (m *AgentMesh) AttachServer(srv *a2a.A2A, sb *signal.Bus, reg *tool.Registry) {
	if srv == nil {
		return
	}
	load := func() int { return 0 } // Phase 4: LifecycleManager reports real load
	srv.SetSelfCard(m.card.Capabilities, m.card.ProtoVersion, load)

	srv.RegisterExtHandler("tasks/cancel", func(ctx context.Context, method string, params json.RawMessage, httpReq *http.Request) (any, error) {
		// Extract trace context so cancel is correlated with the original
		// delegation span.
		_ = extractTraceContext(ctx, propagation.HeaderCarrier(httpReq.Header))
		var p struct {
			TaskID    string `json:"task_id"`
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		if p.SessionID != "" && sb != nil {
			sb.Send(p.SessionID, signal.Interrupt)
		}
		return map[string]any{"status": "cancelled", "task_id": p.TaskID}, nil
	})

	srv.RegisterExtHandler("tasks/get", func(ctx context.Context, method string, params json.RawMessage, httpReq *http.Request) (any, error) {
		// Phase 2 downgrade: the receiver executes tasks synchronously, so
		// there is no in-flight task to poll. Return an honest status rather
		// than pretending to support polling.
		return nil, fmt.Errorf("tasks/get not supported in sync execution mode")
	})

	// Tool federation (proto=4): expose local tools to peers.
	if reg != nil {
		srv.RegisterExtHandler("tools/list", func(ctx context.Context, method string, _ json.RawMessage, _ *http.Request) (any, error) {
			defs, err := reg.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]RemoteToolDef, 0, len(defs))
			for _, d := range defs {
				out = append(out, RemoteToolDef{Name: d.Name, Description: d.Description, Schema: d.Schema})
			}
			return out, nil
		})

		srv.RegisterExtHandler("tools/call", func(ctx context.Context, method string, params json.RawMessage, _ *http.Request) (any, error) {
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			res, err := reg.Execute(ctx, types.ToolCall{Name: p.Name, Arguments: string(p.Arguments)})
			if err != nil {
				return RemoteToolResult{Content: err.Error(), IsError: true}, nil
			}
			if res == nil {
				return RemoteToolResult{Content: "no result"}, nil
			}
			return RemoteToolResult{Content: res.Content, IsError: res.IsError}, nil
		})
	}
}
