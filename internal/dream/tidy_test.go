package dream

import (
	"testing"
)

func TestTidy_UpdatesTotals(t *testing.T) {
	d := &Dream{currentID: 1, fileCooldownDreams: 5, autoApply: false}
	d.state = newState()
	edits := []Edit{
		{ProposalID: "p1", Action: ActionImprove, Target: "commands/deploy.md"},
		{ProposalID: "p2", Action: ActionDeprecate, Target: "commands/old.md"},
		{ProposalID: "p3", Action: ActionCreate, Target: "knowledge/new.md"},
		{ProposalID: "p4", Action: ActionMerge, Target: "knowledge/merged.md"},
	}
	d.tidy(edits)
	if d.state.Totals.FilesImproved != 1 {
		t.Errorf("expected 1 improved, got %d", d.state.Totals.FilesImproved)
	}
	if d.state.Totals.FilesDeprecated != 1 {
		t.Errorf("expected 1 deprecated, got %d", d.state.Totals.FilesDeprecated)
	}
	if d.state.Totals.FilesCreated != 1 {
		t.Errorf("expected 1 created, got %d", d.state.Totals.FilesCreated)
	}
	if d.state.Totals.FilesMerged != 1 {
		t.Errorf("expected 1 merged, got %d", d.state.Totals.FilesMerged)
	}
}

func TestTidy_FileCooldowns(t *testing.T) {
	d := &Dream{currentID: 5, fileCooldownDreams: 5, autoApply: false}
	d.state = newState()
	edits := []Edit{
		{ProposalID: "p1", Target: "commands/x.md"},
	}
	d.tidy(edits)
	cd, ok := d.state.FileCooldowns["commands/x.md"]
	if !ok {
		t.Fatal("expected cooldown entry")
	}
	if cd.CooldownUntilDream != 10 {
		t.Errorf("expected cooldown until dream 10, got %d", cd.CooldownUntilDream)
	}
}

func TestTidy_LastAppliedEdits(t *testing.T) {
	d := &Dream{currentID: 1, fileCooldownDreams: 5, autoApply: false}
	d.state = newState()
	edits := []Edit{
		{ProposalID: "p1", Target: "a.md"},
		{ProposalID: "p2", Target: "b.md"},
	}
	d.tidy(edits)
	if len(d.state.LastAppliedEdits) != 2 {
		t.Fatalf("expected 2, got %d", len(d.state.LastAppliedEdits))
	}
}

func TestTidy_AutoApplySkipsCalibration(t *testing.T) {
	d := &Dream{currentID: 1, fileCooldownDreams: 5, autoApply: true}
	d.state = newState()
	d.state.Calibration.Thresholds[SignalCorrection] = 0.85
	edits := []Edit{{ProposalID: "p1", Action: ActionImprove, Target: "x.md"}}
	d.tidy(edits)
	// Threshold should not change.
	if d.state.Calibration.Thresholds[SignalCorrection] != 0.85 {
		t.Errorf("auto_apply should not change threshold, got %f", d.state.Calibration.Thresholds[SignalCorrection])
	}
}

func TestTidy_NonAutoCalibration_LowAdopt(t *testing.T) {
	d := &Dream{currentID: 1, fileCooldownDreams: 5, autoApply: false,
		calibrationWindow: 10, calibrationMinStep: 0.05,
		calibrationCeiling: 0.95, calibrationFloor: 0.30}
	d.state = newState()
	d.state.Calibration.Thresholds[SignalCorrection] = 0.85
	// Add window entries with low adoption.
	d.state.Calibration.Window = append(d.state.Calibration.Window,
		CalibrationEntry{DreamID: 1, Adopted: 0, Total: 5},
		CalibrationEntry{DreamID: 2, Adopted: 0, Total: 5},
	)
	edits := []Edit{{ProposalID: "p1", Action: ActionImprove, Target: "x.md"}}
	d.tidy(edits)
	// Low adoption → threshold should increase.
	if d.state.Calibration.Thresholds[SignalCorrection] <= 0.85 {
		t.Errorf("expected threshold > 0.85, got %f", d.state.Calibration.Thresholds[SignalCorrection])
	}
}

func TestCountAdopted(t *testing.T) {
	window := []CalibrationEntry{
		{DreamID: 1, Adopted: 2, Total: 4},
		{DreamID: 2, Adopted: 3, Total: 3},
	}
	adopted, total := countAdopted(window, SignalCorrection)
	if adopted != 5 || total != 7 {
		t.Errorf("expected 5/7, got %d/%d", adopted, total)
	}
}

func TestCalibrate_StableZone(t *testing.T) {
	d := &Dream{currentID: 1, fileCooldownDreams: 5, autoApply: false,
		calibrationWindow: 10, calibrationMinStep: 0.05,
		calibrationCeiling: 0.95, calibrationFloor: 0.30}
	d.state = newState()
	d.state.Calibration.Thresholds[SignalCorrection] = 0.85
	// 3/5 = 0.6 → stable zone, no change.
	d.state.Calibration.Window = []CalibrationEntry{
		{DreamID: 1, Adopted: 3, Total: 5},
	}
	d.calibrate([]Edit{{ProposalID: "p1"}})
	if d.state.Calibration.Thresholds[SignalCorrection] != 0.85 {
		t.Errorf("stable zone should not change threshold, got %f", d.state.Calibration.Thresholds[SignalCorrection])
	}
}
