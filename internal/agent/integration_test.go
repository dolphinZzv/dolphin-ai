package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/agent/provider"
	"dolphin/internal/config"
	ctxpkg "dolphin/internal/context"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
)

func TestRunFullSessionWelcomeAndExit(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("hello from LLM"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"say hi", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Agent ready") {
		t.Error("expected welcome message")
	}
	if !strings.Contains(output, "Loaded MCP tools:") {
		t.Error("expected tools list in welcome")
	}
	if !strings.Contains(output, "test_tool") {
		t.Error("expected test_tool in tools list")
	}
	if !strings.Contains(output, "hello from LLM") {
		t.Errorf("expected LLM response in output, got: %s", output)
	}
}

func TestRunHelpCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"/help", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Commands:") {
		t.Error("expected help text")
	}
	if !strings.Contains(output, "/exit") {
		t.Error("expected /exit in help")
	}
	if !strings.Contains(output, "Loaded MCP tools:") {
		t.Error("expected tools in help")
	}
}

func TestRunMaxLoopGeneratesSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 1
	cfg.Session.Summary = true
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("response 1"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}},
			{Content: provider.TextContent("response 2"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"first", "second", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "checkpoint") {
		t.Error("expected checkpoint message at max loop, got:", output)
	}
	if !strings.Contains(output, "response 2") {
		t.Error("expected second response after checkpoint, got:", output)
	}
}

func TestRunEmptyInputSkipped(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 5
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("hi"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	// empty line should be skipped, then "hello" processed
	io := &mockIO{lines: []string{"", "hello", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "hi") {
		t.Error("expected response after skipping empty input, got:", output)
	}
}

func TestRunToolCallAndStreaming(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 10
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{
				Content:    jsonContent(`[{"type":"text","text":"calling tool"},{"type":"tool_use","id":"tu1","name":"test_tool","input":{}}]`),
				ToolCalls:  []provider.ToolCall{{ID: "tu1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &provider.Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    provider.TextContent("tool done"),
				Usage:      &provider.Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"do it", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "calling tool") {
		t.Error("expected reasoning text before tool call, got:", output)
	}
	if !strings.Contains(output, "tool done") {
		t.Error("expected final response after tool, got:", output)
	}
}

// --- E2E: Context compression, multi-tool chain, error recovery ---

func TestRunContextCompressionTriggered(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 20
	// Set a low context limit to trigger compression
	cfg.LLM.MaxContextTokens = 100

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	// Pre-populate many messages to exceed context limit
	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("compressed response"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}, StopReason: "end_turn"},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	// Build up a large message history to trigger compression
	io := &mockIO{lines: []string{"msg1", "msg2", "msg3", "msg4", "msg5", "msg6", "msg7", "msg8", "/exit"}}

	// Run — should not panic even with compression
	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "compressed response") {
		t.Error("expected response despite context compression, got:", output)
	}
}

func TestRunMultiToolChain(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 20
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "tool_a"})
	toolReg.Register(&mockTool{name: "tool_b"})

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{
				Content:    jsonContent(`[{"type":"text","text":"calling tool_a"},{"type":"tool_use","id":"t1","name":"tool_a","input":{}}]`),
				ToolCalls:  []provider.ToolCall{{ID: "t1", Name: "tool_a", Arguments: json.RawMessage(`{}`)}},
				Usage:      &provider.Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    jsonContent(`[{"type":"text","text":"calling tool_b"},{"type":"tool_use","id":"t2","name":"tool_b","input":{}}]`),
				ToolCalls:  []provider.ToolCall{{ID: "t2", Name: "tool_b", Arguments: json.RawMessage(`{}`)}},
				Usage:      &provider.Usage{InputTokens: 15, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    provider.TextContent("all tools done"),
				Usage:      &provider.Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"run chain", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "tool_a") {
		t.Error("expected tool_a to be called, got:", output)
	}
	if !strings.Contains(output, "tool_b") {
		t.Error("expected tool_b to be called, got:", output)
	}
	if !strings.Contains(output, "all tools done") {
		t.Error("expected final response after tool chain, got:", output)
	}
}

func TestRunErrorRecovery(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 20
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	// No tools registered — if LLM tries to call one, it won't be found

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{
				Content:    provider.TextContent("simple response"),
				Usage:      &provider.Usage{InputTokens: 5, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"hello", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "simple response") {
		t.Error("expected response, got:", output)
	}
}

func TestRunTurnContextCancelled(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 10
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("response"), Usage: &provider.Usage{}, StopReason: "end_turn"},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	io := &mockIO{lines: []string{"msg1"}}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Should exit gracefully when context is cancelled
	agt.Run(ctx, io)
	// Test passes if no panic
}

func TestRunTurnWithThinking(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 10
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	// Response with thinking block + text block
	content := json.RawMessage(`[{"type":"thinking","thinking":"let me think about this"},{"type":"text","text":"final answer"}]`)
	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{
				Content:    content,
				Usage:      &provider.Usage{InputTokens: 10, OutputTokens: 20},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: ctxpkg.NewBuilder(),
	}

	io := &mockIO{lines: []string{"complex question", "/exit"}}

	agt.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "final answer") {
		t.Error("expected final answer in output, got:", output)
	}
}

func TestEmailWelcomeOnlyOnFirstConfig(t *testing.T) {
	// Isolate home/userprofile so the email-configured marker doesn't leak.
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	defer os.Setenv("HOME", origHome)
	defer os.Setenv("USERPROFILE", origUserProfile)

	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 20
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("ok"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 2}},
		},
	}

	// First run with email transport — should print welcome and create marker.
	{
		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()
		toolReg := mcp.NewRegistry(cfg)
		toolReg.Register(&mockTool{name: "email_tool"})

		agt := &Agent{
			cfg:        cfg,
			sessMgr:    sessMgr,
			toolReg:    toolReg,
			provider:   prov,
			ctxBuilder: ctxpkg.NewBuilder(),
		}
		io := &mockIO{name: "email", lines: []string{"hi", "/exit"}}
		agt.Run(context.Background(), io)

		output := io.writes.String()
		if !strings.Contains(output, "Agent ready") {
			t.Error("first email run: expected welcome message, got:", output)
		}
		if !strings.Contains(output, "email_tool") {
			t.Error("first email run: expected tools in welcome, got:", output)
		}
		if !config.IsEmailConfigured() {
			t.Error("expected IsEmailConfigured = true after first email run")
		}
	}

	// Second run with email transport — should skip welcome (marker exists).
	{
		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()
		toolReg := mcp.NewRegistry(cfg)

		agt := &Agent{
			cfg:        cfg,
			sessMgr:    sessMgr,
			toolReg:    toolReg,
			provider:   prov,
			ctxBuilder: ctxpkg.NewBuilder(),
		}
		io := &mockIO{name: "email", lines: []string{"hi again", "/exit"}}
		agt.Run(context.Background(), io)

		output := io.writes.String()
		if strings.Contains(output, "Agent ready") {
			t.Error("second email run: expected NO welcome message, but got one")
		}
	}

	// Stdio run — should always print welcome (not email transport).
	{
		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()
		toolReg := mcp.NewRegistry(cfg)

		agt := &Agent{
			cfg:        cfg,
			sessMgr:    sessMgr,
			toolReg:    toolReg,
			provider:   prov,
			ctxBuilder: ctxpkg.NewBuilder(),
		}
		io := &mockIO{name: "stdio", lines: []string{"hi", "/exit"}}
		agt.Run(context.Background(), io)

		output := io.writes.String()
		if !strings.Contains(output, "Agent ready") {
			t.Error("stdio run: expected welcome message always, got:", output)
		}
	}
}

// jsonContent is a helper to create json.RawMessage from a JSON string literal.
func jsonContent(s string) json.RawMessage {
	return json.RawMessage(s)
}
