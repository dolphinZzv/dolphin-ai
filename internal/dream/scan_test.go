package dream

import (
	"context"
	"strings"
	"testing"
	"time"

	"dolphin/internal/types"
)

func TestDetectExplicitPreference(t *testing.T) {
	if !detectExplicitPreference("以后都用 k8s") { t.Error("true") }
	if detectExplicitPreference("正常对话") { t.Error("false") }
}
func TestDetectCorrection(t *testing.T) {
	if !detectCorrection("不对应该用kubectl") { t.Error("true") }
	if detectCorrection("好的") { t.Error("false") }
}
func TestJaccard(t *testing.T) {
	if jaccard([]string{"a"}, []string{"a"}) != 1.0 { t.Error("1.0") }
	if jaccard(nil, nil) != 0 { t.Error("0") }
}
func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" { t.Error("short") }
	if len(truncateStr("hello world long", 5)) > 8 { t.Error("long") }
}
func TestSanitiseFilename(t *testing.T) {
	s := sanitiseFilename("Deploy to K8s!")
	if s == "" || len(s) > 20 { t.Errorf("%q", s) }
}
func TestFormatInt(t *testing.T) {
	if formatInt(0) != "0" { t.Error("0") }
	if formatInt(42) != "42" { t.Error("42") }
}
func TestContainsAny(t *testing.T) {
	if !containsAny("hello", "hello") { t.Error("true") }
	if containsAny("hello", "world") { t.Error("false") }
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
	if res.Sessions != 1 { t.Error("sessions") }
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
	if imp < 1.0 { t.Errorf("penalized: %f", imp) }
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
	if len(r) != 1 || r[0].ID != "p2" { t.Errorf("dedup: %+v", r) }
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
		if p.Action == ActionDeprecate && p.Target == "commands/old.md" { found = true }
	}
	if !found { t.Errorf("no deprecate: %+v", ps) }
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
	if len(ps) > 0 { t.Errorf("cooldown should block: %d", len(ps)) }
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
		if p.Action == ActionImprove && strings.Contains(p.Target, "go-test") { found = true }
	}
	if !found { t.Errorf("no improve: %+v", ps) }
}

func TestScanBrainFiles(t *testing.T) {
	brain := newMockBrain()
	d := &Dream{brain: brain, state: newState()}
	brain.files["commands/x.md"] = "content"
	d.state.Usage.Files["commands/x.md"] = FileUsage{Refs: 5}
	files, err := d.scanBrainFiles(context.Background())
	if err != nil { t.Fatal(err) }
	if len(files) != 1 { t.Fatalf("expected 1: %d", len(files)) }
}

func TestFindMatchingCommand(t *testing.T) {
	d := &Dream{}
	bf := []BrainFile{{Path: "commands/deploy.md"}, {Path: "commands/test.md"}}
	if r := d.findMatchingCommand("deploy", bf); r != "commands/deploy.md" { t.Errorf("%s", r) }
	if r := d.findMatchingCommand("x", bf); r != "" { t.Errorf("%s", r) }
}
