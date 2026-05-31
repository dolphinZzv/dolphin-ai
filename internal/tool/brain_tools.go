package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

// RegisterBrainTools registers builtin tools for brain read/write/list/log.
func RegisterBrainTools(r *Registry, br *brain.Brain) {
	readSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to brain root, e.g. rules/code-style.md"}},"required":["path"]}`)
	writeSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to brain root"},"content":{"type":"string","description":"File content"},"summary":{"type":"string","description":"Change summary"}},"required":["path","content"]}`)

	r.RegisterBuiltin("brain_read", "Read a file from the brain (long-term knowledge directory). Args: {path}", readSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Path == "" {
			return &types.ToolResult{Content: "path is required", IsError: true}, nil
		}
		content, err := br.Read(ctx, req.Path)
		if err != nil {
			return &types.ToolResult{Content: "brain read failed: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: content}, nil
	})

	r.RegisterBuiltin("brain_write", "Write content to a file in the brain (auto-committed via git). Args: {path, content, summary?}", writeSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Path == "" {
			return &types.ToolResult{Content: "path is required", IsError: true}, nil
		}
		if err := br.Write(ctx, req.Path, req.Summary, req.Content); err != nil {
			return &types.ToolResult{Content: "brain write failed: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "written to brain: " + req.Path}, nil
	})

	r.RegisterBuiltin("brain_list", "List all .md files in the brain directory", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		files, err := br.List(ctx)
		if err != nil {
			return &types.ToolResult{Content: "brain list failed: " + err.Error(), IsError: true}, nil
		}
		if len(files) == 0 {
			return &types.ToolResult{Content: "(empty)"}, nil
		}
		return &types.ToolResult{Content: strings.Join(files, "\n")}, nil
	})

	r.RegisterBuiltin("brain_log", "View recent git commit history of the brain. Args: {n?} (default 10)", json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","description":"Number of recent commits"}}}`), func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			N int `json:"n"`
		}
		json.Unmarshal(args, &req)
		if req.N <= 0 {
			req.N = 10
		}
		entries, err := br.GitLog(ctx, req.N)
		if err != nil {
			return &types.ToolResult{Content: "brain log failed: " + err.Error(), IsError: true}, nil
		}
		if len(entries) == 0 {
			return &types.ToolResult{Content: "(no commits)"}, nil
		}
		var sb strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&sb, "%s %s (%s)\n", e.Hash[:8], e.Message, e.Date.Format("2006-01-02 15:04"))
		}
		return &types.ToolResult{Content: sb.String()}, nil
	})
}
