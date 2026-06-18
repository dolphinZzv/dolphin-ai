package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/i18n"
	"dolphin/internal/tool"
	"dolphin/internal/types"
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

		workflowPath := resolvePath(engine.brainDir, params.Path)
		data, err := os.ReadFile(workflowPath)
		if err != nil {
			return &types.ToolResult{Content: "Failed to read workflow file: " + err.Error(), IsError: true}, nil
		}

		spec, err := Parse(data)
		if err != nil {
			return &types.ToolResult{Content: "Invalid workflow file: " + err.Error(), IsError: true}, nil
		}

		// Detach from the tool execution's 30s timeout. Each workflow step
		// uses its own timeout (per-step YAML field or workflow.step_timeout
		// config, default 300s). Set workflow.step_timeout to 0 to disable.
		wfCtx := context.WithoutCancel(ctx)
		result, err := engine.Run(wfCtx, spec, "")
		if err != nil {
			if errors.Is(err, ErrCheckpointReached) {
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

		workflowPath := resolvePath(engine.brainDir, params.Path)
		data, err := os.ReadFile(workflowPath)
		if err != nil {
			return &types.ToolResult{Content: "Failed to read workflow file: " + err.Error(), IsError: true}, nil
		}

		spec, err := Parse(data)
		if err != nil {
			return &types.ToolResult{Content: "Invalid workflow file: " + err.Error(), IsError: true}, nil
		}

		// Detach from the tool execution's 30s timeout.
		wfCtx := context.WithoutCancel(ctx)
		result, err := engine.Continue(wfCtx, spec, "")
		if err != nil {
			if errors.Is(err, ErrCheckpointReached) {
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

func resolvePath(brainDir, relPath string) string {
	if brainDir != "" {
		return filepath.Join(brainDir, relPath)
	}
	return relPath
}
