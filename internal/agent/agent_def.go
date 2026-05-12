package agent

import (
	"os"
	"path/filepath"

	"dolphin/internal/config"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// LoadAgentDefs scans the agents directory and loads all agent definitions.
// Returns a map of agent name → AgentDef, and any error encountered.
// If the directory doesn't exist, returns an empty map (backward compat).
func LoadAgentDefs(agentsDir string) (map[string]*AgentDef, error) {
	defs := make(map[string]*AgentDef)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			zap.S().Infow("agents directory not found, using default agents only",
				"dir", agentsDir,
			)
			return defs, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		yamlPath := filepath.Join(agentsDir, name, "agent.yaml")

		if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
			zap.S().Debugw("skipping agent directory, no agent.yaml", "name", name)
			continue
		}

		def, err := loadAgentYAML(yamlPath)
		if err != nil {
			zap.S().Errorw("failed to load agent definition", "name", name, "error", err)
			continue
		}
		def.Name = name

		// Resolve workspace directory
		if def.Workspace == "" {
			def.Workspace = filepath.Join(filepath.Dir(agentsDir), "workspaces", name)
		}
		// Ensure workspace exists
		os.MkdirAll(def.Workspace, 0755)

		defs[name] = def
		zap.S().Infow("loaded agent definition",
			"name", name,
			"tools", def.Tools,
			"workspace", def.Workspace,
		)
	}

	return defs, nil
}

func loadAgentYAML(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def AgentDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, err
	}

	return &def, nil
}

// AgentDir returns the path to an agent's context directory.
func AgentDir(agentsDir, name string) string {
	return filepath.Join(agentsDir, name)
}

// AgentWorkspace returns the resolved workspace path for an agent.
func AgentWorkspace(cfg *config.PoolConfig, name string) string {
	return filepath.Join(cfg.WorkspaceDir, name)
}

// TempAgentWorkspace returns a temporary workspace path for a coordinator-created agent.
func TempAgentWorkspace(cfg *config.PoolConfig, name string) string {
	return filepath.Join(cfg.WorkspaceDir, "temp-"+name)
}
