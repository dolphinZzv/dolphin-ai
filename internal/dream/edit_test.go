package dream

import (
	"testing"
)

func TestParseEditOutput_Array(t *testing.T) {
	raw := `[{"proposal_id":"p1","action":"improve","target":"cmd/deploy.md","after":"fixed","reasoning":"corrected"}]`
	edits, err := parseEditOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].ProposalID != "p1" {
		t.Errorf("unexpected parse: %+v", edits)
	}
}

func TestParseEditOutput_SingleObject(t *testing.T) {
	raw := `{"proposal_id":"p2","action":"create","target":"know/x.md","after":"content","reasoning":"needed"}`
	edits, err := parseEditOutput(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if edits == nil {
		t.Fatal("edits is nil")
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d: %+v (raw used: %q)", len(edits), edits, raw)
	}
	if edits[0].ProposalID != "p2" {
		t.Errorf("expected p2, got %q", edits[0].ProposalID)
	}
}

func TestParseEditOutput_Wrapper(t *testing.T) {
	raw := `{"edits":[{"proposal_id":"p3","action":"merge","target":"a.md","after":"m","reasoning":"dedup"}]}`
	edits, err := parseEditOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Errorf("expected 1, got %d", len(edits))
	}
}

func TestParseEditOutput_CodeFences(t *testing.T) {
	raw := "```json\n[{\"proposal_id\":\"p1\",\"action\":\"deprecate\",\"target\":\"old.md\",\"after\":null,\"reasoning\":\"unused\"}]\n```"
	edits, err := parseEditOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Errorf("expected 1, got %d", len(edits))
	}
}

func TestParseEditOutput_InvalidJSON(t *testing.T) {
	_, err := parseEditOutput("not json")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFilterEdits_ProposalNotFound(t *testing.T) {
	d := &Dream{}
	edits := []Edit{{ProposalID: "p99", Action: ActionImprove, Target: "x.md", After: "a", Reasoning: "r"}}
	result := d.filterEdits(edits, nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestFilterEdits_DeprecateAlwaysPasses(t *testing.T) {
	d := &Dream{}
	edits := []Edit{{ProposalID: "p1", Action: ActionDeprecate, Target: "old.md"}}
	proposals := []EditProposal{{ID: "p1", Confidence: 0.0}} // low confidence → pass anyway for deprecate
	result := d.filterEdits(edits, proposals)
	if len(result) != 1 {
		t.Fatalf("deprecate should always pass: got %d", len(result))
	}
}

func TestFilterEdits_EmptyAfterRejected(t *testing.T) {
	d := &Dream{}
	edits := []Edit{{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "   "}}
	proposals := []EditProposal{{ID: "p1", Confidence: 0.9, Before: "old content"}}
	result := d.filterEdits(edits, proposals)
	if len(result) != 0 {
		t.Error("empty after should be rejected")
	}
}

func TestFilterEdits_NoReasoningRejected(t *testing.T) {
	d := &Dream{}
	edits := []Edit{{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "new", Reasoning: ""}}
	proposals := []EditProposal{{ID: "p1", Confidence: 0.9, Before: "old"}}
	result := d.filterEdits(edits, proposals)
	if len(result) != 0 {
		t.Error("no reasoning should be rejected")
	}
}

func TestVerifyCausality_CreateNeedsHighConfidence(t *testing.T) {
	d := &Dream{}
	edit := Edit{Action: ActionCreate, Target: "new.md", After: "content"}
	proposal := &EditProposal{ID: "p1", Confidence: 0.5} // too low
	if d.verifyCausality(edit, proposal, nil) {
		t.Error("create with low confidence should be rejected")
	}
}

func TestVerifyCausality_CreatePasses(t *testing.T) {
	d := &Dream{}
	edit := Edit{Action: ActionCreate, Target: "new.md", After: "content"}
	proposal := &EditProposal{ID: "p1", Confidence: 0.90, Evidence: []string{"s1"}}
	if !d.verifyCausality(edit, proposal, nil) {
		t.Error("create with high confidence + evidence should pass")
	}
}

func TestVerifyCausality_MergeNeedsConfidence(t *testing.T) {
	d := &Dream{}
	edit := Edit{Action: ActionMerge, Target: "a.md", After: "merged"}
	proposal := &EditProposal{ID: "p1", Confidence: 0.5}
	if d.verifyCausality(edit, proposal, nil) {
		t.Error("merge with low confidence should be rejected")
	}
}

func TestVerifyCausality_RhetoricalNoPair(t *testing.T) {
	d := &Dream{}
	edit := Edit{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "a"}
	proposal := &EditProposal{ID: "p1", IsRhetorical: true, Confidence: 0.9}
	if d.verifyCausality(edit, proposal, nil) {
		t.Error("rhetorical without L1/L2 pair should be rejected")
	}
}

func TestVerifyCausality_RhetoricalWithPair(t *testing.T) {
	d := &Dream{}
	edit := Edit{ProposalID: "p1", Action: ActionImprove, Target: "x.md", After: "a"}
	proposal := &EditProposal{ID: "p1", IsRhetorical: true, Confidence: 0.9, Evidence: []string{"s1"}}
	pair := &EditProposal{ID: "p2", IsRhetorical: false, Confidence: 0.90, Evidence: []string{"s1"}}
	all := []EditProposal{*proposal, *pair}
	if !d.verifyCausality(edit, proposal, all) {
		t.Error("rhetorical with L1/L2 pair sharing evidence should pass")
	}
}

func TestActiveModel_WithReflectModel(t *testing.T) {
	d := &Dream{reflectModel: "my-model"}
	if d.activeModel() != "my-model" {
		t.Errorf("expected my-model, got %s", d.activeModel())
	}
}

func TestActiveModel_Empty(t *testing.T) {
	d := &Dream{}
	if d.activeModel() != "" {
		t.Errorf("expected empty, got %s", d.activeModel())
	}
}
