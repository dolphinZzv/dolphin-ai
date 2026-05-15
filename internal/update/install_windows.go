//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// InstallBinary writes the new binary as <exec>.next alongside the current
// executable. On Windows, running .exe files are locked by the OS and cannot
// be replaced in-place. The swap is performed by a batch script at startup
// (see main.go) or on the next manual restart.
func InstallBinary(data []byte, execPath string) error {
	stagingPath := execPath + ".next"
	if err := os.WriteFile(stagingPath, data, 0755); err != nil {
		return fmt.Errorf("write staged binary: %w", err)
	}
	return nil
}

// ApplyStagedUpdate checks for and applies a staged update file (<exec>.next).
// If found, it spawns a detached batch script that waits for the calling process
// to exit, then moves the new binary into place. The current process should
// call os.Exit(0) immediately after this returns true.
//
// Returns true if a staged update was found and is being applied.
func ApplyStagedUpdate(execPath string) bool {
	stagingPath := execPath + ".next"
	if _, err := os.Stat(stagingPath); os.IsNotExist(err) {
		return false
	}

	scriptPath := execPath + ".update.bat"
	script := fmt.Sprintf(
		"@echo off\r\n"+
			":loop\r\n"+
			"timeout /t 1 >nul\r\n"+
			"move /y \"%s\" \"%s\" 2>nul\r\n"+
			"if exist \"%s\" goto loop\r\n"+
			"start \"\" \"%s\"\r\n"+
			"del \"%%~f0\"\r\n",
		stagingPath, execPath, stagingPath, execPath,
	)

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return false
	}

	cmd := exec.Command("cmd", "/c", "start", "", "/B", scriptPath)
	cmd.Dir = filepath.Dir(execPath)
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return false
	}

	return true
}
