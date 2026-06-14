package workflow

import (
	"fmt"
	"strings"

	"github.com/rs/xid"
	"gopkg.in/yaml.v3"
)

// Parse reads and validates a workflow YAML file.
func Parse(data []byte) (*WorkflowSpec, error) {
	var spec WorkflowSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("workflow: invalid YAML: %w", err)
	}
	if err := Validate(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// Validate checks a WorkflowSpec for structural correctness.
func Validate(spec *WorkflowSpec) error {
	if spec.Version != "1" {
		return fmt.Errorf("workflow: unsupported version %q, expected \"1\"", spec.Version)
	}
	if spec.Name == "" {
		return fmt.Errorf("workflow: name is required")
	}
	if len(spec.Steps) == 0 {
		return fmt.Errorf("workflow: at least one step is required")
	}

	ids := make(map[string]bool)
	for i := range spec.Steps {
		s := &spec.Steps[i]
		if s.ID == "" {
			return fmt.Errorf("workflow: step %d is missing id", i+1)
		}
		if ids[s.ID] {
			return fmt.Errorf("workflow: duplicate step id %q", s.ID)
		}
		ids[s.ID] = true

		if s.Prompt == "" {
			return fmt.Errorf("workflow: step %q is missing prompt", s.ID)
		}
	}

	// Validate depends_on references and foreach targets.
	for i := range spec.Steps {
		s := &spec.Steps[i]
		for _, dep := range s.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("workflow: step %q depends on unknown step %q", s.ID, dep)
			}
			if dep == s.ID {
				return fmt.Errorf("workflow: step %q depends on itself", s.ID)
			}
		}

		if s.ForEach != "" {
			refStep := resolveForeachRef(s.ForEach)
			if refStep == "" {
				return fmt.Errorf("workflow: step %q has invalid foreach expression %q", s.ID, s.ForEach)
			}
			if !ids[refStep] {
				return fmt.Errorf("workflow: step %q foreach references unknown step %q", s.ID, refStep)
			}
		}
	}

	// Check for cycles.
	if err := detectCycle(spec); err != nil {
		return err
	}

	return nil
}

func resolveForeachRef(expr string) string {
	s := strings.TrimPrefix(expr, "$")
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func detectCycle(spec *WorkflowSpec) error {
	graph := make(map[string][]string)
	for _, s := range spec.Steps {
		graph[s.ID] = s.DependsOn
	}

	visited := make(map[string]int) // 0=white, 1=gray, 2=black
	var dfs func(id string) error
	dfs = func(id string) error {
		state, ok := visited[id]
		if !ok {
			visited[id] = 1 // gray
			for _, dep := range graph[id] {
				if err := dfs(dep); err != nil {
					return err
				}
			}
			visited[id] = 2 // black
			return nil
		}
		if state == 1 {
			return fmt.Errorf("workflow: cycle detected involving step %q", id)
		}
		return nil
	}

	// Start from root-like nodes (no dependents) to catch all cycles.
	for _, s := range spec.Steps {
		if _, ok := visited[s.ID]; !ok {
			if err := dfs(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// GenerateID creates a new unique identifier for workflow-related entities.
func GenerateID() string {
	return xid.New().String()
}
