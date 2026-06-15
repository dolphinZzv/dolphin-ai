package workflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/tool"
	"dolphin/internal/types"

	"gopkg.in/yaml.v3"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func testConfig() *config.Config {
	return config.LoadConfigFromMap(map[string]any{
		"agent.pool_size":       2,
		"workflow.step_timeout": "30s",
	})
}

// mockLLMProvider implements llm.Provider for testing.
type mockLLMProvider struct {
	mu       sync.Mutex
	chunksFn func(req llm.LLMRequest) []llm.LLMChunk
	name     string
}

func (m *mockLLMProvider) Name() string {
	if m.name == "" {
		return "mock"
	}
	return m.name
}

func (m *mockLLMProvider) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk, 32)
	var chunks []llm.LLMChunk
	if m.chunksFn != nil {
		chunks = m.chunksFn(req)
	}
	go func() {
		defer close(ch)
		for _, c := range chunks {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (m *mockLLMProvider) Models(ctx context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}

func textChunk(s string) llm.LLMChunk {
	return llm.LLMChunk{Content: s}
}

func toolChunk(name, id, args string) llm.LLMChunk {
	return llm.LLMChunk{
		ToolCalls: []types.ToolCall{{Name: name, ID: id, Arguments: args}},
	}
}

func emptyRegistry() *tool.Registry { return tool.NewRegistry() }

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

func TestParse(t *testing.T) {
	Convey("Parse", t, func() {
		Convey("valid minimal YAML", func() {
			data := []byte(`
version: "1"
name: test-workflow
steps:
  - id: step1
    prompt: do something
`)
			spec, err := Parse(data)
			So(err, ShouldBeNil)
			So(spec.Version, ShouldEqual, "1")
			So(spec.Name, ShouldEqual, "test-workflow")
			So(len(spec.Steps), ShouldEqual, 1)
			So(spec.Steps[0].ID, ShouldEqual, "step1")
		})

		Convey("invalid YAML", func() {
			_, err := Parse([]byte(`version: "1": broken`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid YAML")
		})

		Convey("missing version", func() {
			_, err := Parse([]byte(`name: test`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unsupported version")
		})

		Convey("wrong version", func() {
			_, err := Parse([]byte(`version: "2"` + "\nname: test\nsteps:\n  - id: s\n    prompt: p"))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unsupported version")
		})

		Convey("missing name", func() {
			_, err := Parse([]byte(`version: "1"` + "\nsteps:\n  - id: s\n    prompt: p"))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "name is required")
		})

		Convey("no steps", func() {
			_, err := Parse([]byte(`version: "1"` + "\nname: test"))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "at least one step")
		})

		Convey("missing step id", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - prompt: hi
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "missing id")
		})

		Convey("duplicate step id", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
    prompt: hi
  - id: step1
    prompt: bye
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "duplicate step id")
		})

		Convey("missing prompt", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "missing prompt")
		})

		Convey("depends on self", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
    prompt: hi
    depends_on: [step1]
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "depends on itself")
		})

		Convey("depends on unknown step", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
    prompt: hi
    depends_on: [nope]
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "depends on unknown step")
		})

		Convey("foreach references unknown step", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
    prompt: hi
  - id: step2
    prompt: bye
    foreach: "$nope.files"
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "foreach references unknown step")
		})

		Convey("invalid foreach expression", func() {
			_, err := Parse([]byte(`
version: "1"
name: test
steps:
  - id: step1
    prompt: hi
  - id: step2
    prompt: bye
    foreach: "$"
`))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid foreach expression")
		})
	})
}

func TestDetectCycle(t *testing.T) {
	Convey("detectCycle", t, func() {
		Convey("no cycle — linear", func() {
			spec := &WorkflowSpec{
				Version: "1",
				Name:    "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
					{ID: "c", Prompt: "c", DependsOn: []string{"b"}},
				},
			}
			So(detectCycle(spec), ShouldBeNil)
		})

		Convey("no cycle — diamond", func() {
			spec := &WorkflowSpec{
				Version: "1",
				Name:    "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
					{ID: "c", Prompt: "c", DependsOn: []string{"a"}},
					{ID: "d", Prompt: "d", DependsOn: []string{"b", "c"}},
				},
			}
			So(detectCycle(spec), ShouldBeNil)
		})

		Convey("cycle detected — direct", func() {
			spec := &WorkflowSpec{
				Version: "1",
				Name:    "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a", DependsOn: []string{"b"}},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
				},
			}
			So(detectCycle(spec), ShouldNotBeNil)
		})

		Convey("cycle detected — indirect", func() {
			spec := &WorkflowSpec{
				Version: "1",
				Name:    "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a", DependsOn: []string{"c"}},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
					{ID: "c", Prompt: "c", DependsOn: []string{"b"}},
				},
			}
			So(detectCycle(spec), ShouldNotBeNil)
		})
	})
}

func TestGenerateID(t *testing.T) {
	Convey("GenerateID", t, func() {
		id1 := GenerateID()
		id2 := GenerateID()
		So(id1, ShouldNotEqual, id2)
		So(len(id1), ShouldBeGreaterThan, 0)
	})
}

// ---------------------------------------------------------------------------
// Template
// ---------------------------------------------------------------------------

func FuzzCompileTemplate(f *testing.F) {
	f.Add("hello $step.field world")
	f.Add("$")
	f.Add("$step")
	f.Add("$step.field")
	f.Add("$step[*].result")
	f.Add("$audit[0].finding")
	f.Add("")
	f.Add("$$")
	f.Add("$.")
	f.Add("$[0]")
	f.Add("$step.field $other.field with text $and_after")
	f.Add("{{already compiled}}")

	f.Fuzz(func(t *testing.T, prompt string) {
		tmpl, err := compile(prompt)
		if err != nil {
			return
		}
		if tmpl == nil {
			t.Errorf("compile returned nil template without error for input: %q", prompt)
		}
	})
}

func TestCompile(t *testing.T) {
	Convey("compile", t, func() {
		Convey("plain text pass-through", func() {
			tmpl, err := compile("hello world")
			So(err, ShouldBeNil)
			var buf strings.Builder
			err = tmpl.Execute(&buf, nil)
			So(err, ShouldBeNil)
			So(buf.String(), ShouldEqual, "hello world")
		})

		Convey("$step.field replacement", func() {
			tmpl, err := compile("Result: $audit.score")
			So(err, ShouldBeNil)
			var buf strings.Builder
			err = tmpl.Execute(&buf, map[string]any{
				"audit": map[string]any{"score": 95},
			})
			So(err, ShouldBeNil)
			So(buf.String(), ShouldEqual, "Result: 95")
		})

		Convey("plain $step reference", func() {
			tmpl, err := compile("Value: $total")
			So(err, ShouldBeNil)
			var buf strings.Builder
			err = tmpl.Execute(&buf, map[string]any{
				"total": 42,
			})
			So(err, ShouldBeNil)
			So(buf.String(), ShouldEqual, "Value: 42")
		})

		Convey("$step[*].field range collection", func() {
			tmpl, err := compile("Files: [$audit[*].result]")
			So(err, ShouldBeNil)
			var buf strings.Builder
			err = tmpl.Execute(&buf, map[string]any{
				"audit": []map[string]any{
					{"result": "ok1"},
					{"result": "ok2"},
				},
			})
			So(err, ShouldBeNil)
			So(buf.String(), ShouldContainSubstring, "ok1")
			So(buf.String(), ShouldContainSubstring, "ok2")
		})

		Convey("$step[0].field index access", func() {
			tmpl, err := compile("First: $audit[0].result")
			So(err, ShouldBeNil)
			var buf strings.Builder
			err = tmpl.Execute(&buf, map[string]any{
				"audit": []map[string]any{
					{"result": "first"},
					{"result": "second"},
				},
			})
			So(err, ShouldBeNil)
			So(buf.String(), ShouldEqual, "First: first")
		})
	})
}

func TestRenderPrompt(t *testing.T) {
	Convey("renderPrompt", t, func() {
		Convey("renders template variables", func() {
			result, err := renderPrompt("Score: $audit.score", map[string]any{
				"audit": map[string]any{"score": 88},
			})
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "Score: 88")
		})

		Convey("error on missing key", func() {
			_, err := renderPrompt("Score: $audit.nope", map[string]any{})
			So(err, ShouldNotBeNil)
		})
	})
}

func TestBuildTemplateData(t *testing.T) {
	Convey("buildTemplateData", t, func() {
		Convey("with single step result", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{
					{ID: "list", Prompt: "list"},
					{ID: "check", Prompt: "check $list.files"},
				},
			}
			rs := newRunState(spec)
			rs.markDone("list", nil, time.Second, map[string]any{"files": []any{"a.go", "b.go"}})

			data := buildTemplateData(rs, "check")
			So(data, ShouldNotBeNil)
			listResult := data["list"]
			So(listResult, ShouldNotBeNil)
		})

		Convey("with foreach instances", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{{ID: "audit", Prompt: "audit"}},
			}
			rs := newRunState(spec)
			rs.markDone("audit", []InstanceResult{
				{Key: "audit[a.go]", Status: StatusDone, Result: map[string]any{"score": 10}},
				{Key: "audit[b.go]", Status: StatusDone, Result: map[string]any{"score": 20}},
			}, time.Second, nil)

			data := buildTemplateData(rs, "")
			instList, ok := data["audit"].([]map[string]any)
			So(ok, ShouldBeTrue)
			So(len(instList), ShouldEqual, 2)
		})

		Convey("with non-map result", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{{ID: "s", Prompt: "p"}},
			}
			rs := newRunState(spec)
			rs.markDone("s", nil, time.Second, "plain string")

			data := buildTemplateData(rs, "")
			So(data["s"], ShouldEqual, "plain string")
		})

		Convey("with failed step not included in data", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{{ID: "s", Prompt: "p"}},
			}
			rs := newRunState(spec)
			rs.markFailed("s", "boom")

			data := buildTemplateData(rs, "")
			// Failed steps are excluded from template data (only StatusDone steps included).
			_, ok := data["s"]
			So(ok, ShouldBeFalse)
		})
	})
}

func TestResolveResult(t *testing.T) {
	Convey("resolveResult", t, func() {
		results := map[string]*StepResult{
			"list": {ID: "list", Status: StatusDone, Result: map[string]any{"files": []any{"a.go"}}},
		}
		Convey("resolves known field", func() {
			v, err := resolveResult(results, "list", "files")
			So(err, ShouldBeNil)
			So(v, ShouldNotBeNil)
		})

		Convey("errors on unknown step", func() {
			_, err := resolveResult(results, "nope", "files")
			So(err, ShouldNotBeNil)
		})

		Convey("errors on unknown field", func() {
			_, err := resolveResult(results, "list", "nope")
			So(err, ShouldNotBeNil)
		})

		Convey("errors on nil result", func() {
			results2 := map[string]*StepResult{
				"empty": {ID: "empty", Status: StatusDone},
			}
			_, err := resolveResult(results2, "empty", "x")
			So(err, ShouldNotBeNil)
		})

		Convey("errors on non-map result", func() {
			results3 := map[string]*StepResult{
				"plain": {ID: "plain", Status: StatusDone, Result: "just a string"},
			}
			_, err := resolveResult(results3, "plain", "x")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestCollectField(t *testing.T) {
	Convey("collectField", t, func() {
		Convey("collects field from all instances", func() {
			results := map[string]*StepResult{
				"audit": {
					ID:     "audit",
					Status: StatusDone,
					Instances: []InstanceResult{
						{Key: "a", Status: StatusDone, Result: map[string]any{"score": 10}},
						{Key: "b", Status: StatusDone, Result: map[string]any{"score": 20}},
					},
				},
			}
			scores, err := collectField(results, "audit", "score")
			So(err, ShouldBeNil)
			So(len(scores), ShouldEqual, 2)
		})

		Convey("errors on unknown step", func() {
			_, err := collectField(map[string]*StepResult{}, "nope", "field")
			So(err, ShouldNotBeNil)
		})

		Convey("skips failed instances", func() {
			results := map[string]*StepResult{
				"audit": {
					ID:     "audit",
					Status: StatusDone,
					Instances: []InstanceResult{
						{Key: "a", Status: StatusDone, Result: map[string]any{"score": 10}},
						{Key: "b", Status: StatusFailed, Result: nil},
					},
				},
			}
			scores, _ := collectField(results, "audit", "score")
			So(len(scores), ShouldEqual, 1)
		})
	})
}

// ---------------------------------------------------------------------------
// Checkpoint / runState
// ---------------------------------------------------------------------------

func TestRunState(t *testing.T) {
	Convey("runState", t, func() {
		spec := &WorkflowSpec{
			Version: "1", Name: "test",
			Steps: []StepSpec{
				{ID: "a", Prompt: "a"},
				{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
				{ID: "c", Prompt: "c", DependsOn: []string{"a"}},
			},
		}

		Convey("newRunState initializes pending", func() {
			rs := newRunState(spec)
			So(rs.statuses["a"], ShouldEqual, StatusPending)
			So(rs.statuses["b"], ShouldEqual, StatusPending)
			So(len(rs.order), ShouldEqual, 3)
		})

		Convey("markRunning, markDone, markFailed, markSkipped", func() {
			rs := newRunState(spec)
			rs.markRunning("a")
			So(rs.statuses["a"], ShouldEqual, StatusRunning)

			rs.markDone("a", nil, time.Millisecond*100, nil)
			So(rs.statuses["a"], ShouldEqual, StatusDone)
			So(rs.results["a"].Duration, ShouldNotBeEmpty)

			rs.markFailed("b", "error reason")
			So(rs.statuses["b"], ShouldEqual, StatusFailed)
			So(rs.results["b"].Error, ShouldEqual, "error reason")

			rs.markSkipped("c")
			So(rs.statuses["c"], ShouldEqual, StatusSkipped)
		})

		Convey("findReady returns steps with all deps done", func() {
			rs := newRunState(spec)
			ready := rs.findReady()
			So(len(ready), ShouldEqual, 1)
			So(ready[0], ShouldEqual, "a")

			rs.markDone("a", nil, 0, nil)
			ready = rs.findReady()
			So(len(ready), ShouldEqual, 2)
			So(ready, ShouldContain, "b")
			So(ready, ShouldContain, "c")
		})

		Convey("allDone when all terminal", func() {
			rs := newRunState(spec)
			So(rs.allDone(), ShouldBeFalse)
			rs.markFailed("a", "fail")
			rs.markSkipped("b")
			rs.markSkipped("c")
			So(rs.allDone(), ShouldBeTrue)
		})

		Convey("skipDependents cascades", func() {
			rs := newRunState(spec)
			rs.markFailed("a", "fail")
			rs.skipDependents("a")
			So(rs.statuses["b"], ShouldEqual, StatusSkipped)
			So(rs.statuses["c"], ShouldEqual, StatusSkipped)
		})

		Convey("skipDependents skips step whose dep was already skipped independently", func() {
			spec2 := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{
					{ID: "x", Prompt: "x"},
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
					{ID: "c", Prompt: "c", DependsOn: []string{"x", "b"}},
				},
			}
			rs := newRunState(spec2)
			rs.markSkipped("x")
			rs.markFailed("a", "fail")
			rs.skipDependents("a")
			So(rs.statuses["b"], ShouldEqual, StatusSkipped)
			So(rs.statuses["c"], ShouldEqual, StatusSkipped)
		})

		Convey("hasFailures and failReason", func() {
			rs := newRunState(spec)
			So(rs.hasFailures(), ShouldBeFalse)
			rs.markFailed("a", "bad things")
			So(rs.hasFailures(), ShouldBeTrue)
			So(rs.failReason(), ShouldContainSubstring, "bad things")
		})
	})
}

func TestValidateContinue(t *testing.T) {
	Convey("validateContinue", t, func() {
		Convey("allows new steps", func() {
			prev := &WorkflowResult{
				Steps: []StepResult{
					{ID: "a", Status: StatusDone},
				},
			}
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b"},
				},
			}
			So(validateContinue(prev, spec), ShouldBeNil)
		})

		Convey("allows completed step to remain", func() {
			prev := &WorkflowResult{
				Steps: []StepResult{
					{ID: "a", Status: StatusDone},
				},
			}
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
				},
			}
			So(validateContinue(prev, spec), ShouldBeNil)
		})

		Convey("allows pending step modifications", func() {
			prev := &WorkflowResult{
				Steps: []StepResult{
					{ID: "a", Status: StatusPending},
				},
			}
			spec := &WorkflowSpec{
				Version: "1", Name: "test",
				Steps: []StepSpec{
					{ID: "a", Prompt: "changed"},
				},
			}
			So(validateContinue(prev, spec), ShouldBeNil)
		})
	})
}

func TestTruncateKey(t *testing.T) {
	Convey("truncateKey", t, func() {
		So(truncateKey("short", 40), ShouldEqual, "short")
		So(truncateKey("a very long string that exceeds forty characters significantly", 40), ShouldNotEqual, "a very long string that exceeds forty characters significantly")
		So(len(truncateKey("a very long string that exceeds forty characters significantly", 40)), ShouldBeLessThanOrEqualTo, 40)
	})
}

// ---------------------------------------------------------------------------
// Result persistence
// ---------------------------------------------------------------------------

func TestResultWriteAndLoad(t *testing.T) {
	Convey("writeResult / loadResult / restoreState", t, func() {
		Convey("write and load result file", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test_result",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
				},
			}
			path := spec.Name + ".result.yaml"
			defer os.Remove(path)

			rs := newRunState(spec)
			rs.markDone("a", nil, time.Second, map[string]any{"ok": true})
			rs.markRunning("b")

			err := writeResult(spec, rs, "running", time.Now(), "")
			So(err, ShouldBeNil)

			loaded, err := loadResult(path)
			So(err, ShouldBeNil)
			So(loaded.Status, ShouldEqual, "running")
			So(len(loaded.Steps), ShouldBeGreaterThanOrEqualTo, 1)
		})

		Convey("loadResult file not found", func() {
			_, err := loadResult("nonexistent.yaml")
			So(err, ShouldNotBeNil)
		})

		Convey("restoreState populates runState from previous result", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test_restore",
				Steps: []StepSpec{
					{ID: "a", Prompt: "a"},
					{ID: "b", Prompt: "b"},
				},
			}
			prev := &WorkflowResult{
				Workflow: "test_restore",
				Status:   "running",
				Steps: []StepResult{
					{ID: "a", Status: StatusDone, Result: map[string]any{"x": 1}},
				},
			}
			rs := newRunState(spec)
			restoreState(spec, prev, rs)
			So(rs.statuses["a"], ShouldEqual, StatusDone)
			So(rs.results["a"].Result, ShouldNotBeNil)
			So(rs.statuses["b"], ShouldEqual, StatusPending) // not in prev
		})

		Convey("restoreState with foreach instances", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "test_restore_foreach",
				Steps: []StepSpec{
					{ID: "audit", Prompt: "audit"},
				},
			}
			prev := &WorkflowResult{
				Steps: []StepResult{
					{
						ID: "audit", Status: StatusDone,
						Instances: []InstanceResult{
							{Key: "audit[a.go]", Status: StatusDone},
							{Key: "audit[b.go]", Status: StatusDone},
						},
					},
				},
			}
			rs := newRunState(spec)
			restoreState(spec, prev, rs)
			So(rs.instance["audit"], ShouldNotBeNil)
			So(len(rs.instance["audit"]), ShouldEqual, 2)
		})
	})
}

func TestBuildStepResults(t *testing.T) {
	Convey("buildStepResults", t, func() {
		spec := &WorkflowSpec{
			Version: "1", Name: "test",
			Steps: []StepSpec{
				{ID: "a", Prompt: "a"},
				{ID: "b", Prompt: "b"},
			},
		}
		rs := newRunState(spec)
		rs.markDone("a", nil, 0, nil)

		results := buildStepResults(rs)
		So(len(results), ShouldEqual, 2)
	})
}

// ---------------------------------------------------------------------------
// Executor helpers (parseOutput, extractJSON, matchSchema)
// ---------------------------------------------------------------------------

func TestParseOutput(t *testing.T) {
	Convey("parseOutput", t, func() {
		Convey("returns raw string when no schema", func() {
			So(parseOutput("hello", nil), ShouldEqual, "hello")
		})

		Convey("extracts JSON from markdown fences", func() {
			result := parseOutput("```json\n{\"score\": 95}\n```", map[string]any{"score": "number"})
			m, ok := result.(map[string]any)
			So(ok, ShouldBeTrue)
			So(m["score"], ShouldEqual, float64(95))
		})

		Convey("returns raw on type mismatch", func() {
			result := parseOutput("```json\n{\"score\": \"not-a-number\"}\n```", map[string]any{"score": "number"})
			So(result, ShouldEqual, "```json\n{\"score\": \"not-a-number\"}\n```")
		})

		Convey("returns raw on invalid JSON", func() {
			result := parseOutput("```json\nnot json\n```", map[string]any{"score": "number"})
			So(result, ShouldEqual, "```json\nnot json\n```")
		})

		Convey("extracts JSON array", func() {
			result := parseOutput("[1, 2, 3]", map[string]any{"type": "array"})
			arr, ok := result.([]any)
			So(ok, ShouldBeTrue)
			So(len(arr), ShouldEqual, 3)
		})
	})
}

func TestExtractJSON(t *testing.T) {
	Convey("extractJSON", t, func() {
		Convey("extracts json from fences", func() {
			s := extractJSON("```json\n{\"a\":1}\n```")
			So(s, ShouldEqual, "{\"a\":1}")
		})

		Convey("extracts from plain fences", func() {
			s := extractJSON("```\n{\"a\":1}\n```")
			So(s, ShouldEqual, "{\"a\":1}")
		})

		Convey("finds bare JSON object", func() {
			s := extractJSON("  {\"a\":1}")
			So(s, ShouldEqual, "{\"a\":1}")
		})

		Convey("finds bare JSON array", func() {
			s := extractJSON("[1,2]")
			So(s, ShouldEqual, "[1,2]")
		})

		Convey("returns empty for non-JSON", func() {
			s := extractJSON("just some text")
			So(s, ShouldEqual, "")
		})
	})
}

func TestMatchSchema(t *testing.T) {
	Convey("matchSchema", t, func() {
		Convey("string type", func() {
			So(matchSchema("hello", "string"), ShouldBeTrue)
			So(matchSchema(42, "string"), ShouldBeFalse)
		})

		Convey("number type", func() {
			So(matchSchema(float64(3.14), "number"), ShouldBeTrue)
			So(matchSchema(42, "number"), ShouldBeTrue)
			So(matchSchema(int64(42), "number"), ShouldBeTrue)
			So(matchSchema("42", "number"), ShouldBeFalse)
		})

		Convey("boolean type", func() {
			So(matchSchema(true, "boolean"), ShouldBeTrue)
			So(matchSchema(1, "boolean"), ShouldBeFalse)
		})

		Convey("array of strings", func() {
			So(matchSchema([]any{"a", "b"}, []any{"string"}), ShouldBeTrue)
			So(matchSchema("not-array", []any{"string"}), ShouldBeFalse)
		})

		Convey("unknown schema type accepts anything", func() {
			So(matchSchema(42, 99), ShouldBeTrue)
		})
	})
}

// ---------------------------------------------------------------------------
// Engine — integration tests with mock LLM
// ---------------------------------------------------------------------------

func TestEngineRunLinear(t *testing.T) {
	Convey("Engine.Run — linear workflow", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		var callCount int32
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				atomic.AddInt32(&callCount, 1)
				msg := req.Messages[0].Content
				var resp string
				switch {
				case strings.Contains(msg, "step a"):
					resp = "```json\n{\"done\": true}\n```"
				case strings.Contains(msg, "step b"):
					resp = "```json\n{\"ok\": \"yes\"}\n```"
				default:
					resp = "ok"
				}
				return []llm.LLMChunk{textChunk(resp)}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "linear",
			Steps: []StepSpec{
				{ID: "a", Prompt: "step a", OutputSchema: map[string]any{"done": "boolean"}},
				{ID: "b", Prompt: "step b", DependsOn: []string{"a"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(callCount, ShouldEqual, 2)
	})
}

func TestEngineRunParallelFanOut(t *testing.T) {
	Convey("Engine.Run — parallel fan-out", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		var mu sync.Mutex
		completed := make(map[string]bool)
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				mu.Lock()
				switch {
				case strings.Contains(msg, "setup"):
					completed["setup"] = true
				case strings.Contains(msg, "branch a"):
					completed["a"] = true
				case strings.Contains(msg, "branch b"):
					completed["b"] = true
				}
				mu.Unlock()
				return []llm.LLMChunk{textChunk("done")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "fanout",
			Steps: []StepSpec{
				{ID: "setup", Prompt: "setup"},
				{ID: "a", Prompt: "branch a", DependsOn: []string{"setup"}},
				{ID: "b", Prompt: "branch b", DependsOn: []string{"setup"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(completed["a"], ShouldBeTrue)
		So(completed["b"], ShouldBeTrue)
	})
}

func TestEngineRunFailureCascade(t *testing.T) {
	Convey("Engine.Run — failure cascades to dependents", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				if strings.Contains(msg, "good") {
					return []llm.LLMChunk{textChunk("ok")}
				}
				return []llm.LLMChunk{
					{Error: &testError{"mock failure"}},
				}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "fail_cascade",
			Steps: []StepSpec{
				{ID: "bad", Prompt: "bad"},
				{ID: "good", Prompt: "good"},
				{ID: "dep_on_bad", Prompt: "dep", DependsOn: []string{"bad"}},
			},
		}
		// Reset call count per test
		mock.chunksFn = func(req llm.LLMRequest) []llm.LLMChunk {
			msg := req.Messages[0].Content
			if strings.Contains(msg, "good") {
				return []llm.LLMChunk{textChunk("ok")}
			}
			return []llm.LLMChunk{{Error: &testError{"mock failure"}}}
		}

		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldNotBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "failed")
	})
}

func TestEngineRunCheckpoint(t *testing.T) {
	Convey("Engine.Run — checkpoint pause", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				if strings.Contains(msg, "prepare") {
					return []llm.LLMChunk{textChunk("ready")}
				}
				return []llm.LLMChunk{textChunk("```json\n{\"reviewed\": true}\n```")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "checkpoint_test",
			Steps: []StepSpec{
				{ID: "prepare", Prompt: "prepare"},
				{ID: "review", Prompt: "review", Checkpoint: true, DependsOn: []string{"prepare"}},
			},
		}
		defer os.Remove("checkpoint_test.result.yaml")

		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldEqual, ErrCheckpointReached)
		So(result, ShouldBeNil)
	})
}

func TestEngineContinue(t *testing.T) {
	Convey("Engine.Continue", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("done")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		Convey("errors without prior result file", func() {
			spec := &WorkflowSpec{
				Version: "1", Name: "no_result",
				Steps: []StepSpec{{ID: "a", Prompt: "a"}},
			}
			_, err := engine.Continue(context.Background(), spec, "")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cannot continue")
		})

		Convey("errors when status is not paused", func() {
			path := "not_paused.result.yaml"
			os.WriteFile(path, []byte(`workflow: not_paused
status: completed
steps: []
`), 0644)
			defer os.Remove(path)

			spec := &WorkflowSpec{
				Version: "1", Name: "not_paused",
				Steps: []StepSpec{{ID: "a", Prompt: "a"}},
			}
			_, err := engine.Continue(context.Background(), spec, "")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cannot continue")
		})
	})
}

func TestEngineParseAndRun(t *testing.T) {
	Convey("Engine.ParseAndRun", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		Convey("parses and runs valid YAML", func() {
			data := []byte(`
version: "1"
name: parse_and_run
steps:
  - id: a
    prompt: hi
`)
			result, err := engine.ParseAndRun(context.Background(), data, "")
			So(err, ShouldBeNil)
			So(result.Status, ShouldEqual, "completed")
		})

		Convey("returns error on invalid YAML", func() {
			_, err := engine.ParseAndRun(context.Background(), []byte(`garbage`), "")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestEngineProgress(t *testing.T) {
	Convey("Engine progress callback", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		var progressMsgs []string
		engine.SetOnProgress(func(tr agentio.TurnResult) {
			progressMsgs = append(progressMsgs, tr.Text)
		})

		spec := &WorkflowSpec{
			Version: "1", Name: "progress_test",
			Steps: []StepSpec{{ID: "a", Prompt: "a"}},
		}
		_, err := engine.Run(context.Background(), spec, "transport-1")
		So(err, ShouldBeNil)
		So(len(progressMsgs), ShouldBeGreaterThan, 0)
	})
}

// ---------------------------------------------------------------------------
// Event publishing
// ---------------------------------------------------------------------------

func TestEnginePublishEvent(t *testing.T) {
	Convey("Engine event publishing", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		var events []event.Event
		eventBus.Subscribe(func(ctx context.Context, e event.Event) {
			events = append(events, e)
		})

		spec := &WorkflowSpec{
			Version: "1", Name: "event_test",
			Steps: []StepSpec{{ID: "a", Prompt: "a"}},
		}
		engine.Run(context.Background(), spec, "")

		So(len(events), ShouldBeGreaterThan, 0)
		hasStart := false
		hasComplete := false
		for _, e := range events {
			if e.Type == event.EventWorkflowStart {
				hasStart = true
			}
			if e.Type == event.EventWorkflowComplete {
				hasComplete = true
			}
		}
		So(hasStart, ShouldBeTrue)
		So(hasComplete, ShouldBeTrue)
	})
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

func TestRunWorkflowHandler(t *testing.T) {
	Convey("runWorkflowHandler", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := runWorkflowHandler(engine, logger)

		Convey("errors on missing path", func() {
			result, err := handler(context.Background(), json.RawMessage(`{}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "path is required")
		})

		Convey("errors on invalid args", func() {
			result, err := handler(context.Background(), json.RawMessage(`bad`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
		})
	})
}

func TestContinueWorkflowHandler(t *testing.T) {
	Convey("continueWorkflowHandler", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := continueWorkflowHandler(engine, logger)

		Convey("errors on missing path", func() {
			result, err := handler(context.Background(), json.RawMessage(`{}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
		})

		Convey("errors on file not found", func() {
			result, err := handler(context.Background(), json.RawMessage(`{"path":"/nonexistent/workflow.yaml"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "Failed to read")
		})
	})
}

func TestRegisterTools(t *testing.T) {
	Convey("RegisterTools", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := tool.NewRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{}
		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		RegisterTools(toolReg, engine, nil, logger)

		tools, err := toolReg.List(context.Background())
		So(err, ShouldBeNil)
		names := make(map[string]bool)
		for _, t := range tools {
			names[t.Name] = true
		}
		So(names["run_workflow"], ShouldBeTrue)
		So(names["continue_workflow"], ShouldBeTrue)
	})
}

// ---------------------------------------------------------------------------
// Engine — tool loop in executor
// ---------------------------------------------------------------------------

func TestEngineToolLoop(t *testing.T) {
	Convey("Engine executor tool loop", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := tool.NewRegistry()

		// Register a simple echo builtin.
		toolReg.RegisterBuiltin(
			"echo",
			"echoes input",
			json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
			func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				var p struct{ Text string }
				json.Unmarshal(args, &p)
				return &types.ToolResult{Content: "echo: " + p.Text}, nil
			},
		)

		eventBus := event.NewBus()

		// First chunk: tool call, second chunk: text after tool result
		callCount := 0
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				callCount++
				if callCount == 1 {
					return []llm.LLMChunk{
						toolChunk("echo", "tc1", `{"text":"hello"}`),
					}
				}
				return []llm.LLMChunk{textChunk("final answer")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		inst := stepInstance{
			StepID: "test",
			Key:    "test",
			Prompt: "use echo",
		}
		result := engine.executeStep(context.Background(), inst)
		So(result.Status, ShouldEqual, StatusDone)
		So(callCount, ShouldEqual, 2)
	})
}

// ---------------------------------------------------------------------------
// Engine — foreach expansion
// ---------------------------------------------------------------------------

func TestEngineForeachExpansion(t *testing.T) {
	Convey("Engine.Run — foreach expansion", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				if strings.Contains(msg, "list files") {
					return []llm.LLMChunk{textChunk("```json\n{\"files\": [\"a.go\", \"b.go\", \"c.go\"]}\n```")}
				}
				return []llm.LLMChunk{textChunk("audited")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "foreach_test",
			Steps: []StepSpec{
				{ID: "list", Prompt: "list files", OutputSchema: map[string]any{"files": "array"}},
				{ID: "audit", Prompt: "audit file $each", ForEach: "$list.files", DependsOn: []string{"list"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
	})
}

func TestEngineForeachWithEachKey(t *testing.T) {
	Convey("Engine.Run — foreach with $each.key access", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("```json\n{\"items\": [{\"name\": \"x\", \"val\": 1}, {\"name\": \"y\", \"val\": 2}]}\n```")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "each_key_test",
			Steps: []StepSpec{
				{ID: "data", Prompt: "get data", OutputSchema: map[string]any{"items": "array"}},
				{ID: "process", Prompt: "process $each.name with value $each.val", ForEach: "$data.items", DependsOn: []string{"data"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
	})
}

// ---------------------------------------------------------------------------
// Engine — resume from checkpoint
// ---------------------------------------------------------------------------

func TestEngineResumeCheckpoint(t *testing.T) {
	Convey("Engine.Run — resume from partial result", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		// Pre-create a result file showing step "a" already done.
		path := "resume_test.result.yaml"
		os.WriteFile(path, []byte(`workflow: resume_test
status: running
steps:
  - id: a
    status: done
    result:
      ok: true
  - id: b
    status: pending
`), 0644)
		defer os.Remove(path)

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				msg := req.Messages[0].Content
				if strings.Contains(msg, "step b") {
					return []llm.LLMChunk{textChunk("done")}
				}
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "resume_test",
			Steps: []StepSpec{
				{ID: "a", Prompt: "step a"},
				{ID: "b", Prompt: "step b", DependsOn: []string{"a"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
	})
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEngineNoSteps(t *testing.T) {
	Convey("Engine.Run — no steps", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()
		mock := &mockLLMProvider{}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		// This is a degenerate case; the spec validation should reject 0 steps
		// but if someone bypasses validation, allDone should return true immediately.
		spec := &WorkflowSpec{
			Version: "1", Name: "empty",
			Steps: []StepSpec{},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
	})
}

func TestEngineContextCancellation(t *testing.T) {
	Convey("Engine.Run — context cancellation", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				time.Sleep(200 * time.Millisecond)
				return []llm.LLMChunk{textChunk("too late")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "cancel_test",
			Steps: []StepSpec{{ID: "a", Prompt: "a"}},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, _ := engine.Run(ctx, spec, "")
		So(result, ShouldNotBeNil)
	})
}

func TestEngineDeadlockDetection(t *testing.T) {
	Convey("Engine.Run — deadlock detection", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()
		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{{Error: &testError{"fail"}}}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "deadlock",
			Steps: []StepSpec{
				{ID: "a", Prompt: "a", DependsOn: []string{"nonexistent"}},
			},
		}
		result, err := engine.Run(context.Background(), spec, "")
		So(err, ShouldNotBeNil)
		So(result, ShouldNotBeNil)
		So(result.Status, ShouldEqual, "failed")
	})
}

func TestSetOnProgress(t *testing.T) {
	Convey("SetOnProgress", t, func() {
		engine := &Engine{}
		called := false
		engine.SetOnProgress(func(tr agentio.TurnResult) {
			called = true
		})
		engine.progress("t1", "hello")
		So(called, ShouldBeTrue)
	})
}

func TestPublishEventWithNilBus(t *testing.T) {
	Convey("publishEvent with nil bus", t, func() {
		engine := &Engine{eventBus: nil}
		// Should not panic.
		engine.publishEvent(context.Background(), event.EventWorkflowStart, "test", nil)
		engine.publishStepEvent(context.Background(), "test", "s1", StatusRunning, nil)
	})
}

func TestExpandForeachErrors(t *testing.T) {
	Convey("expandForeach errors", t, func() {
		logger := testLogger()
		cfg := testConfig()
		engine := NewEngine(nil, nil, nil, logger, nil, cfg)

		Convey("non-foreach step returns single instance", func() {
			step := StepSpec{ID: "s", Prompt: "p"}
			inst, err := engine.expandForeach(step, newRunState(&WorkflowSpec{
				Version: "1", Name: "t",
				Steps: []StepSpec{{ID: "s", Prompt: "p"}},
			}))
			So(err, ShouldBeNil)
			So(len(inst), ShouldEqual, 1)
			So(inst[0].Key, ShouldEqual, "s")
		})
	})
}

// ---------------------------------------------------------------------------
// Engine — Continue with paused result file
// ---------------------------------------------------------------------------

func TestEngineContinueWithPausedFile(t *testing.T) {
	Convey("Engine.Continue with paused result file", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)

		spec := &WorkflowSpec{
			Version: "1", Name: "paused_continue",
			Steps: []StepSpec{
				{ID: "a", Prompt: "a"},
				{ID: "b", Prompt: "b", DependsOn: []string{"a"}},
			},
		}

		// Create a paused result file to resume from.
		resumeFile := spec.Name + ".result.yaml"
		defer func() { _ = os.Remove(resumeFile) }()
		paused := &WorkflowResult{
			Workflow: spec.Name,
			Status:   "paused",
			Steps: []StepResult{
				{ID: "a", Status: StatusDone, Result: map[string]any{"x": 1}},
				{ID: "b", Status: StatusPending},
			},
		}
		data, _ := yaml.Marshal(paused)
		_ = os.WriteFile(resumeFile, data, 0600)
		result, err := engine.Continue(context.Background(), spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
	})
}

// ---------------------------------------------------------------------------
// Tool handlers — full path tests
// ---------------------------------------------------------------------------

func TestRunWorkflowHandlerWithFile(t *testing.T) {
	Convey("runWorkflowHandler with temp file", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := runWorkflowHandler(engine, logger)

		Convey("reads and runs valid workflow file", func() {
			f, _ := os.CreateTemp("", "test-workflow-*.yaml")
			defer os.Remove(f.Name())
			f.WriteString("version: \"1\"\nname: handler_test\nsteps:\n  - id: a\n    prompt: hi\n")
			f.Close()

			result, err := handler(context.Background(), json.RawMessage(`{"path":"`+f.Name()+`"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "completed")
		})

		Convey("errors on invalid workflow YAML", func() {
			f, _ := os.CreateTemp("", "test-bad-workflow-*.yaml")
			defer os.Remove(f.Name())
			f.WriteString("garbage")
			f.Close()

			result, err := handler(context.Background(), json.RawMessage(`{"path":"`+f.Name()+`"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "Invalid workflow")
		})

		Convey("errors on nonexistent file", func() {
			result, err := handler(context.Background(), json.RawMessage(`{"path":"/nonexistent/file.yaml"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "Failed to read")
		})
	})
}

func TestContinueWorkflowHandlerWithFile(t *testing.T) {
	Convey("continueWorkflowHandler with invalid YAML", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{}
		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := continueWorkflowHandler(engine, logger)

		Convey("errors on invalid args JSON", func() {
			result, err := handler(context.Background(), json.RawMessage(`bad`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
		})

		Convey("errors on nonexistent file", func() {
			result, err := handler(context.Background(), json.RawMessage(`{"path":"/nonexistent/continue.yaml"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
		})
	})
}
func TestCollectFieldSkipNilResult(t *testing.T) {
	Convey("collectField skips instances with nil Result", t, func() {
		results := map[string]*StepResult{
			"list": {
				ID:     "list",
				Status: StatusDone,
				Instances: []InstanceResult{
					{Key: "a", Status: StatusDone, Result: nil},
					{Key: "b", Status: StatusDone, Result: map[string]any{"name": "b"}},
				},
			},
		}
		values, err := collectField(results, "list", "name")
		So(err, ShouldBeNil)
		So(len(values), ShouldEqual, 1)
	})
}

// ---------------------------------------------------------------------------
// testError — simple error type for mock error chunks
// ---------------------------------------------------------------------------

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// continueWorkflowHandler — full path tests with result files
// ---------------------------------------------------------------------------

func TestContinueWorkflowHandlerFullPaths(t *testing.T) {
	Convey("continueWorkflowHandler full paths", t, func() {
		logger := testLogger()
		cfg := testConfig()
		toolReg := emptyRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}
		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := continueWorkflowHandler(engine, logger)

		Convey("success: completes workflow from paused result", func() {
			specName := "handler_ct_success"
			resumeFile := specName + ".result.yaml"
			defer os.Remove(resumeFile)
			paused := &WorkflowResult{
				Workflow: specName,
				Status:   "paused",
				Steps: []StepResult{
					{ID: "a", Status: StatusDone, Result: map[string]any{"x": 1}},
					{ID: "b", Status: StatusPending},
				},
			}
			data, _ := yaml.Marshal(paused)
			os.WriteFile(resumeFile, data, 0644)

			yamlData := []byte("version: \"1\"\nname: " + specName + "\nsteps:\n  - id: a\n    prompt: a\n  - id: b\n    prompt: b\n    depends_on: [a]\n")
			yf := writeTempFile(t, specName+".workflow.yaml", string(yamlData))
			defer os.Remove(yf)

			result, err := handler(context.Background(), json.RawMessage(`{"path":"`+yf+`"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "completed")
		})

		Convey("checkpoint: returns paused message when next checkpoint reached", func() {
			specName := "handler_ct_ckpt"
			resumeFile := specName + ".result.yaml"
			defer os.Remove(resumeFile)
			paused := &WorkflowResult{
				Workflow: specName,
				Status:   "paused",
				Steps: []StepResult{
					{ID: "a", Status: StatusDone, Result: map[string]any{"x": 1}},
					{ID: "b", Status: StatusPending},
				},
			}
			data, _ := yaml.Marshal(paused)
			os.WriteFile(resumeFile, data, 0644)

			yamlData := []byte("version: \"1\"\nname: " + specName + "\nsteps:\n  - id: a\n    prompt: a\n  - id: b\n    prompt: b\n    depends_on: [a]\n    checkpoint: true\n")
			yf := writeTempFile(t, specName+".workflow.yaml", string(yamlData))
			defer os.Remove(yf)

			result, err := handler(context.Background(), json.RawMessage(`{"path":"`+yf+`"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "paused")
		})

		Convey("error: Continue fails when completed step removed", func() {
			specName := "handler_ct_err"
			resumeFile := specName + ".result.yaml"
			defer os.Remove(resumeFile)
			paused := &WorkflowResult{
				Workflow: specName,
				Status:   "paused",
				Steps: []StepResult{
					{ID: "a", Status: StatusDone, Result: map[string]any{"x": 1}},
				},
			}
			data, _ := yaml.Marshal(paused)
			os.WriteFile(resumeFile, data, 0644)

			yamlData := []byte("version: \"1\"\nname: " + specName + "\nsteps:\n  - id: b\n    prompt: b\n")
			yf := writeTempFile(t, specName+".workflow.yaml", string(yamlData))
			defer os.Remove(yf)

			result, err := handler(context.Background(), json.RawMessage(`{"path":"`+yf+`"}`))
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "removed")
		})
	})
}

func TestStepZeroTimeout(t *testing.T) {
	Convey("Step with timeout=0 runs without deadline", t, func() {
		logger := testLogger()
		cfg := testConfig()
		cfg.Set("workflow.step_timeout", "0s")

		toolReg := tool.NewRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				// Simulate a step that takes some time.
				return []llm.LLMChunk{textChunk("done")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		spec := &WorkflowSpec{
			Version: "1",
			Name:    "zero_timeout",
			Steps: []StepSpec{
				{ID: "step1", Prompt: "do something"},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(result.Steps[0].Status, ShouldEqual, StatusDone)
	})

	Convey("Per-step timeout=0 overrides config default", t, func() {
		logger := testLogger()
		cfg := testConfig()

		toolReg := tool.NewRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}

		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		spec := &WorkflowSpec{
			Version: "1",
			Name:    "per_step_timeout",
			Steps: []StepSpec{
				{ID: "step1", Prompt: "do something", Timeout: "0s"},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := engine.Run(ctx, spec, "")
		So(err, ShouldBeNil)
		So(result.Status, ShouldEqual, "completed")
		So(result.Steps[0].Status, ShouldEqual, StatusDone)
	})
}

func TestHandlerStripsToolTimeout(t *testing.T) {
	Convey("run_workflow handler ignores expired parent context", t, func() {
		logger := testLogger()
		cfg := testConfig()
		cfg.Set("workflow.step_timeout", "5s")

		toolReg := tool.NewRegistry()
		eventBus := event.NewBus()

		mock := &mockLLMProvider{
			chunksFn: func(req llm.LLMRequest) []llm.LLMChunk {
				return []llm.LLMChunk{textChunk("ok")}
			},
		}
		engine := NewEngine(toolReg, mock, eventBus, logger, nil, cfg)
		handler := runWorkflowHandler(engine, logger)

		yamlData := "version: \"1\"\nname: no_timeout_test\nsteps:\n  - id: a\n    prompt: hello\n"
		yf := writeTempFile(t, "no_timeout_test.workflow.yaml", yamlData)

		// Simulate the tool pipeline's 30s timeout: a context already expired.
		expiredCtx, cancel := context.WithTimeout(context.Background(), -1*time.Second)
		defer cancel()

		result, err := handler(expiredCtx, json.RawMessage(`{"path":"`+yf+`"}`))
		So(err, ShouldBeNil)
		So(result.IsError, ShouldBeFalse)
		So(result.Content, ShouldContainSubstring, "completed")
	})
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := "/tmp/" + name
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	return path
}
