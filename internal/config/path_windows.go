//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultSessionDir() string {
	return filepath.Join(os.TempDir(), "dolphin")
}

func defaultSystemConfigDir() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = os.Getenv("ALLUSERSPROFILE")
	}
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "dolphin")
}
