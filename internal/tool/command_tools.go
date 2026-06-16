package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

// RegisterCommandTools registers builtin tools for command CRUD and execution.
func RegisterCommandTools(r *Registry, br *brain.Brain) {
	listSchema := json.RawMessage(`{"type":"object"}`)
	nameSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Command name"}},"required":["name"]}`)
	upsertSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Command name"},"description":{"type":"string","description":"What this command does"},"content":{"type":"string","description":"Instructions for the LLM to execute"}},"required":["name"]}`)
	toggleSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"enabled":{"type":"boolean"}},"required":["name","enabled"]}`)

	r.RegisterBuiltin("commands_list", "List all available commands with their description and status", listSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		cmds, err := brain.ListCommands(ctx, br)
		if err != nil {
			return &types.ToolResult{Content: "failed to list commands: " + err.Error(), IsError: true}, nil
		}
		if len(cmds) == 0 {
			return &types.ToolResult{Content: "No commands found"}, nil
		}
		var sb strings.Builder
		for _, cmd := range cmds {
			status := "enabled"
			if !cmd.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&sb, "- %s (%s): %s\n", cmd.Name, status, cmd.Description)
		}
		return &types.ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
	})

	r.RegisterBuiltin("command_upsert", "Create, update, or delete a command by name. If only name is provided, deletes. Args: {name, description?, content?}", upsertSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Content     string `json:"content"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Name == "" {
			return &types.ToolResult{Content: "command name is required", IsError: true}, nil
		}

		// Delete: only name provided.
		if req.Description == "" && req.Content == "" {
			if err := brain.DeleteCommand(ctx, br, req.Name); err != nil {
				return &types.ToolResult{Content: "failed to delete: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("command %q deleted", req.Name)}, nil
		}

		existing, err := brain.ReadCommand(ctx, br, req.Name)
		if err == nil && existing != nil {
			// Update.
			if req.Description != "" {
				existing.Description = req.Description
			}
			if req.Content != "" {
				existing.Content = req.Content
			}
			if err := brain.WriteCommand(ctx, br, *existing); err != nil {
				return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("command %q updated", req.Name)}, nil
		}

		// Create.
		cmd := brain.Command{Name: req.Name, Description: req.Description, Enabled: true, Content: req.Content}
		if err := brain.WriteCommand(ctx, br, cmd); err != nil {
			return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: fmt.Sprintf("command %q created", req.Name)}, nil
	})

	r.RegisterBuiltin("command_toggle", "Enable or disable a command. Args: {name, enabled}", toggleSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		existing, err := brain.ReadCommand(ctx, br, req.Name)
		if err != nil {
			return &types.ToolResult{Content: fmt.Sprintf("command %q not found", req.Name), IsError: true}, nil
		}
		existing.Enabled = req.Enabled
		if err := brain.WriteCommand(ctx, br, *existing); err != nil {
			return &types.ToolResult{Content: "failed to toggle command: " + err.Error(), IsError: true}, nil
		}
		status := "enabled"
		if !req.Enabled {
			status = "disabled"
		}
		return &types.ToolResult{Content: fmt.Sprintf("command %q %s", req.Name, status)}, nil
	})

	r.RegisterBuiltin("command_execute", "Read and return a command's content for the LLM to execute. Args: {name}", nameSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		cmd, err := brain.ReadCommand(ctx, br, req.Name)
		if err != nil {
			return &types.ToolResult{Content: fmt.Sprintf("command %q not found", req.Name), IsError: true}, nil
		}
		if !cmd.Enabled {
			return &types.ToolResult{Content: fmt.Sprintf("command %q is disabled", req.Name), IsError: true}, nil
		}
		return &types.ToolResult{Content: cmd.Content}, nil
	})
}
