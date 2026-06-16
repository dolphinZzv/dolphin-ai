package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

// RegisterScriptTools registers builtin tools for script CRUD (LLM-facing).
func RegisterScriptTools(r *Registry, br *brain.Brain) {
	listSchema := json.RawMessage(`{"type":"object"}`)
	upsertSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Script name"},"description":{"type":"string","description":"What this script does"},"content":{"type":"string","description":"Instructions to execute"}},"required":["name"]}`)
	toggleSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"enabled":{"type":"boolean"}},"required":["name","enabled"]}`)

	r.RegisterBuiltin("scripts_list", "List all available scripts with their description and status", listSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		scripts, err := brain.ListScripts(ctx, br)
		if err != nil {
			return &types.ToolResult{Content: "failed to list scripts: " + err.Error(), IsError: true}, nil
		}
		if len(scripts) == 0 {
			return &types.ToolResult{Content: "No scripts found"}, nil
		}
		var sb strings.Builder
		for _, s := range scripts {
			status := "enabled"
			if !s.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&sb, "- %s (%s): %s\n", s.Name, status, s.Description)
		}
		return &types.ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
	})

	r.RegisterBuiltin("script_upsert", "Create, update, or delete a script by name. If only name is provided, deletes. Args: {name, description?, content?}", upsertSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Content     string `json:"content"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Name == "" {
			return &types.ToolResult{Content: "script name is required", IsError: true}, nil
		}

		// Delete: only name provided.
		if req.Description == "" && req.Content == "" {
			if err := brain.DeleteScript(ctx, br, req.Name); err != nil {
				return &types.ToolResult{Content: "failed to delete: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("script %q deleted", req.Name)}, nil
		}

		existing, err := brain.ReadScript(ctx, br, req.Name)
		if err == nil && existing != nil {
			// Update.
			if req.Description != "" {
				existing.Description = req.Description
			}
			if req.Content != "" {
				existing.Content = req.Content
			}
			if err := brain.WriteScript(ctx, br, *existing); err != nil {
				return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("script %q updated", req.Name)}, nil
		}

		// Create.
		s := brain.Script{Name: req.Name, Description: req.Description, Enabled: true, Content: req.Content}
		if err := brain.WriteScript(ctx, br, s); err != nil {
			return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: fmt.Sprintf("script %q created", req.Name)}, nil
	})

	r.RegisterBuiltin("script_toggle", "Enable or disable a script. Args: {name, enabled}", toggleSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		existing, err := brain.ReadScript(ctx, br, req.Name)
		if err != nil {
			return &types.ToolResult{Content: fmt.Sprintf("script %q not found", req.Name), IsError: true}, nil
		}
		existing.Enabled = req.Enabled
		if err := brain.WriteScript(ctx, br, *existing); err != nil {
			return &types.ToolResult{Content: "failed to toggle script: " + err.Error(), IsError: true}, nil
		}
		status := "enabled"
		if !req.Enabled {
			status = "disabled"
		}
		return &types.ToolResult{Content: fmt.Sprintf("script %q %s", req.Name, status)}, nil
	})
}
