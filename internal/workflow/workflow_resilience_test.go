package workflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/h2non/gock"

	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/llm/models"
	"dolphin/internal/tool"
)

func TestWorkflowAPIErrors(t *testing.T) {
	defer gock.Off()

	logger := testLogger()
	cfg := testConfig()
	cfg.Set("agent.pool_size", 4)

	callCount := &atomic.Int32{}
	const mockURL = "http://test-llm.local/v1/chat/completions"

	gock.New(mockURL).
		Post("").
		Persist().
		Reply(200).
		Map(func(resp *http.Response) *http.Response {
			n := callCount.Add(1)
			if n%2 == 1 {
				resp.StatusCode = 503
				resp.Status = "503 Service Unavailable"
				resp.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"service unavailable"}}`))
				resp.ContentLength = -1
			} else {
				body := streamResponse(int(n))
				resp.Body = io.NopCloser(strings.NewReader(body))
				resp.ContentLength = int64(len(body))
			}
			return resp
		})

	provider := models.NewProvider(llm.Config{
		Provider: "test",
		APIType:  "openai",
		APIKey:   "test-key",
		BaseURL:  "http://test-llm.local",
		Models: []llm.ModelConfig{
			{Name: "test-model", Model: "test-model", MaxTokens: 256},
		},
	}, logger)

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()
	engine := NewEngine(toolReg, provider, eventBus, logger, nil, cfg)

	spec := &WorkflowSpec{
		Version: "1",
		Name:    "resilience_test",
		Steps: []StepSpec{
			{ID: "step_1", Prompt: "return {\"value\": 1}", OutputSchema: map[string]any{"value": "number"}},
			{ID: "step_2", Prompt: "return {\"value\": 2}", OutputSchema: map[string]any{"value": "number"}},
			{ID: "step_3", Prompt: "return {\"value\": 3}", OutputSchema: map[string]any{"value": "number"}},
			{ID: "step_4", Prompt: "return {\"value\": 4}", OutputSchema: map[string]any{"value": "number"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := engine.Run(ctx, spec, "")

	t.Logf("workflow status=%s duration=%s err=%v", result.Status, result.Duration, err)
	for _, s := range result.Steps {
		t.Logf("  %s: status=%s result=%v error=%s", s.ID, s.Status, s.Result, s.Error)
	}

	if result.Status != "failed" {
		t.Errorf("expected status=failed due to step errors, got %s", result.Status)
	}

	done := 0
	failed := 0
	for _, s := range result.Steps {
		switch s.Status {
		case StatusDone:
			done++
			if s.Result == nil {
				t.Errorf("step %s succeeded but result is nil", s.ID)
			}
		case StatusFailed:
			failed++
			if s.Error == "" {
				t.Errorf("step %s failed but error is empty", s.ID)
			}
		case StatusPending, StatusReady, StatusRunning, StatusSkipped:
		}
	}

	t.Logf("done=%d failed=%d", done, failed)
	if done < 1 {
		t.Error("at least one step should have succeeded")
	}
	if failed < 1 {
		t.Error("at least one step should have failed with API error")
	}
}

func streamResponse(n int) string {
	content := fmt.Sprintf("```json\n{\"value\": %d}\n```", n)
	return fmt.Sprintf(
		"data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\","+
			"\"choices\":[{\"delta\":{\"content\":%q},\"index\":0,\"finish_reason\":null}]}\n\n"+
			"data: {\"id\":\"chatcmpl-%d\",\"object\":\"chat.completion.chunk\","+
			"\"choices\":[{\"delta\":{\"content\":\"\"},\"index\":0,\"finish_reason\":\"stop\"}]}\n\n"+
			"data: [DONE]\n",
		n, content, n,
	)
}

func TestWorkflowAPIErrorsWithDeps(t *testing.T) {
	defer gock.Off()

	logger := testLogger()
	cfg := testConfig()
	cfg.Set("agent.pool_size", 4)

	callCount := &atomic.Int32{}
	const mockURL = "http://test-llm-dep.local/v1/chat/completions"

	gock.New(mockURL).
		Post("").
		Persist().
		Reply(200).
		Map(func(resp *http.Response) *http.Response {
			n := callCount.Add(1)
			if n == 1 {
				resp.StatusCode = 503
				resp.Status = "503 Service Unavailable"
				resp.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"service unavailable"}}`))
				resp.ContentLength = -1
			} else {
				body := streamResponse(int(n))
				resp.Body = io.NopCloser(strings.NewReader(body))
				resp.ContentLength = int64(len(body))
			}
			return resp
		})

	provider := models.NewProvider(llm.Config{
		Provider: "test",
		APIType:  "openai",
		APIKey:   "test-key",
		BaseURL:  "http://test-llm-dep.local",
		Models: []llm.ModelConfig{
			{Name: "test-model", Model: "test-model", MaxTokens: 256},
		},
	}, logger)

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()
	engine := NewEngine(toolReg, provider, eventBus, logger, nil, cfg)

	// DAG: A1→B1, A2→B2, A3→B3, [B1,B2,B3]→summarize.
	// First LLM call returns 503 — whichever A-step gets it fails,
	// its dependent B-step is skipped, and summarize is skipped.
	// The other two chains should succeed.
	spec := &WorkflowSpec{
		Version: "1",
		Name:    "resilience_deps",
		Steps: []StepSpec{
			{ID: "step_a1", Prompt: "test baidu speed", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_a2", Prompt: "test google speed", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_a3", Prompt: "test bing speed", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_b1", Prompt: "verify baidu {{.step_a1.v}}", DependsOn: []string{"step_a1"}, OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_b2", Prompt: "verify google {{.step_a2.v}}", DependsOn: []string{"step_a2"}, OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_b3", Prompt: "verify bing {{.step_a3.v}}", DependsOn: []string{"step_a3"}, OutputSchema: map[string]any{"v": "number"}},
			{ID: "summarize", Prompt: "compare all", DependsOn: []string{"step_b1", "step_b2", "step_b3"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := engine.Run(ctx, spec, "")

	t.Logf("workflow status=%s duration=%s err=%v", result.Status, result.Duration, err)
	for _, s := range result.Steps {
		t.Logf("  %s: status=%s result=%v error=%s", s.ID, s.Status, s.Result, s.Error)
	}

	statusByID := map[string]StepStatus{}
	for _, s := range result.Steps {
		statusByID[s.ID] = s.Status
	}

	// First call gets 503 — which step fails is non-deterministic with
	// concurrent execution, so find it dynamically.
	pairs := [][2]string{{"step_a1", "step_b1"}, {"step_a2", "step_b2"}, {"step_a3", "step_b3"}}
	failedChain := -1
	for i, p := range pairs {
		if statusByID[p[0]] == StatusFailed {
			failedChain = i
			if statusByID[p[1]] != StatusSkipped {
				t.Errorf("%s failed so %s should be skipped, got %s", p[0], p[1], statusByID[p[1]])
			}
		} else {
			if statusByID[p[0]] != StatusDone {
				t.Errorf("%s should be done, got %s", p[0], statusByID[p[0]])
			}
			if statusByID[p[1]] != StatusDone {
				t.Errorf("%s should be done, got %s", p[1], statusByID[p[1]])
			}
		}
	}
	if failedChain < 0 {
		t.Error("expected exactly one step_a* to fail, got none")
	}

	if statusByID["summarize"] != StatusSkipped {
		t.Errorf("summarize should be skipped, got %s", statusByID["summarize"])
	}

	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %s", result.Status)
	}
}

func TestWorkflowMaxConcurrency(t *testing.T) {
	defer gock.Off()

	logger := testLogger()
	cfg := testConfig()
	cfg.Set("agent.pool_size", 4)

	inFlight := &atomic.Int32{}
	maxInFlight := &atomic.Int32{}
	callCount := &atomic.Int32{}

	const mockURL = "http://test-llm-sem.local/v1/chat/completions"

	gock.New(mockURL).
		Post("").
		Persist().
		Reply(200).
		Map(func(resp *http.Response) *http.Response {
			cur := inFlight.Add(1)
			for {
				m := maxInFlight.Load()
				if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
					break
				}
			}
			n := callCount.Add(1)

			time.Sleep(100 * time.Millisecond)
			inFlight.Add(-1)

			body := streamResponse(int(n))
			resp.Body = io.NopCloser(strings.NewReader(body))
			resp.ContentLength = int64(len(body))
			return resp
		})

	// Route via Manager so MaxConcurrency semaphore is enforced.
	provider := models.NewProvider(llm.Config{
		Provider: "test",
		APIType:  "openai",
		APIKey:   "test-key",
		BaseURL:  "http://test-llm-sem.local",
		Models: []llm.ModelConfig{
			{Name: "test-model", Model: "test-model", MaxTokens: 256, MaxConcurrency: 2},
		},
	}, logger)

	mgr := llm.NewManager()
	mgr.AddProvider("test", provider)
	if err := mgr.SetActiveModel("test-model"); err != nil {
		t.Fatalf("set active model: %v", err)
	}

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()
	engine := NewEngine(toolReg, mgr, eventBus, logger, nil, cfg)

	spec := &WorkflowSpec{
		Version: "1",
		Name:    "concurrency_test",
		Steps: []StepSpec{
			{ID: "s1", Prompt: "return {\"v\":1}", OutputSchema: map[string]any{"v": "number"}},
			{ID: "s2", Prompt: "return {\"v\":2}", OutputSchema: map[string]any{"v": "number"}},
			{ID: "s3", Prompt: "return {\"v\":3}", OutputSchema: map[string]any{"v": "number"}},
			{ID: "s4", Prompt: "return {\"v\":4}", OutputSchema: map[string]any{"v": "number"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := engine.Run(ctx, spec, "")

	t.Logf("workflow status=%s duration=%s max_in_flight=%d", result.Status, result.Duration, maxInFlight.Load())
	for _, s := range result.Steps {
		t.Logf("  %s: status=%s", s.ID, s.Status)
	}

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}

	if m := maxInFlight.Load(); m > 2 {
		t.Errorf("max concurrency exceeded: %d > 2 (model max_concurrency=2)", m)
	}

	t.Logf("max concurrent LLM calls observed: %d (limit: 2)", maxInFlight.Load())
}

func TestWorkflowAllStepsFail(t *testing.T) {
	defer gock.Off()

	logger := testLogger()
	cfg := testConfig()
	cfg.Set("agent.pool_size", 4)

	const mockURL = "http://test-llm-allfail.local/v1/chat/completions"

	// All requests return 503.
	gock.New(mockURL).
		Post("").
		Persist().
		ReplyFunc(func(resp *gock.Response) {
			resp.StatusCode = 503
			resp.BodyString(`{"error":{"message":"service unavailable"}}`)
		})

	provider := models.NewProvider(llm.Config{
		Provider: "test",
		APIType:  "openai",
		APIKey:   "test-key",
		BaseURL:  "http://test-llm-allfail.local",
		Models: []llm.ModelConfig{
			{Name: "test-model", Model: "test-model", MaxTokens: 256},
		},
	}, logger)

	toolReg := tool.NewRegistry()
	eventBus := event.NewBus()

	// Independent parallel steps — all should fail, workflow completes without crash.
	spec := &WorkflowSpec{
		Version: "1",
		Name:    "all_fail_test",
		Steps: []StepSpec{
			{ID: "step_1", Prompt: "test", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_2", Prompt: "test", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_3", Prompt: "test", OutputSchema: map[string]any{"v": "number"}},
			{ID: "step_4", Prompt: "test", OutputSchema: map[string]any{"v": "number"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine(toolReg, provider, eventBus, logger, nil, cfg)
	result, err := engine.Run(ctx, spec, "")

	t.Logf("workflow status=%s err=%v", result.Status, err)
	for _, s := range result.Steps {
		t.Logf("  %s: status=%s error=%s", s.ID, s.Status, s.Error)
	}

	if result.Status != "failed" {
		t.Errorf("expected failed, got %s", result.Status)
	}

	failed := 0
	for _, s := range result.Steps {
		if s.Status == StatusFailed {
			failed++
		}
	}
	if failed != 4 {
		t.Errorf("expected 4 failed steps, got %d", failed)
	}
}
