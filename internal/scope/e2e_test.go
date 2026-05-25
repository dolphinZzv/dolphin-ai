package scope

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// mockDispatcher simulates the AgentPool's async Dispatch + PollResult contract.
type mockDispatcher struct {
	mu      sync.Mutex
	results map[string]*DispatchResult
}

func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{results: make(map[string]*DispatchResult)}
}

func (d *mockDispatcher) Dispatch(agentName string, task DispatchTask) error {
	// Spawn a goroutine to simulate async agent execution
	go func() {
		// Simulate work
		time.Sleep(50 * time.Millisecond)
		res := &DispatchResult{
			TaskID:    task.ID,
			AgentName: agentName,
			Output:    "processed: " + task.Input,
			Success:   true,
		}
		d.mu.Lock()
		d.results[task.ID] = res
		d.mu.Unlock()
	}()
	return nil
}

func (d *mockDispatcher) PollResult(taskID string) *DispatchResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, ok := d.results[taskID]
	if !ok {
		return nil
	}
	delete(d.results, taskID)
	return res
}

// errDispatcher returns an error on Dispatch.
type errDispatcher struct {
	err string
}

func (d *errDispatcher) Dispatch(_ string, _ DispatchTask) error {
	return &dispatchError{d.err}
}

func (d *errDispatcher) PollResult(_ string) *DispatchResult {
	return nil
}

type dispatchError struct{ msg string }

func (e *dispatchError) Error() string { return e.msg }

func writeScopeYAML(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "scopes.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestE2E_LoadAndResolve(t *testing.T) {
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
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if len(m.Warnings) > 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}

	// Resolve files against the loaded scopes
	matched := m.Resolve([]string{
		"frontend/src/ui/button.tsx",
		"backend/internal/api/handler.go",
		"README.md",
	})
	if len(matched) != 2 {
		t.Fatalf("expected 2 scopes matched, got %d", len(matched))
	}
	if len(matched["frontend"]) != 1 || matched["frontend"][0] != "frontend/src/ui/button.tsx" {
		t.Errorf("frontend match wrong: %v", matched["frontend"])
	}
	if len(matched["backend"]) != 1 || matched["backend"][0] != "backend/internal/api/handler.go" {
		t.Errorf("backend match wrong: %v", matched["backend"])
	}
}

func TestE2E_DispatchAndPoll(t *testing.T) {
	yaml := `
scopes:
  - name: test-agent
    dirs:
      - "src"
    role: "test role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	dispatcher := newMockDispatcher()
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	// Dispatch and wait for result
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := router.Dispatch(ctx, "test-agent", DispatchTask{
		ID:    "task-1",
		Input: "hello from test",
	})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected success, got error: %s", results[0].Error)
	}
	if results[0].TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", results[0].TaskID)
	}
	if results[0].AgentName != "test-agent" {
		t.Errorf("expected agent test-agent, got %s", results[0].AgentName)
	}
	if results[0].Output != "processed: hello from test" {
		t.Errorf("unexpected output: %s", results[0].Output)
	}
}

func TestE2E_DispatchTimeout(t *testing.T) {
	yaml := `
scopes:
  - name: slow-agent
    dirs:
      - "slow"
    role: "slow role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	// A dispatcher that never produces a result (simulates a hung agent)
	dispatcher := &slowDispatcher{}
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	// Short timeout to force cancellation of the polling loop
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = router.Dispatch(ctx, "slow-agent", DispatchTask{ID: "task-timeout", Input: "hi"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

// slowDispatcher never produces a result — used to test timeouts.
type slowDispatcher struct{}

func (d *slowDispatcher) Dispatch(_ string, _ DispatchTask) error {
	return nil
}

func (d *slowDispatcher) PollResult(_ string) *DispatchResult {
	return nil
}

func TestE2E_DispatchError(t *testing.T) {
	yaml := `
scopes:
  - name: fail-agent
    dirs:
      - "fail"
    role: "fail role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	dispatcher := &errDispatcher{err: "agent not found: fail-agent"}
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	_, err = router.Dispatch(context.Background(), "fail-agent", DispatchTask{ID: "task-err", Input: "hi"})
	if err == nil {
		t.Fatal("expected dispatch error, got nil")
	}
}

func TestE2E_ContextCancelled(t *testing.T) {
	yaml := `
scopes:
  - name: cancel-agent
    dirs:
      - "cancel"
    role: "cancel role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	dispatcher := &slowDispatcher{}
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = router.Dispatch(ctx, "cancel-agent", DispatchTask{ID: "task-cancel", Input: "hi"})
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected Canceled, got: %v", err)
	}
}

func TestE2E_ContextCancelledAfterDispatch(t *testing.T) {
	yaml := `
scopes:
  - name: cancel-after-dispatch
    dirs:
      - "src"
    role: "test role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	// A dispatcher that sleeps for 200ms before producing result
	dispatcher := &delayedDispatcher{delay: 200 * time.Millisecond}
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = router.Dispatch(ctx, "cancel-after-dispatch", DispatchTask{ID: "task-cancel-after", Input: "hi"})
	if err != context.Canceled {
		t.Errorf("expected Canceled, got: %v", err)
	}
}

// delayedDispatcher produces a result after a configurable delay.
type delayedDispatcher struct {
	delay time.Duration
	mu    sync.Mutex
	res   map[string]*DispatchResult
}

func (d *delayedDispatcher) Dispatch(agentName string, task DispatchTask) error {
	if d.res == nil {
		d.res = make(map[string]*DispatchResult)
	}
	go func() {
		time.Sleep(d.delay)
		d.mu.Lock()
		d.res[task.ID] = &DispatchResult{
			TaskID:    task.ID,
			AgentName: agentName,
			Output:    "delayed result",
			Success:   true,
		}
		d.mu.Unlock()
	}()
	return nil
}

func (d *delayedDispatcher) PollResult(taskID string) *DispatchResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, ok := d.res[taskID]
	if !ok {
		return nil
	}
	delete(d.res, taskID)
	return res
}

func TestE2E_ScopeNotFound(t *testing.T) {
	yaml := `
scopes:
  - name: existing
    dirs:
      - "src"
    role: "existing role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	dispatcher := &errDispatcher{err: "agent not found: nonexistent"}
	router := NewRouter(RouterConfig{Type: "local"}, m, dispatcher)

	// Dispatch to a scope that doesn't exist in the pool
	_, err = router.Dispatch(context.Background(), "nonexistent", DispatchTask{ID: "task-no-scope", Input: "hi"})
	if err == nil {
		t.Fatal("expected error for non-existent scope, got nil")
	}
}

func TestE2E_ResolveFromYAML(t *testing.T) {
	yaml := `
scopes:
  - name: docs
    dirs:
      - "docs"
      - "documentation"
    role: "doc writer"
    tools:
      - shell
  - name: tests
    dirs:
      - "tests"
      - "test"
    role: "test writer"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	// Verify Info() reflects loaded YAML
	infos := m.Info()
	if len(infos) != 2 {
		t.Fatalf("expected 2 scope infos, got %d", len(infos))
	}
	infoMap := make(map[string]ScopeInfo)
	for _, info := range infos {
		infoMap[info.Name] = info
	}

	docs, ok := infoMap["docs"]
	if !ok {
		t.Fatal("expected docs scope in Info()")
	}
	if len(docs.Dirs) != 2 || docs.Dirs[0] != "docs" {
		t.Errorf("unexpected docs dirs: %v", docs.Dirs)
	}

	testsScope, ok := infoMap["tests"]
	if !ok {
		t.Fatal("expected tests scope in Info()")
	}
	if len(testsScope.Dirs) != 2 || testsScope.Dirs[0] != "tests" {
		t.Errorf("unexpected tests dirs: %v", testsScope.Dirs)
	}

	// Verify Resolve
	matched := m.Resolve([]string{
		"docs/readme.md",
		"tests/e2e_test.go",
		"documentation/guide.md",
		"test/unit_test.go",
	})
	if len(matched) != 2 {
		t.Fatalf("expected 2 scopes matched, got %d", len(matched))
	}
	if len(matched["docs"]) != 2 {
		t.Errorf("expected 2 docs matches, got %d: %v", len(matched["docs"]), matched["docs"])
	}
	if len(matched["tests"]) != 2 {
		t.Errorf("expected 2 tests matches, got %d: %v", len(matched["tests"]), matched["tests"])
	}
}

func TestE2E_EmptyRouterWithNoScopes(t *testing.T) {
	// No scopes.yaml — LoadScopes returns nil, NewRouter returns emptyRouter
	m, err := LoadScopes("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}
	if m != nil {
		t.Fatal("expected nil manager")
	}

	router := NewRouter(RouterConfig{}, nil, nil)

	// emptyRouter.Resolve returns empty
	result, err := router.Resolve([]string{"foo.go"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}

	// emptyRouter.Dispatch returns error
	_, err = router.Dispatch(context.Background(), "test", DispatchTask{})
	if err == nil {
		t.Error("expected error from emptyRouter.Dispatch")
	}

	// emptyRouter.Scopes returns nil
	if router.Scopes() != nil {
		t.Error("expected nil scopes from emptyRouter")
	}
}

func TestE2E_ScopesListViaRouter(t *testing.T) {
	yaml := `
scopes:
  - name: frontend
    description: "Frontend UI"
    dirs:
      - "src/ui"
    role: "ui role"
    tools:
      - shell
  - name: backend
    description: "Backend API"
    dirs:
      - "internal/api"
    role: "api role"
    tools:
      - shell
`
	dir := t.TempDir()
	path := writeScopeYAML(t, dir, yaml)

	m, err := LoadScopes(path)
	if err != nil {
		t.Fatalf("LoadScopes failed: %v", err)
	}

	router := NewRouter(RouterConfig{Type: "local"}, m, nil)
	scopes := router.Scopes()

	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(scopes))
	}

	names := map[string]bool{}
	for _, s := range scopes {
		names[s.Name] = true
		if s.Description == "" {
			t.Errorf("scope %q missing description", s.Name)
		}
		if len(s.Dirs) == 0 {
			t.Errorf("scope %q missing dirs", s.Name)
		}
	}
	if !names["frontend"] || !names["backend"] {
		t.Errorf("unexpected scope names: %v", names)
	}
}
