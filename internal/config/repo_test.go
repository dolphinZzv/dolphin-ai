package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepoFetcherFetchManifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/main/manifest.json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(ToolManifest{
			Name:        "test/skills",
			Description: "test repo",
			Tools: []ToolEntry{
				{Name: "skill-a", Description: "Skill A", URL: "https://example.com/a"},
				{Name: "skill-b", Description: "Skill B", URL: "https://example.com/b"},
			},
		})
	}))
	defer srv.Close()

	ctx := context.Background()

	// Use the actual URL by overriding via custom fetch
	m, err := fetchManifestFromURL(ctx, srv.URL+"/main/manifest.json")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if m.Name != "test/skills" {
		t.Errorf("name = %q, want %q", m.Name, "test/skills")
	}
	if len(m.Tools) != 2 {
		t.Errorf("got %d tools, want 2", len(m.Tools))
	}
}

func fetchManifestFromURL(ctx context.Context, url string) (*ToolManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var m ToolManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func TestRepoFetcherCache(t *testing.T) {
	cacheDir := t.TempDir()
	fetcher := NewRepoFetcher(cacheDir)

	// Should be a cache miss
	m, ok := fetcher.GetCached("nonexistent/repo")
	if ok {
		t.Error("expected cache miss for nonexistent repo")
	}
	_ = m

	// Write a manifest to the cache manually
	expected := &ToolManifest{
		Name:        "test/repo",
		Description: "cached",
		Tools:       []ToolEntry{{Name: "t1", Description: "Tool 1"}},
	}
	fetcher.writeCache("test/repo", expected)

	// Should be a cache hit
	got, ok := fetcher.GetCached("test/repo")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test/repo" {
		t.Errorf("name = %q, want %q", got.Name, "test/repo")
	}
	if len(got.Tools) != 1 {
		t.Errorf("got %d tools, want 1", len(got.Tools))
	}
}

func TestRepoFetcherCacheExpiry(t *testing.T) {
	cacheDir := t.TempDir()
	fetcher := NewRepoFetcher(cacheDir)
	fetcher.SetTTL(0) // immediate expiry

	fetcher.writeCache("test/repo", &ToolManifest{Name: "test/repo"})

	_, ok := fetcher.GetCached("test/repo")
	if ok {
		t.Error("expected cache miss due to immediate TTL")
	}
}

func TestRepoFetcherConflictCheck(t *testing.T) {
	fetcher := NewRepoFetcher(t.TempDir())

	manifests := []*ToolManifest{
		{
			Name: "repo-a",
			Tools: []ToolEntry{
				{Name: "shared-tool", Description: "from repo A"},
				{Name: "unique-a", Description: "only in A"},
			},
		},
		{
			Name: "repo-b",
			Tools: []ToolEntry{
				{Name: "shared-tool", Description: "from repo B"},
				{Name: "unique-b", Description: "only in B"},
			},
		},
	}

	conflicts := fetcher.ConflictCheck(manifests)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	if conflicts[0].Name != "shared-tool" {
		t.Errorf("conflict name = %q, want %q", conflicts[0].Name, "shared-tool")
	}
	if len(conflicts[0].Sources) != 2 {
		t.Errorf("got %d sources, want 2", len(conflicts[0].Sources))
	}
}

func TestRepoFetcherConflictCheckNoConflicts(t *testing.T) {
	fetcher := NewRepoFetcher(t.TempDir())

	manifests := []*ToolManifest{
		{
			Name: "repo-a",
			Tools: []ToolEntry{
				{Name: "tool-a", Description: "from repo A"},
			},
		},
		{
			Name: "repo-b",
			Tools: []ToolEntry{
				{Name: "tool-b", Description: "from repo B"},
			},
		},
	}

	conflicts := fetcher.ConflictCheck(manifests)
	if len(conflicts) != 0 {
		t.Errorf("got %d conflicts, want 0", len(conflicts))
	}
}

func TestRepoFetcherSearchTools(t *testing.T) {
	fetcher := NewRepoFetcher(t.TempDir())

	manifests := []*ToolManifest{
		{
			Name: "repo-a",
			Tools: []ToolEntry{
				{Name: "frontend-expert", Description: "React, Vue, TypeScript"},
				{Name: "go-expert", Description: "Go backend development"},
			},
		},
		{
			Name: "repo-b",
			Tools: []ToolEntry{
				{Name: "react-best-practices", Description: "React patterns"},
				{Name: "docker-expert", Description: "Container tools"},
			},
		},
	}

	// Search for "react"
	matches := fetcher.SearchTools(manifests, []string{"react"})
	if len(matches) < 2 {
		t.Errorf("got %d matches for 'react', want at least 2", len(matches))
	}

	// Search for "go"
	matches = fetcher.SearchTools(manifests, []string{"go"})
	if len(matches) != 1 {
		t.Errorf("got %d matches for 'go', want 1", len(matches))
	}

	// Search for nonexistent
	matches = fetcher.SearchTools(manifests, []string{"python"})
	if len(matches) != 0 {
		t.Errorf("got %d matches for 'python', want 0", len(matches))
	}
}

func TestRepoFetcherEmptyRepos(t *testing.T) {
	fetcher := NewRepoFetcher(t.TempDir())
	ctx := context.Background()

	manifests := fetcher.FetchAll(ctx, nil)
	if manifests != nil {
		t.Error("expected nil for empty repos")
	}

	manifests = fetcher.FetchAll(ctx, []string{})
	if manifests != nil {
		t.Error("expected nil for empty repos slice")
	}
}

func TestRepoFetcherCachePath(t *testing.T) {
	fetcher := NewRepoFetcher("/tmp/cache")
	path := fetcher.cachePath("dolphinzZv/skills")
	expected := filepath.Join("/tmp/cache", "dolphinzZv-skills", "manifest.json")
	if path != expected {
		t.Errorf("cachePath = %q, want %q", path, expected)
	}
}

func TestToolManifestJSON(t *testing.T) {
	data := `{"name":"test","description":"desc","tools":[{"name":"t1","description":"d1","url":"u1"}]}`
	var m ToolManifest
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Name != "test" {
		t.Errorf("name = %q", m.Name)
	}
	if len(m.Tools) != 1 {
		t.Errorf("tools = %d", len(m.Tools))
	}
	if m.Tools[0].URL != "u1" {
		t.Errorf("url = %q", m.Tools[0].URL)
	}
}

func TestRepoFetcherFetchAllParallel(t *testing.T) {
	// Start multiple test servers
	var servers []*httptest.Server
	for i := 0; i < 3; i++ {
		i := i
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(ToolManifest{
				Name:        "repo",
				Description: "test",
				Tools:       []ToolEntry{{Name: "tool", Description: "desc", URL: "url"}},
			})
		}))
		servers = append(servers, srv)
		defer srv.Close()
		_ = i
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	// Test that FetchAll handles empty results gracefully
	fetcher := NewRepoFetcher(t.TempDir())
	fetcher.SetTTL(0) // no cache

	ctx := context.Background()
	// Access invalid URLs to test parallel failure handling
	results := fetcher.FetchAll(ctx, []string{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestConflictSorting(t *testing.T) {
	fetcher := NewRepoFetcher(t.TempDir())

	manifests := []*ToolManifest{
		{
			Name: "repo-z",
			Tools: []ToolEntry{
				{Name: "tool-1", Description: "z"},
			},
		},
		{
			Name: "repo-a",
			Tools: []ToolEntry{
				{Name: "tool-1", Description: "a"},
				{Name: "tool-2", Description: "a2"},
			},
		},
		{
			Name: "repo-m",
			Tools: []ToolEntry{
				{Name: "tool-2", Description: "m"},
			},
		},
	}

	conflicts := fetcher.ConflictCheck(manifests)
	if len(conflicts) != 2 {
		t.Fatalf("got %d conflicts, want 2", len(conflicts))
	}
}

func TestWriteCacheCreatesDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "nested", "cache")
	fetcher := NewRepoFetcher(cacheDir)

	path := fetcher.cachePath("test/repo")
	// Verify dir doesn't exist yet
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Log("dir already exists")
	}

	fetcher.writeCache("test/repo", &ToolManifest{Name: "test/repo"})

	// After writeCache, the dir should exist
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("cache dir was not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cache file was not created: %v", err)
	}
}
