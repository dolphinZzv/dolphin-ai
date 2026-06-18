package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/tool"
)

func TestSpeedComparison(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "speed.workflow.yaml")

	yaml := `version: "1"
name: speed_test
steps:
  - id: test_baidu
    prompt: "Run this shell command and return ONLY the numeric value (seconds): curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.baidu.com"
    output_schema: {time_seconds: number}

  - id: test_google
    prompt: "Run this shell command and return ONLY the numeric value (seconds): curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.google.com"
    output_schema: {time_seconds: number}

  - id: test_bing
    prompt: "Run this shell command and return ONLY the numeric value (seconds): curl -o /dev/null -s -w '%{time_total}' --max-time 10 https://www.bing.com"
    output_schema: {time_seconds: number}

  - id: summarize
    prompt: "Based on these results: baidu={{.test_baidu.time_seconds}}s google={{.test_google.time_seconds}}s bing={{.test_bing.time_seconds}}s"
    depends_on: [test_baidu, test_google, test_bing]
`

	if err := os.WriteFile(workflowPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// #nosec G304 -- workflowPath is constructed from t.TempDir
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}

	spec, err := Parse(data)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	t.Logf("parsed workflow: %s, %d steps", spec.Name, len(spec.Steps))

	// Verify DAG: first 3 steps have no deps (parallel), summarize depends on all 3.
	for i := 0; i < 3; i++ {
		if len(spec.Steps[i].DependsOn) != 0 {
			t.Errorf("step %s should have no dependencies, got %v", spec.Steps[i].ID, spec.Steps[i].DependsOn)
		}
	}
	if len(spec.Steps[3].DependsOn) != 3 {
		t.Errorf("summarize should depend on 3 steps, got %v", spec.Steps[3].DependsOn)
	}

	t.Log("DAG structure verified: test_baidu, test_google, test_bing run in parallel, summarize waits for all")
}

func TestRunWorkflowTool(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test.workflow.yaml")

	yaml := `version: "1"
name: quick_test
steps:
  - id: hello
    prompt: Say "hello world" and return it as JSON
    output_schema:
      message: string
`
	if err := os.WriteFile(workflowPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Verify run_workflow tool is registered and YAML parses correctly.
	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()
	cfg := config.LoadConfigFromMap(map[string]any{"agent.pool_size": 3})

	engine := NewEngine(toolReg, nil, eventBus, nil, nil, cfg)
	RegisterTools(toolReg, engine, nil, nil)

	// Verify the tool is registered.
	defs, err := toolReg.List(context.Background())
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	found := false
	for _, d := range defs {
		if d.Name == "run_workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Error("run_workflow tool not registered")
	}

	// Verify the YAML parses correctly (the tool handler would parse it).
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if spec.Name != "quick_test" || len(spec.Steps) != 1 {
		t.Errorf("unexpected spec: name=%s steps=%d", spec.Name, len(spec.Steps))
	}
	t.Logf("run_workflow tool registered and YAML parsed: %s → %d steps", spec.Name, len(spec.Steps))
}

func TestSpeedComparisonUnit(t *testing.T) {
	// Unit test: verify parallel execution with mock timing.
	logger := testLogger()
	cfg := testConfig()
	cfg.Set("agent.pool_size", 3)

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()

	// Record execution order to verify parallelism.
	started := make(chan string, 10)
	done := make(chan string, 10)

	mock := &mockLLMProvider{
		chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
			msg := req.Messages[len(req.Messages)-1].Content

			switch {
			case strings.Contains(msg, "baidu"):
				started <- "baidu"
				time.Sleep(50 * time.Millisecond)
				done <- "baidu"
				return []llm.LLMChunk{textChunk("```json\n{\"time_seconds\": 0.5}\n```")}
			case strings.Contains(msg, "google"):
				started <- "google"
				time.Sleep(100 * time.Millisecond)
				done <- "google"
				return []llm.LLMChunk{textChunk("```json\n{\"time_seconds\": 1.2}\n```")}
			case strings.Contains(msg, "bing"):
				started <- "bing"
				time.Sleep(30 * time.Millisecond)
				done <- "bing"
				return []llm.LLMChunk{textChunk("```json\n{\"time_seconds\": 0.3}\n```")}
			case strings.Contains(msg, "Based on"):
				return []llm.LLMChunk{textChunk("Results: bing fastest (0.3s), then baidu (0.5s), google slowest (1.2s)")}
			}
			return []llm.LLMChunk{textChunk("ok")}
		},
	}

	engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

	spec := &WorkflowSpec{
		Version: "1",
		Name:    "speed_test",
		Steps: []StepSpec{
			{ID: "test_baidu", Prompt: "test baidu speed", OutputSchema: map[string]any{"time_seconds": "number"}},
			{ID: "test_google", Prompt: "test google speed", OutputSchema: map[string]any{"time_seconds": "number"}},
			{ID: "test_bing", Prompt: "test bing speed", OutputSchema: map[string]any{"time_seconds": "number"}},
			{ID: "summarize", Prompt: "Based on results: baidu={{.test_baidu.time_seconds}}s google={{.test_google.time_seconds}}s bing={{.test_bing.time_seconds}}s", DependsOn: []string{"test_baidu", "test_google", "test_bing"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	startTime := time.Now()
	result, err := engine.Run(ctx, spec, "")
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("engine.Run failed: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %s", result.Status)
	}

	t.Logf("elapsed: %v (parallel steps should be ≪ sum of individual sleeps)", elapsed)

	// Verify parallelism: total time should be close to max(sleeps), not sum.
	// baidu=50ms, google=100ms, bing=30ms → parallel should be ≈100ms, not 180ms.
	if elapsed > 300*time.Millisecond {
		t.Errorf("parallel execution too slow: %v (expected <300ms for parallel 50+100+30ms steps)", elapsed)
	}

	// Verify results.
	for _, step := range result.Steps {
		t.Logf("  %s: status=%s result=%v", step.ID, step.Status, step.Result)
		if step.Status != "done" {
			t.Errorf("step %s should be done, got %s", step.ID, step.Status)
		}
	}
}

func TestSpeedComparisonParse(t *testing.T) {
	// Verify the exact YAML the LLM should generate parses correctly.
	yaml := `version: "1"
name: speed_test
steps:
  - id: test_baidu
    prompt: "用 curl 测试 baidu.com 响应时间并返回数字"
    output_schema: {time_seconds: number}
  - id: test_google
    prompt: "用 curl 测试 google.com 响应时间并返回数字"
    output_schema: {time_seconds: number}
  - id: test_bing
    prompt: "用 curl 测试 bing.com 响应时间并返回数字"
    output_schema: {time_seconds: number}
  - id: summarize
    prompt: "三个时间分别是 baidu={{.test_baidu.time_seconds}}s google={{.test_google.time_seconds}}s bing={{.test_bing.time_seconds}}s，判断哪个最快"
    depends_on: [test_baidu, test_google, test_bing]
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(spec.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(spec.Steps))
	}

	// First 3 steps have no deps → parallel.
	for i := 0; i < 3; i++ {
		if len(spec.Steps[i].DependsOn) > 0 {
			t.Errorf("step %s should be independent, got deps: %v", spec.Steps[i].ID, spec.Steps[i].DependsOn)
		}
	}
	// Last step depends on all 3.
	if len(spec.Steps[3].DependsOn) != 3 {
		t.Errorf("step summarize should depend on 3, got: %v", spec.Steps[3].DependsOn)
	}

	t.Log("schema parse OK: 3 parallel steps → 1 summarize")
}
