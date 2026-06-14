package workflow

import (
	"context"
	"encoding/json"
	"os"

	"dolphin/internal/agentio"
	"dolphin/internal/i18n"
	"dolphin/internal/tool"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

// RegisterTools registers run_workflow and continue_workflow as builtin tools.
func RegisterTools(r *tool.Registry, engine *Engine, agentIO *agentio.AgentIO, logger *zap.Logger) {
	r.RegisterBuiltin(
		"run_workflow",
		i18n.T("tool.run_workflow"),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "`+i18n.T("tool.run_workflow_path")+`"}
			},
			"required": ["path"]
		}`),
		runWorkflowHandler(engine, logger),
	)

	r.RegisterBuiltin(
		"continue_workflow",
		i18n.T("tool.continue_workflow"),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "`+i18n.T("tool.continue_workflow_path")+`"}
			},
			"required": ["path"]
		}`),
		continueWorkflowHandler(engine, logger),
	)
}

func runWorkflowHandler(engine *Engine, logger *zap.Logger) tool.BuiltinHandler {
	return func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return &types.ToolResult{Content: "Invalid arguments: " + err.Error(), IsError: true}, nil
		}
		if params.Path == "" {
			return &types.ToolResult{Content: "path is required", IsError: true}, nil
		}

		data, err := os.ReadFile(params.Path)
		if err != nil {
			return &types.ToolResult{Content: "Failed to read workflow file: " + err.Error(), IsError: true}, nil
		}

		spec, err := Parse(data)
		if err != nil {
			return &types.ToolResult{Content: "Invalid workflow file: " + err.Error(), IsError: true}, nil
		}

		result, err := engine.Run(ctx, spec, "")
		if err != nil {
			if err == ErrCheckpointReached {
				return &types.ToolResult{
					Content: "Workflow " + spec.Name + " paused at checkpoint. Review the .result.yaml and call continue_workflow to resume.",
				}, nil
			}
			return &types.ToolResult{Content: "Workflow failed: " + err.Error(), IsError: true}, nil
		}

		summary, _ := json.MarshalIndent(result, "", "  ")
		return &types.ToolResult{Content: "Workflow " + spec.Name + " completed:\n" + string(summary)}, nil
	}
}

func continueWorkflowHandler(engine *Engine, logger *zap.Logger) tool.BuiltinHandler {
	return func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return &types.ToolResult{Content: "Invalid arguments: " + err.Error(), IsError: true}, nil
		}
		if params.Path == "" {
			return &types.ToolResult{Content: "path is required", IsError: true}, nil
		}

		data, err := os.ReadFile(params.Path)
		if err != nil {
			return &types.ToolResult{Content: "Failed to read workflow file: " + err.Error(), IsError: true}, nil
		}

		spec, err := Parse(data)
		if err != nil {
			return &types.ToolResult{Content: "Invalid workflow file: " + err.Error(), IsError: true}, nil
		}

		result, err := engine.Continue(ctx, spec, "")
		if err != nil {
			if err == ErrCheckpointReached {
				return &types.ToolResult{
					Content: "Workflow " + spec.Name + " paused at next checkpoint. Review and call continue_workflow again.",
				}, nil
			}
			return &types.ToolResult{Content: "Workflow failed: " + err.Error(), IsError: true}, nil
		}

		summary, _ := json.MarshalIndent(result, "", "  ")
		return &types.ToolResult{Content: "Workflow " + spec.Name + " completed:\n" + string(summary)}, nil
	}
}
