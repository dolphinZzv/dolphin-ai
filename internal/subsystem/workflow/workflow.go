// Package workflow manages workflow definitions that constrain LLM behavior.
// Workflows are markdown files stored in directories (one per workflow) with
// YAML frontmatter, following the same convention as skills.
//
// File structure: workflows/<name>/WORKFLOW.md
//
//	---
//	name: deploy-check
//	description: Check deployment health
//	---
//
//	When I ask you to run the deployment check, follow these steps:
//	1. Run `kubectl get pods --all-namespaces`
//	2. Run `kubectl get nodes`
//	3. Summarize findings
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/mcp"
	"dolphin/internal/subsystem"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Workflow represents a named workflow definition.
type Workflow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Source      string `json:"source"` // directory origin
}

// Manager loads and manages workflows from directories.
// File structure: <dir>/<name>/WORKFLOW.md with YAML frontmatter.
// Multiple directories are supported; first dir is the primary (writable).
type Manager struct {
	mu        sync.RWMutex
	workflows map[string]*Workflow
	dirs      []string
}

// NewManager creates a workflow manager from one or more directories.
// Empty strings are filtered out. Falls back to [".dolphin/workflows"].
func NewManager(dirs ...string) *Manager {
	filtered := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{".dolphin/workflows"}
	}
	return &Manager{
		workflows: make(map[string]*Workflow),
		dirs:      filtered,
	}
}

// Dir returns the primary workflows directory.
func (m *Manager) Dir() string {
	if len(m.dirs) > 0 {
		return m.dirs[0]
	}
	return ""
}

// Dirs returns all configured directories.
func (m *Manager) Dirs() []string { return m.dirs }

// Load scans all workflow directories and loads WORKFLOW.md files.
// Missing directories are silently skipped.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.workflows = make(map[string]*Workflow)

	for _, dir := range m.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read workflows dir %q: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			wfPath := filepath.Join(dir, name, "WORKFLOW.md")
			data, err := os.ReadFile(wfPath)
			if err != nil {
				continue
			}
			wf := parseWorkflowFile(data, name)
			if wf != nil {
				wf.Source = dir
				if _, exists := m.workflows[wf.Name]; !exists {
					m.workflows[wf.Name] = wf
				}
			}
		}
	}
	return nil
}

// Get returns a workflow by name.
func (m *Manager) Get(name string) (*Workflow, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workflows[name]
	return w, ok
}

// Search returns workflows whose name or description matches the query.
// Multiple space-separated terms are ORed — any term can match.
func (m *Manager) Search(query string) []*Workflow {
	m.mu.RLock()
	defer m.mu.RUnlock()

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var results []*Workflow
	for _, w := range m.workflows {
		name := strings.ToLower(w.Name)
		desc := strings.ToLower(w.Description)
		for _, t := range terms {
			if strings.Contains(name, t) || strings.Contains(desc, t) {
				results = append(results, w)
				break
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// List returns all workflows sorted by name.
func (m *Manager) List() []*Workflow {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Workflow, 0, len(m.workflows))
	for _, w := range m.workflows {
		list = append(list, w)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// ListForAgent returns workflows filtered by the allowed list.
// If allowed is empty, returns all workflows (backward compatible).
func (m *Manager) ListForAgent(allowed []string) []*Workflow {
	all := m.List()
	if len(allowed) == 0 {
		return all
	}
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	filtered := make([]*Workflow, 0, len(allowed))
	for _, w := range all {
		if set[w.Name] {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// GetForAgent returns a workflow by name if it is in the allowed list.
// If allowed is empty, returns the workflow (backward compatible).
func (m *Manager) GetForAgent(name string, allowed []string) (*Workflow, bool) {
	if len(allowed) > 0 {
		ok := false
		for _, a := range allowed {
			if a == name {
				ok = true
				break
			}
		}
		if !ok {
			return nil, false
		}
	}
	return m.Get(name)
}

// NewTemplate creates a new workflow file from a template in the primary directory.
func (m *Manager) NewTemplate(name, description string) error {
	if description == "" {
		description = name
	}
	title := name
	if description != name {
		title = description
	}
	content := fmt.Sprintf("# %s\n\n"+
		"Define the workflow steps here. The LLM MUST follow these steps exactly.\n\n"+
		"## Steps\n\n"+
		"1. First step\n"+
		"2. Second step\n"+
		"3. Final step\n", title)
	return m.Register(name, description, content)
}

// Register adds or updates a workflow and persists it to disk.
func (m *Manager) Register(name, description, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wf := &Workflow{
		Name:        name,
		Description: description,
		Content:     content,
		Source:      m.dirs[0],
	}

	m.workflows[name] = wf

	dir := m.dirs[0]
	wfDir := filepath.Join(dir, name)
	if err := os.MkdirAll(wfDir, 0700); err != nil {
		return fmt.Errorf("create workflow dir: %w", err)
	}

	var sb strings.Builder
	if description != "" {
		fmt.Fprintf(&sb, "---\nname: %s\ndescription: %s\n---\n\n", name, description)
	}
	sb.WriteString(content)

	return os.WriteFile(filepath.Join(wfDir, "WORKFLOW.md"), []byte(sb.String()), 0600)
}

// Unregister removes a workflow from memory and deletes its directory.
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workflows[name]
	if !ok {
		return fmt.Errorf("workflow %q not found", name)
	}

	delete(m.workflows, name)

	wfDir := filepath.Join(w.Source, name)
	if err := os.RemoveAll(wfDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Disable removes a workflow from memory and renames its directory to <name>.disabled/.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workflows[name]
	if !ok {
		return fmt.Errorf("workflow %q not found", name)
	}

	delete(m.workflows, name)

	oldDir := filepath.Join(w.Source, name)
	newDir := filepath.Join(w.Source, name+".disabled")
	if err := os.Rename(oldDir, newDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("disable workflow %q: %w", name, err)
	}
	return nil
}

// Enable restores a previously disabled workflow by renaming <name>.disabled/ back.
func (m *Manager) Enable(name string) error {
	var disabledDir, wfDir string
	found := false
	for _, dir := range m.dirs {
		dd := filepath.Join(dir, name+".disabled")
		if _, err := os.Stat(dd); err == nil {
			disabledDir = dd
			wfDir = filepath.Join(dir, name)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("disabled workflow %q not found", name)
	}

	if err := os.Rename(disabledDir, wfDir); err != nil {
		return fmt.Errorf("enable workflow %q: %w", name, err)
	}

	return m.Load()
}

// Reload re-scans all workflow directories.
func (m *Manager) Reload() error { return m.Load() }

// WatchAndReload periodically reloads workflows when directory mtimes change.
func (m *Manager) WatchAndReload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastMod time.Time
	for _, dir := range m.dirs {
		if info, err := os.Stat(dir); err == nil {
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var latest time.Time
			for _, dir := range m.dirs {
				if info, err := os.Stat(dir); err == nil {
					if info.ModTime().After(latest) {
						latest = info.ModTime()
					}
				}
			}
			if latest.After(lastMod) {
				if err := m.Reload(); err != nil {
					fmt.Fprintf(os.Stderr, "[workflows] reload error: %v\n", err)
				}
				lastMod = latest
			}
		}
	}
}

// Name returns "workflow" — implements subsystem.Provider.
func (m *Manager) Name() string { return "workflow" }

// ContextMD returns a markdown section listing enabled workflows.
// Implements subsystem.Provider.
func (m *Manager) ContextMD() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.workflows) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Workflows\n")
	sb.WriteString("Workflows define exactly how a task must be handled. When a task matches a workflow below, you MUST use run_workflow and follow every step — do not improvise or skip steps.\n")

	names := make([]string, 0, len(m.workflows))
	for name := range m.workflows {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		w := m.workflows[name]
		fmt.Fprintf(&sb, "\n- **%s**: %s", name, w.Description)
	}

	sb.WriteString("\n\nUse load_workflow to inspect steps or run_workflow to execute. Do NOT handle workflow tasks manually.")
	return sb.String()
}

// ToolDefs returns 8 tools for workflow management.
// list_workflows, load_workflow, run_workflow are always available.
// create_workflow, update_workflow, delete_workflow, enable_workflow, disable_workflow
// are self-evolution only.
func (m *Manager) ToolDefs() []subsystem.ToolDef {
	return []subsystem.ToolDef{
		{
			Name:        "list_workflows",
			Description: "List all available workflows with their descriptions.",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler:       m.handleList,
			SelfEvolution: false,
		},
		{
			Name:        "load_workflow",
			Description: "Load a workflow's full content including all steps. Use this before run_workflow to understand the exact workflow steps.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Workflow name to load"},
				},
				"required": []string{"name"},
			},
			Handler:       m.handleLoad,
			SelfEvolution: false,
		},
		{
			Name:        "run_workflow",
			Description: "Execute a workflow by name. Loads the workflow content and returns the steps for you to follow. You MUST follow every step exactly — do not improvise or skip steps.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Workflow name to run"},
				},
				"required": []string{"name"},
			},
			Handler:       m.handleRun,
			SelfEvolution: false,
		},
		{
			Name:        "create_workflow",
			Description: "Create a new workflow with the given name, description, and markdown content. Workflows define step-by-step instructions the LLM must follow.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Unique workflow name"},
					"description": map[string]any{"type": "string", "description": "Brief description of the workflow's purpose"},
					"content":     map[string]any{"type": "string", "description": "Full markdown content with steps and instructions"},
				},
				"required": []string{"name", "description", "content"},
			},
			Handler:       m.handleCreate,
			SelfEvolution: false,
		},
		{
			Name:        "update_workflow",
			Description: "Update an existing workflow's description and content. If the workflow does not exist, it will be created.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Workflow name to update"},
					"description": map[string]any{"type": "string", "description": "Updated description"},
					"content":     map[string]any{"type": "string", "description": "Updated markdown content with steps"},
				},
				"required": []string{"name", "description", "content"},
			},
			Handler:       m.handleUpdate,
			SelfEvolution: false,
		},
		{
			Name:        "delete_workflow",
			Description: "Permanently delete a workflow by name. This cannot be undone — use disable_workflow instead to preserve the files.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Workflow name to delete"},
				},
				"required": []string{"name"},
			},
			Handler:       m.handleDelete,
			SelfEvolution: true,
		},
		{
			Name:        "enable_workflow",
			Description: "Enable a previously disabled workflow. Restores the workflow directory from .disabled/ and reloads it.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Workflow name to enable"},
				},
				"required": []string{"name"},
			},
			Handler:       m.handleEnable,
			SelfEvolution: true,
		},
		{
			Name:        "disable_workflow",
			Description: "Disable a workflow by removing it from memory and renaming its directory to .disabled/. The workflow can be re-enabled later using enable_workflow.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Workflow name to disable"},
				},
				"required": []string{"name"},
			},
			Handler:       m.handleDisable,
			SelfEvolution: true,
		},
	}
}

// ---- Tool handlers ----

type wfParams struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
}

func (m *Manager) handleList(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	wfs := m.List()
	if len(wfs) == 0 {
		return &mcp.ToolResult{Content: "No workflows available. Use create_workflow to create one."}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d workflow(s) available:\n", len(wfs))
	for _, w := range wfs {
		fmt.Fprintf(&sb, "- %s: %s\n", w.Name, w.Description)
	}
	sb.WriteString("\nUse load_workflow to inspect steps or run_workflow to execute.")
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (m *Manager) handleLoad(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}

	w, ok := m.Get(params.Name)
	if !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q not found. Use list_workflows to see available workflows.", params.Name), IsError: true}, nil
	}

	result := fmt.Sprintf("# Workflow: %s\n%s\n\n---\nLoaded workflow %q. Follow the steps above exactly — do not improvise or skip steps.", w.Name, w.Content, w.Name)
	return &mcp.ToolResult{Content: result}, nil
}

func (m *Manager) handleRun(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}

	w, ok := m.Get(params.Name)
	if !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q not found. Use list_workflows to see available workflows.", params.Name), IsError: true}, nil
	}

	result := fmt.Sprintf("# Workflow: %s\n%s\n\n---\nExecuting workflow %q. Follow each step exactly — do not improvise or skip steps.", w.Name, w.Content, w.Name)
	return &mcp.ToolResult{Content: result}, nil
}

func (m *Manager) handleCreate(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Workflow content is required.", IsError: true}, nil
	}
	if err := m.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to create workflow %q: %v", params.Name, err), IsError: true}, nil
	}
	zap.S().Infow("workflow created", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q created successfully.", params.Name)}, nil
}

func (m *Manager) handleUpdate(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Workflow content is required.", IsError: true}, nil
	}
	if err := m.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to update workflow %q: %v", params.Name, err), IsError: true}, nil
	}
	zap.S().Infow("workflow updated", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q updated successfully.", params.Name)}, nil
}

func (m *Manager) handleDelete(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}
	if _, ok := m.Get(params.Name); !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q not found.", params.Name), IsError: true}, nil
	}
	if err := m.Unregister(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to delete workflow %q: %v", params.Name, err), IsError: true}, nil
	}
	zap.S().Infow("workflow deleted", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q deleted permanently.", params.Name)}, nil
}

func (m *Manager) handleEnable(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}
	if err := m.Enable(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to enable workflow %q: %v", params.Name, err), IsError: true}, nil
	}
	zap.S().Infow("workflow enabled", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q enabled and loaded.", params.Name)}, nil
}

func (m *Manager) handleDisable(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params wfParams
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Workflow name is required.", IsError: true}, nil
	}
	if err := m.Disable(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to disable workflow %q: %v", params.Name, err), IsError: true}, nil
	}
	zap.S().Infow("workflow disabled", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Workflow %q disabled. Use enable_workflow to re-enable it.", params.Name)}, nil
}

// ---- Frontmatter parsing ----

// parseWorkflowFile parses a WORKFLOW.md file with optional YAML frontmatter.
func parseWorkflowFile(data []byte, dirname string) *Workflow {
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	wf := &Workflow{
		Name:    dirname,
		Content: content,
	}

	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx > 0 {
			frontmatter := rest[:endIdx]
			body := strings.TrimSpace(rest[endIdx+4:])

			var fm struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			dec := yaml.NewDecoder(strings.NewReader(frontmatter))
			dec.KnownFields(true)
			if err := dec.Decode(&fm); err == nil {
				if fm.Name != "" {
					wf.Name = fm.Name
				}
				wf.Description = fm.Description
			}

			if body != "" {
				wf.Content = body
			}
		}
	}

	return wf
}
