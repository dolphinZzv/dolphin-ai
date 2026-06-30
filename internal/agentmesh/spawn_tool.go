package agentmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// RegisterSpawnTool registers the `spawn_agent` builtin tool. It lets the LLM
// create a temporary child agent for a specific task; the child is killed
// after the task completes.
//
// Only registered when mesh.cfg.Spawner.Enabled is true and a Spawner is
// attached.
func RegisterSpawnTool(reg *tool.Registry, mesh *AgentMesh) {
	if mesh == nil || mesh.Spawner() == nil {
		return
	}
	const name = "spawn_agent"
	const desc = "Spawn a temporary child agent for a specialized task and delegate to it. The child is auto-destroyed when the task finishes. Use for parallel sub-tasks or when no existing agent has the needed capability."
	schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": {"type": "string", "description": "the task to delegate to the spawned child"},
    "capabilities": {"type": "array", "items": {"type": "string"}, "description": "capability tags for the child agent"},
    "model": {"type": "string", "description": "LLM model for the child (omit to inherit parent)"},
    "timeout": {"type": "string", "description": "spawn readiness timeout, e.g. '10s'"},
    "max_rounds": {"type": "integer", "description": "max LLM rounds for the child"},
    "allowed_tools": {"type": "array", "items": {"type": "string"}, "description": "tool whitelist"},
    "denied_tools": {"type": "array", "items": {"type": "string"}, "description": "tool blacklist (e.g. [\"sh curl *\"])"},
    "name": {"type": "string", "description": "child agent name (auto-generated if omitted)"}
  },
  "required": ["task"]
}`)

	reg.RegisterBuiltin(name, desc, schema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var a struct {
			Task         string   `json:"task"`
			Capabilities []string `json:"capabilities"`
			Model        string   `json:"model"`
			Timeout      string   `json:"timeout"`
			MaxRounds    int      `json:"max_rounds"`
			AllowedTools []string `json:"allowed_tools"`
			DeniedTools  []string `json:"denied_tools"`
			Name         string   `json:"name"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if strings.TrimSpace(a.Task) == "" {
			return &types.ToolResult{Content: "task is required", IsError: true}, nil
		}
		spec := AgentSpec{
			Name:         a.Name,
			Capabilities: a.Capabilities,
			Model:        a.Model,
			MaxRounds:    a.MaxRounds,
			AllowedTools: a.AllowedTools,
			DeniedTools:  a.DeniedTools,
		}
		if a.Timeout != "" {
			if d, err := time.ParseDuration(a.Timeout); err == nil {
				spec.Timeout = d
			}
		}
		result, err := mesh.SpawnAndDelegate(ctx, spec, a.Task)
		if err != nil {
			return &types.ToolResult{Content: fmt.Sprintf("spawn_agent failed: %v", err), IsError: true}, nil
		}
		if result == nil {
			return &types.ToolResult{Content: "spawn_agent returned no result", IsError: true}, nil
		}
		return &types.ToolResult{Content: result.Content}, nil
	})
}
