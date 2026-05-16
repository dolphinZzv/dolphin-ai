package config

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"dolphin/internal/i18n"
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
		Name:        "product",
		Skills:      []string{"prd-writing", "user-story-mapping", "competitive-analysis"},
		MCP:         []string{"browser-preview", "figma-to-code"},
		Description: "产品经理 — PRD/用户故事/竞品分析/原型",
	},
	{
		Name:        "design",
		Skills:      []string{"ui-ux-review", "design-system", "accessibility-check"},
		MCP:         []string{"figma-to-code", "browser-preview"},
		Description: "设计师 — UI/UX 审查/设计系统/可访问性",
	},
	{
		Name:        "writer",
		Skills:      []string{"documentation-expert", "api-docs", "technical-blog"},
		MCP:         []string{},
		Description: "技术写作者 — 文档/API 文档/技术博客",
	},
	{
		Name:        "operations",
		Skills:      []string{"data-report", "automation", "content-workflow"},
		MCP:         []string{"browser-preview"},
		Description: "运营 — 数据报表/自动化/内容工作流",
	},
	{
		Name:        "student",
		Skills:      []string{"code-learning", "note-taking", "concept-explanation"},
		MCP:         []string{},
		Description: "学生/学习者 — 代码学习/笔记/概念讲解",
	},
	{
		Name:        "researcher",
		Skills:      []string{"literature-review", "data-analysis", "writing-assistant"},
		MCP:         []string{"browser-preview"},
		Description: "研究员 — 文献综述/数据分析/写作辅助",
	},
	{
		Name:        "general",
		Skills:      []string{"task-automation", "file-management", "web-research"},
		MCP:         []string{"browser-preview"},
		Description: "通用助手 — 任务自动化/文件管理/网络搜索",
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
	return os.WriteFile(path, []byte{}, 0600)
}

// EmailConfiguredMarker returns the path to the email-configured marker file.
func EmailConfiguredMarker() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(hd, UserConfigDir, "email-configured")
}

// IsEmailConfigured checks whether the email transport has already been
// configured and sent its initial startup notification.
func IsEmailConfigured() bool {
	_, err := os.Stat(EmailConfiguredMarker())
	return err == nil
}

// MarkEmailConfigured creates the email-configured marker so future
// startups skip the welcome email.
func MarkEmailConfigured() error {
	path := EmailConfiguredMarker()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0600)
}

// careerKeywords maps career names to search keywords for matching repo tools.
var careerKeywords = map[string][]string{
	"frontend":   {"frontend", "react", "vue", "angular", "typescript", "css", "ui", "browser"},
	"backend":    {"go", "api", "database", "microservice", "rest", "grpc", "backend", "server"},
	"fullstack":  {"fullstack", "frontend", "backend", "react", "go", "api", "database"},
	"devops":     {"docker", "kubernetes", "k8s", "ci", "cd", "deploy", "monitor", "infra"},
	"data":       {"python", "sql", "data", "jupyter", "pandas", "ml", "analytics"},
	"mobile":     {"ios", "android", "swift", "kotlin", "react-native", "flutter", "mobile"},
	"security":   {"security", "audit", "penetration", "vulnerability", "scan", "dependency"},
	"product":    {"prd", "product", "spec", "roadmap", "user-story", "requirement"},
	"design":     {"design", "figma", "ui", "ux", "accessibility", "prototype", "visual"},
	"writer":     {"documentation", "writing", "blog", "markdown", "api-docs", "guide"},
	"operations": {"report", "automation", "workflow", "content", "spreadsheet", "data"},
	"student":    {"learning", "tutorial", "beginner", "concept", "study", "note"},
	"researcher": {"research", "literature", "paper", "analysis", "survey", "academic"},
	"general":    {"automation", "file", "web", "search", "organize", "task"},
}

// AugmentWithRepos fetches tool manifests from configured repos and finds tools
// matching the career profile keywords. Returns matched skill and MCP ToolEntry values.
// This is best-effort: network errors are silent, and results supplement the
// hardcoded CareerTools mapping.
func AugmentWithRepos(profile *CareerProfile, skillRepos, mcpRepos []string) (extraSkills, extraMCP []ToolEntry) {
	if profile == nil {
		return nil, nil
	}

	// Handle merged profiles (multi-select): split on "+" and merge keywords
	var keywords []string
	for _, name := range strings.Split(profile.Name, "+") {
		if kw, ok := careerKeywords[strings.TrimSpace(name)]; ok {
			keywords = append(keywords, kw...)
		}
	}
	// Also search by the profile's built-in tool names directly
	keywords = append(keywords, profile.Skills...)
	keywords = append(keywords, profile.MCP...)
	if len(keywords) == 0 {
		keywords = careerKeywords["general"]
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	cacheDir := filepath.Join(homeDir, UserConfigDir, "cache")
	fetcher := NewRepoFetcher(cacheDir)
	fetcher.SetTTL(1 * time.Hour)
	if ex, err := os.Executable(); err == nil {
		fetcher.SetLocalDir(filepath.Dir(ex))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if len(skillRepos) > 0 {
		manifests := fetcher.FetchAll(ctx, skillRepos)
		extraSkills = fetcher.SearchTools(manifests, keywords)
	}

	if len(mcpRepos) > 0 {
		manifests := fetcher.FetchAll(ctx, mcpRepos)
		extraMCP = fetcher.SearchTools(manifests, keywords)
	}

	return extraSkills, extraMCP
}

// RunFirstRunPrompt shows a domain selection prompt so we can recommend relevant
// tools. Supports multi-select (e.g. "1,3,5"). No data is sent anywhere.
func RunFirstRunPrompt() (*CareerProfile, error) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "============================================")
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.TL(i18n.KeyWelcome))
	fmt.Fprintln(os.Stderr, "============================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySelectDomain))
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyPrivacyNote))

	for i, p := range CareerTools {
		fmt.Fprintf(os.Stderr, "  [%d] %s\n", i+1, p.Description)
	}
	fmt.Fprintf(os.Stderr, "  [s] %s\n", i18n.TL(i18n.KeySkip))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s: ", i18n.TL(i18n.KeyChoice))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" || strings.EqualFold(input, "s") || strings.EqualFold(input, "skip") {
		return nil, nil
	}

	// Parse comma/space-separated numbers
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	})

	var selected []CareerProfile
	seen := make(map[int]bool)
	for _, part := range parts {
		idx := 0
		if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(CareerTools) {
			continue
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		selected = append(selected, CareerTools[idx-1])
	}

	// Try keyword match for any non-numeric input
	if len(selected) == 0 {
		lower := strings.ToLower(input)
		for _, p := range CareerTools {
			if strings.Contains(lower, p.Name) {
				selected = append(selected, p)
			}
		}
	}

	if len(selected) == 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n\n", i18n.TL(i18n.KeyNoMatch))
		return nil, nil
	}

	// Merge all selected profiles
	return mergeProfiles(selected), nil
}

// GenerateSystemMD creates SYSTEM.md with system environment info.
// Content adapts to the user's detected language.
func shellName() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		for _, s := range []string{"pwsh.exe", "powershell.exe", "cmd.exe", "bash.exe"} {
			if _, err := os.Stat(filepath.Join(os.Getenv("SystemRoot"), "System32", s)); err == nil {
				return s
			}
		}
	}
	return "unknown"
}

func GenerateSystemMD(lang i18n.Lang) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	path := filepath.Join(home, UserConfigDir, "SYSTEM.md")

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(i18n.T(i18n.KeySystemMDContent, lang))
	sb.WriteString("\n\n")

	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()

	switch lang {
	case i18n.ZH:
		sb.WriteString(fmt.Sprintf("- 操作系统: %s/%s\n", runtime.GOOS, runtime.GOARCH))
		sb.WriteString(fmt.Sprintf("- 主机名: %s\n", hostname))
		sb.WriteString(fmt.Sprintf("- Shell: %s\n", shellName()))
		sb.WriteString(fmt.Sprintf("- 用户目录: %s\n", home))
		sb.WriteString(fmt.Sprintf("- 工作目录: %s\n", cwd))
		sb.WriteString(fmt.Sprintf("- CPU 核心数: %d\n", runtime.NumCPU()))
	default:
		sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
		sb.WriteString(fmt.Sprintf("- Hostname: %s\n", hostname))
		sb.WriteString(fmt.Sprintf("- Shell: %s\n", shellName()))
		sb.WriteString(fmt.Sprintf("- Home: %s\n", home))
		sb.WriteString(fmt.Sprintf("- Working Dir: %s\n", cwd))
		sb.WriteString(fmt.Sprintf("- CPUs: %d\n", runtime.NumCPU()))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(sb.String()), 0600)
}

// PromptSystemMD asks the user whether to auto-generate SYSTEM.md with system info.
// Returns true if generated.
func PromptSystemMD() (bool, error) {
	lang := i18n.DetectLang()
	fmt.Fprintf(os.Stderr, "\n%s\n", i18n.T(i18n.KeySystemMDPrompt, lang))
	fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeySystemMDExplain, lang))
	fmt.Fprintf(os.Stderr, "  [y] %s  [n] %s\n", i18n.T(i18n.KeySystemMDYes, lang), i18n.T(i18n.KeySystemMDNo, lang))
	fmt.Fprintf(os.Stderr, "%s: ", i18n.T(i18n.KeyChoice, lang))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, nil
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "y" && input != "yes" {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeySystemMDSkipped, lang))
		return false, nil
	}

	path, err := GenerateSystemMD(lang)
	if err != nil {
		return false, fmt.Errorf("generate SYSTEM.md: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.T(i18n.KeySystemMDGenerated, lang), path)
	return true, nil
}

// mergeProfiles combines multiple career profiles into one, deduplicating skills and MCP.
func mergeProfiles(profiles []CareerProfile) *CareerProfile {
	if len(profiles) == 0 {
		return nil
	}
	if len(profiles) == 1 {
		return &profiles[0]
	}

	skillSet := make(map[string]bool)
	mcpSet := make(map[string]bool)
	var names, descs []string

	for _, p := range profiles {
		names = append(names, p.Name)
		descs = append(descs, p.Description)
		for _, s := range p.Skills {
			skillSet[s] = true
		}
		for _, m := range p.MCP {
			mcpSet[m] = true
		}
	}

	merged := &CareerProfile{
		Name:        strings.Join(names, "+"),
		Description: strings.Join(descs, "; "),
	}
	for s := range skillSet {
		merged.Skills = append(merged.Skills, s)
	}
	for m := range mcpSet {
		merged.MCP = append(merged.MCP, m)
	}
	return merged
}
