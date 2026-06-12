package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type CLI struct {
	Name string
	Path string // full path to executable
	Help string // cached --help output, empty until fetched

	mu sync.Mutex
}

// Scan lists executable files in dirs without running --help.
// Later dirs override earlier ones for same-named CLIs.
func Scan(dirs []string, logger *zap.Logger) []CLI {
	seen := make(map[string]int) // name → index in result
	var result []CLI

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			logger.Debug("cli dir not readable", zap.String("dir", dir), zap.Error(err))
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !isExecutable(e) {
				continue
			}
			name := e.Name()
			path := filepath.Join(dir, name)
			if idx, ok := seen[name]; ok {
				result[idx] = CLI{Name: name, Path: path}
			} else {
				seen[name] = len(result)
				result = append(result, CLI{Name: name, Path: path})
			}
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// FetchHelp runs --help for a CLI, caching the result. Safe for concurrent use.
func FetchHelp(c *CLI, logger *zap.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Help != "" {
		return
	}

	for _, flag := range []string{"--help", "-h", "help"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, c.Path, flag)
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			c.Help = string(out)
			return
		}
		logger.Debug("cli help attempt failed",
			zap.String("name", c.Name),
			zap.String("flag", flag),
			zap.Error(err),
		)
		// If context deadline exceeded, don't try the next flag.
		if strings.Contains(err.Error(), "deadline") {
			break
		}
	}

	logger.Warn("unable to get help for CLI", zap.String("name", c.Name))
}

func isExecutable(e os.DirEntry) bool {
	if e.Type()&os.ModeSymlink != 0 {
		return false // skip symlinks
	}
	info, err := e.Info()
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}
