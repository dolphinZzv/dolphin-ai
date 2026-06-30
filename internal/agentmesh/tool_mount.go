package agentmesh

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/types"
)

// ToolMount mounts a remote agent's tools into the local tool.Registry as a
// tool.Executor. Each remote tool is exposed locally as
// "<agentName>/<toolName>" to avoid name collisions.
//
// Phase 4: the tool list is cached with a TTL (60s). Execute forwards to the
// peer's tools/call.
type ToolMount struct {
	agentName string
	client    *A2AClient
	logger    *zap.Logger

	mu       sync.RWMutex
	cache    []types.ToolDef
	cachedAt time.Time
	ttl      time.Duration
}

// NewToolMount builds a ToolMount for the given agent. The client must already
// be negotiated (proto >= 4 required for tools/list, checked lazily on List).
func NewToolMount(agentName string, client *A2AClient, logger *zap.Logger) *ToolMount {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ToolMount{
		agentName: agentName,
		client:    client,
		logger:    logger,
		ttl:       60 * time.Second,
	}
}

// List returns the remote agent's tools, prefixed with the agent name. Cached
// for ttl. If the peer does not support tools/list (proto<4), returns an
// empty list (MountTools skips mounting in that case).
func (m *ToolMount) List(ctx context.Context) ([]types.ToolDef, error) {
	m.mu.RLock()
	if m.cachedAt.IsZero() == false && time.Since(m.cachedAt) < m.ttl && len(m.cache) > 0 {
		defer m.mu.RUnlock()
		return m.cache, nil
	}
	m.mu.RUnlock()

	remote, err := m.client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]types.ToolDef, 0, len(remote))
	for _, r := range remote {
		out = append(out, types.ToolDef{
			Name:        m.agentName + "/" + r.Name,
			Description: fmt.Sprintf("[from %s] %s", m.agentName, r.Description),
			Schema:      r.Schema,
		})
	}

	m.mu.Lock()
	m.cache = out
	m.cachedAt = time.Now()
	m.mu.Unlock()
	return out, nil
}

// Execute forwards a prefixed tool call to the peer's tools/call.
func (m *ToolMount) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	prefix := m.agentName + "/"
	toolName := strings.TrimPrefix(call.Name, prefix)
	if toolName == call.Name {
		// not our prefix → not ours to handle
		return &types.ToolResult{Content: "tool not mounted: " + call.Name, IsError: true}, nil
	}
	res, err := m.client.CallTool(ctx, toolName, call.Arguments)
	if err != nil {
		return &types.ToolResult{ToolCallID: call.ID, Content: err.Error(), IsError: true}, nil
	}
	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    res.Content,
		IsError:    res.IsError,
	}, nil
}

// MountTools fetches the remote agent's tools and registers them as a named
// source on the local registry. Returns ErrProtoTooOld (wrapped) if the peer
// does not support tool federation.
func (m *AgentMesh) MountTools(ctx context.Context, agentName string) error {
	c, ok := m.registry.Get(agentName)
	if !ok {
		return &DelegateError{Code: ErrAgentNotFound, Message: "agent not in registry", Agent: agentName}
	}
	client, err := m.clientFor(ctx, c.Addr)
	if err != nil {
		return err
	}
	if client.NegotiatedProto() < 4 {
		m.logger.Warn("agentmesh: agent too old for tool federation, skipping mount",
			zap.String("agent", agentName),
		)
		return &DelegateError{Code: ErrInternal, Message: "peer proto < 4, tool federation unavailable", Agent: agentName}
	}
	mount := NewToolMount(agentName, client, m.logger)
	// Prime the cache so a proto/availability error surfaces now.
	if _, err := mount.List(ctx); err != nil {
		return err
	}
	// Register as a named source. The tool.Registry is injected by the caller
	// via SetToolRegistry; if absent, mount is a no-op.
	if m.toolReg != nil {
		m.toolReg.AddNamedSource("mount:"+agentName, mount)
	}
	return nil
}
