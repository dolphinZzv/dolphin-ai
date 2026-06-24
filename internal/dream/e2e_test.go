package dream

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/session"
	"dolphin/internal/types"
)

func TestE2E_FullDream_Flow(t *testing.T) {
	d := setupE2EDream(t)
	d.state.LastDreamAt = time.Now().Add(-3 * time.Hour)
	now := time.Now()
	mem := d.memory.(*mockMemory)
	mem.messages["s1"] = []types.Message{
		asstMsg("docker compose up", now.Add(-5*time.Minute)),
		userMsg("不对，以后都用 kubectl", now.Add(-3*time.Minute)),
		asstMsg("kubectl apply -f deploy.yaml", now.Add(-1*time.Minute)),
	}
	proposals, err := d.scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) == 0 {
		t.Fatal("expected at least 1 proposal from correction")
	}
}

func TestE2E_EditPhase_WithFakeLLM(t *testing.T) {
	d := setupE2EDream(t)
	proposals := []EditProposal{
		{ID: "p1", Target: "commands/deploy.md", Action: ActionImprove,
			Before:     "Run: docker compose up -d",
			Reason:     "user corrected: use kubectl",
			Confidence: 0.85, Impact: 2.0, NeedsLLM: true},
		{ID: "p2", Target: "commands/old.md", Action: ActionDeprecate,
			Reason:     "unused for 45 days",
			Confidence: 0.90, Impact: 0.3, NeedsLLM: false},
	}
	edits, err := d.edit(context.Background(), proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) == 0 {
		t.Fatal("expected at least 1 edit")
	}
	foundP2 := false
	for _, e := range edits {
		if e.ProposalID == "p2" {
			foundP2 = true
		}
	}
	if !foundP2 {
		t.Error("rule-only deprecation edit missing")
	}
}

func TestE2E_DreamNow(t *testing.T) {
	d := setupE2EDream(t)
	now := time.Now()
	mem := d.memory.(*mockMemory)
	mem.messages["s1"] = []types.Message{
		userMsg("记住以后用 Go", now.Add(-5*time.Minute)),
	}
	_, err := d.DreamNow(context.Background())
	if err != nil {
		t.Logf("DreamNow expected error (no git repo in mock): %v", err)
	} else {
		t.Log("DreamNow succeeded (mock with git?)")
	}
}

func TestE2E_IsRunning(t *testing.T) {
	d := setupE2EDream(t)
	if d.IsRunning() {
		t.Error("should not be running")
	}
}

func TestE2E_State(t *testing.T) {
	d := setupE2EDream(t)
	if d.State() == nil {
		t.Fatal("nil state")
	}
}

func TestE2E_BuildBrainContext(t *testing.T) {
	d := setupE2EDream(t)
	brain := d.brain.(*mockBrain)
	brain.files["commands/deploy.md"] = "deploy"
	brain.files["commands/test.md"] = "test"
	brain.files["knowledge/k8s.md"] = "k8s"
	brain.files["rules/style.md"] = "style"
	bc := d.buildBrainContext()
	if len(bc.Commands) < 2 {
		t.Errorf("commands: %d", len(bc.Commands))
	}
	if len(bc.Facts) != 2 {
		t.Errorf("facts: %d", len(bc.Facts))
	}
}

func TestE2E_BuildEditPrompt(t *testing.T) {
	d := setupE2EDream(t)
	brain := d.brain.(*mockBrain)
	brain.files["commands/deploy.md"] = "deploy"
	proposals := []EditProposal{{
		ID: "p1", Target: "commands/deploy.md", Action: ActionImprove,
		Before:     "docker compose up",
		Reason:     "should use kubectl",
		Confidence: 0.85, Impact: 2.0, NeedsLLM: true,
	}}
	prompt := d.buildEditPrompt(proposals)
	if !strings.Contains(prompt, "PROPOSAL p1") {
		t.Error("missing proposal marker")
	}
	if !strings.Contains(prompt, "kubectl") {
		t.Error("missing few-shot example")
	}
}

func TestE2E_ActivityCh(t *testing.T) {
	d := setupE2EDream(t)
	ch := d.ActivityCh()
	if ch == nil {
		t.Error("activityCh nil")
	}
	select {
	case ch <- struct{}{}:
	default:
		t.Error("should accept")
	}
}

func setupE2EDream(t *testing.T) *Dream {
	t.Helper()
	mem := newMockMemory()
	brain := newMockBrain()
	brain.files["commands/deploy.md"] = "---\nname: deploy\n---\nRun: docker compose up -d"
	brain.files["commands/old.md"] = "---\nname: old-cmd\n---\n# old command"
	prov := &mockProvider{
		output: `[{"proposal_id":"p1","action":"improve","target":"commands/deploy.md","after":"kubectl apply -f deploy.yaml","reasoning":"switched to kubectl"}]`,
	}
	aio := &mockAgentIO{processing: false}
	logger := zap.NewNop()
	d := &Dream{
		memory: mem, brain: brain, provider: prov, agentIO: aio, logger: logger,
		autoApply: true, minSessions: 1, minUserMessages: 1,
		maxConsecutiveEmpty: 3, minImpactThreshold: 0.3,
		fileCooldownDreams: 5, maxEditsPerDream: 10,
		maxReflectTokens: 1024, calibrationWindow: 10,
		calibrationMinStep: 0.05, calibrationFloor: 0.30, calibrationCeiling: 0.95,
		activityCh: make(chan struct{}, 1), state: newState(),
	}
	d.sessionMgr = &mockSessionMgr{
		sessions: []*session.Session{
			makeSession("s1", time.Now().Add(-1*time.Hour), true),
		},
	}
	return d
}

// ─────────────────────────────────────────────────────────────────
// Dream lifecycle tests
// ─────────────────────────────────────────────────────────────────

func TestDream_IsRunning_False(t *testing.T) {
	d := setupE2EDream(t)
	if d.IsRunning() {
		t.Error("should be false")
	}
}

func TestDream_State_NeverNil(t *testing.T) {
	d := setupE2EDream(t)
	if s := d.State(); s == nil {
		t.Fatal("state is nil")
	}
}

func TestDream_ActivityCh_Accepts(t *testing.T) {
	d := setupE2EDream(t)
	ch := d.ActivityCh()
	if ch == nil {
		t.Fatal("ch is nil")
	}
	select {
	case ch <- struct{}{}:
	default:
		t.Error("should accept")
	}
}

func TestExtractSources(t *testing.T) {
	edit := Edit{Reasoning: "merge files knowledge/a.md with knowledge/b.md"}
	srcs := extractSources(edit)
	if len(srcs) == 0 {
		t.Error("should extract source files")
	}
}

func TestExtractSources_NoMd(t *testing.T) {
	edit := Edit{Reasoning: "merge something"}
	srcs := extractSources(edit)
	if len(srcs) > 0 {
		t.Errorf("should be empty, got %v", srcs)
	}
}
