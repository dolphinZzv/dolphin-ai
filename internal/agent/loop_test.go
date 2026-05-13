package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/transport"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	mu        sync.Mutex
	responses []*ProviderResponse
	callIndex int
}

func (m *mockProvider) Type() ProviderType { return "openai" }
func (m *mockProvider) Name() string       { return "mock" }
func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }
func (m *mockProvider) Complete(_ context.Context, _ ProviderRequest) (*ProviderResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIndex >= len(m.responses) {
		return &ProviderResponse{
			Content: TextContent("done"),
		}, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}
func (m *mockProvider) CompleteStream(_ context.Context, _ ProviderRequest) (<-chan StreamChunk, error) {
	m.mu.Lock()
	if m.callIndex >= len(m.responses) {
		m.mu.Unlock()
		ch := make(chan StreamChunk, 1)
		ch <- StreamChunk{Done: true}
		close(ch)
		return ch, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	m.mu.Unlock()

	ch := make(chan StreamChunk, 10)
	go func() {
		defer close(ch)

		// Stream text from content blocks
		var blocks []map[string]any
		if err := json.Unmarshal(resp.Content, &blocks); err == nil {
			for _, b := range blocks {
				if t, ok := b["text"].(string); ok && t != "" {
					ch <- StreamChunk{Content: TextContent(t)}
				}
			}
		}

		// Stream tool calls
		for _, tc := range resp.ToolCalls {
			ch <- StreamChunk{
				ToolCallBegin: &ToolCallBegin{ID: tc.ID, Name: tc.Name},
			}
			if len(tc.Arguments) > 0 {
				ch <- StreamChunk{ToolCallDelta: string(tc.Arguments)}
			}
		}

		if resp.Usage != nil {
			ch <- StreamChunk{Usage: resp.Usage}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// mockIO implements UserIO for testing.
type mockIO struct {
	lines   []string
	readIdx int
	writes  strings.Builder
	readErr error
}

func (m *mockIO) ReadLine() (string, error) {
	if m.readIdx >= len(m.lines) {
		return "", fmt.Errorf("no more input")
	}
	line := m.lines[m.readIdx]
	m.readIdx++
	return line, m.readErr
}
func (m *mockIO) WriteLine(s string) error {
	m.writes.WriteString(s + "\n")
	return nil
}
func (m *mockIO) WriteString(s string) error {
	m.writes.WriteString(s)
	return nil
}
func (m *mockIO) Context() string { return "" }
func (m *mockIO) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: true, Flushable: false}
}

// mockTool implements mcp.Tool for testing.
type mockTool struct {
	name string
}

func (t *mockTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        t.name,
		Description: "mock tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}
func (t *mockTool) Execute(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{Content: "mock result"}, nil
}

func newTestAgent(cfg *config.Config, provider Provider) *Agent {
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})
	return &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   provider,
		ctxBuilder: NewContextBuilder(),
	}
}

func TestRunTurnNoToolCalls(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("hello from LLM"),
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("say hi")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io, agt.toolReg, nil)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Should have added the assistant response
	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(state.Messages))
	}

	if !strings.Contains(io.writes.String(), "hello from LLM") {
		t.Errorf("expected output to contain 'hello from LLM', got: %s", io.writes.String())
	}
}

func TestRunTurnWithToolCall(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	// First response: tool call, second: final text
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    json.RawMessage(`[{"type":"text","text":"calling tool"},{"type":"tool_use","id":"tu_1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    TextContent("tool result received"),
				Usage:      &Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("do something")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io, agt.toolReg, nil)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Should have user + assistant(tool call) + tool(result) + assistant(final) = 4
	if len(state.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(state.Messages))
	}

	output := io.writes.String()
	if !strings.Contains(output, "tool result received") {
		t.Errorf("expected final output, got: %s", output)
	}
}

func TestRunTurnTruncatesLargeResult(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    json.RawMessage(`[{"type":"text","text":"calling"},{"type":"tool_use","id":"tu_1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	// Override tool to return large result
	agt.toolReg.Register(&mockToolLargeResult{})

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("big result")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io, agt.toolReg, nil)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Find the tool result message and check it was truncated
	for _, m := range state.Messages {
		if m.Role == "tool" {
			var blocks []map[string]any
			json.Unmarshal(m.Content, &blocks)
			for _, b := range blocks {
				if b["type"] == "tool_result" {
					content := b["content"]
					contentStr, _ := json.Marshal(content)
					if !strings.Contains(string(contentStr), "truncated") {
						t.Error("expected truncation notice in tool result")
					}
				}
			}
		}
	}
}

// mockToolLargeResult returns a result that exceeds the truncation limit.
type mockToolLargeResult struct{}

func (t *mockToolLargeResult) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{Name: "test_tool", Description: "large result", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (t *mockToolLargeResult) Execute(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{Content: strings.Repeat("x", 5000)}, nil
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("429 too many requests"), true},
		{fmt.Errorf("500 internal"), true},
		{fmt.Errorf("502 bad gateway"), true},
		{fmt.Errorf("503 unavailable"), true},
		{fmt.Errorf("connection refused"), true},
		{fmt.Errorf("timeout exceeded"), true},
		{fmt.Errorf("EOF"), true},
		{fmt.Errorf("400 bad request"), false},
		{fmt.Errorf("invalid input"), false},
		{fmt.Errorf("permission denied"), false},
	}
	for _, tt := range tests {
		got := isRetryable(tt.err)
		if got != tt.want {
			t.Errorf("isRetryable(%q) = %v, want %v", tt.err.Error(), got, tt.want)
		}
	}
}

func TestIsRetryableSubstringInWord(t *testing.T) {
	// "timeout" as part of another word should still match
	err := fmt.Errorf("notimeoutmate")
	if !isRetryable(err) {
		t.Errorf("expected true for 'notimeoutmate' (contains timeout)")
	}
}

func TestEstimateTokensPureASCII(t *testing.T) {
	// ASCII: ~4 bytes per token, so 40 bytes ≈ 10 tokens (bytes/4 heuristic, improved to /3.5)
	tokens := estimateTokens("hello world this is a test message")
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
	// 40 chars ≈ 40 bytes. estimateTokens computes: 0 CJK + 40*10/35 = 11
	if tokens < 5 || tokens > 20 {
		t.Errorf("expected ~11 tokens for 40-byte ASCII, got %d", tokens)
	}
}

func TestEstimateTokensPureCJK(t *testing.T) {
	// Pure Chinese: each char is 3 UTF-8 bytes, ~1 token each
	chinese := "你好世界这是一个测试消息" // 10 Chinese chars = 30 bytes = ~10 tokens
	tokens := estimateTokens(chinese)
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
	// 10 CJK chars * 1 token each = 10, plus minimal non-CJK (brackets etc)
	if tokens < 8 || tokens > 15 {
		t.Errorf("expected ~10 tokens for 10 CJK chars, got %d", tokens)
	}
}

func TestEstimateTokensMixed(t *testing.T) {
	mixed := "你好 world 测试 test" // 4 CJK chars + 11 ASCII bytes
	asciiTokens := estimateTokens("world test")
	cjkTokens := estimateTokens("你好测试")
	mixedTokens := estimateTokens(mixed)
	// Mixed should be roughly the sum of its parts
	if mixedTokens < cjkTokens || mixedTokens < asciiTokens {
		t.Errorf("mixed tokens (%d) should not be less than either component (cjk=%d, ascii=%d)",
			mixedTokens, cjkTokens, asciiTokens)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	if tokens := estimateTokens(""); tokens != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", tokens)
	}
}

func TestEstimateTokensCJKHeavier(t *testing.T) {
	// CJK estimate should be higher per byte than ASCII estimate for same byte count.
	// 30 bytes of CJK: 10 chars * 1 token = 10 tokens
	// 30 bytes of ASCII: 30 chars * 10/35 ≈ 8 tokens
	cjk30 := estimateTokens("你好世界你好世界你好")                       // 10 CJK chars, 30 bytes
	ascii30 := estimateTokens("abcdefghijklmnopqrstuvwxyzabcd") // 30 ASCII chars, 30 bytes
	if cjk30 <= ascii30 {
		t.Errorf("30 CJK bytes should estimate more tokens than 30 ASCII bytes, got cjk=%d ascii=%d", cjk30, ascii30)
	}
}

func TestEstimateTokensJSONContent(t *testing.T) {
	// Typical JSON content from tool results
	json := `[{"type":"text","text":"result content here with some data"}]`
	tokens := estimateTokens(json)
	if tokens <= 0 {
		t.Errorf("expected positive token count for JSON, got %d", tokens)
	}
}

func TestNewWithAnthropicProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Type = "anthropic"
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "claude-3-opus"
	cfg.Session.Dir = t.TempDir()
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	agt := New(cfg, sessMgr, toolReg)
	if agt == nil {
		t.Fatal("New returned nil")
	}
	if agt.provider.Type() != "anthropic" {
		t.Errorf("expected anthropic provider, got %v", agt.provider.Type())
	}
}

func TestNewWithOpenAIProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Type = "openai"
	cfg.LLM.APIKey = "test-key"
	cfg.Session.Dir = t.TempDir()
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	agt := New(cfg, sessMgr, toolReg)
	if agt == nil {
		t.Fatal("New returned nil")
	}
	if agt.provider.Type() != ProviderOpenAI {
		t.Errorf("expected openai provider, got %v", agt.provider.Type())
	}
}

func TestNewWithDefaultProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Type = "unsupported"
	cfg.LLM.APIKey = "test-key"
	cfg.Session.Dir = t.TempDir()
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	agt := New(cfg, sessMgr, toolReg)
	if agt == nil {
		t.Fatal("New returned nil")
	}
	// Default should be openai
	if agt.provider.Type() != ProviderOpenAI {
		t.Errorf("expected openai provider, got %v", agt.provider.Type())
	}
}

func TestExtractFinalResponse(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: TextContent("hello")},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hi there"}]`)},
	}
	result := extractFinalResponse(msgs)
	if result != "hi there" {
		t.Errorf("got %q, want %q", result, "hi there")
	}
}

func TestExtractFinalResponseNoAssistant(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
	}
	result := extractFinalResponse(msgs)
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestExtractFinalResponseEmpty(t *testing.T) {
	result := extractFinalResponse(nil)
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestRunTaskBasic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("task result"),
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	result, err := agt.RunTask(
		context.Background(),
		"do something",
		"system prompt",
		agt.toolReg,
		"",
	)
	if err != nil {
		t.Fatalf("RunTask error: %v", err)
	}
	if result.Output != "task result" {
		t.Errorf("Output = %q, want %q", result.Output, "task result")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q", result.Status)
	}
	if !result.Success {
		t.Error("expected Success = true")
	}
}

// --- Summary generation tests ---

func TestGenerateSummaryDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = false
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess:       sess,
		Turn:       5,
		StopReason: "user_exit",
	}

	// generateSummary should return early without writing a file
	agt.generateSummary(sess, state)

	// Verify no summary file was created
	path := filepath.Join(cfg.Session.Dir, string(sess.ID)+"-summary.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected no summary file when Summary config is disabled")
	}
}

func TestGenerateSummaryEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	sess.Turn = 3
	state := &LoopState{
		Sess:          sess,
		Turn:          3,
		ToolCallCount: 5,
		ErrorCount:    1,
		StopReason:    "user_exit",
	}

	agt.generateSummary(sess, state)

	// Verify summary file exists and has correct content
	path := filepath.Join(cfg.Session.Dir, string(sess.ID)+"-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var sum map[string]any
	if err := json.Unmarshal(data, &sum); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if sum["turns"] != float64(3) {
		t.Errorf("turns = %v, want 3", sum["turns"])
	}
	if sum["tool_call_count"] != float64(5) {
		t.Errorf("tool_call_count = %v, want 5", sum["tool_call_count"])
	}
	if sum["error_count"] != float64(1) {
		t.Errorf("error_count = %v, want 1", sum["error_count"])
	}
	if sum["state"] != "user_exit" {
		t.Errorf("state = %v, want user_exit", sum["state"])
	}
}

func TestGenerateSummaryStopReasons(t *testing.T) {
	tests := []struct {
		stopReason string
		wantState  string
		wantSkip   bool // transport_error + 0 activity → skip
	}{
		{"interrupted", "interrupted", false},
		{"user_exit", "user_exit", false},
		{"max_loop", "max_loop", false},
		{"transport_error", "transport_error", true}, // 0 turns → skip
		{"", "completed", false},
		{"unknown", "completed", false},
	}

	for _, tt := range tests {
		t.Run(tt.stopReason, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Session.Summary = true
			cfg.Session.Dir = t.TempDir()
			agt := newTestAgent(cfg, &mockProvider{})

			sess, _ := agt.sessMgr.NewSession(10)
			state := &LoopState{
				Sess:       sess,
				StopReason: tt.stopReason,
			}

			agt.generateSummary(sess, state)

			path := filepath.Join(cfg.Session.Dir, string(sess.ID)+"-summary.json")
			if tt.wantSkip {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Error("expected no summary file for transport_error with 0 turns")
				}
				return
			}
			data, _ := os.ReadFile(path)
			var sum map[string]any
			json.Unmarshal(data, &sum)
			if sum["state"] != tt.wantState {
				t.Errorf("state = %v, want %v", sum["state"], tt.wantState)
			}
		})
	}
}

func TestGenerateSummaryTransportErrorWithActivity(t *testing.T) {
	// transport_error with actual turns should still generate a summary
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	sess.Turn = 3
	state := &LoopState{
		Sess:          sess,
		Turn:          3,
		ToolCallCount: 5,
		ErrorCount:    1,
		StopReason:    "transport_error",
	}

	agt.generateSummary(sess, state)

	path := filepath.Join(cfg.Session.Dir, string(sess.ID)+"-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected summary file for transport_error with activity: %v", err)
	}
	var sum map[string]any
	json.Unmarshal(data, &sum)
	if sum["state"] != "transport_error" {
		t.Errorf("state = %v, want transport_error", sum["state"])
	}
	if sum["turns"] != float64(3) {
		t.Errorf("turns = %v, want 3", sum["turns"])
	}
}

// TestSessionFullLifecycleWithSummary is an E2E test covering
// session creation → turns → summary generation → file verification.
func TestSessionFullLifecycleWithSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000

	// Simulate a multi-turn conversation with tool calls
	prov := &mockProvider{
		responses: []*ProviderResponse{
			// Turn 1: tool call
			{
				Content:    json.RawMessage(`[{"type":"text","text":"let me check"},{"type":"tool_use","id":"tu_1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    TextContent("the result is 42"),
				Usage:      &Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
			// Turn 2: text only
			{
				Content:    TextContent("goodbye"),
				Usage:      &Usage{InputTokens: 30, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	sess, _ := agt.sessMgr.NewSession(50)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("what is the answer")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	// Run turn 1
	err := agt.runTurn(context.Background(), state, "system prompt", io, agt.toolReg, nil)
	if err != nil {
		t.Fatalf("runTurn 1: %v", err)
	}

	// Run turn 2
	state.Messages = append(state.Messages, Message{Role: "user", Content: TextContent("thanks")})
	state.Turn = 2
	sess.Turn = 2
	err = agt.runTurn(context.Background(), state, "system prompt", io, agt.toolReg, nil)
	if err != nil {
		t.Fatalf("runTurn 2: %v", err)
	}

	// Now generate summary
	state.StopReason = "user_exit"
	agt.generateSummary(sess, state)

	// Verify summary file
	path := filepath.Join(cfg.Session.Dir, string(sess.ID)+"-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile summary: %v", err)
	}

	var sum map[string]any
	if err := json.Unmarshal(data, &sum); err != nil {
		t.Fatalf("Unmarshal summary: %v", err)
	}

	if sum["session_id"] != string(sess.ID) {
		t.Errorf("session_id = %v, want %v", sum["session_id"], sess.ID)
	}
	if sum["turns"] != float64(2) {
		t.Errorf("turns = %v, want 2", sum["turns"])
	}
	if sum["state"] != "user_exit" {
		t.Errorf("state = %v, want user_exit", sum["state"])
	}
	// tool_call_count should be at least 1 (from turn 1)
	if tc, ok := sum["tool_call_count"].(float64); !ok || tc < 1 {
		t.Errorf("tool_call_count = %v, want >= 1", sum["tool_call_count"])
	}
}

// --- E2E: Agent.Run with transport disconnect ---

// TestE2EAgentRunReadLineErrorSkipsSummary verifies the full Run lifecycle:
// when ReadLine fails immediately, the transport_error state + 0 turns
// causes generateSummary to skip writing a summary file.
func TestE2EAgentRunReadLineErrorSkipsSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.MaxLoop = 10

	agt := newTestAgent(cfg, &mockProvider{})

	// Empty lines: ReadLine immediately returns error
	io := &mockIO{lines: []string{}}

	ctx := context.Background()
	agt.Run(ctx, io)

	// Verify no summary file (transport_error + 0 turns → skip)
	entries, _ := os.ReadDir(cfg.Session.Dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-summary.json") {
			t.Errorf("expected no summary for transport_error + 0 turns, found: %s", e.Name())
		}
	}
}

func TestRunTaskWithParentSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("parent linked result"),
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	result, err := agt.RunTask(
		context.Background(),
		"task",
		"prompt",
		agt.toolReg,
		"parent-session-id",
	)
	if err != nil {
		t.Fatalf("RunTask error: %v", err)
	}
	if result.Output != "parent linked result" {
		t.Errorf("Output = %q", result.Output)
	}
}
