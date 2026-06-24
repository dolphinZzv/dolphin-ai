package dream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the persistent runtime metadata for Dream, stored at
// .dolphin/dream.json (primary, gitignored) and shadowed to
// brain/.dream/state.json (backup, git-tracked).
type State struct {
	LastDreamAt      time.Time               `json:"last_dream_at"`
	LastDreamID      int                     `json:"last_dream_id"`
	ConsecutiveEmpty int                     `json:"consecutive_empty"`
	OpenBranch       string                  `json:"open_branch"`
	LastMergeSHA     string                  `json:"last_merge_sha,omitempty"`
	Totals           StateTotals             `json:"totals"`
	Usage            UsageData               `json:"usage"`
	FileCooldowns    map[string]FileCooldown `json:"file_cooldowns"`
	Calibration      CalibrationData         `json:"calibration"`
	LastAppliedEdits []AppliedEdit           `json:"last_applied_edits"`
	EditFeedback     map[string]string       `json:"edit_feedback"`
	LastAlerted      map[string]int          `json:"last_alerted"`
	BackupStale      bool                    `json:"backup_stale"`
}

// StateTotals tracks cumulative edit counts across all dreams.
type StateTotals struct {
	FilesImproved   int `json:"files_improved"`
	FilesDeprecated int `json:"files_deprecated"`
	FilesMerged     int `json:"files_merged"`
	FilesCreated    int `json:"files_created"`
}

// UsageData tracks how often brain files are referenced.
type UsageData struct {
	Files map[string]FileUsage `json:"files"`
}

// FileUsage records reference statistics for a single brain file.
type FileUsage struct {
	Refs int       `json:"refs"`
	Last time.Time `json:"last"`
}

// FileCooldown prevents repeated editing of the same file.
type FileCooldown struct {
	LastEditedDream    int `json:"last_edited_dream"`
	CooldownUntilDream int `json:"cooldown_until_dream"`
}

// CalibrationData holds per-signal-type confidence thresholds, adjusted over
// time based on manual review feedback.
type CalibrationData struct {
	Thresholds map[SignalType]float64 `json:"thresholds"`
	Window     []CalibrationEntry     `json:"window"`
}

// CalibrationEntry records the outcome of a single dream for calibration.
type CalibrationEntry struct {
	DreamID int `json:"dream_id"`
	Adopted int `json:"adopted"`
	Total   int `json:"total"`
}

// AppliedEdit records an edit that was applied for implicit-feedback tracking.
type AppliedEdit struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	AppliedAt time.Time `json:"applied_at"`
}

// ---------------------------------------------------------------------------
// State I/O
// ---------------------------------------------------------------------------

const (
	statePrimary = ".dolphin/dream.json"
	stateBackup  = ".dolphin/brain/.dream/state.json"
)

var defaultThresholds = map[SignalType]float64{
	SignalCorrection:   0.85,
	SignalPreference:   0.95,
	SignalRepetition:   0.80,
	SignalRefinement:   0.65,
	SignalObsolescence: 0.70,
}

func newState() *State {
	return &State{
		Usage:         UsageData{Files: make(map[string]FileUsage)},
		FileCooldowns: make(map[string]FileCooldown),
		Calibration: CalibrationData{
			Thresholds: copyMap(defaultThresholds),
		},
		EditFeedback: make(map[string]string),
		LastAlerted:  make(map[string]int),
	}
}

func loadState(primaryPath, backupPath string) (*State, error) {
	s, err := loadJSON[State](primaryPath)
	if err == nil {
		if s.Usage.Files == nil {
			s.Usage.Files = make(map[string]FileUsage)
		}
		if s.FileCooldowns == nil {
			s.FileCooldowns = make(map[string]FileCooldown)
		}
		if s.Calibration.Thresholds == nil {
			s.Calibration.Thresholds = defaultThresholds
		}
		if s.EditFeedback == nil {
			s.EditFeedback = make(map[string]string)
		}
		if s.LastAlerted == nil {
			s.LastAlerted = make(map[string]int)
		}
		return s, nil
	}
	// Try backup.
	s, err = loadJSON[State](backupPath)
	if err == nil {
		return s, nil
	}
	return nil, err
}

func (s *State) save(primaryPath, backupPath string) error {
	// Write primary.
	if err := writeJSON(primaryPath, s); err != nil {
		return err
	}
	// Write backup (best-effort).
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err == nil {
		if writeJSON(backupPath, s) != nil {
			s.BackupStale = true
		} else {
			s.BackupStale = false
		}
	}
	return nil
}

func loadJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
