package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ToolManifest is a JSON manifest listing available tools in a repo.
type ToolManifest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Tools       []ToolEntry `json:"tools"`
}

// ToolEntry is a single tool entry in a manifest.
type ToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// Conflict describes a tool that exists in multiple repos.
type Conflict struct {
	Name    string
	Sources []RepoSource
}

// RepoSource identifies which repo a conflicting tool comes from.
type RepoSource struct {
	Repo        string
	Description string
}

// RepoFetcher fetches and caches tool manifests from GitHub repos.
type RepoFetcher struct {
	cacheDir string
	ttl      time.Duration
	mu       sync.RWMutex
}

// NewRepoFetcher creates a fetcher that caches manifests to cacheDir.
func NewRepoFetcher(cacheDir string) *RepoFetcher {
	return &RepoFetcher{
		cacheDir: cacheDir,
		ttl:      24 * time.Hour,
	}
}

// SetTTL overrides the default 24h cache TTL.
func (f *RepoFetcher) SetTTL(ttl time.Duration) {
	f.ttl = ttl
}

// FetchManifest fetches the manifest.json for a single repo.
// Repo name format: "owner/repo" (e.g. "dolphinzZv/skills").
func (f *RepoFetcher) FetchManifest(ctx context.Context, repoName string) (*ToolManifest, error) {
	// Check cache first
	if m, ok := f.cacheHit(repoName); ok {
		return m, nil
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/manifest.json", repoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}

	var m ToolManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest from %s: %w", repoName, err)
	}

	if m.Name == "" {
		m.Name = repoName
	}

	f.writeCache(repoName, &m)
	return &m, nil
}

// FetchAll fetches manifests from multiple repos in parallel.
// Failures for individual repos are logged but do not stop the fetch.
func (f *RepoFetcher) FetchAll(ctx context.Context, repos []string) []*ToolManifest {
	if len(repos) == 0 {
		return nil
	}

	type result struct {
		manifest *ToolManifest
		index    int
	}

	results := make([]*ToolManifest, len(repos))
	var wg sync.WaitGroup
	ch := make(chan result, len(repos))

	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			m, err := f.FetchManifest(ctx, repo)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[repo] fetch %s: %v\n", repo, err)
				return
			}
			ch <- result{manifest: m, index: i}
		}(i, repo)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for r := range ch {
		results[r.index] = r.manifest
	}

	// Compact nil entries
	var out []*ToolManifest
	for _, m := range results {
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}

// GetCached returns a cached manifest if available and not expired.
func (f *RepoFetcher) GetCached(repoName string) (*ToolManifest, bool) {
	return f.cacheHit(repoName)
}

// ConflictCheck detects tools with the same name across multiple manifests.
func (f *RepoFetcher) ConflictCheck(manifests []*ToolManifest) []Conflict {
	seen := make(map[string][]RepoSource)
	for _, m := range manifests {
		for _, t := range m.Tools {
			seen[t.Name] = append(seen[t.Name], RepoSource{
				Repo:        m.Name,
				Description: t.Description,
			})
		}
	}

	var conflicts []Conflict
	for name, sources := range seen {
		if len(sources) > 1 {
			conflicts = append(conflicts, Conflict{
				Name:    name,
				Sources: sources,
			})
		}
	}
	return conflicts
}

// SearchTools searches across manifests for tools matching the given keywords.
// Returns matching ToolEntry values with their repo source.
func (f *RepoFetcher) SearchTools(manifests []*ToolManifest, keywords []string) []ToolEntry {
	var matches []ToolEntry
	for _, m := range manifests {
		for _, t := range m.Tools {
			haystack := strings.ToLower(t.Name + " " + t.Description)
			for _, kw := range keywords {
				if strings.Contains(haystack, strings.ToLower(kw)) {
					matches = append(matches, t)
					break
				}
			}
		}
	}
	return matches
}

// cacheHit checks the cache for a non-expired manifest.
func (f *RepoFetcher) cacheHit(repoName string) (*ToolManifest, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	path := f.cachePath(repoName)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(info.ModTime()) > f.ttl {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var m ToolManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return &m, true
}

// writeCache writes a manifest to the cache.
func (f *RepoFetcher) writeCache(repoName string, m *ToolManifest) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := f.cachePath(repoName)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	data, _ := json.Marshal(m)
	os.WriteFile(path, data, 0644)
}

// cachePath returns the cache file path for a repo, replacing "/" with "-".
func (f *RepoFetcher) cachePath(repoName string) string {
	safe := strings.ReplaceAll(repoName, "/", "-")
	return filepath.Join(f.cacheDir, safe, "manifest.json")
}
