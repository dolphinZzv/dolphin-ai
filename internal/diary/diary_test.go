package diary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dolphinzZ/internal/session"
)

// writeTestSessionSummary creates a -summary.json file for testing.
func writeTestSessionSummary(dir string, id string, startedAt time.Time, turns, toolCalls, errors, compressions int, state string) {
	sum := session.Summary{
		SessionID:        session.SessionID(id),
		StartedAt:        startedAt,
		EndedAt:          startedAt.Add(time.Hour),
		Turns:            turns,
		MaxLoop:          50,
		ToolCallCount:    toolCalls,
		ErrorCount:       errors,
		CompressionCount: compressions,
		State:            state,
	}
	data, _ := json.MarshalIndent(sum, "", "  ")
	os.WriteFile(filepath.Join(dir, id+"-summary.json"), data, 0644)
}

// writeTestCompressionEvent appends a compression event to a .jsonl file.
func writeTestCompressionEvent(dir string, id string, level int, covered int, summary string) {
	meta := session.CompressMeta{
		Level:        level,
		CoveredCount: covered,
		Summary:      summary,
		TokensSaved:  100,
	}
	content, _ := json.Marshal(meta)
	evt := session.SessionEvent{
		Timestamp: time.Now(),
		SessionID: session.SessionID(id),
		Type:      session.EventCompression,
		Content:   content,
	}
	data, _ := json.Marshal(evt)
	f, err := os.OpenFile(filepath.Join(dir, id+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))
}

func TestDiarySyncWritesDayEntry(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "abc123", now, 10, 5, 2, 1, "completed")
	writeTestCompressionEvent(sessionDir, "abc123", 1, 6, "summary text here")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	if err := d.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Check day.json exists
	dayPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "day.json")
	entry, err := d.readEntry(dayPath)
	if err != nil {
		t.Fatalf("read day entry: %v", err)
	}
	if entry.Level != LevelDay {
		t.Errorf("expected level day, got %s", entry.Level)
	}
	if entry.Stats.SessionCount != 1 {
		t.Errorf("expected 1 session, got %d", entry.Stats.SessionCount)
	}
	if entry.Stats.TotalTurns != 10 {
		t.Errorf("expected 10 turns, got %d", entry.Stats.TotalTurns)
	}
	if entry.Stats.TotalToolCalls != 5 {
		t.Errorf("expected 5 tool calls, got %d", entry.Stats.TotalToolCalls)
	}
	if entry.Stats.CompressionCount != 1 {
		t.Errorf("expected 1 compression, got %d", entry.Stats.CompressionCount)
	}

	// Check sessions.jsonl
	sessionsPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "sessions.jsonl")
	refs := d.loadDaySessionsFrom(sessionsPath)
	if len(refs) != 1 {
		t.Fatalf("expected 1 session ref, got %d", len(refs))
	}
	if refs[0].SessionID != "abc123" {
		t.Errorf("expected abc123, got %s", refs[0].SessionID)
	}
	if len(refs[0].Compressions) != 1 {
		t.Errorf("expected 1 compression, got %d", len(refs[0].Compressions))
	}
	if refs[0].Compressions[0].Summary != "summary text here" {
		t.Errorf("unexpected summary: %s", refs[0].Compressions[0].Summary)
	}
}

func TestDiarySyncCascadeWeekMonthYear(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	// Use a Monday as anchor, write sessions on Mon-Wed same week
	now := time.Now()
	daysSinceMonday := int(now.Weekday()) - 1
	if daysSinceMonday < 0 {
		daysSinceMonday = 6
	}
	monday := now.AddDate(0, 0, -daysSinceMonday)
	for i := 0; i < 3; i++ {
		dayTime := monday.AddDate(0, 0, i) // Mon, Tue, Wed
		id := "sess" + string(rune('a'+i))
		writeTestSessionSummary(sessionDir, id, dayTime, 5+i, 2+i, 0, 0, "completed")
	}

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	if err := d.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Check week entry exists
	year, week := monday.ISOWeek()
	weekKey := fmt.Sprintf("%d-W%02d", year, week)
	monthStr := monday.Format("01")
	weekPath := filepath.Join(diaryDir, monday.Format("2006"), monthStr, weekKey+".json")
	weekEntry, err := d.readEntry(weekPath)
	if err != nil {
		t.Fatalf("read week entry: %v", err)
	}
	if weekEntry.Level != LevelWeek {
		t.Errorf("expected level week, got %s", weekEntry.Level)
	}
	if weekEntry.Stats.SessionCount != 3 {
		t.Errorf("expected 3 sessions in week, got %d", weekEntry.Stats.SessionCount)
	}

	// Check month entry exists
	monthPath := filepath.Join(diaryDir, monday.Format("2006"), monday.Format("01"), "month.json")
	monthEntry, err := d.readEntry(monthPath)
	if err != nil {
		t.Fatalf("read month entry: %v", err)
	}
	if monthEntry.Level != LevelMonth {
		t.Errorf("expected level month, got %s", monthEntry.Level)
	}

	// Check year entry exists
	yearPath := filepath.Join(diaryDir, monday.Format("2006"), "year.json")
	yearEntry, err := d.readEntry(yearPath)
	if err != nil {
		t.Fatalf("read year entry: %v", err)
	}
	if yearEntry.Level != LevelYear {
		t.Errorf("expected level year, got %s", yearEntry.Level)
	}
	if yearEntry.Stats.SessionCount != 3 {
		t.Errorf("expected 3 sessions in year, got %d", yearEntry.Stats.SessionCount)
	}
}

func TestDiarySyncIncrementalSkipsProcessedSessions(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 0, "completed")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	// First sync
	if err := d.Sync(); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Ensure mtime differs from last_sync.json (1s granularity on some filesystems)
	time.Sleep(time.Second)

	// Add a new session
	writeTestSessionSummary(sessionDir, "sess2", now, 3, 1, 0, 0, "completed")

	// Second sync — should only add sess2, not duplicate sess1
	if err := d.Sync(); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	sessionsPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "sessions.jsonl")
	refs := d.loadDaySessionsFrom(sessionsPath)
	if len(refs) != 2 {
		t.Fatalf("expected 2 session refs, got %d", len(refs))
	}
	ids := make(map[string]bool)
	for _, r := range refs {
		ids[r.SessionID] = true
	}
	if !ids["sess1"] || !ids["sess2"] {
		t.Error("missing session IDs")
	}
}

func TestDiarySyncIdempotent(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 0, "completed")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	// Run sync twice
	d.Sync()
	d.Sync()

	sessionsPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "sessions.jsonl")
	refs := d.loadDaySessionsFrom(sessionsPath)
	if len(refs) != 1 {
		t.Fatalf("expected 1 session ref, got %d", len(refs))
	}
}

func TestDiaryPruneDaySessions(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("sess%d", i)
		sessionTime := now.Add(time.Duration(i) * time.Minute)
		writeTestSessionSummary(sessionDir, id, sessionTime, 1, 0, 0, 0, "completed")
	}

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 3, // Keep only 3 newest
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	sessionsPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "sessions.jsonl")
	refs := d.loadDaySessionsFrom(sessionsPath)
	if len(refs) != 3 {
		t.Fatalf("expected 3 sessions after prune, got %d", len(refs))
	}
	// Should keep the newest (last) sessions
	if refs[0].SessionID == "sess0" {
		t.Error("expected oldest session to be pruned")
	}
}

func TestDiaryPruneWeekDays(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	// Use a Monday as anchor, then write sessions on Mon-Thu of the same week
	now := time.Now()
	daysSinceMonday := int(now.Weekday()) - 1
	if daysSinceMonday < 0 {
		daysSinceMonday = 6 // Sunday → -6 days to Monday
	}
	monday := now.AddDate(0, 0, -daysSinceMonday)

	// Write sessions on 5 different weekdays in the same week
	for i := 0; i < 5; i++ {
		dayTime := monday.AddDate(0, 0, i)
		id := fmt.Sprintf("sess%d", i)
		writeTestSessionSummary(sessionDir, id, dayTime, 1, 0, 0, 0, "completed")
	}

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    3, // Keep only 3 newest days
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	// The oldest day (Monday) should be removed since we keep only 3 days
	oldestDay := monday
	oldestDayPath := filepath.Join(diaryDir, oldestDay.Format("2006"), oldestDay.Format("01"), oldestDay.Format("02"))
	if _, err := os.Stat(oldestDayPath); !os.IsNotExist(err) {
		t.Error("expected oldest day dir to be removed")
	}
	// Tuesday should also be removed (5 days → keep 3, remove 2 oldest)
	secondOldest := monday.AddDate(0, 0, 1)
	secondOldestPath := filepath.Join(diaryDir, secondOldest.Format("2006"), secondOldest.Format("01"), secondOldest.Format("02"))
	if _, err := os.Stat(secondOldestPath); !os.IsNotExist(err) {
		t.Error("expected second oldest day dir to be removed")
	}
}

func TestDiaryPruneMonthWeeks(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	// Write sessions across 4 different weeks
	for i := 0; i < 4; i++ {
		dayTime := now.AddDate(0, 0, -i*7) // 1 per week
		id := fmt.Sprintf("sess%d", i)
		writeTestSessionSummary(sessionDir, id, dayTime, 1, 0, 0, 0, "completed")
	}

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  2, // Keep only 2 newest weeks
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	// Verify month entry has at most 2 weeks
	monthPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), "month.json")
	entry, err := d.readEntry(monthPath)
	if err != nil {
		t.Fatalf("read month entry: %v", err)
	}
	if len(entry.Children) > 2 {
		t.Errorf("expected <= 2 week children, got %d", len(entry.Children))
	}
}

func TestDiaryLoadContext(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 0, "completed")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	// Load year context
	entries, err := d.LoadContext(0)
	if err != nil {
		t.Fatalf("LoadContext(0): %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least year entry")
	}
	if entries[0].Level != LevelYear {
		t.Errorf("expected year level, got %s", entries[0].Level)
	}

	// Load with months
	entries, err = d.LoadContext(1)
	if err != nil {
		t.Fatalf("LoadContext(1): %v", err)
	}
	hasMonth := false
	for _, e := range entries {
		if e.Level == LevelMonth {
			hasMonth = true
			break
		}
	}
	if !hasMonth {
		t.Error("expected month entries with depth=1")
	}
}

func TestDiaryTryLockPreventsConcurrentSync(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 0, "completed")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	var wg sync.WaitGroup
	results := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = d.Sync()
		}(i)
	}
	wg.Wait()

	for _, err := range results {
		if err != nil {
			t.Errorf("Sync returned error: %v", err)
		}
	}

	// Only one session should exist (no duplication)
	sessionsPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "sessions.jsonl")
	refs := d.loadDaySessionsFrom(sessionsPath)
	if len(refs) != 1 {
		t.Errorf("expected 1 session, got %d", len(refs))
	}
}

func TestDiaryAtomicWriteNoTempFileLeft(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 0, "completed")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	// Walk diary dir, ensure no .tmp files remain
	filepath.Walk(diaryDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if filepath.Ext(path) == ".tmp" {
			t.Errorf("found leftover tmp file: %s", path)
		}
		return nil
	})
}

func TestDiaryPruneTotalSize(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	// Create sessions in 3 different months of the same year
	for m := 0; m < 3; m++ {
		monthTime := time.Date(2026, time.Month(4+m), 15, 12, 0, 0, 0, time.UTC)
		id := fmt.Sprintf("sess%d", m)
		writeTestSessionSummary(sessionDir, id, monthTime, 10, 5, 1, 0, "completed")
	}

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  2, // Keep only 2 newest months per year
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	// Should have pruned the oldest month (April)
	yearPath := filepath.Join(diaryDir, "2026", "year.json")
	entry, err := d.readEntry(yearPath)
	if err != nil {
		t.Fatalf("read year entry: %v", err)
	}
	if len(entry.Children) > 2 {
		t.Errorf("expected <= 2 month children after year prune, got %d", len(entry.Children))
	}
	// The oldest month (04) dir should be removed
	oldestMonthPath := filepath.Join(diaryDir, "2026", "04")
	if _, err := os.Stat(oldestMonthPath); !os.IsNotExist(err) {
		t.Error("expected oldest month dir (04) to be removed")
	}
}

func TestDiarySummaryAggregation(t *testing.T) {
	sessionDir := t.TempDir()
	diaryDir := t.TempDir()

	now := time.Now()
	writeTestSessionSummary(sessionDir, "sess1", now, 5, 2, 0, 1, "completed")
	writeTestCompressionEvent(sessionDir, "sess1", 1, 6, "L1 summary content")
	writeTestCompressionEvent(sessionDir, "sess1", 2, 3, "L2 merged summary")

	d := New(Config{
		Dir:            diaryDir,
		MaxDaySessions: 200,
		MaxWeekDays:    7,
		MaxMonthWeeks:  5,
		MaxYearMonths:  12,
		MaxTotalMB:     500,
	}, sessionDir)

	d.Sync()

	dayPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), now.Format("02"), "day.json")
	entry, _ := d.readEntry(dayPath)

	// Summary should contain L1 content
	if len(entry.Summary) == 0 {
		t.Error("expected non-empty day summary")
	}

	// Week summary should aggregate day summary
	year, week := now.ISOWeek()
	weekPath := filepath.Join(diaryDir, now.Format("2006"), now.Format("01"), fmt.Sprintf("%d-W%02d.json", year, week))
	weekEntry, _ := d.readEntry(weekPath)
	if len(weekEntry.Summary) == 0 {
		t.Error("expected non-empty week summary")
	}
}
