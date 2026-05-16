package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// resolveConfigDir returns the config base directory, preferring the project-local
// .dolphin/ when it exists, falling back to ~/.dolphin/.
func resolveConfigDir() (string, error) {
	if _, err := os.Stat(ProjectConfigDir); err == nil {
		return ProjectConfigDir, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(homeDir, UserConfigDir), nil
}

// ApplyTools installs matched skills and MCP servers from repo manifests.
// Skills are downloaded as .md files to <config>/skills/.
// MCP servers are merged into <config>/config.yaml mcp.servers without duplicates.
// Config dir priority: .dolphin/ (project-local) > ~/.dolphin/ (user-wide).
func ApplyTools(skills, mcpServers []ToolEntry) error {
	baseDir, err := resolveConfigDir()
	if err != nil {
		return err
	}
	skillsDir := filepath.Join(baseDir, "skills")

	// Apply skills
	if len(skills) > 0 {
		if err := applySkills(skills, skillsDir); err != nil {
			return fmt.Errorf("apply skills: %w", err)
		}
	}

	// Apply MCP servers
	if len(mcpServers) > 0 {
		if err := applyMCPServers(mcpServers, baseDir); err != nil {
			return fmt.Errorf("apply mcp servers: %w", err)
		}
	}

	return nil
}

// applySkills downloads skill markdown files to the skills directory.
// Each skill is stored as <skillsDir>/<name>/SKILL.md.
// If a download URL is available and reachable (2s timeout), the content is fetched;
// otherwise a minimal template is created locally.
func applySkills(skills []ToolEntry, skillsDir string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	for _, s := range skills {
		skillDir := filepath.Join(skillsDir, s.Name)
		dst := filepath.Join(skillDir, "SKILL.md")

		// Skip if already installed
		if _, err := os.Stat(dst); err == nil {
			continue
		}

		content := skillTemplate(s)
		if s.URL != "" {
			if data, err := downloadSkill(client, s.URL); err == nil {
				content = string(data)
			}
		}

		if err := os.MkdirAll(skillDir, 0700); err != nil {
			return fmt.Errorf("mkdir %s: %w", skillDir, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0600); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}

// applyMCPServers merges new MCP servers into config.yaml without duplicates.
func applyMCPServers(servers []ToolEntry, baseDir string) error {
	configPath := filepath.Join(baseDir, ConfigFileName+".yaml")

	// Read existing full config as map
	full := make(map[string]any)
	if data, err := os.ReadFile(configPath); err == nil {
		yaml.Unmarshal(data, &full)
	}

	// Read existing mcp section, preserving other mcp settings
	mcpSection, _ := full["mcp"].(map[string]any)
	if mcpSection == nil {
		mcpSection = make(map[string]any)
		full["mcp"] = mcpSection
	}
	existingServers, _ := mcpSection["servers"].(map[string]any)
	if existingServers == nil {
		existingServers = make(map[string]any)
		mcpSection["servers"] = existingServers
	}

	added := 0
	for _, s := range servers {
		if s.Command == "" {
			continue
		}
		if _, exists := existingServers[s.Name]; exists {
			continue
		}
		existingServers[s.Name] = map[string]any{
			"type":    "stdio",
			"command": s.Command,
			"args":    s.Args,
		}
		added++
	}

	if added == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(full)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configPath, data, 0600)
}

// downloadSkill fetches skill content from a raw GitHub URL.
func downloadSkill(client *http.Client, url string) ([]byte, error) {
	rawURL := toRawGitHubURL(url)
	if rawURL == "" {
		return nil, fmt.Errorf("cannot convert to raw URL: %s", url)
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max
}

// toRawGitHubURL converts a GitHub blob/directory URL to a raw URL.
// Directory: "https://github.com/org/repo/blob/main/dir/" → ".../dir/skill.md"
// File:      "https://github.com/org/repo/blob/main/skill.md" → ".../skill.md"
func toRawGitHubURL(url string) string {
	url = strings.TrimRight(url, "/")
	prefix := "https://github.com/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(url, prefix)
	parts := strings.SplitN(rest, "/", 4) // org, repo, "blob", ref/path
	if len(parts) < 4 || parts[2] != "blob" {
		return ""
	}
	refPath := parts[3]
	// If path already ends with .md, use it as-is
	if strings.HasSuffix(refPath, ".md") {
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", parts[0], parts[1], refPath)
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/skill.md", parts[0], parts[1], refPath)
}

// skillTemplate builds a minimal skill markdown file from a ToolEntry.
func skillTemplate(s ToolEntry) string {
	var sb strings.Builder
	if s.Description != "" {
		sb.WriteString(fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n", s.Name, s.Description))
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", s.Name))
	sb.WriteString("Add your skill content here.\n")
	return sb.String()
}
