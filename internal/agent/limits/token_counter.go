package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dolphin/internal/config"
)

type TokenCounter struct {
	config *config.LimitsConfig

	RequestsDaily   int
	RequestsWeekly  int
	RequestsMonthly int

	InputTokensDaily    int
	OutputTokensDaily   int
	InputTokensWeekly   int
	OutputTokensWeekly  int
	InputTokensMonthly  int
	OutputTokensMonthly int

	LastResetDaily   int64
	LastResetWeekly  int64
	LastResetMonthly int64

	mu sync.RWMutex
}

func NewTokenCounter(cfg *config.LimitsConfig) *TokenCounter {
	tc := &TokenCounter{config: cfg}
	tc.load()
	tc.checkAndResetIfNeeded()
	return tc
}

func (tc *TokenCounter) load() {
	path := tc.persistPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var state counterState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	tc.RequestsDaily = state.RequestsDaily
	tc.RequestsWeekly = state.RequestsWeekly
	tc.RequestsMonthly = state.RequestsMonthly
	tc.InputTokensDaily = state.InputTokensDaily
	tc.OutputTokensDaily = state.OutputTokensDaily
	tc.InputTokensWeekly = state.InputTokensWeekly
	tc.OutputTokensWeekly = state.OutputTokensWeekly
	tc.InputTokensMonthly = state.InputTokensMonthly
	tc.OutputTokensMonthly = state.OutputTokensMonthly
	tc.LastResetDaily = state.LastResetDaily
	tc.LastResetWeekly = state.LastResetWeekly
	tc.LastResetMonthly = state.LastResetMonthly
}

func (tc *TokenCounter) persistPath() string {
	return filepath.Join(config.SessionsDir(), "limits_counter.json")
}

func (tc *TokenCounter) Persist() {
	path := tc.persistPath()

	tmpPath := path + ".tmp"
	state := counterState{
		RequestsDaily:       tc.RequestsDaily,
		RequestsWeekly:      tc.RequestsWeekly,
		RequestsMonthly:     tc.RequestsMonthly,
		InputTokensDaily:    tc.InputTokensDaily,
		OutputTokensDaily:   tc.OutputTokensDaily,
		InputTokensWeekly:   tc.InputTokensWeekly,
		OutputTokensWeekly:  tc.OutputTokensWeekly,
		InputTokensMonthly:  tc.InputTokensMonthly,
		OutputTokensMonthly: tc.OutputTokensMonthly,
		LastResetDaily:      tc.LastResetDaily,
		LastResetWeekly:     tc.LastResetWeekly,
		LastResetMonthly:    tc.LastResetMonthly,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}

	os.Rename(tmpPath, path)
}

func (tc *TokenCounter) checkAndResetIfNeeded() {
	now := time.Now().Unix()

	if tc.LastResetDaily == 0 {
		tc.LastResetDaily = now
	} else if now-tc.LastResetDaily >= 86400 {
		tc.RequestsDaily = 0
		tc.InputTokensDaily = 0
		tc.OutputTokensDaily = 0
		tc.LastResetDaily = now
	}

	if tc.LastResetWeekly == 0 {
		tc.LastResetWeekly = now
	} else if now-tc.LastResetWeekly >= 604800 {
		tc.RequestsWeekly = 0
		tc.InputTokensWeekly = 0
		tc.OutputTokensWeekly = 0
		tc.LastResetWeekly = now
	}

	if tc.LastResetMonthly == 0 {
		tc.LastResetMonthly = now
	} else if now-tc.LastResetMonthly >= 2592000 {
		tc.RequestsMonthly = 0
		tc.InputTokensMonthly = 0
		tc.OutputTokensMonthly = 0
		tc.LastResetMonthly = now
	}
}

func (tc *TokenCounter) ResetLevel(level string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	switch level {
	case "daily":
		tc.RequestsDaily = 0
		tc.InputTokensDaily = 0
		tc.OutputTokensDaily = 0
		tc.LastResetDaily = time.Now().Unix()
	case "weekly":
		tc.RequestsWeekly = 0
		tc.InputTokensWeekly = 0
		tc.OutputTokensWeekly = 0
		tc.LastResetWeekly = time.Now().Unix()
	case "monthly":
		tc.RequestsMonthly = 0
		tc.InputTokensMonthly = 0
		tc.OutputTokensMonthly = 0
		tc.LastResetMonthly = time.Now().Unix()
	}

	tc.Persist()
}

type counterState struct {
	RequestsDaily       int
	RequestsWeekly      int
	RequestsMonthly     int
	InputTokensDaily    int
	OutputTokensDaily   int
	InputTokensWeekly   int
	OutputTokensWeekly  int
	InputTokensMonthly  int
	OutputTokensMonthly int
	LastResetDaily      int64
	LastResetWeekly     int64
	LastResetMonthly    int64
}
