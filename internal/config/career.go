package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CareerProfile maps a career name to recommended skills and MCP tools.
type CareerProfile struct {
	Name        string   `json:"name"`
	Skills      []string `json:"skills"`
	MCP         []string `json:"mcp"`
	Description string   `json:"description"`
}

// CareerTools is the built-in career-to-tool mapping.
// Each entry maps a career choice to recommended tools from the skill/MCP repos.
var CareerTools = []CareerProfile{
	{
		Name:        "frontend",
		Skills:      []string{"frontend-expert", "react-best-practices", "vue-best-practices"},
		MCP:         []string{"browser-preview", "figma-to-code"},
		Description: "前端工程师 — React/Vue/TypeScript/CSS",
	},
	{
		Name:        "backend",
		Skills:      []string{"go-expert", "api-design", "database-design"},
		MCP:         []string{"postgres-tools", "redis-tools"},
		Description: "后端工程师 — Go/API/数据库/微服务",
	},
	{
		Name:        "fullstack",
		Skills:      []string{"frontend-expert", "go-expert", "api-design"},
		MCP:         []string{"browser-preview", "postgres-tools"},
		Description: "全栈工程师 — 前后端 + 数据库",
	},
	{
		Name:        "devops",
		Skills:      []string{"docker-expert", "kubernetes-expert", "ci-cd-expert"},
		MCP:         []string{"docker-tools", "k8s-tools"},
		Description: "DevOps/SRE — Docker/K8s/CI-CD/监控",
	},
	{
		Name:        "data",
		Skills:      []string{"python-expert", "data-analysis", "sql-expert"},
		MCP:         []string{"postgres-tools", "jupyter-tools"},
		Description: "数据工程师/科学家 — Python/SQL/数据分析",
	},
	{
		Name:        "mobile",
		Skills:      []string{"swift-expert", "kotlin-expert", "react-native-expert"},
		MCP:         []string{"simulator-tools", "device-tools"},
		Description: "移动端工程师 — iOS/Android/React Native",
	},
	{
		Name:        "security",
		Skills:      []string{"security-audit", "penetration-testing", "code-review"},
		MCP:         []string{"security-scanner", "dependency-checker"},
		Description: "安全工程师 — 安全审计/渗透测试/代码审查",
	},
	{
		Name:        "general",
		Skills:      []string{"code-review", "documentation-expert", "testing-expert"},
		MCP:         []string{},
		Description: "通用开发 — 代码审查/文档/测试",
	},
}

// FirstRunMarker returns the path to the first-run marker file.
func FirstRunMarker() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(hd, UserConfigDir, "first-run")
}

// IsFirstRun checks whether this is the application's first run.
// Returns true when the marker file does NOT exist.
func IsFirstRun() bool {
	_, err := os.Stat(FirstRunMarker())
	return os.IsNotExist(err)
}

// MarkFirstRunDone removes the first-run marker.
func MarkFirstRunDone() error {
	return os.Remove(FirstRunMarker())
}

// CreateFirstRunMarker creates the first-run marker file.
func CreateFirstRunMarker() error {
	path := FirstRunMarker()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0644)
}

// RunFirstRunPrompt displays the career selection prompt and returns the chosen profile.
// This is used during the first-run experience to guide tool loading.
func RunFirstRunPrompt() (*CareerProfile, error) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "============================================")
	fmt.Fprintln(os.Stderr, "  Welcome to DolphinzZ — AI Coding Agent")
	fmt.Fprintln(os.Stderr, "============================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "To recommend the right tools for you, please select your role:")

	for i, p := range CareerTools {
		fmt.Fprintf(os.Stderr, "  [%d] %s\n", i+1, p.Description)
	}
	fmt.Fprintf(os.Stderr, "  [s] Skip — I'll configure tools later\n")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Choice: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" || strings.EqualFold(input, "s") || strings.EqualFold(input, "skip") {
		return nil, nil
	}

	// Try numeric selection
	idx := 0
	if _, err := fmt.Sscanf(input, "%d", &idx); err == nil && idx >= 1 && idx <= len(CareerTools) {
		return &CareerTools[idx-1], nil
	}

	// Try keyword match
	lower := strings.ToLower(input)
	for _, p := range CareerTools {
		if strings.Contains(lower, p.Name) {
			return &p, nil
		}
	}

	fmt.Fprintf(os.Stderr, "\nNo matching role found. Skipping tool recommendation.\n")
	fmt.Fprintf(os.Stderr, "You can add skills and MCP tools later in ~/.dolphinzZ/config.yaml\n\n")
	return nil, nil
}
