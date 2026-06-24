package dream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewState(t *testing.T) {
	s := newState()
	if s.Usage.Files == nil {
		t.Error("usage files map should be initialised")
	}
	if s.Calibration.Thresholds == nil {
		t.Error("calibration thresholds should be initialised")
	}
}
func TestStateSaveLoadRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	s := newState()
	s.LastDreamID = 5
	s.Totals.FilesImproved = 3
	s.Usage.Files["c/x.md"] = FileUsage{Refs: 10}
	if err := s.save(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json")); err != nil {
		t.Fatal(err)
	}
	l, err := loadState(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	if l.LastDreamID != 5 {
		t.Errorf("got %d", l.LastDreamID)
	}
}
func TestStateRecoveryFromBackup(t *testing.T) {
	tmp := t.TempDir()
	s := newState()
	s.LastDreamID = 7
	os.MkdirAll(filepath.Join(tmp, "b"), 0o755)
	s.save(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	os.Remove(filepath.Join(tmp, "d.json"))
	l, err := loadState(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	if l.LastDreamID != 7 {
		t.Errorf("got %d", l.LastDreamID)
	}
}
func TestStateBootstrapWhenNone(t *testing.T) {
	_, err := loadState(filepath.Join(t.TempDir(), "d.json"), filepath.Join(t.TempDir(), "b", "s.json"))
	if err == nil {
		t.Fatal("expected error")
	}
}
func TestDefaultThresholds(t *testing.T) {
	if defaultThresholds[SignalCorrection] != 0.85 {
		t.Error("correction")
	}
	if defaultThresholds[SignalPreference] != 0.95 {
		t.Error("preference")
	}
}
func TestState_NilMapsInitialized(t *testing.T) {
	tmp := t.TempDir()
	s := newState()
	s.Usage = UsageData{}
	s.save(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	l, _ := loadState(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	if l.Usage.Files == nil {
		t.Error("Files should be initialised on load")
	}
}
func TestLoadJSON_InvalidContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("not json"), 0o600)
	if _, err := loadJSON[State](p); err == nil {
		t.Error("expected error")
	}
}

func TestLoadState_NilCalibrationThresholds(t *testing.T) {
	tmp := t.TempDir()
	// Write a state file that's valid JSON but has nil Calibration.Thresholds.
	s := newState()
	s.Calibration.Thresholds = nil
	s.save(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	l, _ := loadState(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	if l.Calibration.Thresholds == nil {
		t.Error("thresholds should be restored to defaults when nil")
	}
	if l.Calibration.Thresholds[SignalCorrection] != 0.85 {
		t.Errorf("expected default 0.85, got %f", l.Calibration.Thresholds[SignalCorrection])
	}
}

func TestLoadState_NilFeedbackAndAlerted(t *testing.T) {
	tmp := t.TempDir()
	s := newState()
	s.EditFeedback = nil
	s.LastAlerted = nil
	s.save(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	l, _ := loadState(filepath.Join(tmp, "d.json"), filepath.Join(tmp, "b", "s.json"))
	if l.EditFeedback == nil {
		t.Error("EditFeedback should be initialised")
	}
	if l.LastAlerted == nil {
		t.Error("LastAlerted should be initialised")
	}
}

func TestSave_PrimaryWriteFails(t *testing.T) {
	s := newState()
	err := s.save("/dev/null/impossible/dream.json", "/tmp/x.json")
	if err == nil {
		t.Error("expected error when primary write fails")
	}
}
