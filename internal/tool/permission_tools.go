package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"dolphin/internal/event"
	"dolphin/internal/i18n"
	"dolphin/internal/permission"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

func init() {
	i18n.Register("tool",
		"en", i18n.Dict{
			"request_perm_prompt": "The assistant requests permission to execute [%s].\nReason: %s",
			"request_perm_args":   "\nArguments: %s",
		},
		"zh", i18n.Dict{
			"request_perm_prompt": "助手请求执行工具 [%s]。\n原因: %s",
			"request_perm_args":   "\n参数: %s",
		},
	)
}

// RegisterPermissionTool registers a meta-tool that allows the LLM to proactively
// request user permission for executing a specific tool before actually calling it.
func RegisterPermissionTool(r *Registry, ps *permission.Store, getTransport func(id string) transport.IO) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"tool_name": {
				"type": "string",
				"description": "The name of the tool that needs permission"
			},
			"reason": {
				"type": "string",
				"description": "Why this tool needs to be executed"
			},
			"arguments": {
				"type": "object",
				"description": "The arguments that will be passed to the tool (optional)"
			}
		},
		"required": ["tool_name", "reason"]
	}`)

	r.RegisterBuiltin("request_permission",
		"Proactively request user permission before executing a tool. Call this when you need user approval for an operation that may require explicit consent, then wait for the result before proceeding.",
		schema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				ToolName  string           `json:"tool_name"`
				Reason    string           `json:"reason"`
				Arguments *json.RawMessage `json:"arguments,omitempty"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{
					Content: fmt.Sprintf("invalid args: %s", err.Error()),
					IsError: true,
				}, nil
			}

			if getTransport == nil {
				return &types.ToolResult{
					Content: "permission system not available",
					IsError: true,
				}, nil
			}

			info := transport.GetInfo(ctx)
			if info == nil {
				return &types.ToolResult{
					Content: "no transport context available",
					IsError: true,
				}, nil
			}

			tio := getTransport(info.ID)
			if tio == nil {
				return &types.ToolResult{
					Content: fmt.Sprintf("transport %q not found", info.ID),
					IsError: true,
				}, nil
			}

			prompt := fmt.Sprintf(i18n.T("tool.request_perm_prompt"), req.ToolName, req.Reason)
			if req.Arguments != nil && len(*req.Arguments) > 0 {
				prompt += fmt.Sprintf(i18n.T("tool.request_perm_args"), string(*req.Arguments))
			}

			permResult, err := tio.RequestPermission(ctx, prompt)
			if err != nil {
				return &types.ToolResult{
					Content: fmt.Sprintf("permission request failed: %s", err.Error()),
					IsError: true,
				}, nil
			}

			switch permResult {
			case transport.PermissionAlways:
				if ps != nil {
					argsToSave := json.RawMessage(`{}`)
					if req.Arguments != nil && len(*req.Arguments) > 0 && string(*req.Arguments) != "null" {
						argsToSave = *req.Arguments
					}
					if err := ps.AddAllow(req.ToolName, argsToSave); err != nil {
						return &types.ToolResult{
							Content: fmt.Sprintf("Permission granted for [%s] (once), but failed to persist rule: %s", req.ToolName, err.Error()),
						}, nil
					}
				}
				return &types.ToolResult{
					Content: fmt.Sprintf("Permission granted for [%s] and saved to rules. You may proceed to call this tool.", req.ToolName),
				}, nil
			case transport.PermissionOnce:
				return &types.ToolResult{
					Content: fmt.Sprintf("Permission granted for [%s] (this time only). You may proceed.", req.ToolName),
				}, nil
			default:
				return &types.ToolResult{
					Content: fmt.Sprintf("Permission denied by user for [%s]. Do not attempt this operation unless the user explicitly asks for it.", req.ToolName),
				}, nil
			}
		},
	)
}

// RegisterEmitEventTool registers a builtin tool that allows the LLM to emit
// custom events into the event bus for monitoring or automation.
func RegisterEmitEventTool(r *Registry, bus *event.Bus) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Event name, used to identify the event type"
			},
			"desc": {
				"type": "string",
				"description": "Event description or message content"
			}
		},
		"required": ["name", "desc"]
	}`)

	r.RegisterBuiltin("emit_event",
		"Emit a custom event into the event bus for monitoring or automation purposes. Args: {name, desc}",
		schema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				Name string `json:"name"`
				Desc string `json:"desc"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{
					Content: fmt.Sprintf("invalid args: %s", err.Error()),
					IsError: true,
				}, nil
			}

			if bus != nil {
				bus.Publish(ctx, event.Event{
					Type:      event.EventLLMEmit,
					Timestamp: time.Now(),
					Payload: map[string]any{
						"name": req.Name,
						"desc": req.Desc,
					},
				})
			}

			return &types.ToolResult{
				Content: fmt.Sprintf("Event [%s] emitted.", req.Name),
			}, nil
		},
	)
}
