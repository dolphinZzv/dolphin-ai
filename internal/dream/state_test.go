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
	if v, ok := s.Calibration.Thresholds[SignalCorrection]; !ok || v != 0.85 {
		t.Errorf("default correction threshold should be 0.85, got %v", v)
	}
}

func TestStateSaveLoadRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	primary := filepath.Join(tmpDir, "dream.json")
	backup := filepath.Join(tmpDir, "backup", "state.json")

	s := newState()
	s.LastDreamID = 5
	s.Totals.FilesImproved = 3
	s.Usage.Files["commands/x.md"] = FileUsage{Refs: 10}

	if err := s.save(primary, backup); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadState(primary, backup)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastDreamID != 5 {
		t.Errorf("expected 5, got %d", loaded.LastDreamID)
	}
	if loaded.Totals.FilesImproved != 3 {
		t.Errorf("expected 3, got %d", loaded.Totals.FilesImproved)
	}
}

func TestStateRecoveryFromBackup(t *testing.T) {
	tmpDir := t.TempDir()
	primary := filepath.Join(tmpDir, "dream.json")
	backup := filepath.Join(tmpDir, "backup", "state.json")

	s := newState()
	s.LastDreamID = 7
	_ = os.MkdirAll(filepath.Dir(backup), 0o755)
	_ = s.save(primary, backup)

	// Remove primary.
	_ = os.Remove(primary)

	loaded, err := loadState(primary, backup)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastDreamID != 7 {
		t.Errorf("expected 7 from backup, got %d", loaded.LastDreamID)
	}
}

func TestStateBootstrapWhenNone(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := loadState(filepath.Join(tmpDir, "dream.json"), filepath.Join(tmpDir, "b", "state.json"))
	if err == nil {
		t.Fatal("expected error when no state files exist")
	}
}

func TestDefaultThresholds(t *testing.T) {
	if v := defaultThresholds[SignalCorrection]; v != 0.85 {
		t.Errorf("correction: %f", v)
	}
	if v := defaultThresholds[SignalPreference]; v != 0.95 {
		t.Errorf("preference: %f", v)
	}
	if v := defaultThresholds[SignalRefinement]; v != 0.65 {
		t.Errorf("refinement: %f", v)
	}
}
