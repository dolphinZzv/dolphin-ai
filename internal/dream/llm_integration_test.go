//go:build integration
// +build integration

package dream

import (
	"context"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/llm"
	"dolphin/internal/session"

	_ "dolphin/internal/llm/models"

	"go.uber.org/zap"
)

func TestLLMIntegration_Phase2_DeepSeekFlash(t *testing.T) {
	cfg, err := config.LoadConfig("../../config.yaml")
	if err != nil {
		t.Fatalf("load config.yaml: %v", err)
	}

	apiKey := cfg.GetString("llm.deepseek_anthropic.api_key")
	if apiKey == "" {
		t.Skip("llm.deepseek_anthropic.api_key not configured")
	}

	baseURL := cfg.GetString("llm.deepseek_anthropic.base_url")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/anthropic"
	}

	// Build real LLM Manager.
	mgr := llm.NewManager()
	sectionCfg := llm.Config{
		Provider: "deepseek_anthropic",
		Vendor:   cfg.GetString("llm.deepseek_anthropic.provider"),
		APIType:  cfg.GetString("llm.deepseek_anthropic.api_type"),
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Timeout:  60 * time.Second,
		Models:   makeModels(),
	}
	if sectionCfg.APIType == "" {
		sectionCfg.APIType = "anthropic"
	}

	logger := zap.NewNop()
	for _, mc := range sectionCfg.Models {
		factory, err := llm.LookupModelProvider(mc.Name, sectionCfg.APIType)
		if err != nil {
			t.Fatalf("LookupModelProvider(%s, %s): %v", mc.Name, sectionCfg.APIType, err)
		}
		mgr.AddProvider("deepseek_anthropic", factory(sectionCfg, logger))
	}

	if err := mgr.SetActiveModel("deepseek-v4-flash"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}

	// Setup Dream.
	brain := newMockBrain()
	brain.files["commands/deploy.md"] = "---\nname: deploy\n---\n1. Run: docker compose up -d\n2. Verify: curl localhost:8080/health"
	brain.files["commands/test.md"] = "---\nname: test\n---\n1. Run: go test ./..."
	brain.files["knowledge/k8s.md"] = "k8s is preferred over docker compose."
	brain.files["knowledge/docker.md"] = "Docker Compose is the deployment method."

	d := &Dream{
		memory: newMockMemory(), brain: brain, provider: mgr,
		autoApply: true, minSessions: 1, minUserMessages: 1,
		maxConsecutiveEmpty: 3, minImpactThreshold: 0.3,
		fileCooldownDreams: 5, maxEditsPerDream: 10, maxReflectTokens: 4096,
		calibrationWindow: 10, calibrationMinStep: 0.05,
		calibrationFloor: 0.30, calibrationCeiling: 0.95,
		activityCh: make(chan struct{}, 1), state: newState(),
	}
	d.sessionMgr = &mockSessionMgr{
		sessions: []*session.Session{makeSession("s1", time.Now().Add(-1*time.Hour), true)},
	}

	// Realistic proposals.
	proposals := []EditProposal{
		{ID: "p1", Target: "commands/deploy.md", Action: ActionImprove,
			Before: "---\nname: deploy\n---\n1. Run: docker compose up -d\n2. Verify: curl localhost:8080/health",
			Reason: "用户纠正：用 kubectl 不用 docker compose", Evidence: []string{"s1"}, Confidence: 0.90, Impact: 2.0, NeedsLLM: true},
		{ID: "p2", Target: "commands/test.md", Action: ActionImprove,
			Before: "---\nname: test\n---\n1. Run: go test ./...",
			Reason: "用户要求：顺便跑 lint", Evidence: []string{"s1"}, Confidence: 0.85, Impact: 1.5, NeedsLLM: true},
		{ID: "p3", Target: "knowledge/deploy.md", Action: ActionCreate,
			Before: "", Reason: "跨 3 个 session 重复问部署方式，需要创建知识文件",
			Evidence: []string{"s1", "s2", "s3"}, Confidence: 0.95, Impact: 1.8, NeedsLLM: true},
		{ID: "p4", Target: "knowledge/k8s.md", Action: ActionMerge,
			Before: "k8s is preferred over docker compose.",
			Sources: []string{"knowledge/docker.md"},
			Reason: "k8s.md 和 docker.md 内容重叠", Evidence: []string{"dup"}, Confidence: 0.75, Impact: 0.8, NeedsLLM: true},
		{ID: "p5", Target: "commands/unused.md", Action: ActionDeprecate,
			Reason: "65 天未使用", Confidence: 0.90, Impact: 0.3, NeedsLLM: false},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	start := time.Now()
	edits, err := d.edit(ctx, proposals)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Phase 2 LLM call failed after %v: %v", elapsed.Round(time.Second), err)
	}
	t.Logf("LLM call completed in %v — %d edits", elapsed.Round(time.Second), len(edits))

	for _, e := range edits {
		t.Logf("  [%s] %s %40s | after=%4d chars | %s",
			e.ProposalID, e.Action, e.Target[:min(40, len(e.Target))], len(e.After), e.Reasoning[:min(60, len(e.Reasoning))])
	}

	// Structural validation.
	if len(edits) < 1 {
		t.Fatal("expected at least 1 edit")
	}

	for _, e := range edits {
		switch e.ProposalID {
		case "p1":
			if !strings.Contains(strings.ToLower(e.After), "kubectl") {
				t.Error("p1: deploy improvement should mention kubectl")
			}
		case "p2":
			if !strings.Contains(strings.ToLower(e.After), "lint") {
				t.Error("p2: test improvement should mention lint")
			}
		case "p3":
			if e.After == "" {
				t.Error("p3: create should have content")
			}
		case "p4":
			if e.After == "" {
				t.Error("p4: merge should have content")
			}
		case "p5":
			if e.Action != ActionDeprecate {
				t.Error("p5: should be deprecate action")
			}
		}
	}

	// Causality verification.
	passed := d.filterEdits(edits, proposals)
	t.Logf("Causality: %d/%d passed", len(passed), len(edits))
	if len(passed) < len(edits)-1 { // at most p5 might fail (deprecate always passes)
		t.Errorf("too many edits failed verification: %d/%d", len(passed), len(edits))
	}

	t.Log("✅ Phase 2 integration test passed — deepseek flash produced valid edits")
}

func makeModels() []llm.ModelConfig {
	return []llm.ModelConfig{
		{Name: "deepseek-v4-flash", Model: "deepseek-v4-flash", Vendor: "deepseek", APIType: "anthropic", MaxRetries: 2, MaxTokens: 4096, Timeout: 60 * time.Second, Stream: true, StreamSet: true},
	}
}
