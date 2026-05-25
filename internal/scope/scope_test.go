package scope

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTestScopes(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "scopes.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadScopes_FileNotFound(t *testing.T) {
	m, err := LoadScopes("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if m != nil {
		t.Fatal("expected nil manager for missing file")
	}
}

func TestLoadScopes_Valid(t *testing.T) {
	yaml := `
scopes:
  - name: frontend
    description: "Frontend UI"
    dirs:
      - "frontend"
      - "src/ui"
    role: "You are a frontend specialist."
    tools:
      - shell
    timeout: 120
  - name: backend
    description: "Backend API"
    dirs:
      - "backend"
      - "internal/api"
    role: "You are a backend specialist."
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeTestScopes(t, dir, yaml)
	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	scopes := m.Scopes()
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(scopes))
	}
	if scopes[0].Name != "frontend" {
		t.Errorf("expected first scope name 'frontend', got %q", scopes[0].Name)
	}
}

func TestLoadScopes_Empty(t *testing.T) {
	yaml := `scopes: []`
	dir := t.TempDir()
	path := writeTestScopes(t, dir, yaml)
	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m != nil {
		t.Fatal("expected nil manager for empty scopes")
	}
}

func TestLoadScopes_MissingFields(t *testing.T) {
	yaml := `
scopes:
  - name: ""
    dirs:
      - "src"
    role: "role"
  - name: valid
    dirs:
      - "src"
    role: "role"
  - name: valid
    dirs:
      - "other"
    role: "other role"
`
	dir := t.TempDir()
	path := writeTestScopes(t, dir, yaml)
	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager with at least one valid scope")
	}
	scopes := m.Scopes()
	if len(scopes) != 1 {
		t.Fatalf("expected 1 valid scope, got %d", len(scopes))
	}
	if scopes[0].Name != "valid" {
		t.Errorf("expected scope name 'valid', got %q", scopes[0].Name)
	}
}

func TestResolve_DirPrefix(t *testing.T) {
	m := &Manager{
		scopes: []Scope{
			{Name: "frontend", Dirs: []string{"frontend", "src/ui"}},
			{Name: "backend", Dirs: []string{"backend", "internal/api"}},
		},
	}

	result := m.Resolve([]string{
		"frontend/src/ui/button.tsx",
		"backend/internal/api/handler.go",
		"README.md",
	})

	if len(result) != 2 {
		t.Fatalf("expected 2 scopes in result, got %d", len(result))
	}
	if len(result["frontend"]) != 1 || result["frontend"][0] != "frontend/src/ui/button.tsx" {
		t.Errorf("unexpected frontend files: %v", result["frontend"])
	}
	if len(result["backend"]) != 1 || result["backend"][0] != "backend/internal/api/handler.go" {
		t.Errorf("unexpected backend files: %v", result["backend"])
	}
}

func TestResolve_FileMatchesDir(t *testing.T) {
	m := &Manager{
		scopes: []Scope{
			{Name: "docs", Dirs: []string{"docs"}},
		},
	}

	result := m.Resolve([]string{"docs"})
	if len(result["docs"]) != 1 {
		t.Errorf("expected docs to match 'docs' itself")
	}
}

func TestResolve_MultipleMatches(t *testing.T) {
	m := &Manager{
		scopes: []Scope{
			{Name: "frontend", Dirs: []string{"src"}},
			{Name: "backend", Dirs: []string{"src"}},
		},
	}

	result := m.Resolve([]string{"src/ui/button.tsx"})
	if len(result["frontend"]) != 1 {
		t.Errorf("expected frontend to match src/")
	}
	if len(result["backend"]) != 1 {
		t.Errorf("expected backend to match src/")
	}
}

func TestMatchFile(t *testing.T) {
	tests := []struct {
		file string
		dirs []string
		want bool
	}{
		{"frontend/src/ui/button.tsx", []string{"frontend"}, true},
		{"frontend", []string{"frontend"}, true},
		{"frontend-extra/file.go", []string{"frontend"}, false},
		{"backend/main.go", []string{"frontend"}, false},
		{"src/ui/button.tsx", []string{"src/ui"}, true},
		{"src/ui", []string{"src/ui"}, true},
		{"src/ui-extra/file.go", []string{"src/ui"}, false},
		{"foo/bar/baz.go", []string{"foo/bar"}, true},
	}
	for _, tt := range tests {
		got := matchFile(tt.file, tt.dirs)
		if got != tt.want {
			t.Errorf("matchFile(%q, %v) = %v, want %v", tt.file, tt.dirs, got, tt.want)
		}
	}
}

func TestEmptyRouter(t *testing.T) {
	r := &emptyRouter{}
	result, err := r.Resolve([]string{"foo.go"})
	if err != nil {
		t.Fatalf("emptyRouter.Resolve failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
	_, err = r.Dispatch(context.TODO(), "test", DispatchTask{})
	if err == nil {
		t.Error("expected error from emptyRouter.Dispatch")
	}
	if r.Scopes() != nil {
		t.Error("expected nil scopes from emptyRouter")
	}
}

func TestScopeInfo(t *testing.T) {
	m := &Manager{
		scopes: []Scope{
			{Name: "a", Description: "desc A", Dirs: []string{"dirA"}},
			{Name: "b", Description: "desc B", Dirs: []string{"dirB"}},
		},
	}
	infos := m.Info()
	if len(infos) != 2 {
		t.Fatalf("expected 2 scope infos, got %d", len(infos))
	}
	if infos[0].Name != "a" || infos[0].Description != "desc A" {
		t.Errorf("unexpected first scope info: %+v", infos[0])
	}
}

func TestLoadScopes_Warnings(t *testing.T) {
	yaml := `
scopes:
  - name: ""
    dirs:
      - "src"
    role: "role"
  - name: valid
    dirs:
      - "src"
    role: "valid role"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if len(m.Warnings) == 0 {
		t.Error("expected warnings for invalid scope definitions")
	}
}

func TestNewRouter_NilManager(t *testing.T) {
	r := NewRouter(RouterConfig{}, nil, nil)
	if r == nil {
		t.Fatal("expected non-nil router")
	}
	if _, ok := r.(*emptyRouter); !ok {
		t.Fatalf("expected emptyRouter, got %T", r)
	}
}

func TestNewRouter_Local(t *testing.T) {
	m := &Manager{scopes: []Scope{{Name: "test", Dirs: []string{"src"}, Role: "role"}}}
	r := NewRouter(RouterConfig{Type: "local"}, m, nil)
	lr, ok := r.(*localRouter)
	if !ok {
		t.Fatalf("expected localRouter, got %T", r)
	}
	if lr.mgr != m {
		t.Error("mgr not set correctly")
	}
}

func TestLocalRouter_Scopes(t *testing.T) {
	m := &Manager{scopes: []Scope{
		{Name: "a", Dirs: []string{"dirA"}},
		{Name: "b", Dirs: []string{"dirB"}},
	}}
	r := NewRouter(RouterConfig{Type: "local"}, m, nil)
	infos := r.Scopes()
	if len(infos) != 2 {
		t.Fatalf("expected 2 scope infos, got %d", len(infos))
	}
	if infos[0].Name != "a" || infos[1].Name != "b" {
		t.Errorf("unexpected scope order: %+v", infos)
	}
}
