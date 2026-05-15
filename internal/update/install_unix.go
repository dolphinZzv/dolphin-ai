//go:build !windows

package update

import (
	"fmt"
	"os"
)

// InstallBinary performs an atomic swap of the current executable on Unix systems.
// Running processes continue using the old inode; new processes use the new binary.
func InstallBinary(data []byte, execPath string) error {
	tmpBin := execPath + ".dolphin-tmp"
	if err := os.WriteFile(tmpBin, data, 0755); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}

	backupPath := execPath + ".bak"
	os.Remove(backupPath)

	if err := os.Rename(execPath, backupPath); err != nil {
		os.Remove(tmpBin)
		return fmt.Errorf("backup current binary: %w", err)
	}

	if err := os.Rename(tmpBin, execPath); err != nil {
		os.Rename(backupPath, execPath)
		os.Remove(tmpBin)
		return fmt.Errorf("install new binary: %w", err)
	}

	os.Remove(backupPath)
	return nil
}

// ApplyStagedUpdate is a no-op on Unix—in-place binary replacement via
// os.Rename works fine while the process is running, so there are never
// staged updates to apply.
func ApplyStagedUpdate(execPath string) bool { return false }
