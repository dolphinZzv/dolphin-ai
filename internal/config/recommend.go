package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Recommendation holds the recommended tools for a project + career combination.
type Recommendation struct {
	Skills []string `json:"skills"`
	MCP    []string `json:"mcp"`
	Source string   `json:"source"` // "keyword" or "llm"
	Repos  []string `json:"repos"`  // repos that were checked
	Error  string   `json:"error,omitempty"`
}

// RecommendTools runs the full recommendation pipeline:
// 1. Detect project from workDir
// 2. Fetch repo manifests (skill + MCP repos)
// 3. Keyword-match against project + career profile
// Returns nil if no project detected and no career given.
func RecommendTools(ctx context.Context, workDir string, profile *CareerProfile, skillRepos, mcpRepos []string) *Recommendation {
	result := &Recommendation{
		Source: "keyword",
	}

	project := DetectProject(workDir)

	// Build search keywords from project + career
	var keywords []string
	if project != nil && !project.IsEmpty() {
		keywords = append(keywords, project.Keywords()...)
	}
	if profile != nil {
		if careerKW, ok := careerKeywords[profile.Name]; ok {
			keywords = append(keywords, careerKW...)
		}
	}

	if len(keywords) == 0 && len(skillRepos) == 0 && len(mcpRepos) == 0 {
		return nil
	}

	// Fetch repo manifests (best-effort, short timeout)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	cacheDir := filepath.Join(homeDir, UserConfigDir, "cache")
	fetcher := NewRepoFetcher(cacheDir)
	if ex, err := os.Executable(); err == nil {
		fetcher.SetLocalDir(filepath.Dir(ex))
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Fetch skill repos
	var skillManifests []*ToolManifest
	if len(skillRepos) > 0 {
		skillManifests = fetcher.FetchAll(fetchCtx, skillRepos)
		for _, repo := range skillRepos {
			result.Repos = append(result.Repos, repo)
		}
	}

	// Fetch MCP repos
	var mcpManifests []*ToolManifest
	if len(mcpRepos) > 0 {
		mcpManifests = fetcher.FetchAll(fetchCtx, mcpRepos)
		for _, repo := range mcpRepos {
			result.Repos = append(result.Repos, repo)
		}
	}

	// Match keywords against manifests
	if len(keywords) > 0 {
		if skillMatches := fetcher.SearchTools(skillManifests, keywords); len(skillMatches) > 0 {
			for _, m := range skillMatches {
				result.Skills = append(result.Skills, m.Name)
			}
		}
		if mcpMatches := fetcher.SearchTools(mcpManifests, keywords); len(mcpMatches) > 0 {
			for _, m := range mcpMatches {
				result.MCP = append(result.MCP, m.Name)
			}
		}
	}

	// Augment with career profile built-in tools
	if profile != nil {
		result.Skills = append(result.Skills, profile.Skills...)
		result.MCP = append(result.MCP, profile.MCP...)
	}

	result.Skills = dedupe(result.Skills)
	result.MCP = dedupe(result.MCP)

	return result
}

// AsyncResult carries the result of an async recommendation run.
type AsyncResult struct {
	Recommendation *Recommendation
	Error          error
}

// RunAsyncRecommendation runs RecommendTools in a goroutine and returns a channel
// that receives the result. Does not block the caller.
func RunAsyncRecommendation(ctx context.Context, workDir string, profile *CareerProfile, skillRepos, mcpRepos []string) <-chan *AsyncResult {
	ch := make(chan *AsyncResult, 1)
	go func() {
		defer close(ch)
		result := &AsyncResult{}
		// Use a background context so the async work isn't tied to the caller's ctx
		bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		rec := RecommendTools(bgCtx, workDir, profile, skillRepos, mcpRepos)
		if rec != nil {
			result.Recommendation = rec
		}
		ch <- result
	}()
	return ch
}

// PrintRecommendation formats a recommendation for display to the user.
func PrintRecommendation(rec *Recommendation) string {
	if rec == nil || (len(rec.Skills) == 0 && len(rec.MCP) == 0) {
		return ""
	}
	s := "\n=== Tool recommendations ===\n"
	if len(rec.Skills) > 0 {
		s += fmt.Sprintf("Skills: %v\n", rec.Skills)
	}
	if len(rec.MCP) > 0 {
		s += fmt.Sprintf("MCP:    %v\n", rec.MCP)
	}
	s += fmt.Sprintf("\nLoad to: [p] project  [a] global  [n] skip\n")
	return s
}

func dedupe(slice []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range slice {
		if s == "" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
