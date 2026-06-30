package setup

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/agentloop"
	"dolphin/internal/agentmesh"
	"dolphin/internal/transport/a2a"
)

// AgentMeshBootstrapper wires the AgentMesh into the running pipeline.
//
// It runs after Transports (index 120) so the A2A server exists, and after
// Tools/Workflow so the tool registry and workflow engine are available.
// When agents.enabled=false this is a no-op: no mesh is created, no tools are
// registered, and behaviour is identical to pre-upgrade.
type AgentMeshBootstrapper struct{}

func (b *AgentMeshBootstrapper) Name() string { return "agentmesh" }
func (b *AgentMeshBootstrapper) Index() int   { return 121 }

func (b *AgentMeshBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	cfg := agentmesh.LoadAgentConfig(c.Config)
	if !cfg.Enabled {
		return nil
	}

	mesh := agentmesh.NewAgentMesh(cfg, c.EventBus, c.Logger)
	c.AgentMesh = mesh

	// Wire the mesh context injector so delegate_to_agent can read the
	// current session id from the tool-execution context.
	agentloop.MeshCtxInjector = func(ctx context.Context, sessionID string, depth int) context.Context {
		ctx = agentmesh.WithParentSession(ctx, sessionID)
		ctx = agentmesh.WithDelegationDepth(ctx, depth)
		return ctx
	}

	// Find the A2A transport instance and attach server-side handlers
	// (agents/discover, agents/ping, tasks/cancel, tools/list, tools/call).
	for _, tio := range c.Transports {
		if srv, ok := tio.(*a2a.A2A); ok {
			mesh.AttachServer(srv, c.SignalBus, c.ToolReg)
			break
		}
	}

	// Expose the local tool registry so MountTools can register remote tools.
	mesh.SetToolRegistry(c.ToolReg)

	// Register the LLM-facing tools.
	agentmesh.RegisterDelegateTool(c.ToolReg, mesh)
	if cfg.Spawner.Enabled {
		sp := agentmesh.NewSpawner(cfg.Spawner.Bin, c.Config, cfg.Spawner.MaxSpawned, c.Logger)
		mesh.SetSpawner(sp)
		agentmesh.RegisterSpawnTool(c.ToolReg, mesh)
	}

	// Wire into workflow so steps with `agent:` delegate.
	if c.WorkflowEngine != nil {
		c.WorkflowEngine.SetDelegator(agentmesh.NewWorkflowDelegator(mesh))
	}

	c.Logger.Info("agent mesh enabled",
		zap.String("name", cfg.Name),
		zap.Int("peers", len(mesh.ListAgents())),
	)
	return nil
}
