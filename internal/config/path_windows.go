//go:build windows

package config

import (
	"os"
	"path/filepath"
)

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
