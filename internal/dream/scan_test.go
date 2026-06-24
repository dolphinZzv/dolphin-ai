package dream

import (
	"context"
	"strings"
	"testing"
	"time"

	"dolphin/internal/types"
)

func TestDetectExplicitPreference(t *testing.T) {
	if !detectExplicitPreference("以后都用 k8s") {
		t.Error("true")
	}
	if detectExplicitPreference("正常对话") {
		t.Error("false")
	}
}
func TestDetectCorrection(t *testing.T) {
	if !detectCorrection("不对应该用kubectl") {
		t.Error("true")
	}
	if detectCorrection("好的") {
		t.Error("false")
	}
}
func TestJaccard(t *testing.T) {
	if jaccard([]string{"a"}, []string{"a"}) != 1.0 {
		t.Error("1.0")
	}
	if jaccard(nil, nil) != 0 {
		t.Error("0")
	}
}
func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" {
		t.Error("short")
	}
	if len(truncateStr("hello world long", 5)) > 8 {
		t.Error("long")
	}
}
func TestSanitiseFilename(t *testing.T) {
	s := sanitiseFilename("Deploy to K8s!")
	if s == "" || len(s) > 20 {
		t.Errorf("%q", s)
	}
}
func TestFormatInt(t *testing.T) {
	if formatInt(0) != "0" {
		t.Error("0")
	}
	if formatInt(42) != "42" {
		t.Error("42")
	}
}
func TestContainsAny(t *testing.T) {
	if !containsAny("hello", "hello") {
		t.Error("true")
	}
	if containsAny("hello", "world") {
		t.Error("false")
	}
}

func TestScan_TeachableMoment_L1(t *testing.T) {
	now := time.Now()
	snap := sessionSnapshot{ID: "s1", Messages: []types.Message{
		asstMsg("docker compose up", now.Add(-3*time.Minute)),
		userMsg("不对，用 kubectl", now.Add(-2*time.Minute)),
		asstMsg("kubectl apply", now.Add(-1*time.Minute)),
	}}
	d := &Dream{}
	res := d.extractFromSessions([]sessionSnapshot{snap})
	if len(res.TeachableMoments) != 1 {
		t.Fatalf("expected 1, got %d", len(res.TeachableMoments))
	}
}

func TestScan_EmptyMemory(t *testing.T) {
	d := &Dream{}
	snap := sessionSnapshot{ID: "e", Messages: nil}
	res := d.extractFromSessions([]sessionSnapshot{snap})
	if res.Sessions != 1 {
		t.Error("sessions")
	}
}

func TestImpact_HighRefs(t *testing.T) {
	d := &Dream{}
	if imp := d.computeImpact(EditSignal{Type: SignalCorrection}, BrainFile{ReferencedCount: 30}); imp < 2.0 {
		t.Errorf("low: %f", imp)
	}
}
func TestImpact_ZeroRefs(t *testing.T) {
	d := &Dream{}
	imp := d.computeImpact(EditSignal{Type: SignalCorrection}, BrainFile{ReferencedCount: 0})
	if imp < 1.0 {
		t.Errorf("penalized: %f", imp)
	}
}
func TestImpact_Obsolescence(t *testing.T) {
	d := &Dream{}
	if imp := d.computeImpact(EditSignal{Type: SignalObsolescence}, BrainFile{}); imp > 0.5 {
		t.Errorf("not low: %f", imp)
	}
}
func TestImpact_FreshnessDecay(t *testing.T) {
	d := &Dream{}
	sig := EditSignal{Type: SignalCorrection, FirstSeen: time.Now().Add(-30 * 24 * time.Hour)}
	if imp := d.computeImpact(sig, BrainFile{}); imp > 1.0 {
		t.Errorf("not decayed: %f", imp)
	}
}

func TestBuildProposals_Dedup(t *testing.T) {
	ps := []EditProposal{
		{ID: "p1", Target: "a.md", Impact: 1.0},
		{ID: "p2", Target: "a.md", Impact: 2.0},
	}
	r := sortAndDedup(ps)
	if len(r) != 1 || r[0].ID != "p2" {
		t.Errorf("dedup: %+v", r)
	}
}

func TestBuildProposals_Deprecate(t *testing.T) {
	d := &Dream{minImpactThreshold: 0.1, maxEditsPerDream: 10}
	brain := newMockBrain()
	d.brain = brain
	d.state = newState()
	bf := []BrainFile{{Path: "commands/old.md", ReferencedCount: 0, LastReferenced: time.Now().Add(-50 * 24 * time.Hour)}}
	ps := d.buildProposals(&ExtractResult{}, bf)
	found := false
	for _, p := range ps {
		if p.Action == ActionDeprecate && p.Target == "commands/old.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("no deprecate: %+v", ps)
	}
}

func TestBuildProposals_Cooldown(t *testing.T) {
	d := &Dream{minImpactThreshold: 0.1, maxEditsPerDream: 10}
	brain := newMockBrain()
	d.brain = brain
	d.state = newState()
	d.state.LastDreamID = 10
	d.state.FileCooldowns["commands/old.md"] = FileCooldown{LastEditedDream: 8, CooldownUntilDream: 13}
	bf := []BrainFile{{Path: "commands/old.md", ReferencedCount: 0, LastReferenced: time.Now().Add(-50 * 24 * time.Hour)}}
	ps := d.buildProposals(&ExtractResult{}, bf)
	if len(ps) > 0 {
		t.Errorf("cooldown should block: %d", len(ps))
	}
}

func TestBuildProposals_RepeatedCommand(t *testing.T) {
	d := &Dream{minImpactThreshold: 0.3, maxEditsPerDream: 10}
	brain := newMockBrain()
	d.brain = brain
	d.state = newState()
	ext := &ExtractResult{RepeatedCommands: []RepeatedPattern{
		{ToolName: "exec", Pattern: "go-test", Count: 6, Sessions: []string{"s1", "s2", "s3", "s4"}}},
	}
	bf := []BrainFile{{Path: "commands/go-test.md", ReferencedCount: 5}}
	ps := d.buildProposals(ext, bf)
	found := false
	for _, p := range ps {
		if p.Action == ActionImprove && strings.Contains(p.Target, "go-test") {
			found = true
		}
	}
	if !found {
		t.Errorf("no improve: %+v", ps)
	}
}

func TestScanBrainFiles(t *testing.T) {
	brain := newMockBrain()
	d := &Dream{brain: brain, state: newState()}
	brain.files["commands/x.md"] = "content"
	d.state.Usage.Files["commands/x.md"] = FileUsage{Refs: 5}
	files, err := d.scanBrainFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1: %d", len(files))
	}
}

func TestFindMatchingCommand(t *testing.T) {
	d := &Dream{}
	bf := []BrainFile{{Path: "commands/deploy.md"}, {Path: "commands/test.md"}}
	if r := d.findMatchingCommand("deploy", bf); r != "commands/deploy.md" {
		t.Errorf("%s", r)
	}
	if r := d.findMatchingCommand("x", bf); r != "" {
		t.Errorf("%s", r)
	}
}

// ==================================================================
// Additional tests for uncovered branches
// ==================================================================

func TestExtractRepeatedPatterns(t *testing.T) {
	now := time.Now()
	d := &Dream{}
	// Need >= minRepeatPatterns (5) of the same pattern.
	msgs := []types.Message{}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, toolMsg("output", "call_exec_abc", false, now))
	}
	snap := sessionSnapshot{ID: "s1", Messages: msgs}
	var res ExtractResult
	d.extractRepeatedPatterns(snap, &res)
	if len(res.RepeatedCommands) < 1 {
		t.Fatalf("expected >=1 repeated pattern, got %d", len(res.RepeatedCommands))
	}
	for _, rc := range res.RepeatedCommands {
		if rc.Count < minRepeatPatterns {
			t.Errorf("pattern %s count %d < %d", rc.Pattern, rc.Count, minRepeatPatterns)
		}
	}
}

func TestExtractSimilarQuestions_CrossSession(t *testing.T) {
	now := time.Now()
	d := &Dream{}
	// Nearly identical messages for high Jaccard.
	sessions := []sessionSnapshot{
		{ID: "s1", Messages: []types.Message{userMsg("deploy k8s guide tool", now.Add(-5*24*time.Hour))}},
		{ID: "s2", Messages: []types.Message{userMsg("deploy k8s guide tool help", now.Add(-3*24*time.Hour))}},
	}
	var res ExtractResult
	d.extractSimilarQuestions(sessions, &res)
	if len(res.SimilarQuestions) < 1 {
		t.Errorf("expected >=1 similar group, got %d. Jaccard of 'deploy k8s guide' vs 'deploy k8s guide help' should be > 0.7", len(res.SimilarQuestions))
	}
}

func TestExtractSimilarQuestions_SlashCommandsIgnored(t *testing.T) {
	now := time.Now()
	d := &Dream{}
	sessions := []sessionSnapshot{
		{ID: "s1", Messages: []types.Message{userMsg("/tools on", now)}},
		{ID: "s2", Messages: []types.Message{userMsg("/tools off", now)}},
	}
	var res ExtractResult
	d.extractSimilarQuestions(sessions, &res)
	if len(res.SimilarQuestions) > 0 {
		t.Errorf("slash commands should not be grouped: %+v", res.SimilarQuestions)
	}
}

func TestDetectFactMerges(t *testing.T) {
	d := &Dream{}
	brain := newMockBrain()
	d.brain = brain
	// Same set of keywords = Jaccard 1.0 = definitely > 0.7.
	brain.files["knowledge/deploy-k8s.md"] = "deploy k8s kubectl apply production server"
	brain.files["knowledge/deploy-prod.md"] = "deploy k8s kubectl apply production server"
	brain.files["commands/unrelated.md"] = "unrelated content here"
	brainFiles := []BrainFile{
		{Path: "knowledge/deploy-k8s.md"},
		{Path: "knowledge/deploy-prod.md"},
		{Path: "commands/unrelated.md"},
	}
	idSeq := 0
	proposals := d.detectFactMerges(brainFiles, &idSeq)
	if len(proposals) == 0 {
		t.Errorf("expected merge proposal, got 0")
	}
	for _, p := range proposals {
		if p.Action != ActionMerge {
			t.Errorf("expected Merge, got %s", p.Action)
		}
	}
}

func TestFindMatchingFact(t *testing.T) {
	d := &Dream{}
	brain := newMockBrain()
	d.brain = brain
	// Content must share keywords with search terms for Jaccard > 0.5.
	brain.files["knowledge/k8s.md"] = "deploy k8s kubernetes kubectl"
	brain.files["knowledge/docker.md"] = "docker compose local"
	brainFiles := []BrainFile{{Path: "knowledge/k8s.md"}, {Path: "knowledge/docker.md"}}
	if r := d.findMatchingFact([]string{"k8s", "deploy", "kubernetes"}, brainFiles); r != "knowledge/k8s.md" {
		t.Errorf("expected knowledge/k8s.md, got '%s'", r)
	}
	if r := d.findMatchingFact([]string{"database", "postgres"}, brainFiles); r != "" {
		t.Errorf("expected empty, got '%s'", r)
	}
}

func TestInferTarget(t *testing.T) {
	d := &Dream{}
	brainFiles := []BrainFile{
		{Path: "commands/docker-deploy.md"},
		{Path: "knowledge/k8s.md"},
	}
	// "docker compose up" → keywords [docker, compose, up]
	// "docker" matches "commands/docker-deploy.md"
	if r := d.inferTarget("docker compose up", brainFiles); !strings.Contains(r, "docker-deploy") {
		t.Errorf("expected docker-deploy, got %s", r)
	}
	if r := d.inferTarget("simple text", brainFiles); r == "" {
		t.Error("should not be empty")
	}
	if r := d.inferTarget("git push origin main", nil); r == "" {
		t.Error("should not be empty for nil brainFiles")
	}
}

func TestBuildBrainContext_AllPrefixes(t *testing.T) {
	d := &Dream{}
	brain := newMockBrain()
	d.brain = brain
	brain.files["commands/deploy.md"] = "deploy"
	brain.files["commands/test.md"] = "test"
	brain.files["knowledge/k8s.md"] = "k8s"
	brain.files["rules/style.md"] = "style"
	brain.files["skills/foo.md"] = "foo"
	brain.files["subscriptions/watch.md"] = "watch"

	bc := d.buildBrainContext()
	if len(bc.Commands) < 2 {
		t.Errorf("commands: %d", len(bc.Commands))
	}
	if len(bc.Facts) < 2 {
		t.Errorf("facts: %d", len(bc.Facts))
	}
}

func TestBuildEditPrompt_ContainsRules(t *testing.T) {
	d := &Dream{reflectModel: "test-model", maxReflectTokens: 2048}
	brain := newMockBrain()
	d.brain = brain
	proposals := []EditProposal{
		{ID: "p1", Target: "commands/x.md", Action: ActionImprove, Before: "old", Reason: "fix", Confidence: 0.9, Impact: 2.0, NeedsLLM: true},
	}
	prompt := d.buildEditPrompt(proposals)
	for _, want := range []string{"PROPOSAL p1", "kubectl", "Output ONLY valid JSON", "proposal_id", "reasoning"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing: %s", want)
		}
	}
}

func TestBuildEditPrompt_ActiveModel(t *testing.T) {
	d := &Dream{reflectModel: "custom-model", maxReflectTokens: 512}
	if d.activeModel() != "custom-model" {
		t.Errorf("expected custom-model, got %s", d.activeModel())
	}
}

func TestFilterEdits_JaccardTooSimilar(t *testing.T) {
	d := &Dream{}
	edits := []Edit{
		{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "same content here", Reasoning: "changed"},
	}
	proposals := []EditProposal{
		{ID: "p1", Confidence: 0.9, Before: "same content here"},
	}
	// Jaccard = 1.0 → should be rejected.
	result := d.filterEdits(edits, proposals)
	if len(result) != 0 {
		t.Errorf("high Jaccard should be rejected: got %d", len(result))
	}
}

func TestFilterEdits_ImproveWithPatternMatch(t *testing.T) {
	d := &Dream{}
	edits := []Edit{
		{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "fixed", Reasoning: "corrected"},
	}
	proposals := []EditProposal{
		{ID: "p1", Confidence: 0.9, Before: "docker compose up", Evidence: []string{"docker"}},
	}
	result := d.filterEdits(edits, proposals)
	if len(result) != 1 {
		t.Errorf("should pass with matching evidence: got %d", len(result))
	}
}

func TestFilterEdits_ImproveWithEvidenceInBefore(t *testing.T) {
	d := &Dream{}
	edits := []Edit{
		{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "fixed", Reasoning: "corrected docker issue"},
	}
	proposals := []EditProposal{
		{ID: "p1", Confidence: 0.9, Before: "docker compose up is the old way", Evidence: []string{"docker"},
			Reason: "docker fix"},
	}
	result := d.filterEdits(edits, proposals)
	if len(result) != 1 {
		t.Errorf("should pass with evidence keyword match: got %d", len(result))
	}
}

func TestShareEvidence(t *testing.T) {
	a := &EditProposal{Evidence: []string{"s1", "s2"}}
	b := &EditProposal{Evidence: []string{"s2", "s3"}}
	if !shareEvidence(a, b) {
		t.Error("should share s2")
	}
	c := &EditProposal{Evidence: []string{"s4"}}
	if shareEvidence(a, c) {
		t.Error("should not share")
	}
}

func TestFormatID(t *testing.T) {
	if formatID(0) != "p1" {
		t.Errorf("got %s", formatID(0))
	}
	if formatID(9) != "p10" {
		t.Errorf("got %s", formatID(9))
	}
}
