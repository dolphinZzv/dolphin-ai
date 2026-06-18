package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type tuiPrefs struct {
	ShowTools    bool `json:"show_tools"`
	ShowThinking bool `json:"show_thinking"`
}

func prefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dolphin", "tui.json"), nil
}

func loadPrefs() (tuiPrefs, error) {
	path, err := prefsPath()
	if err != nil {
		return tuiPrefs{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tuiPrefs{}, err
	}
	var p tuiPrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return tuiPrefs{}, err
	}
	return p, nil
}

func savePrefs(p tuiPrefs) error {
	path, err := prefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
