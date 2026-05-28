// Package diary manages session history, compression, and chronological summary.
package diary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/session"

	"go.uber.org/zap"
)

// Level is the diary entry level.
type Level string

const (
	LevelDay   Level = "day"
	LevelWeek  Level = "week"
	LevelMonth Level = "month"
	LevelYear  Level = "year"
)

// Config holds diary configuration.
type Config struct {
	Dir            string
	MaxDaySessions int
	MaxWeekDays    int
	MaxMonthWeeks  int
	MaxYearMonths  int
	MaxTotalMB     int
}

// SessionRef is a lightweight reference to a session summary.
type SessionRef struct {
	SessionID        string                 `json:"session_id"`
	Turns            int                    `json:"turns"`
	ToolCallCount    int                    `json:"tool_call_count"`
	ErrorCount       int                    `json:"error_count"`
	CompressionCount int                    `json:"compression_count"`
	Compressions     []session.CompressMeta `json:"compressions,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	State            string                 `json:"state"`
	StartedAt        time.Time              `json:"started_at"`
	EndedAt          time.Time              `json:"ended_at"`
}

// DiaryEntry is a summary at a specific time level.
type DiaryEntry struct {
	Level    Level      `json:"level"`
	Date     string     `json:"date"`
	Summary  string     `json:"summary"`
	Stats    Stats      `json:"stats"`
	Children []ChildRef `json:"children"`
}

// Stats aggregates session statistics.
type Stats struct {
	SessionCount     int `json:"session_count"`
	TotalTurns       int `json:"total_turns"`
	TotalToolCalls   int `json:"total_tool_calls"`
	TotalErrors      int `json:"total_errors"`
	CompressionCount int `json:"compression_count"`
}

// ChildRef points to a child diary entry.
type ChildRef struct {
	Date  string `json:"date"`
	Level Level  `json:"level"`
	Path  string `json:"path"`
}

// Diary manages the hierarchical time-based diary.
type Diary struct {
	dir        string
	cfg        Config
	sessionDir string
	mu         sync.Mutex
}

// New creates a new Diary.
func New(cfg Config, sessionDir string) *Diary {
	return &Diary{
		dir:        cfg.Dir,
		cfg:        cfg,
		sessionDir: sessionDir,
	}
}

// OnConfigChange handles diary config hot-reload. Updates the internal config.
func (d *Diary) OnConfigChange(oldCfg, newCfg *config.Config) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfg = Config{
		Dir:            newCfg.Diary.Dir,
		MaxDaySessions: newCfg.Diary.MaxDaySessions,
		MaxWeekDays:    newCfg.Diary.MaxWeekDays,
		MaxMonthWeeks:  newCfg.Diary.MaxMonthWeeks,
		MaxYearMonths:  newCfg.Diary.MaxYearMonths,
		MaxTotalMB:     newCfg.Diary.MaxTotalMB,
	}
	d.dir = newCfg.Diary.Dir
}

// Sync scans session summaries and writes diary entries for unprocessed dates.
func (d *Diary) Sync() error {
	if !d.mu.TryLock() {
		return nil // another Sync is already running
	}
	defer d.mu.Unlock()

	if d.dir == "" {
		return nil
	}

	lastSync := d.readLastSync()

	// Scan session summaries newer than last sync
	entries, err := os.ReadDir(d.sessionDir)
	if err != nil {
		return fmt.Errorf("read session dir: %w", err)
	}

	// Group sessions by date, collect affected dates for cascade
	affectedDates := make(map[string]bool)
	daySessions := make(map[string][]SessionRef)

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "-summary.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !info.ModTime().After(lastSync) {
			continue
		}

		ref, err := d.readSessionSummary(filepath.Join(d.sessionDir, name))
		if err != nil {
			zap.S().Warnw("diary: skip unreadable session summary", "file", name, "error", err)
			continue
		}
		if ref == nil {
			continue
		}

		dateKey := ref.StartedAt.Format("2006-01-02")
		daySessions[dateKey] = append(daySessions[dateKey], *ref)
		affectedDates[dateKey] = true
	}

	if len(daySessions) == 0 {
		return nil
	}

	// Sort dates for deterministic output
	sortedDates := make([]string, 0, len(daySessions))
	for dk := range daySessions {
		sortedDates = append(sortedDates, dk)
	}
	sort.Strings(sortedDates)

	// Write day entries
	for _, dateKey := range sortedDates {
		sessions := daySessions[dateKey]
		dayDate, _ := time.Parse("2006-01-02", dateKey)

		// Load existing sessions if day already exists
		existing := d.loadDaySessions(dayDate)
		existingIDs := make(map[string]bool)
		for _, s := range existing {
			existingIDs[s.SessionID] = true
		}
		for _, s := range sessions {
			if !existingIDs[s.SessionID] {
				existing = append(existing, s)
			}
		}
		sort.Slice(existing, func(i, j int) bool {
			return existing[i].StartedAt.Before(existing[j].StartedAt)
		})

		if err := d.writeDay(dayDate, existing); err != nil {
			zap.S().Warnw("diary: write day failed", "date", dateKey, "error", err)
			continue
		}
	}

	// Targeted cascade: update week/month/year for each affected date
	affectedWeeks := make(map[string]bool)
	affectedMonths := make(map[string]bool)
	affectedYears := make(map[string]bool)

	for dateKey := range affectedDates {
		dayDate, _ := time.Parse("2006-01-02", dateKey)
		year, week := dayDate.ISOWeek()
		wk := fmt.Sprintf("%d-W%02d", year, week)
		mo := dayDate.Format("2006-01")
		yr := dayDate.Format("2006")
		affectedWeeks[wk] = true
		affectedMonths[mo] = true
		affectedYears[yr] = true
	}

	for wk := range affectedWeeks {
		d.cascadeWeek(wk)
	}
	for mo := range affectedMonths {
		d.cascadeMonth(mo)
	}
	for yr := range affectedYears {
		d.cascadeYear(yr)
	}

	// Prune
	d.pruneAll()

	// Write last sync timestamp
	d.writeLastSync(time.Now())

	return nil
}

// LoadContext returns diary entries for progressive reading.
// depth: 0=year only, 1=+months, 2=+weeks, 3=+days.
func (d *Diary) LoadContext(depth int) ([]DiaryEntry, error) {
	if depth < 0 {
		depth = 0
	}
	if depth > 3 {
		depth = 3
	}

	var result []DiaryEntry

	years, err := d.listEntries(LevelYear)
	if err != nil {
		return nil, err
	}
	result = append(result, years...)

	if depth >= 1 {
		for _, y := range years {
			months, _ := d.listEntriesUnder(y.Date, LevelMonth)
			result = append(result, months...)

			if depth >= 2 {
				for _, m := range months {
					weeks, _ := d.listEntriesUnder(y.Date+"/"+m.Date[5:7], LevelWeek)
					result = append(result, weeks...)

					if depth >= 3 {
						for _, w := range weeks {
							days, _ := d.listDaysForWeek(w.Date)
							result = append(result, days...)
						}
					}
				}
			}
		}
	}

	return result, nil
}

// readSessionSummary reads a -summary.json file and returns a SessionRef.
func (d *Diary) readSessionSummary(path string) (*SessionRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sum session.Summary
	if err := json.Unmarshal(data, &sum); err != nil {
		return nil, err
	}

	ref := &SessionRef{
		SessionID:        string(sum.SessionID),
		Turns:            sum.Turns,
		ToolCallCount:    sum.ToolCallCount,
		ErrorCount:       sum.ErrorCount,
		CompressionCount: sum.CompressionCount,
		Summary:          sum.Summary,
		State:            sum.State,
		StartedAt:        sum.StartedAt,
		EndedAt:          sum.EndedAt,
	}

	// Read compression events from the session .jsonl
	jsonlPath := strings.TrimSuffix(path, "-summary.json") + ".jsonl"
	ref.Compressions = d.readCompressions(jsonlPath)

	return ref, nil
}

// readCompressions extracts compression events from a session .jsonl file.
func (d *Diary) readCompressions(path string) []session.CompressMeta {
	events, err := session.ReadEvents(path)
	if err != nil {
		return nil
	}
	var result []session.CompressMeta
	for _, evt := range events {
		if evt.Type == session.EventCompression && len(evt.Content) > 0 {
			var meta session.CompressMeta
			if json.Unmarshal(evt.Content, &meta) == nil {
				result = append(result, meta)
			}
		}
	}
	return result
}

// loadDaySessions loads existing sessions from a day's sessions.jsonl.
func (d *Diary) loadDaySessions(date time.Time) []SessionRef {
	path := d.daySessionsPath(date)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var refs []SessionRef
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ref SessionRef
		if json.Unmarshal([]byte(line), &ref) == nil {
			refs = append(refs, ref)
		}
	}
	return refs
}

// writeDay writes a day's sessions.jsonl and day.json.
func (d *Diary) writeDay(date time.Time, sessions []SessionRef) error {
	dir := d.dayDir(date)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write sessions.jsonl
	var sb strings.Builder
	for _, s := range sessions {
		b, _ := json.Marshal(s)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	sessionsPath := d.daySessionsPath(date)
	if err := atomicWrite(sessionsPath, []byte(sb.String())); err != nil {
		return err
	}

	// Build day entry
	entry := d.buildDayEntry(date, sessions)
	dayPath := filepath.Join(dir, "day.json")
	return atomicWriteJSON(dayPath, entry)
}

// buildDayEntry builds a DiaryEntry from a day's sessions.
func (d *Diary) buildDayEntry(date time.Time, sessions []SessionRef) DiaryEntry {
	stats := Stats{}
	var summaries []string
	var children []ChildRef

	for _, s := range sessions {
		stats.SessionCount++
		stats.TotalTurns += s.Turns
		stats.TotalToolCalls += s.ToolCallCount
		stats.TotalErrors += s.ErrorCount
		stats.CompressionCount += s.CompressionCount
		for _, c := range s.Compressions {
			if c.Summary != "" {
				summaries = append(summaries, fmt.Sprintf("[L%d] %s", c.Level, c.Summary))
			}
		}
		if len(s.Compressions) == 0 && s.Summary != "" {
			summaries = append(summaries, s.Summary)
		}
		children = append(children, ChildRef{
			Date:  s.StartedAt.Format("15:04"),
			Level: LevelDay,
			Path:  s.SessionID,
		})
	}

	summary := fmt.Sprintf("%s: %d sessions, %d turns, %d tool calls",
		date.Format("2006-01-02"), stats.SessionCount, stats.TotalTurns, stats.TotalToolCalls)
	if len(summaries) > 0 {
		summary += "\n" + strings.Join(summaries, "\n")
	}

	return DiaryEntry{
		Level:    LevelDay,
		Date:     date.Format("2006-01-02"),
		Summary:  summary,
		Stats:    stats,
		Children: children,
	}
}

// cascadeWeek rebuilds a week entry from its day entries.
func (d *Diary) cascadeWeek(weekKey string) {
	var year int
	var week int
	_, _ = fmt.Sscanf(weekKey, "%d-W%02d", &year, &week)

	// Find all day dirs belonging to this week
	monthDir := filepath.Join(d.dir, fmt.Sprintf("%04d", year))
	entries, err := os.ReadDir(monthDir)
	if err != nil {
		return
	}

	var dayEntries []DiaryEntry
	for _, monthEntry := range entries {
		if !monthEntry.IsDir() || len(monthEntry.Name()) != 2 {
			continue
		}
		dayParent := filepath.Join(monthDir, monthEntry.Name())
		dayDirs, err := os.ReadDir(dayParent)
		if err != nil {
			continue
		}
		for _, dayDir := range dayDirs {
			if !dayDir.IsDir() || len(dayDir.Name()) != 2 {
				continue
			}
			dayPath := filepath.Join(dayParent, dayDir.Name(), "day.json")
			entry, err := d.readEntry(dayPath)
			if err != nil || entry == nil {
				continue
			}
			entryDate, err := time.Parse("2006-01-02", entry.Date)
			if err != nil {
				continue
			}
			y, w := entryDate.ISOWeek()
			if y == year && w == week {
				dayEntries = append(dayEntries, *entry)
			}
		}
	}

	if len(dayEntries) == 0 {
		return
	}

	sort.Slice(dayEntries, func(i, j int) bool {
		return dayEntries[i].Date < dayEntries[j].Date
	})

	// Aggregate
	stats := Stats{}
	var summaries []string
	var children []ChildRef
	for _, de := range dayEntries {
		stats.SessionCount += de.Stats.SessionCount
		stats.TotalTurns += de.Stats.TotalTurns
		stats.TotalToolCalls += de.Stats.TotalToolCalls
		stats.TotalErrors += de.Stats.TotalErrors
		stats.CompressionCount += de.Stats.CompressionCount
		if de.Summary != "" {
			summaries = append(summaries, de.Summary)
		}
		children = append(children, ChildRef{
			Date:  de.Date,
			Level: LevelDay,
			Path:  filepath.Join(de.Date[:4], de.Date[5:7], de.Date[8:10]),
		})
	}

	summary := fmt.Sprintf("Week %s: %d days, %d sessions, %d turns",
		weekKey, len(dayEntries), stats.SessionCount, stats.TotalTurns)
	if len(summaries) > 0 {
		summary += "\n" + strings.Join(summaries, "\n")
	}

	entry := DiaryEntry{
		Level:    LevelWeek,
		Date:     weekKey,
		Summary:  summary,
		Stats:    stats,
		Children: children,
	}

	// Write week file in the month that contains the most days of this week
	// Use the month of the first day entry
	mo := dayEntries[0].Date[5:7]
	weekDir := filepath.Join(d.dir, fmt.Sprintf("%04d", year), mo)
	os.MkdirAll(weekDir, 0700)
	weekPath := filepath.Join(weekDir, weekKey+".json")
	atomicWriteJSON(weekPath, entry)
}

// cascadeMonth rebuilds a month entry from its week entries.
func (d *Diary) cascadeMonth(moKey string) {
	yearStr := moKey[:4]
	monthStr := moKey[5:7]
	monthDir := filepath.Join(d.dir, yearStr, monthStr)

	entries, err := os.ReadDir(monthDir)
	if err != nil {
		return
	}

	var weekEntries []DiaryEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := entry.Name()
		if name == "month.json" || !strings.Contains(name, "-W") {
			continue
		}
		weekPath := filepath.Join(monthDir, name)
		e, err := d.readEntry(weekPath)
		if err != nil || e == nil {
			continue
		}
		weekEntries = append(weekEntries, *e)
	}

	if len(weekEntries) == 0 {
		return
	}

	sort.Slice(weekEntries, func(i, j int) bool {
		return weekEntries[i].Date < weekEntries[j].Date
	})

	stats := Stats{}
	var summaries []string
	var children []ChildRef
	for _, we := range weekEntries {
		stats.SessionCount += we.Stats.SessionCount
		stats.TotalTurns += we.Stats.TotalTurns
		stats.TotalToolCalls += we.Stats.TotalToolCalls
		stats.TotalErrors += we.Stats.TotalErrors
		stats.CompressionCount += we.Stats.CompressionCount
		if we.Summary != "" {
			summaries = append(summaries, we.Summary)
		}
		children = append(children, ChildRef{
			Date:  we.Date,
			Level: LevelWeek,
			Path:  filepath.Join(yearStr, monthStr, we.Date+".json"),
		})
	}

	summary := fmt.Sprintf("%s: %d weeks, %d sessions, %d turns",
		moKey, len(weekEntries), stats.SessionCount, stats.TotalTurns)
	if len(summaries) > 0 {
		summary += "\n" + strings.Join(summaries, "\n")
	}

	entry := DiaryEntry{
		Level:    LevelMonth,
		Date:     moKey,
		Summary:  summary,
		Stats:    stats,
		Children: children,
	}

	monthPath := filepath.Join(monthDir, "month.json")
	atomicWriteJSON(monthPath, entry)
}

// cascadeYear rebuilds a year entry from its month entries.
func (d *Diary) cascadeYear(yrKey string) {
	yearDir := filepath.Join(d.dir, yrKey)
	entries, err := os.ReadDir(yearDir)
	if err != nil {
		return
	}

	var monthEntries []DiaryEntry
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 2 {
			continue
		}
		monthPath := filepath.Join(yearDir, entry.Name(), "month.json")
		e, err := d.readEntry(monthPath)
		if err != nil || e == nil {
			continue
		}
		monthEntries = append(monthEntries, *e)
	}

	if len(monthEntries) == 0 {
		return
	}

	sort.Slice(monthEntries, func(i, j int) bool {
		return monthEntries[i].Date < monthEntries[j].Date
	})

	stats := Stats{}
	var summaries []string
	var children []ChildRef
	for _, me := range monthEntries {
		stats.SessionCount += me.Stats.SessionCount
		stats.TotalTurns += me.Stats.TotalTurns
		stats.TotalToolCalls += me.Stats.TotalToolCalls
		stats.TotalErrors += me.Stats.TotalErrors
		stats.CompressionCount += me.Stats.CompressionCount
		if me.Summary != "" {
			summaries = append(summaries, me.Summary)
		}
		children = append(children, ChildRef{
			Date:  me.Date,
			Level: LevelMonth,
			Path:  filepath.Join(yrKey, me.Date[5:7]),
		})
	}

	summary := fmt.Sprintf("%s: %d months, %d sessions, %d turns",
		yrKey, len(monthEntries), stats.SessionCount, stats.TotalTurns)
	if len(summaries) > 0 {
		summary += "\n" + strings.Join(summaries, "\n")
	}

	entry := DiaryEntry{
		Level:    LevelYear,
		Date:     yrKey,
		Summary:  summary,
		Stats:    stats,
		Children: children,
	}

	yearPath := filepath.Join(yearDir, "year.json")
	atomicWriteJSON(yearPath, entry)
}

// readEntry reads a DiaryEntry from a JSON file.
func (d *Diary) readEntry(path string) (*DiaryEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entry DiaryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// listEntries lists entries at a given level.
func (d *Diary) listEntries(level Level) ([]DiaryEntry, error) {
	if level == LevelYear {
		return d.listYearEntries()
	}
	return nil, nil
}

func (d *Diary) listYearEntries() ([]DiaryEntry, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, err
	}
	var result []DiaryEntry
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 4 {
			continue
		}
		yearPath := filepath.Join(d.dir, e.Name(), "year.json")
		entry, err := d.readEntry(yearPath)
		if err != nil || entry == nil {
			continue
		}
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

func (d *Diary) listEntriesUnder(parent string, level Level) ([]DiaryEntry, error) {
	dir := filepath.Join(d.dir, parent)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []DiaryEntry
	for _, e := range entries {
		if level == LevelMonth && e.IsDir() && len(e.Name()) == 2 {
			mp := filepath.Join(dir, e.Name(), "month.json")
			entry, err := d.readEntry(mp)
			if err != nil || entry == nil {
				continue
			}
			result = append(result, *entry)
		}
		if level == LevelWeek && !e.IsDir() && strings.Contains(e.Name(), "-W") {
			entry, err := d.readEntry(filepath.Join(dir, e.Name()))
			if err != nil || entry == nil {
				continue
			}
			result = append(result, *entry)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

func (d *Diary) listDaysForWeek(weekKey string) ([]DiaryEntry, error) {
	// weekKey format: "2026-W18"
	var year, week int
	_, _ = fmt.Sscanf(weekKey, "%d-W%02d", &year, &week)

	// Find the first day of this ISO week and iterate 7 days
	// Jan 4 is always in week 1
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	weekday := jan4.Weekday()
	if weekday == 0 {
		weekday = 7
	}
	week1Start := jan4.AddDate(0, 0, -int(weekday)+1)
	weekStart := week1Start.AddDate(0, 0, (week-1)*7)

	var result []DiaryEntry
	for i := 0; i < 7; i++ {
		day := weekStart.AddDate(0, 0, i)
		dayPath := filepath.Join(d.dayDir(day), "day.json")
		entry, err := d.readEntry(dayPath)
		if err != nil || entry == nil {
			continue
		}
		result = append(result, *entry)
	}
	return result, nil
}

func (d *Diary) dayDir(date time.Time) string {
	return filepath.Join(d.dir, date.Format("2006"), date.Format("01"), date.Format("02"))
}

func (d *Diary) daySessionsPath(date time.Time) string {
	return filepath.Join(d.dayDir(date), "sessions.jsonl")
}

// lastSyncPath returns the path to the last sync timestamp file.
func (d *Diary) lastSyncPath() string {
	return filepath.Join(d.dir, "last_sync.json")
}

func (d *Diary) readLastSync() time.Time {
	data, err := os.ReadFile(d.lastSyncPath())
	if err != nil {
		return time.Time{} // zero time = sync everything
	}
	var ts struct {
		TS time.Time `json:"ts"`
	}
	if json.Unmarshal(data, &ts) != nil {
		return time.Time{}
	}
	return ts.TS
}

func (d *Diary) writeLastSync(ts time.Time) {
	os.MkdirAll(d.dir, 0700)
	data, _ := json.Marshal(struct {
		TS time.Time `json:"ts"`
	}{TS: ts})
	atomicWrite(d.lastSyncPath(), data)
}

// atomicWrite writes data to a file atomically via temp file + rename.
// Uses os.CreateTemp to prevent symlink races (CWE-367).
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// atomicWriteJSON marshals v as indented JSON and writes atomically.
func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}
