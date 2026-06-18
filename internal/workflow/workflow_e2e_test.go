//go:build e2e
// +build e2e

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	_ "dolphin/internal/llm/deepseek"
	_ "dolphin/internal/llm/volcengine"
	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// loadConfig loads the project config.yaml. Falls back to env vars.
func loadConfig() (*config.Config, error) {
	paths := []string{"config.yaml", "../../config.yaml", "../../../config.yaml"}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return config.LoadConfig(p)
		}
	}
	return nil, os.ErrNotExist
}

// createVolcengineProvider creates a Manager with the volcengine_agent provider.
func createVolcengineProvider(cfg *config.Config, logger *zap.Logger) llm.Provider {
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
				Timeout:     60 * time.Second,
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

// createE2EEngine builds an Engine with a real LLM provider.
func createE2EEngine(t *testing.T) (*Engine, *config.Config, *zap.Logger) {
	t.Helper()

	logger, _ := zap.NewDevelopment()

	cfg, err := loadConfig()
	if err != nil {
		t.Skipf("config.yaml not found, skipping E2E test: %v", err)
	}

	provider := createVolcengineProvider(cfg, logger)
	if provider == nil {
		t.Skip("could not create volcengine provider")
	}

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()

	engine := NewEngine(toolReg, provider, eventBus, logger, nil, cfg)

	return engine, cfg, logger
}

// ---------------------------------------------------------------------------
// E2E: Single step — LLM outputs structured JSON
// ---------------------------------------------------------------------------

func TestE2ESingleStepJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: single step with JSON output", t, func() {
		engine, _, _ := createE2EEngine(t)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_single_json",
			Steps: []StepSpec{
				{
					ID:           "greet",
					Prompt:       "请输出一个 JSON 对象，包含两个字段：language（值为 'Go'）和 year（值为 2024）。只输出 JSON，不要包含其他文字。",
					OutputSchema: map[string]any{"language": "string", "year": "number"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")

		step0 := result.Steps[0]
		So(step0.Status, ShouldEqual, StatusDone)
		m, ok := step0.Result.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["language"], ShouldEqual, "Go")
	})
}

// ---------------------------------------------------------------------------
// E2E: Multi-step with dependency — first step produces data, second consumes via $step.field
// ---------------------------------------------------------------------------

func TestE2EMultiStepTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: multi-step with template variable", t, func() {
		engine, _, _ := createE2EEngine(t)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_multi_template",
			Steps: []StepSpec{
				{
					ID:     "list",
					Prompt: "请输出一个 JSON 对象，包含一个字段 langs，值为字符串数组：[\"Go\", \"Rust\", \"Python\"]。只输出 JSON。",
					OutputSchema: map[string]any{
						"langs": []any{"string"},
					},
				},
				{
					ID:        "summary",
					Prompt:    "请用中文写一句话总结以下编程语言：$list.langs",
					DependsOn: []string{"list"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")

		// First step should produce a langs array.
		listStep := result.Steps[0]
		So(listStep.Status, ShouldEqual, StatusDone)

		// Second step should produce a Chinese summary string.
		summaryStep := result.Steps[1]
		So(summaryStep.Status, ShouldEqual, StatusDone)
		t.Logf("summary output: %v", summaryStep.Result)
	})
}

// ---------------------------------------------------------------------------
// E2E: Foreach — list files then audit each file
// ---------------------------------------------------------------------------

func TestE2EForeach(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: foreach expansion", t, func() {
		engine, _, _ := createE2EEngine(t)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_foreach",
			Steps: []StepSpec{
				{
					ID:     "list",
					Prompt: "请输出一个 JSON 对象，包含一个字段 files，值为字符串数组：[\"main.go\", \"config.yaml\", \"README.md\"]。只输出 JSON。",
					OutputSchema: map[string]any{
						"files": []any{"string"},
					},
				},
				{
					ID:        "audit",
					Prompt:    "用一句话简短描述文件 $each 可能包含什么内容。不超过 30 个字。",
					ForEach:   "$list.files",
					DependsOn: []string{"list"},
				},
				{
					ID:        "report",
					Prompt:    "请汇总以下审计结果，用中文写一个简短的报告：$audit[*].result",
					DependsOn: []string{"audit"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")

		// Audit step should have 3 instances.
		auditStep := result.Steps[1]
		So(auditStep.ID, ShouldEqual, "audit")
		So(auditStep.Status, ShouldEqual, StatusDone)
		So(len(auditStep.Instances), ShouldEqual, 3)
		for _, inst := range auditStep.Instances {
			So(inst.Status, ShouldEqual, StatusDone)
			t.Logf("  audit %s: %v", inst.Key, inst.Result)
		}

		// Report step should produce a summary.
		reportStep := result.Steps[2]
		So(reportStep.Status, ShouldEqual, StatusDone)
		t.Logf("report: %v", reportStep.Result)
	})
}

// ---------------------------------------------------------------------------
// E2E: Checkpoint — pause and continue with tool call
// ---------------------------------------------------------------------------

func TestE2ECheckpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: checkpoint pause and continue", t, func() {
		engine, _, _ := createE2EEngine(t)
		cleanup := "e2e_checkpoint.result.yaml"

		// Step 1: non-checkpoint, Step 2: checkpoint
		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_checkpoint",
			Steps: []StepSpec{
				{
					ID:     "prepare",
					Prompt: "输出 JSON：{\"ready\": true}。只输出 JSON。",
					OutputSchema: map[string]any{
						"ready": "boolean",
					},
				},
				{
					ID:         "review",
					Prompt:     "输出 JSON：{\"approved\": true}。只输出 JSON。",
					Checkpoint: true,
					DependsOn:  []string{"prepare"},
				},
				{
					ID:           "deploy",
					Prompt:       "请严格输出以下 JSON，不要修改任何字段的值：{\"deployed\": true}",
					DependsOn:    []string{"review"},
					OutputSchema: map[string]any{"deployed": "boolean"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		defer os.Remove(cleanup)

		// First run: should pause at checkpoint after prepare+review.
		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldEqual, ErrCheckpointReached)
		So(result, ShouldBeNil)

		// Verify result file was written.
		_, statErr := os.Stat(cleanup)
		So(statErr, ShouldBeNil)

		// Continue: should complete the deploy step.
		result2, err2 := engine.Continue(ctx, spec, "")
		So(err2, ShouldBeNil)
		So(result2, ShouldNotBeNil)
		So(result2.Status, ShouldEqual, "completed")

		deployStep := result2.Steps[2]
		So(deployStep.ID, ShouldEqual, "deploy")
		So(deployStep.Status, ShouldEqual, StatusDone)
		m, ok := deployStep.Result.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["deployed"], ShouldEqual, true)
	})
}

// ---------------------------------------------------------------------------
// E2E: Tool loop — LLM calls a builtin tool and uses the result
// ---------------------------------------------------------------------------

func TestE2EToolLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: LLM tool loop", t, func() {
		engine, _, _ := createE2EEngine(t)

		// Register a simple calculation tool.
		engine.toolReg.RegisterBuiltin(
			"add",
			"Adds two numbers and returns the sum",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"a": {"type": "integer", "description": "First number"},
					"b": {"type": "integer", "description": "Second number"}
				},
				"required": ["a", "b"]
			}`),
			func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				var params struct {
					A int `json:"a"`
					B int `json:"b"`
				}
				json.Unmarshal(args, &params)
				return &types.ToolResult{
					Content: itoa(params.A + params.B),
				}, nil
			},
		)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_tool_loop",
			Steps: []StepSpec{
				{
					ID:     "calc",
					Prompt: "请使用 add 工具计算 123 + 456 的结果，然后输出 JSON：{\"sum\": <结果>}。只输出 JSON。",
					OutputSchema: map[string]any{
						"sum": "number",
					},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")

		m, ok := result.Steps[0].Result.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["sum"], ShouldEqual, float64(579))
	})
}

// ---------------------------------------------------------------------------
// E2E: Fan-out parallel — 3 independent steps run concurrently
// ---------------------------------------------------------------------------

func TestE2EParallelFanOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: parallel fan-out", t, func() {
		engine, cfg, _ := createE2EEngine(t)
		cfg.Set("agent.pool_size", 3)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_parallel",
			Steps: []StepSpec{
				{
					ID:           "task_a",
					Prompt:       "输出 JSON：{\"name\": \"A\", \"value\": 1}。只输出 JSON。",
					OutputSchema: map[string]any{"name": "string", "value": "number"},
				},
				{
					ID:           "task_b",
					Prompt:       "输出 JSON：{\"name\": \"B\", \"value\": 2}。只输出 JSON。",
					OutputSchema: map[string]any{"name": "string", "value": "number"},
				},
				{
					ID:           "task_c",
					Prompt:       "输出 JSON：{\"name\": \"C\", \"value\": 3}。只输出 JSON。",
					OutputSchema: map[string]any{"name": "string", "value": "number"},
				},
				{
					ID:        "merge",
					Prompt:    "汇总以下任务结果，输出 JSON：{\"total\": <所有 value 之和>, \"names\": [所有 name]}。只输出 JSON。\n\n任务A: $task_a\n任务B: $task_b\n任务C: $task_c",
					DependsOn: []string{"task_a", "task_b", "task_c"},
					OutputSchema: map[string]any{
						"total": "number",
						"names": []any{"string"},
					},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		start := time.Now()
		result, err := engine.Run(ctx, spec, "")
		elapsed := time.Since(start)
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")

		// Parallel steps should run faster than sequential 4-step.
		t.Logf("fan-out 4 steps completed in %v", elapsed)

		mergeStep := result.Steps[3]
		So(mergeStep.Status, ShouldEqual, StatusDone)
		m, ok := mergeStep.Result.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["total"], ShouldEqual, float64(6)) // 1+2+3 = 6
	})
}

// ---------------------------------------------------------------------------
// E2E: Error recovery — step with bad tool call fails gracefully
// ---------------------------------------------------------------------------

func TestE2EExecutionError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: execution error handling", t, func() {
		engine, _, _ := createE2EEngine(t)

		// Register a tool that always returns an error.
		engine.toolReg.RegisterBuiltin(
			"broken_tool",
			"Always fails",
			json.RawMessage(`{"type": "object", "properties": {}}`),
			func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				return &types.ToolResult{
					Content: "this tool is broken",
					IsError: true,
				}, nil
			},
		)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_error",
			Steps: []StepSpec{
				{
					ID:           "try_broken",
					Prompt:       "请调用 broken_tool 工具，然后无论如何，输出 JSON：{\"handled\": true}。只输出 JSON。",
					OutputSchema: map[string]any{"handled": "boolean"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)

		// The step may succeed (LLM might recover from tool error) or fail.
		// Either way the engine should handle it gracefully.
		t.Logf("error test status: %s, step status: %s", result.Status, result.Steps[0].Status)
	})
}

// ---------------------------------------------------------------------------
// E2E: Progress callback verification
// ---------------------------------------------------------------------------

func TestE2EProgressCallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: progress callback receives events", t, func() {
		engine, _, _ := createE2EEngine(t)

		var progressMsgs []string
		engine.SetOnProgress(func(tr agentio.TurnResult) {
			progressMsgs = append(progressMsgs, tr.Text)
		})

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_progress",
			Steps: []StepSpec{
				{
					ID:           "hello",
					Prompt:       "输出 JSON：{\"msg\": \"hello world\"}。只输出 JSON。",
					OutputSchema: map[string]any{"msg": "string"},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		_, err := engine.Run(ctx, spec, "test-transport")
		So(err, ShouldBeNil)
		So(len(progressMsgs), ShouldBeGreaterThan, 0)

		t.Logf("progress messages received: %d", len(progressMsgs))
		for _, msg := range progressMsgs {
			t.Logf("  [progress] %s", msg)
		}
	})
}

// ---------------------------------------------------------------------------
// E2E: Speed comparison — 3 parallel curl steps → 1 summarize
// ---------------------------------------------------------------------------

func TestE2ESpeedComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	Convey("E2E: speed comparison — parallel curl baidu/google/bing", t, func() {
		engine, cfg, _ := createE2EEngine(t)
		cfg.Set("agent.pool_size", 3)

		// Register a shell tool so the LLM can actually run curl.
		engine.toolReg.RegisterBuiltin(
			"shell",
			"Run a shell command and return its output. Args: {command: string}",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {"type": "string", "description": "The shell command to execute"}
				},
				"required": ["command"]
			}`),
			func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				var params struct {
					Command string `json:"command"`
				}
				json.Unmarshal(args, &params)
				cmd := execCommand(ctx, params.Command)
				return &types.ToolResult{Content: cmd}, nil
			},
		)

		spec := &WorkflowSpec{
			Version: "1",
			Name:    "e2e_speed_comparison",
			Steps: []StepSpec{
				{
					ID:     "test_baidu",
					Prompt: "请使用 shell 工具执行命令，测量访问 baidu.com 的响应时间，然后输出 JSON：{\"time_seconds\": <数字>}。命令示例：curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.baidu.com。只输出 JSON。",
					OutputSchema: map[string]any{
						"time_seconds": "number",
					},
				},
				{
					ID:     "test_google",
					Prompt: "请使用 shell 工具执行命令，测量访问 google.com 的响应时间，然后输出 JSON：{\"time_seconds\": <数字>}。命令示例：curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.google.com。只输出 JSON。",
					OutputSchema: map[string]any{
						"time_seconds": "number",
					},
				},
				{
					ID:     "test_bing",
					Prompt: "请使用 shell 工具执行命令，测量访问 bing.com 的响应时间，然后输出 JSON：{\"time_seconds\": <数字>}。命令示例：curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.bing.com。只输出 JSON。",
					OutputSchema: map[string]any{
						"time_seconds": "number",
					},
				},
				{
					ID:        "summarize",
					Prompt:    "以下是用 curl 测量三个网站的响应时间（秒）：\n- baidu.com: $test_baidu.time_seconds 秒\n- google.com: $test_google.time_seconds 秒\n- bing.com: $test_bing.time_seconds 秒\n\n请判断哪个网站最快，用中文回答。输出 JSON：{\"fastest\": \"<最快网站>\", \"baidu_time\": $test_baidu.time_seconds, \"google_time\": $test_google.time_seconds, \"bing_time\": $test_bing.time_seconds}。只输出 JSON。",
					DependsOn: []string{"test_baidu", "test_google", "test_bing"},
					OutputSchema: map[string]any{
						"fastest":     "string",
						"baidu_time":  "number",
						"google_time": "number",
						"bing_time":   "number",
					},
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		start := time.Now()
		result, err := engine.Run(ctx, spec, "")
		elapsed := time.Since(start)
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")

		t.Logf("total elapsed: %v (parallel curl should be much less than sum of individual times)", elapsed)

		for _, step := range result.Steps {
			t.Logf("  %s: status=%s result=%v", step.ID, step.Status, step.Result)
			So(step.Status, ShouldEqual, StatusDone)
		}

		// Verify summarize step has the merged result.
		summarize := result.Steps[3]
		m, ok := summarize.Result.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["fastest"], ShouldNotBeEmpty)
		t.Logf("fastest website: %v", m["fastest"])
		t.Logf("  baidu: %vs, google: %vs, bing: %vs", m["baidu_time"], m["google_time"], m["bing_time"])
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func execCommand(ctx context.Context, command string) string {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("error: %v\noutput: %s", err, string(out))
	}
	return string(out)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
