package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

// RegisterSubscriptionTools registers builtin tools for subscription CRUD.
func RegisterSubscriptionTools(r *Registry, br *brain.Brain) {
	listSchema := json.RawMessage(`{"type":"object"}`)
	upsertSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Subscription name"},
			"description": {"type": "string", "description": "What this subscription does"},
			"event_pattern": {"type": "string", "description": "Event type glob, e.g. llm.*, file.*, *"},
			"filter_path": {"type": "string", "description": "Optional path glob filter (for file.* events)"},
			"content": {"type": "string", "description": "Message to send to the LLM when triggered"},
			"enabled": {"type": "boolean"}
		},
		"required": ["name"]
	}`)
	toggleSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"enabled":{"type":"boolean"}},"required":["name","enabled"]}`)

	r.RegisterBuiltin("subscriptions_list", "List all subscriptions with their status and event patterns", listSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		subs, err := brain.ListSubscriptions(ctx, br)
		if err != nil {
			return &types.ToolResult{Content: "failed to list subscriptions: " + err.Error(), IsError: true}, nil
		}
		if len(subs) == 0 {
			return &types.ToolResult{Content: "No subscriptions found"}, nil
		}
		var sb strings.Builder
		for _, sub := range subs {
			status := "enabled"
			if !sub.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&sb, "- %s (%s) [%s]: %s\n", sub.Name, status, sub.EventPattern, sub.Description)
		}
		return &types.ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
	})

	r.RegisterBuiltin("subscription_upsert", "Create, update, or delete a subscription by name. If only name is provided, deletes. Args: {name, event_pattern?, description?, filter_path?, content?, enabled?}", upsertSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			EventPattern string `json:"event_pattern"`
			FilterPath   string `json:"filter_path"`
			Content      string `json:"content"`
			Enabled      *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Name == "" {
			return &types.ToolResult{Content: "subscription name is required", IsError: true}, nil
		}

		// Delete: only name provided, no other fields.
		if req.EventPattern == "" && req.Description == "" && req.Content == "" && req.FilterPath == "" && req.Enabled == nil {
			if err := brain.DeleteSubscription(ctx, br, req.Name); err != nil {
				return &types.ToolResult{Content: "failed to delete: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("subscription %q deleted", req.Name)}, nil
		}

		existing, err := brain.ReadSubscription(ctx, br, req.Name)
		if err == nil && existing != nil {
			// Update existing.
			if req.Description != "" {
				existing.Description = req.Description
			}
			if req.EventPattern != "" {
				existing.EventPattern = req.EventPattern
			}
			if req.Content != "" {
				existing.Content = req.Content
			}
			if req.Enabled != nil {
				existing.Enabled = *req.Enabled
			}
			if req.FilterPath != "" {
				existing.Filters = brain.SubscriptionFilter{Path: req.FilterPath}
			}
			if err := brain.WriteSubscription(ctx, br, *existing); err != nil {
				return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("subscription %q updated", req.Name)}, nil
		}

		// Create new.
		sub := brain.Subscription{
			Name:         req.Name,
			Description:  req.Description,
			EventPattern: req.EventPattern,
			Enabled:      true,
			Content:      req.Content,
		}
		if req.FilterPath != "" {
			sub.Filters = brain.SubscriptionFilter{Path: req.FilterPath}
		}
		if err := brain.WriteSubscription(ctx, br, sub); err != nil {
			return &types.ToolResult{Content: "failed: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: fmt.Sprintf("subscription %q created", req.Name)}, nil
	})

	r.RegisterBuiltin("subscription_toggle", "Enable or disable a subscription. Args: {name, enabled}", toggleSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		existing, err := brain.ReadSubscription(ctx, br, req.Name)
		if err != nil {
			return &types.ToolResult{Content: fmt.Sprintf("subscription %q not found", req.Name), IsError: true}, nil
		}
		existing.Enabled = req.Enabled
		if err := brain.WriteSubscription(ctx, br, *existing); err != nil {
			return &types.ToolResult{Content: "failed to toggle subscription: " + err.Error(), IsError: true}, nil
		}
		status := "enabled"
		if !req.Enabled {
			status = "disabled"
		}
		return &types.ToolResult{Content: fmt.Sprintf("subscription %q %s", req.Name, status)}, nil
	})
}
