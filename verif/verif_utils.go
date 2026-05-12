package verif

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var projectRoot string

func init() {
	_, filename, _, _ := runtime.Caller(0)
	projectRoot = filepath.Dir(filepath.Dir(filename))
}

// runGo runs a Go command in the project root and returns combined output.
func runGo(args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
