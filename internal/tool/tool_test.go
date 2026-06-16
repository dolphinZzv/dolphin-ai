package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/types"
	"github.com/h2non/gock"
)

// mockExecutor is a simple mock that returns predefined tool definitions.
type mockExecutor struct {
	defs []types.ToolDef
	err  error
}

func (m *mockExecutor) List(ctx context.Context) ([]types.ToolDef, error) {
	return m.defs, m.err
}

func (m *mockExecutor) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    "executed: " + call.Name,
	}, nil
}

// mockSkillStore implements SkillStore for testing.
type mockSkillStore struct {
	skills map[string]skill.Skill
}

func newMockSkillStore() *mockSkillStore {
	return &mockSkillStore{skills: make(map[string]skill.Skill)}
}

func (m *mockSkillStore) List(ctx context.Context) ([]skill.Skill, error) {
	var list []skill.Skill
	for _, s := range m.skills {
		list = append(list, s)
	}
	return list, nil
}

func (m *mockSkillStore) Get(ctx context.Context, name string) (*skill.Skill, error) {
	s, ok := m.skills[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return &s, nil
}

func (m *mockSkillStore) Save(ctx context.Context, sk skill.Skill) error {
	m.skills[sk.Name] = sk
	return nil
}

func (m *mockSkillStore) Delete(ctx context.Context, name string) error {
	delete(m.skills, name)
	return nil
}

func (m *mockSkillStore) Search(ctx context.Context, query string) ([]skill.Skill, error) {
	var results []skill.Skill
	for _, s := range m.skills {
		results = append(results, s)
	}
	return results, nil
}

func TestRegistry_RegisterBuiltinAndExecute(t *testing.T) {
	r := NewRegistry()

	r.RegisterBuiltin("echo", "echo the input", json.RawMessage(`{"type":"object"}`),
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{ToolCallID: "id1", Content: string(args)}, nil
		})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "id1",
		Name:      "echo",
		Arguments: `{"msg":"hello"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if result.Content != `{"msg":"hello"}` {
		t.Fatalf("expected '{\"msg\":\"hello\"}', got '%s'", result.Content)
	}
}

func TestRegistry_ExecuteBuiltinNoArgs(t *testing.T) {
	r := NewRegistry()

	r.RegisterBuiltin("ping", "ping", nil,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{ToolCallID: "id1", Content: "pong"}, nil
		})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:   "id1",
		Name: "ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "pong" {
		t.Fatalf("expected 'pong', got '%s'", result.Content)
	}
}

func TestRegistry_ExecuteNotFound(t *testing.T) {
	r := NewRegistry()

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:   "id1",
		Name: "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing tool")
	}
	if result.Content != `tool "nonexistent" not found` {
		t.Fatalf("unexpected message: %s", result.Content)
	}
}

func TestRegistry_ExecuteFromSource(t *testing.T) {
	r := NewRegistry()

	r.AddSource(&mockExecutor{
		defs: []types.ToolDef{
			{Name: "remote_tool", Description: "a remote tool"},
		},
	})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:   "id2",
		Name: "remote_tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "executed: remote_tool" {
		t.Fatalf("expected 'executed: remote_tool', got '%s'", result.Content)
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	r.RegisterBuiltin("tool_a", "desc a", json.RawMessage(`{"a":1}`),
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{}, nil
		})
	r.RegisterBuiltin("tool_b", "desc b", json.RawMessage(`{"b":2}`),
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{}, nil
		})

	r.AddSource(&mockExecutor{
		defs: []types.ToolDef{
			{Name: "source_tool", Description: "from source"},
		},
	})

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["tool_a"] {
		t.Fatal("missing tool_a")
	}
	if !names["tool_b"] {
		t.Fatal("missing tool_b")
	}
	if !names["source_tool"] {
		t.Fatal("missing source_tool")
	}
}

func TestRegistry_ListSkipsSourceError(t *testing.T) {
	r := NewRegistry()

	r.RegisterBuiltin("builtin", "desc", nil,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{}, nil
		})

	r.AddSource(&mockExecutor{err: errors.New("list error")})

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(defs))
	}
}

func TestCatalog_SearchByName(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "filesystem", Description: "File system operations", URL: "http://fs", Tags: []string{"fs", "io"}},
		{Name: "database", Description: "Database queries", URL: "http://db", Tags: []string{"sql", "db"}},
	})

	results := c.Search("filesystem")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "filesystem" {
		t.Fatalf("expected 'filesystem', got '%s'", results[0].Name)
	}
}

func TestCatalog_SearchByDescription(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "fs", Description: "File system operations"},
		{Name: "db", Description: "Database queries"},
	})

	results := c.Search("database")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "db" {
		t.Fatalf("expected 'db', got '%s'", results[0].Name)
	}
}

func TestCatalog_SearchByTag(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "fs", Tags: []string{"io", "storage"}},
		{Name: "web", Tags: []string{"http", "api"}},
	})

	results := c.Search("http")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "web" {
		t.Fatalf("expected 'web', got '%s'", results[0].Name)
	}
}

func TestCatalog_SearchCaseInsensitive(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "FileSystem", Description: "file thing"},
	})

	results := c.Search("filesystem")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestCatalog_SearchNoMatch(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "fs", Description: "file system"},
	})

	results := c.Search("zzz_nonexistent_zzz")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestCatalog_SearchEmptyQuery(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "fs", Description: "file system", Tags: []string{"io"}},
		{Name: "db", Description: "database", Tags: []string{"sql"}},
	})

	// Empty query matches everything because strings.Contains("anything","") is true.
	// Each entry is appended once per name/description match and once per tag match.
	results := c.Search("")
	if len(results) != 4 {
		t.Fatalf("expected 4 results for empty query, got %d", len(results))
	}
}

func TestMetaHandler_mcp_search(t *testing.T) {
	c := NewCatalog([]CatalogEntry{
		{Name: "my-tool", Description: "useful tool", URL: "http://example.com/mcp"},
	})
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	handler, ok := handlers["mcp_search"]
	if !ok {
		t.Fatal("mcp_search handler not found")
	}

	args, _ := json.Marshal(map[string]string{"query": "useful"})
	result, err := handler.Handler(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	var entries []CatalogEntry
	if err := json.Unmarshal([]byte(result.Content), &entries); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "my-tool" {
		t.Fatalf("expected 'my-tool', got '%s'", entries[0].Name)
	}
}

func TestMetaHandler_mcp_searchInvalidArgs(t *testing.T) {
	c := NewCatalog(nil)
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	handler, ok := handlers["mcp_search"]
	if !ok {
		t.Fatal("mcp_search handler not found")
	}

	result, err := handler.Handler(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestMetaHandler_mcp_load(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.test.server").
		Post("/").
		Persist().
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"tools": []map[string]any{
					{
						"name":        "loaded_tool",
						"description": "loaded via mcp_load",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			},
		})

	c := NewCatalog(nil)
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	handler, ok := handlers["mcp_load"]
	if !ok {
		t.Fatal("mcp_load handler not found")
	}

	// Use a trailing slash URL so the Go HTTP client path matches gock's Post("/").
	args, _ := json.Marshal(map[string]string{"url": "http://mcp.test.server/"})
	result, err := handler.Handler(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", result.Content)
	}
	if result.Content != `loaded 1 tools from http://mcp.test.server/` {
		t.Fatalf("unexpected content: %s", result.Content)
	}

	// Verify the client was added as a source.
	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range defs {
		if d.Name == "loaded_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("loaded_tool not found in registry after mcp_load")
	}
}

func TestMetaHandler_mcp_loadInvalidArgs(t *testing.T) {
	c := NewCatalog(nil)
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	handler, ok := handlers["mcp_load"]
	if !ok {
		t.Fatal("mcp_load handler not found")
	}

	result, err := handler.Handler(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestMetaHandler_mcp_loadConnectionError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.nonexistent").
		Post("/").
		ReplyError(errors.New("connection refused"))

	c := NewCatalog(nil)
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	handler, ok := handlers["mcp_load"]
	if !ok {
		t.Fatal("mcp_load handler not found")
	}

	args, _ := json.Marshal(map[string]string{"url": "http://mcp.nonexistent/"})
	result, err := handler.Handler(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for connection error")
	}
}

func TestMetaHandler_ContainsBothKeys(t *testing.T) {
	c := NewCatalog(nil)
	r := NewRegistry()
	handlers := MetaHandler(c, r)

	if _, ok := handlers["mcp_search"]; !ok {
		t.Fatal("missing mcp_search")
	}
	if _, ok := handlers["mcp_load"]; !ok {
		t.Fatal("missing mcp_load")
	}
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
}

func TestExecuteWithTimeout(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltin("fast", "fast tool", nil,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{ToolCallID: "id1", Content: "done"}, nil
		})

	result, err := ExecuteWithTimeout(context.Background(), r, types.ToolCall{
		ID:   "id1",
		Name: "fast",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" {
		t.Fatalf("expected 'done', got '%s'", result.Content)
	}
}

func TestRegistry_SourceManagement(t *testing.T) {
	r := NewRegistry()

	exec := &mockExecutor{defs: []types.ToolDef{{Name: "tool1"}}}
	r.AddNamedSource("my-source", exec)

	sources := r.ListSources()
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "my-source" {
		t.Errorf("expected 'my-source', got '%s'", sources[0].Name)
	}
	if !sources[0].Enabled {
		t.Errorf("expected source to be enabled by default")
	}

	if err := r.DisableSource("my-source"); err != nil {
		t.Fatal(err)
	}
	sources = r.ListSources()
	if sources[0].Enabled {
		t.Errorf("expected source to be disabled")
	}

	if err := r.EnableSource("my-source"); err != nil {
		t.Fatal(err)
	}
	sources = r.ListSources()
	if !sources[0].Enabled {
		t.Errorf("expected source to be re-enabled")
	}

	if err := r.SetSourceEnabled("unknown", false); err == nil {
		t.Fatal("expected error for unknown source")
	}

	r.AddNamedSource("broken", &mockExecutor{err: errors.New("list error")})
	active2 := r.ListActiveSources(context.Background())
	if len(active2) != 1 {
		t.Errorf("expected 1 active source, got %d", len(active2))
	}

	r.AddSource(exec)
	sources = r.ListSources()
	hasAuto := false
	for _, s := range sources {
		if strings.HasPrefix(s.Name, "source_") {
			hasAuto = true
		}
	}
	if !hasAuto {
		t.Errorf("expected auto-named source (source_N)")
	}
}

func TestExecuteWithTimeoutExceeded(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltin("hangs", "hangs forever", nil,
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			<-ctx.Done()
			return &types.ToolResult{Content: "cancelled"}, nil
		})

	result, err := ExecuteWithTimeout(context.Background(), r, types.ToolCall{
		ID:   "id1",
		Name: "hangs",
	}, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for timeout")
	}
	if result.Content != "tool execution timed out" {
		t.Fatalf("expected timeout message, got: %s", result.Content)
	}
}

func TestSkillAdapter(t *testing.T) {
	store := newMockSkillStore()
	store.Save(context.Background(), skill.Skill{Name: "test", Prompt: "hello"})

	adapter := SkillAdapter{Store: store}

	// Test List
	skills, err := adapter.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	// Test Get
	sk, err := adapter.Get(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Prompt != "hello" {
		t.Fatalf("expected 'hello', got '%s'", sk.Prompt)
	}

	// Test Save
	err = adapter.Save(context.Background(), skill.Skill{Name: "another"})
	if err != nil {
		t.Fatal(err)
	}

	// Test Search
	results, err := adapter.Search(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(results))
	}

	// Test Delete
	err = adapter.Delete(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	skills, _ = adapter.List(context.Background())
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill after delete, got %d", len(skills))
	}
}

func TestRegisterSkillTools(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	// Verify all tools are registered.
	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"skill_upsert": false,
		"skill_search": false,
		"skill_load":   false,
	}

	if len(defs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(defs))
	}

	for _, d := range defs {
		if _, ok := expected[d.Name]; ok {
			expected[d.Name] = true
		} else {
			t.Fatalf("unexpected tool: %s", d.Name)
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("missing tool: %s", name)
		}
	}
}

func TestSkillUpsertCreate(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(skill.Skill{Name: "my_skill", Prompt: "do something"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "skill_upsert",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "skill 'my_skill' saved" {
		t.Fatalf("unexpected content: %s", result.Content)
	}

	// Verify it was saved.
	sk, err := store.Get(context.Background(), "my_skill")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Prompt != "do something" {
		t.Fatalf("expected 'do something', got '%s'", sk.Prompt)
	}
}

func TestSkillUpsertInvalidArgs(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "skill_upsert",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSkill_search(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	store.Save(context.Background(), skill.Skill{Name: "found_skill", Prompt: "hello"})
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(map[string]string{"query": "found"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-2",
		Name:      "skill_search",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var skills []skill.Skill
	if err := json.Unmarshal([]byte(result.Content), &skills); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "found_skill" {
		t.Fatalf("unexpected results: %+v", skills)
	}
}

func TestSkill_searchInvalidArgs(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-2",
		Name:      "skill_search",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSkill_load(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	store.Save(context.Background(), skill.Skill{Name: "my_skill", Prompt: "do something", Enabled: false})
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(map[string]string{"name": "my_skill"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-3",
		Name:      "skill_load",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "skill 'my_skill' loaded" {
		t.Fatalf("unexpected content: %s", result.Content)
	}

	// Verify Enabled was set to true.
	sk, _ := store.Get(context.Background(), "my_skill")
	if !sk.Enabled {
		t.Fatal("expected skill to be enabled after load")
	}
}

func TestSkill_loadInvalidArgs(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-3",
		Name:      "skill_load",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSkill_loadNotFound(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-4",
		Name:      "skill_load",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing skill")
	}
}

func TestSkillUpsertUpdate(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	store.Save(context.Background(), skill.Skill{Name: "my_skill", Prompt: "original"})
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(skill.Skill{Name: "my_skill", Prompt: "updated"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-5",
		Name:      "skill_upsert",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "skill 'my_skill' saved" {
		t.Fatalf("unexpected content: %s", result.Content)
	}

	sk, _ := store.Get(context.Background(), "my_skill")
	if sk.Prompt != "updated" {
		t.Fatalf("expected 'updated', got '%s'", sk.Prompt)
	}
}

func TestSkillUpsertDelete(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	store.Save(context.Background(), skill.Skill{Name: "my_skill", Prompt: "do something"})
	RegisterSkillTools(r, store)

	args, _ := json.Marshal(map[string]string{"name": "my_skill"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-6",
		Name:      "skill_upsert",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "skill 'my_skill' deleted" {
		t.Fatalf("unexpected content: %s", result.Content)
	}

	_, err = store.Get(context.Background(), "my_skill")
	if err == nil {
		t.Fatal("expected skill to be deleted")
	}
}

// removed - covered by TestSkillUpsertInvalidArgs
func _removed(t *testing.T) {
	r := NewRegistry()
	store := newMockSkillStore()
	RegisterSkillTools(r, store)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-6",
		Name:      "skill_upsert",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestRegisterSessionTools(t *testing.T) {
	r := NewRegistry()
	mgr := session.NewManager(t.TempDir())

	RegisterSessionTools(r, mgr)

	ctx := context.Background()
	s := mgr.Create(ctx)
	mgr.Create(ctx)

	t.Run("session_list lists sessions", func(t *testing.T) {
		result, err := r.Execute(ctx, types.ToolCall{
			ID:        "call-1",
			Name:      "session_list",
			Arguments: `{}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.Content)
		}
		if !strings.Contains(result.Content, s.ID) {
			t.Errorf("expected session ID in output, got: %s", result.Content)
		}
	})

	t.Run("session_switch switches to session", func(t *testing.T) {
		result, err := r.Execute(ctx, types.ToolCall{
			ID:        "call-2",
			Name:      "session_switch",
			Arguments: `{"id":"` + s.ID + `"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.Content)
		}
		if !strings.Contains(result.Content, s.ID) {
			t.Errorf("expected switched to session ID, got: %s", result.Content)
		}
	})

	t.Run("session_switch with empty id returns error", func(t *testing.T) {
		result, err := r.Execute(ctx, types.ToolCall{
			ID:        "call-3",
			Name:      "session_switch",
			Arguments: `{"id":""}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatal("expected IsError for empty id")
		}
	})

	t.Run("session_switch with invalid json returns error", func(t *testing.T) {
		result, err := r.Execute(ctx, types.ToolCall{
			ID:        "call-4",
			Name:      "session_switch",
			Arguments: `not json`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatal("expected IsError for invalid json")
		}
	})

	t.Run("session_switch to nonexistent returns error", func(t *testing.T) {
		result, err := r.Execute(ctx, types.ToolCall{
			ID:        "call-5",
			Name:      "session_switch",
			Arguments: `{"id":"nonexistent"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatal("expected IsError for nonexistent session")
		}
	})
}
