package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/scheduler"
	"dolphin/internal/types"
)

// RegisterSchedulerTools registers builtin tools for cron task management.
func RegisterSchedulerTools(r *Registry, sched *scheduler.Scheduler) {
	createSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"schedule":{"type":"string","description":"Cron expression, e.g. */5 * * * *"},"command":{"type":"string","description":"Shell command to execute"}},"required":["name","schedule","command"]}`)
	listSchema := json.RawMessage(`{"type":"object"}`)
	deleteSchema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)

	r.RegisterBuiltin("cron_create", "Create a scheduled task. Args: {name, schedule, command}", createSchema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				Name     string `json:"name"`
				Schedule string `json:"schedule"`
				Command  string `json:"command"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
			}
			t, err := sched.Create(ctx, req.Name, req.Schedule, req.Command)
			if err != nil {
				return &types.ToolResult{Content: "failed to create task: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: fmt.Sprintf("task %q created (id: %s)", t.Name, t.ID)}, nil
		})

	r.RegisterBuiltin("cron_list", "List all scheduled tasks with their status", listSchema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			tasks := sched.List()
			if len(tasks) == 0 {
				return &types.ToolResult{Content: "no scheduled tasks"}, nil
			}
			var sb strings.Builder
			for _, t := range tasks {
				status := t.LastStatus
				if status == "" {
					status = "pending"
				}
				lastRun := "-"
				if t.LastRunAt != nil {
					lastRun = t.LastRunAt.Format("2006-01-02 15:04:05")
				}
				enabled := "enabled"
				if !t.Enabled {
					enabled = "disabled"
				}
				sb.WriteString(fmt.Sprintf("- %s (%s) [%s] schedule: %s, command: %s, last: %s, runs: %d\n",
					t.Name, t.ID, enabled, t.Schedule, t.Command, lastRun, t.RunCount))
				if t.LastStatus != "" {
					sb.WriteString(fmt.Sprintf("  status: %s\n", status))
				}
			}
			return &types.ToolResult{Content: sb.String()}, nil
		})

	r.RegisterBuiltin("cron_delete", "Delete a scheduled task by ID. Args: {id}", deleteSchema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
			}
			if err := sched.Delete(ctx, req.ID); err != nil {
				return &types.ToolResult{Content: "failed to delete: " + err.Error(), IsError: true}, nil
			}
			return &types.ToolResult{Content: "task deleted"}, nil
		})

	delaySchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"delay":{"type":"string","description":"Duration like 5m, 30s, 1h, 2h30m"},"command":{"type":"string","description":"Shell command to execute"}},"required":["name","delay","command"]}`)
	r.RegisterBuiltin("cron_delay", "Schedule a one-shot delayed task. Args: {name, delay, command}", delaySchema,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				Name    string `json:"name"`
				Delay   string `json:"delay"`
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
			}
			t, err := sched.ScheduleOnce(ctx, req.Name, req.Delay, req.Command)
			if err != nil {
				return &types.ToolResult{Content: "failed to schedule: " + err.Error(), IsError: true}, nil
			}
			fireAt := "-"
			if t.FireAt != nil {
				fireAt = t.FireAt.Format("2006-01-02 15:04:05")
			}
			return &types.ToolResult{Content: fmt.Sprintf("task %q scheduled (id: %s, fires at: %s)", t.Name, t.ID, fireAt)}, nil
		})
}
