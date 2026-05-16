package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/i18n"
	"dolphin/internal/mcp"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/skill"
)

func TestMain(m *testing.M) {
	i18n.SetLang(i18n.EN)
	os.Exit(m.Run())
}

func TestReplayMessagesUserAssistant(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"hello"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"hi there"`)},
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"what time is it"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"12:00"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || string(msgs[0].Content) != `"hello"` {
		t.Errorf("msg[0] mismatch: role=%q content=%s", msgs[0].Role, string(msgs[0].Content))
	}
	if msgs[1].Role != "assistant" || string(msgs[1].Content) != `"hi there"` {
		t.Errorf("msg[1] mismatch: role=%q content=%s", msgs[1].Role, string(msgs[1].Content))
	}
}

func TestReplayMessagesWithToolResults(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"list files"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"running"},{"type":"tool_use","id":"tc1","name":"shell","input":{"command":"ls"}}]`)},
		{Type: session.EventToolResult, ToolName: "shell", ToolResult: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tc1","content":[{"type":"text","text":"file1.txt"}]}]`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"done"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msg[0] expected user")
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1] expected assistant")
	}
	// Tool result
	if msgs[2].Role != "tool" {
		t.Errorf("msg[2] expected tool, got %q", msgs[2].Role)
	}
	if msgs[3].Role != "assistant" {
		t.Errorf("msg[3] expected assistant")
	}
}

func TestReplayMessagesSkipsSystemAndToolCall(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventSystem, Content: json.RawMessage(`"system event"`)},
		{Type: session.EventToolCall, ToolName: "shell", ToolInput: json.RawMessage(`{"command":"date"}`)},
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"hello"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user message")
	}
}

func TestReplayMessagesSkipsEmptyContent(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user"}, // no content
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"ok"`)},
		{Type: session.EventToolResult}, // no result content
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected assistant message")
	}
}

func TestReplayMessagesEmptyInput(t *testing.T) {
	msgs := replayMessages(nil)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for nil input, got %d", len(msgs))
	}

	msgs = replayMessages([]session.SessionEvent{})
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty input, got %d", len(msgs))
	}
}

func TestReplayMessagesSkipsMessageWithoutRole(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Content: json.RawMessage(`"no role"`)}, // missing role
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"has role"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"}, // 1m30s rounds to 1m
		{5 * time.Minute, "5m"},
		{70 * time.Minute, "1h10m"}, // 1h10m
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCoordinatorToolDefinition(t *testing.T) {
	def := mcp.ToolDefinition{Name: "test"}
	tool := &handlerTool{def: def}
	if d := tool.Definition(); d.Name != "test" {
		t.Errorf("got %q", d.Name)
	}
}

func TestParseCommandName(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"/analyze-competitor huawei", "analyze-competitor"},
		{"/dev-run", "dev-run"},
		{"/review", "review"},
		{"just text", ""},
		{"", ""},
		{"/", ""},
		{"/   ", ""},
		{"normal text no slash", ""},
	}
	for _, tt := range tests {
		got := parseCommandName(tt.line)
		if got != tt.want {
			t.Errorf("parseCommandName(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestBuildDynamicPromptBase(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), PoolConfig{}))
	coord.basePrompt = "You are a helpful assistant."

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "You are a helpful assistant.") {
		t.Error("expected base prompt in dynamic prompt")
	}
	if !strings.Contains(prompt, "Coordinator Instructions") {
		t.Error("expected coordinator instructions")
	}
}

func TestBuildDynamicPromptWithAgents(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	agt := newTestAgent(cfg, &mockProvider{})
	pool := NewAgentPool(context.Background(), PoolConfig{})
	pool.Add("worker1", &AgentDef{Name: "worker1", Role: "worker"}, AgentUser, agt, agt.toolReg)
	pool.Add("worker2", &AgentDef{Name: "worker2", Role: "helper"}, AgentUser, agt, agt.toolReg)
	coord := NewCoordinator(agt, pool)
	coord.basePrompt = "base"

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "worker1") {
		t.Error("expected worker1 in dynamic prompt")
	}
	if !strings.Contains(prompt, "worker2") {
		t.Error("expected worker2 in dynamic prompt")
	}
	if !strings.Contains(prompt, "Available Agents") {
		t.Error("expected Available Agents section")
	}
}

func TestBuildDynamicPromptWithSkills(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), PoolConfig{}))

	skillDir := t.TempDir()
	os.WriteFile(filepath.Join(skillDir, "review.md"), []byte("---\nname: code-review\ndescription: Review code quality\n---\n# Content"), 0644)
	skillMgr := skill.NewManager(skillDir)
	skillMgr.Load()
	coord.SetSkillManager(skillMgr)
	coord.basePrompt = "base"

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "code-review") {
		t.Error("expected code-review skill in prompt, got:", prompt)
	}
	if !strings.Contains(prompt, "Available Skills") {
		t.Error("expected Available Skills section")
	}
}

func TestBuildDynamicPromptWithPendingResults(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), PoolConfig{}))
	coord.basePrompt = "base"

	coord.pending = []TaskResult{
		{AgentName: "worker1", TaskID: "t1", Success: true, Output: "done", Status: "completed", DurationMs: 100},
		{AgentName: "worker2", TaskID: "t2", Success: false, Error: "failed", Status: "error", DurationMs: 50},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "worker1") {
		t.Error("expected worker1 in pending results")
	}
	if !strings.Contains(prompt, "Pending Agent Results") {
		t.Error("expected Pending Agent Results section")
	}
}

func TestBuildDynamicPromptTruncatesLongResults(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.MaxPendingResultLen = 20
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), NewPoolConfigFromConfig(cfg.Pool)))
	coord.basePrompt = "base"

	longOutput := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 10) // 260 chars
	coord.pending = []TaskResult{
		{AgentName: "worker1", TaskID: "t1", Success: true, Output: longOutput, Status: "completed", DurationMs: 100},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, "...") {
		t.Error("expected truncated output with '...' suffix, got:", prompt)
	}
	if strings.Contains(prompt, longOutput) {
		t.Error("full long output should NOT be present when truncated")
	}
}

func TestBuildDynamicPromptNoTruncationWhenZero(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.MaxPendingResultLen = 0
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), NewPoolConfigFromConfig(cfg.Pool)))
	coord.basePrompt = "base"

	longOutput := strings.Repeat("x", 600) // longer than old hardcoded 500
	coord.pending = []TaskResult{
		{AgentName: "worker1", TaskID: "t1", Success: true, Output: longOutput, Status: "completed", DurationMs: 100},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, longOutput) {
		t.Error("full output should be present when MaxPendingResultLen=0 (no truncation)")
	}
}

func TestBuildDynamicPromptTruncationDoesNotAffectShortResults(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	cfg.Pool.MaxPendingResultLen = 100
	agt := newTestAgent(cfg, &mockProvider{})
	coord := NewCoordinator(agt, NewAgentPool(context.Background(), NewPoolConfigFromConfig(cfg.Pool)))
	coord.basePrompt = "base"

	shortOutput := "short result"
	coord.pending = []TaskResult{
		{AgentName: "worker1", TaskID: "t1", Success: true, Output: shortOutput, Status: "completed", DurationMs: 100},
	}

	prompt := coord.buildDynamicPrompt()
	if !strings.Contains(prompt, shortOutput) {
		t.Error("short output should be present in full")
	}
	if strings.Contains(prompt, shortOutput+"...") {
		t.Error("short output should NOT have '...' suffix")
	}
}

func TestCoordinatorRunExitCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)
	io := &mockIO{lines: []string{"/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "dolphin Coordinator Ready") {
		t.Error("expected welcome message, got:", output)
	}
}

func TestCoordinatorRunHelpCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "shell"})
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)
	io := &mockIO{lines: []string{"/help", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Commands:") {
		t.Error("expected Commands section in help")
	}
	if !strings.Contains(output, "/exit") {
		t.Error("expected /exit in help")
	}
	if !strings.Contains(output, "/skills") {
		t.Error("expected /skills in help")
	}
	if !strings.Contains(output, "/commands") {
		t.Error("expected /commands in help")
	}
	if !strings.Contains(output, "/agents") {
		t.Error("expected /agents in help")
	}
	if !strings.Contains(output, "shell") {
		t.Error("expected shell tool in help")
	}
}

func TestCoordinatorRunSkillsCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	skillDir := t.TempDir()
	os.WriteFile(filepath.Join(skillDir, "review.md"), []byte("---\nname: code-review\ndescription: Review code\n---\n# Content"), 0644)
	skillMgr := skill.NewManager(skillDir)
	skillMgr.Load()
	coord.SetSkillManager(skillMgr)

	io := &mockIO{lines: []string{"/skills", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "code-review") {
		t.Error("expected code-review in skills listing, got:", output)
	}
}

func TestCoordinatorRunCommandsListing(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	cmdDir := t.TempDir()
	os.WriteFile(filepath.Join(cmdDir, "review.md"), []byte("---\nname: review\ndescription: Review code\n---\n# Review instructions"), 0644)
	os.WriteFile(filepath.Join(cmdDir, "deploy.md"), []byte("---\nname: deploy\ndescription: Deploy app\n---\n# Deploy instructions"), 0644)
	cmdMgr := command.NewManager(cmdDir)
	cmdMgr.Load()
	coord.SetCommandManager(cmdMgr)

	io := &mockIO{lines: []string{"/commands", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "/review") {
		t.Error("expected /review in commands listing, got:", output)
	}
	if !strings.Contains(output, "/deploy") {
		t.Error("expected /deploy in commands listing, got:", output)
	}
	if !strings.Contains(output, "Review code") {
		t.Error("expected review description", output)
	}
	if !strings.Contains(output, "Deploy app") {
		t.Error("expected deploy description", output)
	}
}

func TestCoordinatorRunCustomCommandDispatchedToLLM(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{Content: TextContent("Code review results here"), Usage: &Usage{InputTokens: 10, OutputTokens: 20}},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	cmdDir := t.TempDir()
	os.WriteFile(filepath.Join(cmdDir, "review.md"), []byte("---\nname: review\ndescription: Review code\n---\n## Review Steps\n1. Check logic\n2. Check performance"), 0644)
	cmdMgr := command.NewManager(cmdDir)
	cmdMgr.Load()
	coord.SetCommandManager(cmdMgr)

	io := &mockIO{lines: []string{"/review main.go", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Code review results") {
		t.Error("expected LLM response for custom command, got:", output)
	}
	cmd, _ := cmdMgr.Get("review")
	if cmd.CallCount != 1 {
		t.Errorf("expected command call count 1, got %d", cmd.CallCount)
	}
}

func TestCoordinatorRunUnknownSlashFallsThrough(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{Content: TextContent("I don't know this command"), Usage: &Usage{InputTokens: 5, OutputTokens: 10}},
		},
	}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	io := &mockIO{lines: []string{"/unknown", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "don't know") {
		t.Error("expected LLM to handle unknown /command, got:", output)
	}
}

func TestCoordinatorRunCancelAllTasks(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)
	io := &mockIO{lines: []string{"/cancel", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "All running tasks cancelled") {
		t.Error("expected cancel message, got:", output)
	}
}

func TestCoordinatorSetCommandManager(t *testing.T) {
	cmdMgr := command.NewManager()
	c := &Coordinator{}
	c.SetCommandManager(cmdMgr)
	if c.commands != cmdMgr {
		t.Error("SetCommandManager failed")
	}
}

func TestCoordinatorSetSkillManager(t *testing.T) {
	skillMgr := skill.NewManager()
	c := &Coordinator{}
	c.SetSkillManager(skillMgr)
	if c.skills != skillMgr {
		t.Error("SetSkillManager failed")
	}
}

func TestCoordinatorPrintSkillsNil(t *testing.T) {
	io := &mockIO{}
	c := &Coordinator{}
	c.printSkills(io)
	output := io.writes.String()
	if !strings.Contains(output, "Skills system not available") {
		t.Error("expected not available message, got:", output)
	}
}

func TestCoordinatorPrintCommandsNil(t *testing.T) {
	io := &mockIO{}
	c := &Coordinator{}
	c.printCommands(io)
	output := io.writes.String()
	if !strings.Contains(output, "Commands system not available") {
		t.Error("expected not available message, got:", output)
	}
}

func TestCoordinatorPrintCommandsEmpty(t *testing.T) {
	cmdDir := t.TempDir()
	cmdMgr := command.NewManager(cmdDir)
	cmdMgr.Load()
	io := &mockIO{}
	c := &Coordinator{}
	c.SetCommandManager(cmdMgr)
	c.printCommands(io)
	output := io.writes.String()
	if !strings.Contains(output, "No user-defined commands") {
		t.Error("expected empty message, got:", output)
	}
}

func TestCoordinatorCancelSpecificTask(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)
	io := &mockIO{lines: []string{"/cancel nonexistent-id", "/exit"}}

	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "No running task found") {
		t.Error("expected not found message, got:", output)
	}
}

func TestCoordinatorSetCronManager(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()
	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	if c.cronMgr != cronMgr {
		t.Error("SetCronManager failed")
	}
}

func TestCoordinatorPrintCronTasksNoManager(t *testing.T) {
	io := &mockIO{}
	c := &Coordinator{}
	c.printCronTasks(io)
	output := io.writes.String()
	if !strings.Contains(output, "Cron scheduler not available") {
		t.Error("expected not available message, got:", output)
	}
}

func TestCoordinatorPrintCronTasksEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	io := &mockIO{}
	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	c.printCronTasks(io)
	output := io.writes.String()
	if !strings.Contains(output, "No scheduled tasks") {
		t.Error("expected no tasks message, got:", output)
	}
}

func TestCoordinatorPrintCronTasksPopulated(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	// Add a cron task directly
	cronMgr.AddTask(&scheduler.CronTask{
		Name:     "test-task",
		Schedule: "0 9 * * *",
		Task:     "do something",
		Enabled:  true,
	})

	io := &mockIO{}
	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	c.printCronTasks(io)
	output := io.writes.String()
	if !strings.Contains(output, "test-task") {
		t.Error("expected test-task in cron listing, got:", output)
	}
	if !strings.Contains(output, "0 9") {
		t.Error("expected schedule in cron listing, got:", output)
	}
	if !strings.Contains(output, "enabled") {
		t.Error("expected enabled status, got:", output)
	}
}

func TestProcessDueTasksContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dueCh := make(chan scheduler.CronTask)
	c := &Coordinator{}
	// Should return immediately without panic
	c.processDueTasks(ctx, dueCh, "")
}

func TestCoordinatorPrintAgentsEmpty(t *testing.T) {
	io := &mockIO{}
	pool := NewAgentPool(context.Background(), PoolConfig{})
	c := &Coordinator{pool: pool}
	c.printAgents(io)
	output := io.writes.String()
	if !strings.Contains(output, "No agents configured") {
		t.Error("expected no agents message, got:", output)
	}
}

func TestCoordinatorPrintAgentsPopulated(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	agt := newTestAgent(cfg, &mockProvider{})
	pool := NewAgentPool(context.Background(), PoolConfig{})
	pool.Add("worker1", &AgentDef{Name: "worker1", Role: "test agent"}, AgentUser, agt, agt.toolReg)

	io := &mockIO{}
	c := &Coordinator{pool: pool}
	c.printAgents(io)
	output := io.writes.String()
	if !strings.Contains(output, "worker1") {
		t.Error("expected worker1 in agents listing, got:", output)
	}
	if !strings.Contains(output, "AGENT") {
		t.Error("expected header in agents listing, got:", output)
	}
}

func TestCoordinatorHandleSearchMCPToolsNoResults(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "shell"})

	agt := &Agent{cfg: cfg, toolReg: toolReg, ctxBuilder: NewContextBuilder()}
	c := NewCoordinator(agt, NewAgentPool(context.Background(), PoolConfig{}))
	result, err := c.handleSearchMCPTools(context.Background(), json.RawMessage(`{"query":"nonexistent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "No MCP tools found") {
		t.Error("expected no tools found, got:", result.Content)
	}
}

func TestCoordinatorHandleSearchMCPToolsWithResults(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000
	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "shell"})
	toolReg.Register(&mockTool{name: "read_file"})

	agt := &Agent{cfg: cfg, toolReg: toolReg, ctxBuilder: NewContextBuilder()}
	c := NewCoordinator(agt, NewAgentPool(context.Background(), PoolConfig{}))
	result, err := c.handleSearchMCPTools(context.Background(), json.RawMessage(`{"query":"shell"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "shell") {
		t.Error("expected shell in results, got:", result.Content)
	}
}

func TestCoordinatorHandleListCronTasksNoManager(t *testing.T) {
	c := &Coordinator{}
	result, err := c.handleListCronTasks(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when cronMgr is nil")
	}
}

func TestCoordinatorHandleToggleCronTaskNoManager(t *testing.T) {
	c := &Coordinator{}
	result, err := c.handleToggleCronTask(context.Background(), json.RawMessage(`{"name":"test","enabled":true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when cronMgr is nil")
	}
}

func TestCoordinatorHandleToggleCronTaskInvalidInput(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	c := &Coordinator{cronMgr: cronMgr}
	result, err := c.handleToggleCronTask(context.Background(), json.RawMessage(`invalid json`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid input")
	}
}

func TestCoordinatorHandleCancelTaskInvalidInput(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	c := &Coordinator{pool: pool}
	result, err := c.handleCancelTask(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid input")
	}
}

func TestCoordinatorHandleCancelTaskNotFound(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	c := &Coordinator{pool: pool}
	result, err := c.handleCancelTask(context.Background(), json.RawMessage(`{"task_id":"nonexistent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent task")
	}
	if !strings.Contains(result.Content, "No running task") {
		t.Error("expected no running task message, got:", result.Content)
	}
}

func TestCoordinatorHandleSearchSkillsNoManager(t *testing.T) {
	c := &Coordinator{}
	result, err := c.handleSearchSkills(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when skills is nil")
	}
}

func TestCoordinatorHandleSearchSkillsNoResults(t *testing.T) {
	skillMgr := skill.NewManager(t.TempDir())
	skillMgr.Load()

	c := &Coordinator{skills: skillMgr}
	result, err := c.handleSearchSkills(context.Background(), json.RawMessage(`{"query":"nonexistent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "No skills found") {
		t.Error("expected no skills found, got:", result.Content)
	}
}

func TestCoordinatorHandleLoadSkillNoManager(t *testing.T) {
	c := &Coordinator{}
	result, err := c.handleLoadSkill(context.Background(), json.RawMessage(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when skills is nil")
	}
}

func TestCoordinatorHandleLoadSkillNotFound(t *testing.T) {
	skillMgr := skill.NewManager(t.TempDir())
	skillMgr.Load()

	c := &Coordinator{skills: skillMgr}
	result, err := c.handleLoadSkill(context.Background(), json.RawMessage(`{"name":"nonexistent"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent skill")
	}
}

func TestCoordinatorPrintCronTasksDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	cronMgr.AddTask(&scheduler.CronTask{
		Name:     "disabled-task",
		Schedule: "0 9 * * *",
		Task:     "do something",
		Enabled:  false,
	})

	io := &mockIO{}
	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	c.printCronTasks(io)
	output := io.writes.String()
	if !strings.Contains(output, "disabled-task") {
		t.Error("expected disabled-task in cron listing, got:", output)
	}
	if !strings.Contains(output, "disabled") {
		t.Error("expected disabled status, got:", output)
	}
}

func TestCoordinatorHandleListCronTasksEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	result, err := c.handleListCronTasks(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "No scheduled tasks") {
		t.Error("expected empty message, got:", result.Content)
	}
}

func TestCoordinatorHandleListCronTasksPopulated(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()
	cronMgr.AddTask(&scheduler.CronTask{
		Name:     "daily-task",
		Schedule: "0 9 * * *",
		Task:     "do work",
		Enabled:  true,
	})

	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	result, err := c.handleListCronTasks(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "daily-task") {
		t.Error("expected daily-task in list, got:", result.Content)
	}
	if !strings.Contains(result.Content, "enabled") {
		t.Error("expected enabled status, got:", result.Content)
	}
}

func TestCoordinatorHandleToggleCronTaskToggleOff(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()
	cronMgr.AddTask(&scheduler.CronTask{
		Name:     "test-toggle",
		Schedule: "0 9 * * *",
		Task:     "do work",
		Enabled:  true,
	})

	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	result, err := c.handleToggleCronTask(context.Background(), json.RawMessage(`{"name":"test-toggle","enabled":false}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "disabled") {
		t.Error("expected disabled message, got:", result.Content)
	}
}

func TestCoordinatorHandleAddCronTask(t *testing.T) {
	cfg := config.DefaultConfig()
	cronDir := t.TempDir()
	cfg.Crontab.File = filepath.Join(cronDir, "CRONTAB.md")
	cronMgr := scheduler.NewManager(cfg.Crontab)
	cronMgr.Load()

	c := &Coordinator{}
	c.SetCronManager(cronMgr)
	result, err := c.handleAddCronTask(context.Background(), json.RawMessage(`{"name":"daily","schedule":"0 9 * * *","description":"daily job","task":"do the thing"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "Scheduled task") {
		t.Error("expected success message, got:", result.Content)
	}
}

func TestCoordinatorHandleAddCronTaskNoManager(t *testing.T) {
	c := &Coordinator{}
	result, err := c.handleAddCronTask(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when cronMgr is nil")
	}
}

func TestCoordinatorToolExecute(t *testing.T) {
	executed := false
	tool := &handlerTool{
		handler: func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			executed = true
			return &mcp.ToolResult{Content: "ok"}, nil
		},
	}
	_, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !executed {
		t.Error("handler was not called")
	}
}

func TestFormatDurationEdgeCases(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
		{-5 * time.Minute, "-300s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// --- E2E: Summary lifecycle ---

// TestE2ERunTaskGeneratesSummary verifies that a sub-agent task via RunTask
// generates its own summary file linked to the parent session.
func TestE2ERunTaskGeneratesSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("sub-agent result"),
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	result, err := agt.RunTask(
		context.Background(),
		"analyze this",
		"sub-agent system prompt",
		agt.toolReg,
		"parent-session-123",
	)
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want completed", result.Status)
	}

	// Verify child summary file exists
	summaryPath := filepath.Join(config.SessionsDir(), result.TaskID+"-summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("ReadFile child summary: %v", err)
	}
	var sum map[string]any
	json.Unmarshal(data, &sum)
	if sum["state"] != "completed" {
		t.Errorf("child state = %v, want completed", sum["state"])
	}
}

// TestE2EParentChildSummaryChain verifies that both parent and child sessions
// produce their own summary files, and the child JSONL contains the parent link.
func TestE2EParentChildSummaryChain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	agt := newTestAgent(cfg, &mockProvider{
		responses: []*ProviderResponse{
			{Content: TextContent("child done"), Usage: &Usage{InputTokens: 5, OutputTokens: 3}, StopReason: "end_turn"},
		},
	})

	// Create parent session
	parentSess, _ := agt.sessMgr.NewSession(50)
	defer parentSess.Close()

	// Run a child task linked to parent
	result, err := agt.RunTask(
		context.Background(),
		"child task",
		"child prompt",
		agt.toolReg,
		parentSess.ID,
	)
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	// Generate parent summary
	parentState := &LoopState{
		Sess:          parentSess,
		Turn:          2,
		ToolCallCount: 1,
		StopReason:    "user_exit",
	}
	agt.generateSummary(parentSess, parentState)

	// Verify both summary files exist
	parentSummaryPath := filepath.Join(config.SessionsDir(), string(parentSess.ID)+"-summary.json")
	childSummaryPath := filepath.Join(config.SessionsDir(), result.TaskID+"-summary.json")

	if _, err := os.Stat(parentSummaryPath); os.IsNotExist(err) {
		t.Error("parent summary file missing")
	}
	if _, err := os.Stat(childSummaryPath); os.IsNotExist(err) {
		t.Error("child summary file missing")
	}

	// Verify parent summary content
	pData, _ := os.ReadFile(parentSummaryPath)
	var pSum map[string]any
	json.Unmarshal(pData, &pSum)
	if pSum["state"] != "user_exit" {
		t.Errorf("parent state = %v", pSum["state"])
	}

	// Verify child summary content
	cData, _ := os.ReadFile(childSummaryPath)
	var cSum map[string]any
	json.Unmarshal(cData, &cSum)
	if cSum["state"] != "completed" {
		t.Errorf("child state = %v", cSum["state"])
	}

	// Verify child JSONL contains parent link
	childJSONL := filepath.Join(config.SessionsDir(), result.TaskID+".jsonl")
	jData, _ := os.ReadFile(childJSONL)
	if !strings.Contains(string(jData), string(parentSess.ID)) {
		t.Error("child JSONL should reference parent session ID")
	}
}

// TestE2ETransportErrorSkipsSummary confirms the generateSummary short-circuit:
// transport_error + 0 turns + 0 tool calls → no file written.
func TestE2ETransportErrorSkipsSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Summary = true
	config.SetSessionsDir(t.TempDir())

	agt := newTestAgent(cfg, &mockProvider{})
	sess, _ := agt.sessMgr.NewSession(50)
	defer sess.Close()

	// Simulate: transport disconnected before any activity
	state := &LoopState{
		Sess:          sess,
		Turn:          0,
		ToolCallCount: 0,
		StopReason:    "transport_error",
	}
	agt.generateSummary(sess, state)

	// Should NOT have written a summary
	summaryPath := filepath.Join(config.SessionsDir(), string(sess.ID)+"-summary.json")
	if _, err := os.Stat(summaryPath); !os.IsNotExist(err) {
		t.Error("expected no summary for transport_error with 0 activity")
	}
}

func TestConfigToolList(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]string{"action": "list"})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "llm.temperature") {
		t.Error("expected llm.temperature in list output")
	}
	if !strings.Contains(result.Content, "mcp.shell.timeout_seconds") {
		t.Error("expected mcp.shell.timeout_seconds in list output")
	}
}

func TestConfigToolGet(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxSubTurns = 7
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]string{"action": "get", "path": "llm.max_sub_turns"})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "7") {
		t.Errorf("expected value 7 in output, got: %s", result.Content)
	}
}

func TestConfigToolGetInvalidPath(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]string{"action": "get", "path": "nonexistent.path"})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid path")
	}
}

func TestConfigToolSetInt(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.TimeoutSeconds = 30
	c := &Coordinator{Agent: &Agent{cfg: cfg}}

	input, _ := json.Marshal(map[string]any{
		"action": "set",
		"path":   "mcp.shell.timeout_seconds",
		"value":  60,
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if cfg.MCP.Shell.TimeoutSeconds != 60 {
		t.Errorf("expected 60, got %d", cfg.MCP.Shell.TimeoutSeconds)
	}
}

func TestConfigToolSetBool(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.Headless = false
	c := &Coordinator{Agent: &Agent{cfg: cfg}}

	input, _ := json.Marshal(map[string]any{
		"action": "set",
		"path":   "mcp.cdp.headless",
		"value":  true,
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !cfg.MCP.CDP.Headless {
		t.Error("expected Headless to be true")
	}
}

func TestConfigToolSetString(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.CompressMode = "drop"
	c := &Coordinator{Agent: &Agent{cfg: cfg, compressor: &DropCompressor{}}}

	input, _ := json.Marshal(map[string]any{
		"action": "set",
		"path":   "llm.compress_mode",
		"value":  "summarize",
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if cfg.LLM.CompressMode != "summarize" {
		t.Errorf("expected summarize, got %q", cfg.LLM.CompressMode)
	}
}

func TestConfigToolSetInvalidPath(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]any{
		"action": "set",
		"path":   "mcp.nonexistent",
		"value":  1,
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid path")
	}
}

func TestConfigToolSetWrongType(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]any{
		"action": "set",
		"path":   "mcp.shell.enabled",
		"value":  "not_a_boolean",
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for wrong type")
	}
}

func TestConfigToolSave(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.TimeoutSeconds = 99
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	// Save to project config path (within allowed config dir)
	savePath := filepath.Join(config.ProjectConfigDir, "test-"+config.ConfigFileName+".yaml")

	input, _ := json.Marshal(map[string]string{
		"action": "save",
		"file":   savePath,
	})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	defer os.Remove(savePath)

	// Read back and verify
	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "99") {
		t.Errorf("saved config should contain 99, got: %s", string(data))
	}
}

func TestConfigToolUnknownAction(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]string{"action": "invalid"})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for unknown action")
	}
}

func TestCoordinatorHandleSkillNewNilManager(t *testing.T) {
	io := &mockIO{}
	c := &Coordinator{}
	c.handleSkillNew("/skills new my-skill", io)
	output := io.writes.String()
	if !strings.Contains(output, i18n.T(i18n.KeySkillsNotAvail, i18n.EN)) {
		t.Error("expected not available message, got:", output)
	}
}

func TestCoordinatorHandleSkillNewNoName(t *testing.T) {
	skillDir := t.TempDir()
	skillMgr := skill.NewManager(skillDir)
	c := &Coordinator{}
	c.SetSkillManager(skillMgr)

	io := &mockIO{}
	c.handleSkillNew("/skills new", io)
	output := io.writes.String()
	if !strings.Contains(output, "Usage:") {
		t.Error("expected usage message, got:", output)
	}
}

func TestCoordinatorHandleSkillNewCreatesFile(t *testing.T) {
	skillDir := t.TempDir()
	skillMgr := skill.NewManager(skillDir)
	c := &Coordinator{}
	c.SetSkillManager(skillMgr)

	io := &mockIO{}
	c.handleSkillNew("/skills new custom-review", io)
	output := io.writes.String()
	if !strings.Contains(output, "Created") {
		t.Error("expected Created message, got:", output)
	}
	if !strings.Contains(output, skillDir) {
		t.Error("expected skill dir path in output, got:", output)
	}

	content, err := os.ReadFile(filepath.Join(skillDir, "custom-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "name: custom-review") {
		t.Error("frontmatter missing name")
	}
	if !strings.Contains(s, "## Overview") {
		t.Error("missing template sections")
	}
}

func TestCoordinatorSkillNewViaRun(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 50
	cfg.LLM.MaxContextTokens = 100000

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	skillDir := t.TempDir()
	skillMgr := skill.NewManager(skillDir)
	skillMgr.Load()
	coord.SetSkillManager(skillMgr)

	io := &mockIO{lines: []string{"/skills new my-test-skill", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Created") {
		t.Error("expected Created in output, got:", output)
	}
	if !strings.Contains(output, "my-test-skill") {
		t.Error("expected skill name in output, got:", output)
	}

	if _, err := os.Stat(filepath.Join(skillDir, "my-test-skill", "SKILL.md")); os.IsNotExist(err) {
		t.Error("skill file was not created")
	}
}

func TestCoordinatorStatusCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.Model = "test-model"

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	skillDir := t.TempDir()
	skillMgr := skill.NewManager(skillDir)
	skillMgr.Register("test-skill", "test", "# Test")
	coord.SetSkillManager(skillMgr)

	coord.SetCommandManager(command.NewManager(t.TempDir()))

	io := &mockIO{lines: []string{"/status", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Status:") {
		t.Error("expected Status header, got:", output)
	}
	if !strings.Contains(output, "mock") {
		t.Error("expected provider name, got:", output)
	}
	if !strings.Contains(output, "test-model") {
		t.Error("expected model name, got:", output)
	}
	if !strings.Contains(output, "Skills:") {
		t.Error("expected skills count, got:", output)
	}
	if !strings.Contains(output, "Commands:") {
		t.Error("expected commands count, got:", output)
	}
	if !strings.Contains(output, "Memory:") {
		t.Error("expected memory, got:", output)
	}
}

func TestCoordinatorStatusCommandMinimal(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.Model = "minimal-model"

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	io := &mockIO{lines: []string{"/status", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Status:") {
		t.Error("expected Status header, got:", output)
	}
	if !strings.Contains(output, "Agents:") {
		t.Error("expected agents count, got:", output)
	}
	if !strings.Contains(output, "MCP tools:") {
		t.Error("expected MCP tools count, got:", output)
	}
}

func TestCoordinatorSessionsCommandNoSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	io := &mockIO{lines: []string{"/sessions", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	// Run() creates a session, so there's always at least 1. The "no sessions"
	// path requires an empty directory before the run — here we verify the
	// sessions command works and shows the self-created session.
	if !strings.Contains(output, "Sessions") {
		t.Error("expected Sessions header, got:", output)
	}
}

func TestCoordinatorSessionsCommandWithSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	// Create a couple of session files
	for i := 0; i < 2; i++ {
		sess, err := sessMgr.NewSession(50)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		sess.LogMessage("user", json.RawMessage(`"hello"`))
		sess.LogMessage("assistant", json.RawMessage(`"hi"`))
		sess.Close()
	}

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	io := &mockIO{lines: []string{"/sessions", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "Sessions") {
		t.Error("expected Sessions header, got:", output)
	}
	if !strings.Contains(output, "turns") {
		t.Error("expected turns in output, got:", output)
	}
}

func TestCoordinatorHelpIncludesStatusAndSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()

	toolReg := mcp.NewRegistry(cfg)
	prov := &mockProvider{}

	agt := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   prov,
		ctxBuilder: NewContextBuilder(),
	}

	pool := NewAgentPool(context.Background(), PoolConfig{})
	coord := NewCoordinator(agt, pool)

	io := &mockIO{lines: []string{"/help", "/exit"}}
	coord.Run(context.Background(), io)

	output := io.writes.String()
	if !strings.Contains(output, "/status") {
		t.Error("expected /status in help, got:", output)
	}
	if !strings.Contains(output, "/sessions") {
		t.Error("expected /sessions in help, got:", output)
	}
}

func TestConfigToolListShowsAllRegisteredPaths(t *testing.T) {
	cfg := config.DefaultConfig()
	c := &Coordinator{Agent: &Agent{cfg: cfg}}
	input, _ := json.Marshal(map[string]string{"action": "list"})
	result, err := c.handleConfig(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range configurablePaths {
		if !strings.Contains(result.Content, entry.path) {
			t.Errorf("path %q not found in list output", entry.path)
		}
	}
}
