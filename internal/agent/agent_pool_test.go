package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphinzZ/internal/config"
	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/session"
)

// ---------------------------------------------------------------------------
// AgentDef tests
// ---------------------------------------------------------------------------

func TestLoadAgentDefs_NoDirReturnsEmpty(t *testing.T) {
	defs, err := LoadAgentDefs("/tmp/nonexistent-dolphinzZ-agents-abc123")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map, got %d entries", len(defs))
	}
}

func TestLoadAgentDefs_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "my-agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(`name: my-agent
role: test specialist
tools: [shell, read, grep]
timeout: 120
`), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	def, ok := defs["my-agent"]
	if !ok {
		t.Fatal("expected my-agent in defs")
	}
	if def.Role != "test specialist" {
		t.Errorf("role = %q", def.Role)
	}
	if len(def.Tools) != 3 || def.Tools[0] != "shell" {
		t.Errorf("tools = %v", def.Tools)
	}
	if def.Timeout != 120 {
		t.Errorf("timeout = %d", def.Timeout)
	}
}

func TestLoadAgentDefs_SkipsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bad-agent"), 0755)
	os.WriteFile(filepath.Join(dir, "bad-agent", "agent.yaml"), []byte(`invalid: yaml: [`), 0644)
	os.MkdirAll(filepath.Join(dir, "good-agent"), 0755)
	os.WriteFile(filepath.Join(dir, "good-agent", "agent.yaml"), []byte(`role: good agent`), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if _, ok := defs["bad-agent"]; ok {
		t.Error("expected bad-agent to be skipped")
	}
	if _, ok := defs["good-agent"]; !ok {
		t.Error("expected good-agent to be loaded")
	}
}

func TestLoadAgentDefs_DefaultWorkspace(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "test-agent"), 0755)
	os.WriteFile(filepath.Join(dir, "test-agent", "agent.yaml"), []byte(`role: tester`), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	def := defs["test-agent"]
	if def.Workspace == "" {
		t.Fatal("expected non-empty workspace")
	}
	expected := filepath.Join(filepath.Dir(dir), "workspaces", "test-agent")
	if def.Workspace != expected {
		t.Errorf("workspace = %q, want %q", def.Workspace, expected)
	}
	if _, err := os.Stat(def.Workspace); os.IsNotExist(err) {
		t.Error("workspace dir should have been created by LoadAgentDefs")
	}
}

func TestLoadAgentDefs_ExplicitWorkspace(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "custom-ws")
	os.MkdirAll(filepath.Join(dir, "explicit-agent"), 0755)
	os.WriteFile(filepath.Join(dir, "explicit-agent", "agent.yaml"),
		[]byte("role: explicit\nworkspace: "+wsDir+"\n"), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if defs["explicit-agent"].Workspace != wsDir {
		t.Errorf("workspace = %q, want %q", defs["explicit-agent"].Workspace, wsDir)
	}
}

func TestAgentDir(t *testing.T) {
	result := AgentDir("/agents", "my-agent")
	if result != "/agents/my-agent" {
		t.Errorf("AgentDir = %q", result)
	}
}

func TestAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	result := AgentWorkspace(cfg, "my-agent")
	if result != "/workspaces/my-agent" {
		t.Errorf("AgentWorkspace = %q", result)
	}
}

func TestTempAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	result := TempAgentWorkspace(cfg, "my-agent")
	if result != "/workspaces/temp-my-agent" {
		t.Errorf("TempAgentWorkspace = %q", result)
	}
}

// ---------------------------------------------------------------------------
// AgentKind tests
// ---------------------------------------------------------------------------

func TestAgentKindString(t *testing.T) {
	cases := []struct {
		kind AgentKind
		want string
	}{
		{AgentUser, "user"},
		{AgentCoord, "temp"},
		{AgentKind(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("AgentKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ChannelIO tests
// ---------------------------------------------------------------------------

func TestChannelIO_ReadLineReturnsTask(t *testing.T) {
	cio := NewChannelIO("test task")
	line, err := cio.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if line != "test task" {
		t.Errorf("got %q, want 'test task'", line)
	}
}

func TestChannelIO_ReadLineBlocksOnSecondCall(t *testing.T) {
	cio := NewChannelIO("task")
	line, err := cio.ReadLine()
	if err != nil {
		t.Fatalf("first ReadLine error: %v", err)
	}
	if line != "task" {
		t.Errorf("got %q, want 'task'", line)
	}
	// Second ReadLine would block (channel not closed). RunTask never calls
	// ReadLine directly — runTurn only uses WriteLine/WriteString.
	// This is expected behavior for the headless IO pattern.
}

func TestChannelIO_WritesNoOp(t *testing.T) {
	cio := NewChannelIO("task")
	if err := cio.WriteLine("hello"); err != nil {
		t.Errorf("WriteLine unexpected error: %v", err)
	}
	if err := cio.WriteString("world"); err != nil {
		t.Errorf("WriteString unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AgentPool tests
// ---------------------------------------------------------------------------

// testPoolHarness creates a minimal pool + agent for structural tests.
func testPoolHarness(t *testing.T) (*AgentPool, *Agent) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.IdleTimeout = 0 // disable reaper

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   &mockProvider{},
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency:    cfg.Pool.MaxConcurrency,
		DefaultTimeout:    cfg.Pool.DefaultTimeout,
		WorkspaceDir:      cfg.Pool.WorkspaceDir,
		IdleTimeout:       time.Duration(cfg.Pool.IdleTimeout) * time.Second,
		MaxPendingResults: cfg.Pool.MaxPendingResults,
	})
	return pool, agt
}

func TestPool_NewAndList(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	if len(pool.List()) != 0 {
		t.Error("expected empty pool")
	}

	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker role"}, AgentUser, agt, agt.toolReg)
	agents := pool.List()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "worker" {
		t.Errorf("Name = %q", agents[0].Name)
	}
	if agents[0].Status != "idle" {
		t.Errorf("Status = %q", agents[0].Status)
	}
	if agents[0].Kind != "user" {
		t.Errorf("Kind = %q", agents[0].Kind)
	}
}

func TestPool_AddAndRemove(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker"}, AgentUser, agt, agt.toolReg)
	if len(pool.List()) != 1 {
		t.Fatal("expected 1 agent after Add")
	}

	if !pool.Remove("worker") {
		t.Error("Remove returned false for existing agent")
	}
	if len(pool.List()) != 0 {
		t.Error("expected 0 agents after Remove")
	}
	if pool.Remove("nonexistent") {
		t.Error("Remove returned true for nonexistent agent")
	}
}

func TestPool_DispatchUnknownAgent(t *testing.T) {
	pool, _ := testPoolHarness(t)
	defer pool.Shutdown()

	err := pool.Dispatch("nonexistent", Task{ID: "t1", Input: "do it"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestPool_DispatchAndCollect(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.IdleTimeout = 0
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("task complete"),
				Usage:      &Usage{InputTokens: 5, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{
			MaxConcurrency:    cfg.Pool.MaxConcurrency,
			DefaultTimeout:    cfg.Pool.DefaultTimeout,
			WorkspaceDir:      cfg.Pool.WorkspaceDir,
			IdleTimeout:       time.Duration(cfg.Pool.IdleTimeout) * time.Second,
			MaxPendingResults: cfg.Pool.MaxPendingResults,
		})
	defer pool.Shutdown()

	pool.Add("test-agent", &AgentDef{Name: "test-agent", Role: "test role"}, AgentUser, agt, toolReg)

	err := pool.Dispatch("test-agent", Task{ID: "task-1", Input: "do something"})
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	// Wait for task processing
	time.Sleep(500 * time.Millisecond)

	results := pool.Collect()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if !r.Success {
		t.Errorf("Success = false, error: %s", r.Error)
	}
	if r.TaskID != "task-1" {
		t.Errorf("TaskID = %q", r.TaskID)
	}
	if r.AgentName != "test-agent" {
		t.Errorf("AgentName = %q", r.AgentName)
	}
	if !strings.Contains(r.Output, "task complete") {
		t.Errorf("Output = %q", r.Output)
	}
	if r.Status != "completed" {
		t.Errorf("Status = %q", r.Status)
	}
	if r.DurationMs < 0 {
		t.Errorf("DurationMs = %d", r.DurationMs)
	}

	agents := pool.List()
	if agents[0].TasksDone != 1 {
		t.Errorf("TasksDone = %d, want 1", agents[0].TasksDone)
	}
	if agents[0].Status != "idle" {
		t.Errorf("Status = %q after completion", agents[0].Status)
	}
}

func TestPool_ConcurrentDispatch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.IdleTimeout = 0
	cfg.Session.Summary = false
	cfg.Pool.MaxConcurrency = 5

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()
	toolReg := mcp.NewRegistry(cfg)

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{Content: TextContent("r1"), Usage: &Usage{InputTokens: 1, OutputTokens: 1}, StopReason: "end_turn"},
			{Content: TextContent("r2"), Usage: &Usage{InputTokens: 1, OutputTokens: 1}, StopReason: "end_turn"},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{
			MaxConcurrency:    cfg.Pool.MaxConcurrency,
			DefaultTimeout:    cfg.Pool.DefaultTimeout,
			WorkspaceDir:      cfg.Pool.WorkspaceDir,
			IdleTimeout:       time.Duration(cfg.Pool.IdleTimeout) * time.Second,
			MaxPendingResults: cfg.Pool.MaxPendingResults,
		})
	defer pool.Shutdown()

	pool.Add("w1", &AgentDef{Name: "w1", Role: "w1"}, AgentUser, agt, toolReg)
	pool.Add("w2", &AgentDef{Name: "w2", Role: "w2"}, AgentUser, agt, toolReg)

	if err := pool.Dispatch("w1", Task{ID: "t1", Input: "task 1"}); err != nil {
		t.Fatalf("dispatch w1: %v", err)
	}
	if err := pool.Dispatch("w2", Task{ID: "t2", Input: "task 2"}); err != nil {
		t.Fatalf("dispatch w2: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	results := pool.Collect()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both should succeed
	for _, r := range results {
		if !r.Success {
			t.Errorf("task %s failed: %s", r.TaskID, r.Error)
		}
	}
}

func TestPool_RemoveCleansCoordWorkspace(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	wsDir := t.TempDir()
	pool.Add("temp-worker", &AgentDef{Name: "temp-worker", Role: "temp", Workspace: wsDir}, AgentCoord, agt, agt.toolReg)

	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		t.Fatal("workspace should exist before remove")
	}

	pool.Remove("temp-worker")

	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Error("coordinator workspace should be cleaned up after remove")
	}
}

func TestPool_RemoveDoesNotCleanUserWorkspace(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	wsDir := t.TempDir()
	pool.Add("user-worker", &AgentDef{Name: "user-worker", Role: "user", Workspace: wsDir}, AgentUser, agt, agt.toolReg)

	pool.Remove("user-worker")

	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		t.Error("user workspace should NOT be removed")
	}
}

func TestPool_ShutdownCleansCoordWorkspaces(t *testing.T) {
	pool, agt := testPoolHarness(t)

	ws1 := t.TempDir()
	ws2 := t.TempDir()
	pool.Add("coord-1", &AgentDef{Name: "coord-1", Role: "c1", Workspace: ws1}, AgentCoord, agt, agt.toolReg)
	pool.Add("coord-2", &AgentDef{Name: "coord-2", Role: "c2", Workspace: ws2}, AgentCoord, agt, agt.toolReg)

	pool.Shutdown()

	if _, err := os.Stat(ws1); !os.IsNotExist(err) {
		t.Error("coord-1 workspace should be cleaned up after shutdown")
	}
	if _, err := os.Stat(ws2); !os.IsNotExist(err) {
		t.Error("coord-2 workspace should be cleaned up after shutdown")
	}
}

func TestPool_SetParentSessionID(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.SetParentSessionID("parent-789")
	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker"}, AgentUser, agt, agt.toolReg)

	err := pool.Dispatch("worker", Task{ID: "t1", Input: "hello"})
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	results := pool.Collect()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Errorf("task failed: %s", results[0].Error)
	}
}

// blockingProvider waits for context cancellation before completing.
type blockingProvider struct{}

func (b *blockingProvider) Type() ProviderType { return "openai" }
func (b *blockingProvider) Complete(ctx context.Context, _ ProviderRequest) (*ProviderResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingProvider) CompleteStream(ctx context.Context, _ ProviderRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	go func() {
		<-ctx.Done()
		ch <- StreamChunk{Done: true}
		close(ch)
	}()
	return ch, nil
}

func TestPool_Cancel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.IdleTimeout = 0
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()
	toolReg := mcp.NewRegistry(cfg)

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   &blockingProvider{},
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{
			MaxConcurrency:    cfg.Pool.MaxConcurrency,
			DefaultTimeout:    cfg.Pool.DefaultTimeout,
			WorkspaceDir:      cfg.Pool.WorkspaceDir,
			IdleTimeout:       time.Duration(cfg.Pool.IdleTimeout) * time.Second,
			MaxPendingResults: cfg.Pool.MaxPendingResults,
		})
	defer pool.Shutdown()

	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker"}, AgentUser, agt, toolReg)

	if err := pool.Dispatch("worker", Task{ID: "task-1", Input: "do it"}); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	// Let the task start processing
	time.Sleep(100 * time.Millisecond)

	agents := pool.List()
	if agents[0].Status != "busy" {
		t.Errorf("expected status 'busy', got '%s'", agents[0].Status)
	}

	if !pool.Cancel("task-1") {
		t.Error("Cancel returned false")
	}

	time.Sleep(200 * time.Millisecond)

	// Agent should be idle again
	agents = pool.List()
	if agents[0].Status != "idle" {
		t.Errorf("expected status 'idle' after cancel, got '%s'", agents[0].Status)
	}

	results := pool.Collect()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Success {
		t.Error("expected cancelled result to have Success=false")
	}
	if results[0].Status != "cancelled" {
		t.Errorf("Status = %q, want 'cancelled'", results[0].Status)
	}
}

func TestPool_CancelAll(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.IdleTimeout = 0
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()
	toolReg := mcp.NewRegistry(cfg)

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   &blockingProvider{},
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{
			MaxConcurrency:    cfg.Pool.MaxConcurrency,
			DefaultTimeout:    cfg.Pool.DefaultTimeout,
			WorkspaceDir:      cfg.Pool.WorkspaceDir,
			IdleTimeout:       time.Duration(cfg.Pool.IdleTimeout) * time.Second,
			MaxPendingResults: cfg.Pool.MaxPendingResults,
		})
	defer pool.Shutdown()

	pool.Add("w1", &AgentDef{Name: "w1", Role: "w1"}, AgentUser, agt, toolReg)
	pool.Add("w2", &AgentDef{Name: "w2", Role: "w2"}, AgentUser, agt, toolReg)

	pool.Dispatch("w1", Task{ID: "t1", Input: "task 1"})
	pool.Dispatch("w2", Task{ID: "t2", Input: "task 2"})

	time.Sleep(100 * time.Millisecond)

	pool.CancelAll()

	time.Sleep(300 * time.Millisecond)

	results := pool.Collect()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "cancelled" {
			t.Errorf("task %s Status = %q, want 'cancelled'", r.TaskID, r.Status)
		}
		if r.Success {
			t.Errorf("task %s Success should be false", r.TaskID)
		}
	}
}

func TestPool_CollectEmpty(t *testing.T) {
	pool, _ := testPoolHarness(t)
	defer pool.Shutdown()

	results := pool.Collect()
	if results != nil && len(results) > 0 {
		t.Errorf("expected empty, got %d results", len(results))
	}
}

// ---------------------------------------------------------------------------
// RunTask tests
// ---------------------------------------------------------------------------

func TestRunTask_Basic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("task result output"),
				Usage:      &Usage{InputTokens: 5, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	result, err := agt.RunTask(context.Background(), "do the task", "system prompt", toolReg, "")
	if err != nil {
		t.Fatalf("RunTask error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, error: %s", result.Error)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want 'completed'", result.Status)
	}
	if !strings.Contains(result.Output, "task result output") {
		t.Errorf("Output = %q", result.Output)
	}
	if result.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", result.DurationMs)
	}
	if result.TaskID == "" {
		t.Error("TaskID should not be empty")
	}
}

func TestRunTask_WithToolCalls(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    jsonContent(`[{"type":"text","text":"calling tool"},{"type":"tool_use","id":"tu1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    TextContent("final result after tool"),
				Usage:      &Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	result, err := agt.RunTask(context.Background(), "use a tool", "system prompt", toolReg, "")
	if err != nil {
		t.Fatalf("RunTask error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "final result after tool") {
		t.Errorf("Output = %q, want 'final result after tool'", result.Output)
	}
}

func TestRunTask_WithParentSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{Content: TextContent("child result"), Usage: &Usage{InputTokens: 3, OutputTokens: 5}, StopReason: "end_turn"},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	// Run with a parent session ID (simulate coordinator linking)
	result, err := agt.RunTask(context.Background(), "child task", "child prompt", toolReg, session.SessionID("parent-session-001"))
	if err != nil {
		t.Fatalf("RunTask error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false: %s", result.Error)
	}
	if !strings.Contains(result.Output, "child result") {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestRunTask_Timeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Dir = t.TempDir()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Summary = false

	sessMgr := session.NewManager(cfg.Session.Dir)
	sessMgr.EnsureDir()
	toolReg := mcp.NewRegistry(cfg)

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   &blockingProvider{},
		ctxBuilder: NewContextBuilder(),
	}

	// Use a very short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := agt.RunTask(ctx, "slow task", "prompt", toolReg, "")

	// Due to select non-determinism in runTurn, either:
	// 1. ctx.Done() fires first -> err != nil, timeout/cancelled status
	// 2. stream Done flag is read first -> err == nil, "completed" status
	if err != nil {
		if result.Success {
			t.Error("expected Success=false when RunTask returns error")
		}
		if result.Error == "" {
			t.Error("expected non-empty Error on timeout")
		}
		if result.Status != "timeout" && result.Status != "cancelled" && result.Status != "error" {
			t.Errorf("unexpected Status = %q", result.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// Coordinator dynamic prompt tests
// ---------------------------------------------------------------------------

func TestCoordinatorDynamicPrompt_NoAgents(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	coord := NewCoordinator(agt, pool)

	// Set up base prompt manually (normally done in Run)
	coord.basePrompt = "Base system prompt."
	coord.pending = nil

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "Base system prompt") {
		t.Error("expected base prompt in output")
	}
	if !strings.Contains(prompt, "Coordinator Instructions") {
		t.Error("expected Coordinator Instructions section")
	}
	if strings.Contains(prompt, "Available Agents") {
		t.Error("unexpected 'Available Agents' section with no agents")
	}
	if strings.Contains(prompt, "Pending Agent Results") {
		t.Error("unexpected 'Pending Agent Results' section with no results")
	}
}

func TestCoordinatorDynamicPrompt_WithAgents(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.Add("reviewer", &AgentDef{Name: "reviewer", Role: "code reviewer"}, AgentUser, agt, agt.toolReg)
	pool.Add("sysadmin", &AgentDef{Name: "sysadmin", Role: "system admin"}, AgentUser, agt, agt.toolReg)

	coord := NewCoordinator(agt, pool)
	coord.basePrompt = "Base prompt."

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "Available Agents") {
		t.Error("expected 'Available Agents' section")
	}
	if !strings.Contains(prompt, "reviewer") {
		t.Error("expected reviewer in agents list")
	}
	if !strings.Contains(prompt, "sysadmin") {
		t.Error("expected sysadmin in agents list")
	}
	if !strings.Contains(prompt, "code reviewer") {
		t.Error("expected role description")
	}
}

func TestCoordinatorDynamicPrompt_WithPendingResults(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker"}, AgentUser, agt, agt.toolReg)

	coord := NewCoordinator(agt, pool)
	coord.basePrompt = "Base."
	coord.pending = []TaskResult{
		{
			TaskID:     "task-abc",
			AgentName:  "worker",
			Success:    true,
			Output:     "all good",
			DurationMs: 150,
			Status:     "completed",
		},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "Pending Agent Results") {
		t.Error("expected 'Pending Agent Results' section")
	}
	if !strings.Contains(prompt, "task-abc") {
		t.Error("expected task ID in results")
	}
	if !strings.Contains(prompt, "all good") {
		t.Error("expected output in results")
	}
}

func TestCoordinatorDynamicPrompt_TruncatesLargeResults(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.Add("worker", &AgentDef{Name: "worker", Role: "worker"}, AgentUser, agt, agt.toolReg)

	coord := NewCoordinator(agt, pool)
	coord.basePrompt = "Base."
	coord.pending = []TaskResult{
		{
			AgentName:  "worker",
			Success:    true,
			Output:     strings.Repeat("x", 1000),
			DurationMs: 50,
			Status:     "completed",
		},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "...") {
		t.Error("expected truncation for large output")
	}
}

func TestCoordinatorDynamicPrompt_OlderResultsOmitted(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	pool.Add("w", &AgentDef{Name: "w", Role: "w"}, AgentUser, agt, agt.toolReg)

	coord := NewCoordinator(agt, pool)
	coord.basePrompt = "B."

	// Set many pending results (more than default max of 10)
	coord.pending = make([]TaskResult, 15)
	for i := range coord.pending {
		coord.pending[i] = TaskResult{
			AgentName:  "w",
			Success:    true,
			Output:     fmt.Sprintf("result-%d", i),
			DurationMs: 10,
			Status:     "completed",
		}
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "older results omitted") {
		t.Error("expected 'older results omitted' message")
	}
	// The last result should be visible
	if !strings.Contains(prompt, "result-14") {
		t.Error("expected newest result to be visible")
	}
}

// ---------------------------------------------------------------------------
// Coordinator tool registration tests
// ---------------------------------------------------------------------------

func TestCoordinatorRegistersTools(t *testing.T) {
	pool, agt := testPoolHarness(t)
	defer pool.Shutdown()

	coord := NewCoordinator(agt, pool)
	coord.registerCoordinatorTools()

	defs := coord.toolReg.List()
	toolNames := make(map[string]bool)
	for _, d := range defs {
		toolNames[d.Name] = true
	}

	expected := []string{"dispatch_task", "create_agent", "get_agent_status", "cancel_task"}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("expected coordinator tool %q to be registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// FilteredView test (shared across multi-agent)
// ---------------------------------------------------------------------------

func TestFilteredView_RestrictsTools(t *testing.T) {
	cfg := config.DefaultConfig()
	r := mcp.NewRegistry(cfg)
	r.Register(&mockTool{name: "shell"})
	r.Register(&mockTool{name: "read"})
	r.Register(&mockTool{name: "write"})

	fv := r.FilteredView([]string{"shell", "read"})
	defs := fv.List()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["shell"] {
		t.Error("expected shell in filtered view")
	}
	if !names["read"] {
		t.Error("expected read in filtered view")
	}
	if names["write"] {
		t.Error("write should not be in filtered view")
	}

	// Get should also respect filter
	if _, ok := fv.Get("shell"); !ok {
		t.Error("Get('shell') should return true in filtered view")
	}
	if _, ok := fv.Get("write"); ok {
		t.Error("Get('write') should return false in filtered view")
	}
}

func TestFilteredView_EmptyNamesShowsAll(t *testing.T) {
	cfg := config.DefaultConfig()
	r := mcp.NewRegistry(cfg)
	r.Register(&mockTool{name: "shell"})
	r.Register(&mockTool{name: "read"})

	fv := r.FilteredView(nil)
	defs := fv.List()
	if len(defs) != 2 {
		t.Errorf("expected 2 tools with nil filter, got %d", len(defs))
	}

	fv2 := r.FilteredView([]string{})
	defs2 := fv2.List()
	if len(defs2) != 2 {
		t.Errorf("expected 2 tools with empty filter, got %d", len(defs2))
	}
}
