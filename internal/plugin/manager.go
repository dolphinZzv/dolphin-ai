package plugin

import (
	"fmt"

	"dolphin/internal/event"
	"dolphin/internal/hook"

	"go.uber.org/zap"
)

// Manager handles plugin lifecycle: register, load scripts, activate.
type Manager struct {
	hooks   *hook.Registry
	bus     *event.EventBus
	plugins []Plugin
}

// NewManager creates a new Manager backed by the given hook registry and event bus.
func NewManager(hooks *hook.Registry, bus *event.EventBus) *Manager {
	return &Manager{
		hooks: hooks,
		bus:   bus,
	}
}

// Register adds a built-in (Go) plugin. Called before Activate.
func (m *Manager) Register(p Plugin) {
	m.plugins = append(m.plugins, p)
	zap.S().Debugw("plugin registered", "name", p.Name())
}

// LoadScripts loads script plugins from dir. Called before Activate.
func (m *Manager) LoadScripts(dir string) error {
	scriptPlugins, err := LoadScripts(dir)
	if err != nil {
		return fmt.Errorf("load script plugins: %w", err)
	}
	m.plugins = append(m.plugins, scriptPlugins...)
	return nil
}

// Activate calls Register(reg) on every registered plugin, which populates
// the hook registry and event bus. Call once after all Register/LoadScripts calls.
func (m *Manager) Activate() {
	if len(m.plugins) == 0 {
		zap.S().Debugw("plugin: no plugins loaded")
		return
	}

	reg := NewRegistry()
	for _, p := range m.plugins {
		p.Register(reg)
	}
	reg.ApplyTo(m.hooks, m.bus)

	zap.S().Infow("plugins activated",
		"count", len(m.plugins),
		"hooks", reg.HooksAdded,
		"events", reg.EventsAdded,
	)
}

// List returns the names of all loaded plugins.
func (m *Manager) List() []string {
	names := make([]string, len(m.plugins))
	for i, p := range m.plugins {
		names[i] = p.Name()
	}
	return names
}
