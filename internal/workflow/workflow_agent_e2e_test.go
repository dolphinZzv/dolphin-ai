//go:build e2e
// +build e2e

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	_ "dolphin/internal/llm/deepseek"
	_ "dolphin/internal/llm/volcengine"
	"dolphin/internal/memory"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

// e2eSessionStore is a lightweight session store.
type e2eSessionStore struct {
	sessions map[string]*session.Session
}

func (s *e2eSessionStore) Get(id string) *session.Session {
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess := &session.Session{ID: id}
	if s.sessions == nil {
		s.sessions = make(map[string]*session.Session)
	}
	s.sessions[id] = sess
	return sess
}

// soulSection injects the SOUL.md guidance into the system prompt.
type soulSection struct {
	content string
}

func (s *soulSection) Name() string                                   { return "soul" }
func (s *soulSection) Index() int                                     { return 50 }
func (s *soulSection) BuildContent(_ context.Context) (string, error) { return s.content, nil }

// TestAgentWorkflowDAG verifies the agent autonomously creates a .workflow.yaml
// and calls run_workflow for a complex multi-step DAG task.
func TestAgentWorkflowDAG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping autonomous workflow DAG e2e test in short mode")
	}

	logger, _ := zap.NewDevelopment()

	// 1. Load config.
	cfg, err := loadE2EConfig()
	if err != nil {
		t.Skipf("config.yaml not found, skipping: %v", err)
	}
	cfg.Set("agent.workmode", "yolo")
	cfg.Set("agent.pool_size", 4)
	cfg.Set("agent.max_rounds", 30)

	// 2. Create LLM provider.
	provider := createE2EProvider(cfg, logger)

	// 3. Create tool registry.
	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()

	// Register shell tool.
	toolReg.RegisterBuiltin(
		"shell",
		"Run a shell command and return its output. Args: {command: string, timeout?: number}",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The shell command to execute"},
				"timeout": {"type": "number", "description": "Optional timeout in seconds"}
			},
			"required": ["command"]
		}`),
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var params struct {
				Command string  `json:"command"`
				Timeout float64 `json:"timeout"`
			}
			json.Unmarshal(args, &params)
			timeout := 30 * time.Second
			if params.Timeout > 0 {
				timeout = time.Duration(params.Timeout * float64(time.Second))
			}
			cmdCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(cmdCtx, "sh", "-c", params.Command)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return &types.ToolResult{Content: fmt.Sprintf("error: %v\noutput: %s", err, string(out))}, nil
			}
			return &types.ToolResult{Content: string(out)}, nil
		},
	)

	// Register run_workflow + continue_workflow tools.
	engine := NewEngine(toolReg, provider, eventBus, logger, nil, cfg)
	RegisterTools(toolReg, engine, nil, nil)

	// 4. Read SOUL.md for system prompt guidance.
	soulContent := "Be concise and direct."
	if data, err := os.ReadFile(filepath.Join(cfg.GetString("agent.workspace"), "SOUL.md")); err == nil {
		soulContent = string(data)
	}

	// 5. Create test transport.
	tt := transport.NewTestTransport("test")

	// 6. Set up AgentIO.
	sigBus := signal.NewBus()
	sessMgr := session.NewManager(t.TempDir())
	aio := agentio.NewAgentIO(32, sessMgr, sigBus, logger, "test-agent")
	aio.RegisterTransport(tt.ID(), tt)

	// 7. Set up memory.
	mem := memory.NewFileMemory(&e2eSessionStore{})

	// 8. Build compositor with SOUL.md injected.
	ctxBuilder := &agentloop.ContextBuilderStage{
		Workspace: ".",
		Workmode:  "yolo",
		EventBus:  eventBus,
	}
	ctxBuilder.RegisterSection(&soulSection{content: soulContent})

	compositor := agentloop.NewCompositor(
		[]agentloop.Stage{
			&agentloop.MemoryReadStage{Memory: mem},
			ctxBuilder,
		},
		[]agentloop.Stage{
			&agentloop.LLMStage{
				Provider:     provider,
				MaxTokens:    4096,
				MaxRetries:   3,
				ToolRegistry: toolReg,
				EventBus:     eventBus,
				Logger:       logger,
			},
			&agentloop.ToolStage{
				ToolRegistry: toolReg,
				SignalBus:    sigBus,
				Timeout:      30 * time.Second,
				Logger:       logger,
				EventBus:     eventBus,
				Workmode:     "yolo",
			},
			&agentloop.MemoryWriteStage{
				Memory:   mem,
				EventBus: eventBus,
			},
		},
		30,
	)

	// 9. Create AgentLoop.
	loop := agentloop.NewAgentLoop(aio.Queue(), compositor, logger, eventBus, aio, 1)

	// Track everything.
	var progressMsgs []string
	var toolCalls []string
	type toolResult struct {
		Name    string
		Content string
	}
	var toolResults []toolResult
	loopDone := make(chan struct{})

	loop.SetOnResult(func(r agentio.TurnResult) {
		if r.Text != "" {
			progressMsgs = append(progressMsgs, r.Text)
		}
		if r.ToolCall != nil {
			toolCalls = append(toolCalls, r.ToolCall.Name)
		}
		if r.ToolResult != nil {
			toolResults = append(toolResults, toolResult{
				Name:    r.ToolResult.ToolCallID,
				Content: r.ToolResult.Content,
			})
		}
		if r.Done {
			close(loopDone)
		}
	})

	// 10. Run and send the complex multi-step DAG prompt.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	go loop.Run(ctx)

	prompt := `请创建 .workflow.yaml 文件并用 run_workflow 执行，来并行分析三个网站的响应速度。

workflow DAG 结构要求（必须有 depends_on 形成依赖关系）：

阶段A（3个并行步骤，无依赖）：
  - step_a1: curl 测 https://www.baidu.com 的 HTTP 响应时间，输出 JSON: {site, time_seconds}
  - step_a2: curl 测 https://www.google.com 的 HTTP 响应时间，输出 JSON: {site, time_seconds}
  - step_a3: curl 测 https://www.bing.com 的 HTTP 响应时间，输出 JSON: {site, time_seconds}

阶段B（3个并行步骤，分别依赖阶段A的对应步骤）：
  - step_b1: depends_on=[step_a1], ping -c 2 www.baidu.com 测延迟，输出 JSON: {site, ping_ms}
  - step_b2: depends_on=[step_a2], ping -c 2 www.google.com 测延迟，输出 JSON: {site, ping_ms}
  - step_b3: depends_on=[step_a3], ping -c 2 www.bing.com 测延迟，输出 JSON: {site, ping_ms}

阶段C（1个汇总步骤，依赖全部阶段B）：
  - summarize: depends_on=[step_b1,step_b2,step_b3], 比较所有结果判断哪个最快，输出 JSON: {fastest, ranking: [...]}

要求：
1. 每个步骤的 prompt 简洁明确，curl 命令直接用英文描述即可
2. 每个步骤都要有 output_schema
3. summarize 步骤用模板变量 $step_b1.ping_ms 等引用上游数据
4. 先 shell 写文件，再 run_workflow 执行`

	aio.SendTurn(ctx, &agentio.Turn{
		TurnID:      "turn-2",
		Input:       prompt,
		TransportID: tt.ID(),
	})

	// 11. Wait for completion.
	select {
	case <-loopDone:
		t.Log("agent completed turn")
	case <-ctx.Done():
		t.Logf("timeout waiting for agent: %v", ctx.Err())
	}
	cancel()

	// 12. Analyze results.
	output := tt.Output()
	t.Logf("=== Agent output ===")
	t.Log(output)

	fullText := strings.Join(progressMsgs, "")
	t.Logf("=== Full response text (%d chars) ===", len(fullText))
	t.Log(fullText)

	t.Logf("tool calls: %v", toolCalls)
	for _, tr := range toolResults {
		t.Logf("  tool result [%s]: %.200s", tr.Name, tr.Content)
	}

	hasShell := false
	hasRunWorkflow := false
	for _, tc := range toolCalls {
		switch tc {
		case "shell":
			hasShell = true
		case "run_workflow":
			hasRunWorkflow = true
		}
	}

	t.Logf("shell used: %v, run_workflow used: %v", hasShell, hasRunWorkflow)

	// Primary assertion: the agent must use run_workflow for this complex DAG task.
	if !hasRunWorkflow {
		t.Error("agent should use run_workflow for a multi-stage DAG task with dependencies")
	}

	// Verify the output contains the expected analysis.
	if !strings.Contains(fullText, "baidu") {
		t.Error("output should mention baidu")
	}
}

func loadE2EConfig() (*config.Config, error) {
	paths := []string{"../../config.yaml", "../../../config.yaml", "config.yaml"}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return config.LoadConfig(p)
		}
	}
	return nil, os.ErrNotExist
}

func createE2EProvider(cfg *config.Config, logger *zap.Logger) llm.Provider {
	provider := llm.NewProvider(llm.Config{
		Provider:   "volcengine_agent",
		Vendor:     cfg.GetString("llm.volcengine_agent.provider"),
		APIType:    cfg.GetString("llm.volcengine_agent.api_type"),
		APIKey:     cfg.GetString("llm.volcengine_agent.api_key"),
		BaseURL:    cfg.GetString("llm.volcengine_agent.base_url"),
		MaxTokens:  cfg.GetInt("llm.max_tokens"),
		MaxRetries: cfg.GetInt("llm.max_retries"),
		Timeout:    cfg.GetDuration("llm.timeout"),
		Headers:    cfg.GetStringMap("llm.volcengine_agent.headers"),
		Models: []llm.ModelConfig{
			{
				Name:        "deepseek-v4-flash",
				Provider:    "volcengine_agent",
				Vendor:      "volcengine",
				Model:       "deepseek-v4-flash",
				APIType:     "openai",
				MaxTokens:   4096,
				MaxRetries:  3,
				Timeout:     120 * time.Second,
				Temperature: 0,
			},
		},
	}, logger)

	mgr := llm.NewManager()
	mgr.AddProvider("volcengine_agent", provider)
	if err := mgr.SetActiveModel("deepseek-v4-flash"); err != nil {
		logger.Warn("failed to set active model", zap.Error(err))
	}
	return mgr
}
