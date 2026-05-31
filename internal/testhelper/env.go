package testhelper

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var loadOnce sync.Once

// LoadEnv reads .env from the project root by walking up from CWD.
// It only sets env vars that are not already set.
func LoadEnv() {
	loadOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		for range 5 {
			path := filepath.Join(dir, ".env")
			data, err := os.ReadFile(path)
			if err == nil {
				for line := range strings.SplitSeq(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					pair := strings.SplitN(line, "=", 2)
					if len(pair) != 2 {
						continue
					}
					key := strings.TrimSpace(pair[0])
					val := strings.TrimSpace(pair[1])
					val = strings.Trim(val, `"'`)
					if os.Getenv(key) == "" {
						os.Setenv(key, val)
					}
				}
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	})
}

// APIKey returns the Anthropic API key from .env.
func APIKey() string {
	LoadEnv()
	return os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY")
}
