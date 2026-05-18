package diary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// pruneAll runs all four prune levels.
func (d *Diary) pruneAll() {
	if err := d.pruneDays(); err != nil {
		zap.S().Warnw("diary: day prune failed", "error", err)
	}
	if err := d.pruneWeeks(); err != nil {
		zap.S().Warnw("diary: week prune failed", "error", err)
	}
	if err := d.pruneMonths(); err != nil {
		zap.S().Warnw("diary: month prune failed", "error", err)
	}
	if err := d.pruneYears(); err != nil {
		zap.S().Warnw("diary: year prune failed", "error", err)
	}
	if err := d.pruneTotalSize(); err != nil {
		zap.S().Warnw("diary: total size prune failed", "error", err)
	}
}

// pruneDays limits sessions per day to MaxDaySessions.
func (d *Diary) pruneDays() error {
	limit := d.cfg.MaxDaySessions
	if limit <= 0 {
		return nil
	}

	monthPaths, err := d.yearMonthDirs()
	if err != nil {
		return err
	}
	for _, mp := range monthPaths {
		dayDirs, err := os.ReadDir(mp)
		if err != nil {
			continue
		}
		for _, dd := range dayDirs {
			if !dd.IsDir() || len(dd.Name()) != 2 {
				continue
			}
			dayDir := filepath.Join(mp, dd.Name())
			sessionsPath := filepath.Join(dayDir, "sessions.jsonl")
			refs := d.loadDaySessionsFrom(sessionsPath)
			if len(refs) <= limit {
				continue
			}
			// Remove oldest, keep newest
			pruned := refs[len(refs)-limit:]
			if err := d.writeDaySessions(sessionsPath, pruned); err != nil {
				zap.S().Warnw("diary: prune day sessions failed", "path", sessionsPath, "error", err)
				continue
			}
			date, _ := parseDateFromPath(dayDir)
			entry := d.buildDayEntry(date, pruned)
			dayPath := filepath.Join(dayDir, "day.json")
			atomicWriteJSON(dayPath, entry)
		}
	}
	return nil
}

// pruneWeeks limits days per week to MaxWeekDays.
func (d *Diary) pruneWeeks() error {
	limit := d.cfg.MaxWeekDays
	if limit <= 0 {
		return nil
	}

	monthPaths, err := d.yearMonthDirs()
	if err != nil {
		return err
	}
	for _, mp := range monthPaths {
		entries, err := os.ReadDir(mp)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.Contains(e.Name(), "-W") || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			weekPath := filepath.Join(mp, e.Name())
			entry, err := d.readEntry(weekPath)
			if err != nil || entry == nil {
				continue
			}
			if len(entry.Children) <= limit {
				continue
			}
			// Remove oldest day dirs, keep newest
			pruned := entry.Children[len(entry.Children)-limit:]
			for _, ch := range entry.Children[:len(entry.Children)-limit] {
				_ = os.RemoveAll(filepath.Join(d.dir, ch.Path))
			}
			entry.Children = pruned
			atomicWriteJSON(weekPath, entry)
		}
	}
	return nil
}

// pruneMonths limits weeks per month to MaxMonthWeeks.
func (d *Diary) pruneMonths() error {
	limit := d.cfg.MaxMonthWeeks
	if limit <= 0 {
		return nil
	}

	monthPaths, err := d.yearMonthDirs()
	if err != nil {
		return err
	}
	for _, mp := range monthPaths {
		monthJSON := filepath.Join(mp, "month.json")
		entry, err := d.readEntry(monthJSON)
		if err != nil || entry == nil {
			continue
		}
		if len(entry.Children) <= limit {
			continue
		}
		pruned := entry.Children[len(entry.Children)-limit:]
		for _, ch := range entry.Children[:len(entry.Children)-limit] {
			_ = os.Remove(filepath.Join(d.dir, ch.Path))
		}
		entry.Children = pruned
		atomicWriteJSON(monthJSON, entry)
	}
	return nil
}

// pruneYears limits months per year to MaxYearMonths.
func (d *Diary) pruneYears() error {
	limit := d.cfg.MaxYearMonths
	if limit <= 0 {
		return nil
	}

	yearDirs, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	for _, yd := range yearDirs {
		if !yd.IsDir() || len(yd.Name()) != 4 {
			continue
		}
		yearPath := filepath.Join(d.dir, yd.Name(), "year.json")
		entry, err := d.readEntry(yearPath)
		if err != nil || entry == nil {
			continue
		}
		if len(entry.Children) <= limit {
			continue
		}
		pruned := entry.Children[len(entry.Children)-limit:]
		for _, ch := range entry.Children[:len(entry.Children)-limit] {
			_ = os.RemoveAll(filepath.Join(d.dir, ch.Path))
		}
		entry.Children = pruned
		atomicWriteJSON(yearPath, entry)
	}
	return nil
}

// pruneTotalSize removes oldest year directories if diary exceeds max size.
func (d *Diary) pruneTotalSize() error {
	limitMB := d.cfg.MaxTotalMB
	if limitMB <= 0 {
		return nil
	}

	size, err := dirSize(d.dir)
	if err != nil {
		return err
	}
	limitBytes := int64(limitMB) * 1024 * 1024
	if size <= limitBytes {
		return nil
	}

	// Collect year directories sorted by name (oldest first)
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	var yearDirs []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == 4 {
			yearDirs = append(yearDirs, e.Name())
		}
	}
	sort.Strings(yearDirs)

	for _, yd := range yearDirs {
		if size <= limitBytes {
			break
		}
		yearDir := filepath.Join(d.dir, yd)
		ys, _ := dirSize(yearDir)
		os.RemoveAll(yearDir)
		size -= ys
		zap.S().Infow("diary: pruned year for total size limit", "year", yd, "freed_mb", ys/(1024*1024))
	}
	return nil
}

// yearMonthDirs returns all year/month subdirectory paths under d.dir.
func (d *Diary) yearMonthDirs() ([]string, error) {
	yearDirs, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, yd := range yearDirs {
		if !yd.IsDir() || len(yd.Name()) != 4 {
			continue
		}
		monthDirs, err := os.ReadDir(filepath.Join(d.dir, yd.Name()))
		if err != nil {
			continue
		}
		for _, md := range monthDirs {
			if !md.IsDir() || len(md.Name()) != 2 {
				continue
			}
			out = append(out, filepath.Join(d.dir, yd.Name(), md.Name()))
		}
	}
	return out, nil
}

// loadDaySessionsFrom reads sessions from a path.
func (d *Diary) loadDaySessionsFrom(path string) []SessionRef {
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

// writeDaySessions writes sessions to a path (used after pruning).
func (d *Diary) writeDaySessions(path string, refs []SessionRef) error {
	var sb strings.Builder
	for _, s := range refs {
		b, _ := json.Marshal(s)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return atomicWrite(path, []byte(sb.String()))
}

// dirSize calculates total size of a directory recursively.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// parseDateFromPath extracts a time.Time from a directory path like .../2026/05/10.
func parseDateFromPath(path string) (time.Time, error) {
	// Path ends with YYYY/MM/DD
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return time.Time{}, fmt.Errorf("invalid date path: %s", path)
	}
	dateStr := parts[len(parts)-3] + "-" + parts[len(parts)-2] + "-" + parts[len(parts)-1]
	return time.Parse("2006-01-02", dateStr)
}
