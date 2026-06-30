package agentmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// RegisterDelegateTool registers the `delegate_to_agent` builtin tool on the
// given tool registry. The tool lets the LLM delegate a task to another agent.
//
// If mesh is nil or disabled, the tool is still registered but returns an
// error when invoked, so the LLM sees a clear "disabled" message rather than
// a missing tool.
func RegisterDelegateTool(reg *tool.Registry, mesh *AgentMesh) {
	const name = "delegate_to_agent"
	const desc = "Delegate a task to another AI agent. Use when a sub-task needs specialized capabilities (code review, security scan, data analysis) or parallel processing."
	schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "agent": {"type": "string", "description": "target agent name, or omit to let the router match by capabilities"},
    "task": {"type": "string", "description": "the task to delegate; be specific"},
    "capabilities": {"type": "array", "items": {"type": "string"}, "description": "required capability tags; used for routing when agent is omitted"},
    "mode": {"type": "string", "enum": ["sync", "async"], "description": "sync=wait for result (default), async=return task id immediately"},
    "timeout": {"type": "string", "description": "timeout e.g. '30s', '5m'. default '10m'"},
    "context": {"type": "object", "description": "shared context", "properties": {
      "messages": {"type": "array", "description": "relevant history messages"},
      "files": {"type": "array", "items": {"type": "string"}, "description": "file paths to share (small files inlined)"}
    }}
  },
  "required": ["task"]
}`)

	reg.RegisterBuiltin(name, desc, schema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return executeDelegate(ctx, mesh, args)
	})
}

// delegateArgs is the LLM-facing argument shape.
type delegateArgs struct {
	Agent        string                  `json:"agent"`
	Task         string                  `json:"task"`
	Capabilities []string                `json:"capabilities"`
	Mode         string                  `json:"mode"`
	Timeout      string                  `json:"timeout"`
	Context      *delegateContextArgs    `json:"context"`
}

type delegateContextArgs struct {
	Messages []ContextMessage `json:"messages"`
	Files    []string         `json:"files"`
}

func executeDelegate(ctx context.Context, mesh *AgentMesh, raw json.RawMessage) (*types.ToolResult, error) {
	if mesh == nil || !mesh.Enabled() {
		return &types.ToolResult{Content: "agent mesh is disabled; cannot delegate", IsError: true}, nil
	}
	var args delegateArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Task) == "" {
		return &types.ToolResult{Content: "task is required", IsError: true}, nil
	}

	payload := DelegatePayload{
		Task:             args.Task,
		ParentSessionID:  sessionIDFromCtx(ctx),
		DelegationDepth:  depthFromCtx(ctx) + 1,
		PreferredAgent:   args.Agent,
		RequiredCapabilities: args.Capabilities,
		Timeout:          args.Timeout,
		ReplyMode:        ReplySync,
	}
	if args.Mode == "async" {
		payload.ReplyMode = ReplyAsync
	}
	if args.Context != nil {
		payload.Context.Messages = args.Context.Messages
		payload.Context.Files = loadFiles(args.Context.Files)
	}

	result, err := mesh.Delegate(ctx, payload)
	if err != nil {
		return &types.ToolResult{Content: fmt.Sprintf("delegate failed: %v", err), IsError: true}, nil
	}
	if result == nil {
		return &types.ToolResult{Content: "delegate returned no result", IsError: true}, nil
	}
	content := result.Content
	if content == "" && result.Error != nil {
		content = result.Error.Error()
	}
	return &types.ToolResult{Content: content}, nil
}

// loadFiles reads small (<100KB) files inline. Large or unreadable files are
// skipped with a note rather than failing the whole delegation.
func loadFiles(paths []string) []SharedFile {
	out := make([]SharedFile, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			out = append(out, SharedFile{Path: p, Mode: FileReference, Hash: ""})
			continue
		}
		if info.Size() > 100*1024 {
			// large file → reference mode (caller needs shared fs)
			out = append(out, SharedFile{Path: p, Mode: FileReference})
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, SharedFile{Path: p, Mode: FileInline, Content: string(data)})
	}
	return out
}
