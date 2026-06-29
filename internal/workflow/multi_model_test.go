package workflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// ---------------------------------------------------------------------------
// StepSpec — model field
// ---------------------------------------------------------------------------

func TestStepSpecModelField(t *testing.T) {
	Convey("StepSpec model field", t, func() {
		Convey("parses model from YAML", func() {
			data := []byte(`
version: "1"
name: model-test
steps:
  - id: step1
    prompt: do something
    model: "deepseek-v4-pro"
  - id: step2
    prompt: do something else
`)
			spec, err := Parse(data)
			So(err, ShouldBeNil)
			So(spec.Steps[0].Model, ShouldEqual, "deepseek-v4-pro")
			So(spec.Steps[1].Model, ShouldEqual, "")
		})

		Convey("model is optional — omitted by default", func() {
			data := []byte(`
version: "1"
name: no-model
steps:
  - id: s
    prompt: p
`)
			spec, err := Parse(data)
			So(err, ShouldBeNil)
			So(spec.Steps[0].Model, ShouldEqual, "")
		})

		Convey("model survives full round-trip: YAML → StepSpec → stepInstance → LLMRequest", func() {
			logger := testLogger()
			cfg := testConfig()
			toolReg := emptyRegistry()
			eventBus := event.NewBus()

			var capturedModel string
			mock := &mockLLMProvider{
				chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
					capturedModel = req.Model
					return []llm.LLMChunk{textChunk("done")}
				},
			}

			engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
			spec := &WorkflowSpec{
				Version: "1", Name: "model_roundtrip",
				Steps: []StepSpec{
					{ID: "s", Prompt: "do it", Model: "volcengine_agent/kimi-k2.7-code"},
				},
			}
			result, err := engine.Run(context.Background(), spec, "")
			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, "completed")
			So(capturedModel, ShouldEqual, "volcengine_agent/kimi-k2.7-code")
		})

		Convey("empty model leaves LLMRequest.Model empty (manager uses active model)", func() {
			logger := testLogger()
			cfg := testConfig()
			toolReg := emptyRegistry()
			eventBus := event.NewBus()

			var capturedModel string
			mock := &mockLLMProvider{
				chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
					capturedModel = req.Model
					return []llm.LLMChunk{textChunk("done")}
				},
			}

			engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
			spec := &WorkflowSpec{
				Version: "1", Name: "no_model",
				Steps: []StepSpec{
					{ID: "s", Prompt: "do it"},
				},
			}
			_, err := engine.Run(context.Background(), spec, "")
			So(err, ShouldBeNil)
			So(capturedModel, ShouldEqual, "") // manager will use active model
		})
	})
}

// ---------------------------------------------------------------------------
// StepSpec — model field in foreach instances
// ---------------------------------------------------------------------------

func TestModelFieldInForeach(t *testing.T) {
	Convey("model field propagates to foreach instances", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		var modelCount int
		var capturedModels []string
		var mu sync.Mutex

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				if strings.Contains(msg, "list files") {
					return []llm.LLMChunk{textChunk("```json\n{\"files\": [\"a.go\", \"b.go\"]}\n```")}
				}
				mu.Lock()
				modelCount++
				capturedModels = append(capturedModels, req.Model)
				mu.Unlock()
				return []llm.LLMChunk{textChunk("audited")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "foreach_model",
			Steps: []StepSpec{
				{ID: "list", Prompt: "list files", OutputSchema: map[string]any{"files": "array"}},
				{ID: "audit", Prompt: "audit $each", ForEach: "$list.files",
					DependsOn: []string{"list"}, Model: "volcengine_agent/minimax-m3"},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(modelCount, ShouldEqual, 2) // 2 foreach instances
		for _, m := range capturedModels {
			So(m, ShouldEqual, "volcengine_agent/minimax-m3")
		}
	})
}

// ---------------------------------------------------------------------------
// Different models per step (fan-out with model override)
// ---------------------------------------------------------------------------

func TestMultiModelFanOut(t *testing.T) {
	Convey("Engine.Run — parallel steps with different models", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		var mu sync.Mutex
		usedModels := make(map[string]int)
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				mu.Lock()
				usedModels[req.Model]++
				mu.Unlock()
				return []llm.LLMChunk{textChunk("```json\n{\"done\": true}\n```")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "multi_model",
			Steps: []StepSpec{
				{ID: "setup", Prompt: "setup"},
				{ID: "a", Prompt: "branch a", Model: "model-alpha", DependsOn: []string{"setup"}},
				{ID: "b", Prompt: "branch b", Model: "model-beta", DependsOn: []string{"setup"}},
				{ID: "c", Prompt: "branch c", Model: "model-gamma", DependsOn: []string{"setup"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(usedModels["model-alpha"], ShouldEqual, 1)
		So(usedModels["model-beta"], ShouldEqual, 1)
		So(usedModels["model-gamma"], ShouldEqual, 1)
	})
}

// ---------------------------------------------------------------------------
// Model field — step execution with tool round
// ---------------------------------------------------------------------------

func TestModelFieldInToolRound(t *testing.T) {
	Convey("model field is present in all tool-round LLM calls", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := tool.NewRegistry()

		toolReg.RegisterBuiltin(
			"ping",
			"ping tool",
			json.RawMessage(`{"type":"object"}`),
			func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				return &types.ToolResult{Content: "pong"}, nil
			},
		)

		eventBus := event.NewBus()

		round := 0
		var modelsInRounds []string
		var mu sync.Mutex
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				mu.Lock()
				modelsInRounds = append(modelsInRounds, req.Model)
				round++
				mu.Unlock()
				if round == 1 {
					return []llm.LLMChunk{toolChunk("ping", "tc1", `{}`)}
				}
				return []llm.LLMChunk{textChunk("final")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		inst := stepInstance{
			StepID: "test",
			Key:    "test",
			Prompt: "use ping",
			Model:  "my-custom-model",
		}
		result := engine.executeStep(context.Background(), inst)
		So(result.Status, ShouldEqual, StatusDone)
		So(round, ShouldEqual, 2)
		for _, m := range modelsInRounds {
			So(m, ShouldEqual, "my-custom-model")
		}
	})
}

// ---------------------------------------------------------------------------
// Template — Go template {{if}} / {{range}} / {{index}} interop
// ---------------------------------------------------------------------------

func TestNativeGoTemplateInPrompt(t *testing.T) {
	Convey("native Go template syntax works alongside $step.field shortcuts", t, func() {
		data := map[string]any{
			"review": map[string]any{
				"score":   85,
				"summary": "good",
				"findings": []any{
					map[string]any{"title": "nil pointer", "severity": "critical", "file": "main.go"},
					map[string]any{"title": "no tests", "severity": "minor", "file": "util.go"},
				},
			},
		}

		Convey("{{if}} guard skips block when key missing", func() {
			prompt := `{{if index . "missing"}}SHOULD_NOT_APPEAR{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldNotContainSubstring, "SHOULD_NOT_APPEAR")
		})

		Convey("{{if}} shows block when key present", func() {
			prompt := `{{if index . "review"}}PRESENT{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldContainSubstring, "PRESENT")
		})

		Convey("{{range}} iterates findings", func() {
			prompt := `{{range (index . "review" "findings")}}- {{.title}} ({{.severity}}){{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldContainSubstring, "nil pointer (critical)")
			So(result, ShouldContainSubstring, "no tests (minor)")
		})

		Convey("{{index}} chaining accesses nested fields", func() {
			prompt := `Score: {{index . "review" "score"}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "Score: 85")
		})

		Convey("mixed $shortcut and native syntax", func() {
			prompt := `Summary: $review.summary Findings: {{range (index . "review" "findings")}}-{{.title}}-{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldContainSubstring, "Summary: good")
			So(result, ShouldContainSubstring, "-nil pointer--no tests-")
		})

		Convey("nested {{with}} + {{range}} for safe access", func() {
			prompt := `{{with index . "review"}}Score: {{.score}}{{range (index . "findings")}}- {{.title}}{{end}}{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "Score: 85- nil pointer- no tests")
		})

		Convey("{{with}} on nil sub-key skips gracefully", func() {
			prompt := `{{with index . "review" "positives"}}POSITIVES{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldNotContainSubstring, "POSITIVES")
		})

		Convey("$shortcut and {{if}} can coexist in same prompt", func() {
			prompt := `$review.summary {{if index . "review"}}exists{{end}}`
			result, err := renderPrompt(prompt, data)
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "good exists")
		})
	})
}

// ---------------------------------------------------------------------------
// Workflow YAML — parse the actual multi-model-review file
// ---------------------------------------------------------------------------

func TestParseMultiModelReviewWorkflow(t *testing.T) {
	Convey("Parse multi-model-review.workflow.yaml", t, func() {
		data, err := os.ReadFile("../brain/seed/workflow/multi-model-review.workflow.yaml")
		So(err, ShouldBeNil)

		spec, err := Parse(data)
		So(err, ShouldBeNil)
		So(spec.Version, ShouldEqual, "1")
		So(spec.Name, ShouldEqual, "multi-model-review")
		So(spec.Description, ShouldNotBeEmpty)

		// All 6 steps present.
		So(len(spec.Steps), ShouldEqual, 6)
		stepIDs := make(map[string]bool)
		for _, s := range spec.Steps {
			stepIDs[s.ID] = true
		}
		So(stepIDs["collect_content"], ShouldBeTrue)
		So(stepIDs["kimi_review"], ShouldBeTrue)
		So(stepIDs["minimax_review"], ShouldBeTrue)
		So(stepIDs["doubao_review"], ShouldBeTrue)
		So(stepIDs["glm_review"], ShouldBeTrue)
		So(stepIDs["synthesize"], ShouldBeTrue)

		// Build lookup map.
		steps := make(map[string]StepSpec)
		for _, s := range spec.Steps {
			steps[s.ID] = s
		}

		Convey("collect_content has [string] output_schema", func() {
			s := steps["collect_content"]
			changedType, ok := s.OutputSchema["changed_files"]
			So(ok, ShouldBeTrue)
			arr, ok := changedType.([]any)
			So(ok, ShouldBeTrue)
			So(len(arr), ShouldEqual, 1)
			So(arr[0], ShouldEqual, "string")
		})

		Convey("review steps have correct model and output_schema", func() {
			for _, id := range []string{"kimi_review", "minimax_review", "doubao_review", "glm_review"} {
				s := steps[id]
				So(s.Model, ShouldNotBeEmpty)
				So(s.DependsOn, ShouldResemble, []string{"collect_content"})
				_, hasFindings := s.OutputSchema["findings"]
				So(hasFindings, ShouldBeTrue)
				_, hasPositives := s.OutputSchema["positives"]
				So(hasPositives, ShouldBeTrue)
			}
			So(steps["kimi_review"].Model, ShouldEqual, "volcengine_agent/kimi-k2.7-code")
			So(steps["minimax_review"].Model, ShouldEqual, "volcengine_agent/minimax-m3")
			So(steps["doubao_review"].Model, ShouldEqual, "volcengine_agent/doubao-seed-2.0-code")
			So(steps["glm_review"].Model, ShouldEqual, "volcengine_agent/glm-5.2")
		})

		Convey("synthesize depends on all 4 review steps", func() {
			s := steps["synthesize"]
			So(s.DependsOn, ShouldResemble, []string{"kimi_review", "minimax_review", "doubao_review", "glm_review"})
		})

		Convey("no cycles in DAG", func() {
			So(detectCycle(spec), ShouldBeNil)
		})

		Convey("synthesize prompt uses native Go template {{range}} and {{if}}", func() {
			prompt := steps["synthesize"].Prompt
			So(prompt, ShouldContainSubstring, "{{range")
			So(prompt, ShouldContainSubstring, "{{if index .")
			So(prompt, ShouldContainSubstring, "{{end}}")
			So(prompt, ShouldNotContainSubstring, "$minimize_review")
			So(prompt, ShouldNotContainSubstring, "minimize_review")
		})

		Convey("no typo: $minimize_review should not appear anywhere", func() {
			raw := string(data)
			So(raw, ShouldNotContainSubstring, "minimize")
			So(raw, ShouldNotContainSubstring, "$minimize")
		})
	})
}

// ---------------------------------------------------------------------------
// Engine — full multi-model-review simulation (with mocks)
// ---------------------------------------------------------------------------

func TestMultiModelReviewEndToEnd(t *testing.T) {
	Convey("multi-model-review E2E — all steps with correct model routing", t, func() {
		logger := testLogger()
		cfg := testConfig()
		cfg.Set("agent.pool_size", 4)
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		type call struct{ step, model string }
		var calls []call
		var mu sync.Mutex

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				mu.Lock()
				var stepID string
				switch {
				case strings.Contains(msg, "代码内容"):
					stepID = "collect_content"
				case strings.Contains(msg, "设计模式"):
					stepID = "kimi_review"
				case strings.Contains(msg, "防御性编程视角"):
					stepID = "minimax_review"
				case strings.Contains(msg, "边界条件"):
					stepID = "doubao_review"
				case strings.Contains(msg, "从架构层面"):
					stepID = "glm_review"
				case strings.Contains(msg, "去重") || strings.Contains(msg, "严重性裁决"):
					stepID = "synthesize"
				default:
					stepID = "unknown"
				}
				calls = append(calls, call{stepID, req.Model})
				mu.Unlock()

				switch stepID {
				case "collect_content":
					return []llm.LLMChunk{textChunk(`{"diff_text":"diff --git a/main.go b/main.go","staged_diff_text":"","commit_log":"abc feat","changed_files":["main.go"],"summary":"更新 main.go"}`)}
				case "kimi_review", "minimax_review", "doubao_review", "glm_review":
					return []llm.LLMChunk{textChunk(`{"findings":[{"file":"main.go","type":"bug","severity":"major","title":"test","description":"desc","suggestion":"fix"}],"summary":"done","score":85,"positives":["good"]}`)}
				case "synthesize":
					return []llm.LLMChunk{textChunk(`{"summary":"all good","consensus_score":82,"critical_findings":["fix this"],"major_findings":[],"minor_findings":[],"positives":["clean"],"action_items":[{"priority":"P0","file":"main.go","title":"fix","description":"do it"}],"model_assessment":"decent"}`)}
				default:
					return []llm.LLMChunk{textChunk("ok")}
				}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		data, err := os.ReadFile("../brain/seed/workflow/multi-model-review.workflow.yaml")
		So(err, ShouldBeNil)

		result, err := engine.ParseAndRun(context.Background(), data, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")

		mu.Lock()
		So(len(calls), ShouldEqual, 6)
		stepModel := make(map[string]string)
		for _, c := range calls {
			stepModel[c.step] = c.model
		}
		mu.Unlock()

		So(stepModel["collect_content"], ShouldEqual, "")
		So(stepModel["kimi_review"], ShouldEqual, "volcengine_agent/kimi-k2.7-code")
		So(stepModel["minimax_review"], ShouldEqual, "volcengine_agent/minimax-m3")
		So(stepModel["doubao_review"], ShouldEqual, "volcengine_agent/doubao-seed-2.0-code")
		So(stepModel["glm_review"], ShouldEqual, "volcengine_agent/glm-5.2")
		So(stepModel["synthesize"], ShouldEqual, "")
	})
}

// ---------------------------------------------------------------------------
// Engine — serial pool (pool_size=1)
// ---------------------------------------------------------------------------

func TestMultiModelWithSerialPool(t *testing.T) {
	Convey("Engine.Run — multi-model with pool_size=1", t, func() {
		logger := testLogger()
		cfg := config.LoadConfigFromMap(map[string]any{
			"agent.pool_size":       1,
			"workflow.step_timeout": "30s",
		})
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		var order []string
		var mu sync.Mutex

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				mu.Lock()
				switch {
				case strings.Contains(msg, "collect"):
					order = append(order, "collect")
				case strings.Contains(msg, "branch a"):
					order = append(order, "a")
				case strings.Contains(msg, "branch b"):
					order = append(order, "b")
				case strings.Contains(msg, "branch c"):
					order = append(order, "c")
				}
				mu.Unlock()
				return []llm.LLMChunk{textChunk("```json\n{\"done\": true}\n```")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "serial_pool",
			Steps: []StepSpec{
				{ID: "setup", Prompt: "collect"},
				{ID: "a", Prompt: "branch a", Model: "model-a", DependsOn: []string{"setup"}},
				{ID: "b", Prompt: "branch b", Model: "model-b", DependsOn: []string{"setup"}},
				{ID: "c", Prompt: "branch c", Model: "model-c", DependsOn: []string{"setup"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(order[0], ShouldEqual, "collect")
		So(len(order), ShouldEqual, 4)
	})
}

// ---------------------------------------------------------------------------
// parseOutput — [string] array schema
// ---------------------------------------------------------------------------

func TestParseOutputArraySchema(t *testing.T) {
	Convey("parseOutput with [string] array schema", t, func() {
		schema := map[string]any{
			"changed_files": []any{"string"},
			"summary":       "string",
		}

		Convey("valid array passes schema check", func() {
			result := parseOutput(`{"changed_files":["a.go","b.go"],"summary":"hi"}`, schema)
			m, ok := result.(map[string]any)
			So(ok, ShouldBeTrue)
			files, ok := m["changed_files"].([]any)
			So(ok, ShouldBeTrue)
			So(len(files), ShouldEqual, 2)
		})

		Convey("non-array fails schema check, returns raw", func() {
			result := parseOutput(`{"changed_files":"not-an-array","summary":"hi"}`, schema)
			_, ok := result.(map[string]any)
			So(ok, ShouldBeFalse)
			So(result, ShouldContainSubstring, "changed_files")
		})

		Convey("extra fields not in schema are preserved", func() {
			result := parseOutput(`{"changed_files":["a.go"],"summary":"hi","unexpected":42}`, schema)
			m, ok := result.(map[string]any)
			So(ok, ShouldBeTrue)
			So(m["unexpected"], ShouldEqual, float64(42))
		})
	})
}
