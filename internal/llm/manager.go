package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Manager holds multiple providers and routes requests by model name.
// It implements the Provider interface, acting as a proxy layer.
type Manager struct {
	mu sync.RWMutex

	providers  map[string]Provider // provider name → Provider
	modelIndex map[string]string   // model name → provider name
	active     string              // current active model name
	models     []ModelConfig       // cached aggregated model list
}

// NewManager creates an empty Manager.
func NewManager() *Manager {
	return &Manager{
		providers:  make(map[string]Provider),
		modelIndex: make(map[string]string),
	}
}

// Name returns "manager".
func (m *Manager) Name() string { return "manager" }

// AddProvider registers a provider and indexes its models.
func (m *Manager) AddProvider(name string, provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.providers[name] = provider

	// Index models from this provider.
	models, err := provider.Models(context.Background())
	if err != nil {
		return
	}
	for _, mc := range models {
		qualified := name + "/" + mc.Name
		// Prefer qualified name if short name collides.
		if existing, ok := m.modelIndex[mc.Name]; ok && existing != name {
			// Conflict — keep both under qualified keys.
			m.modelIndex[qualified] = name
		} else {
			m.modelIndex[mc.Name] = name
		}
		m.modelIndex[qualified] = name
	}
	m.rebuildModelListLocked()
}

// resolveModel converts a model name to a provider name.
// It checks qualified names (provider/model) first, then short names.
func (m *Manager) resolveModel(modelName string) (string, error) {
	// Try exact match first.
	if prov, ok := m.modelIndex[modelName]; ok {
		return prov, nil
	}
	// Try matching on the short name (after provider/ prefix).
	if _, after, found := strings.Cut(modelName, "/"); found {
		if prov, ok := m.modelIndex[after]; ok {
			return prov, nil
		}
	}
	return "", fmt.Errorf("llm: unknown model %q", modelName)
}

// ActiveModel returns the current active model name.
func (m *Manager) ActiveModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// SetActiveModel switches the active model if it exists.
func (m *Manager) SetActiveModel(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	providerName, err := m.resolveModel(name)
	if err != nil {
		return err
	}

	// Use qualified name (provider/name) if the model list uses it due to
	// name collisions across providers.
	qualified := providerName + "/" + name
	for _, mc := range m.models {
		if mc.Name == qualified {
			m.active = qualified
			return nil
		}
	}
	m.active = name
	return nil
}

// CompleteStream routes to the provider that serves the requested model.
func (m *Manager) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	modelName := req.Model
	if modelName == "" {
		modelName = m.activeModel()
	}

	providerName, err := m.resolveModel(modelName)
	if err != nil {
		return nil, err
	}

	provider, ok := m.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("llm: provider %q not found", providerName)
	}

	// Use the original API model name, not the qualified routing name.
	apiModel := modelName
	for _, mc := range m.models {
		if mc.Name == modelName {
			apiModel = mc.Model
			if mc.Temperature != 0 {
				req.Temperature = mc.Temperature
			}
			if mc.MaxTokens != 0 {
				req.MaxTokens = mc.MaxTokens
			}
			if mc.Timeout != 0 {
				req.Timeout = mc.Timeout
			}
			if mc.ReasoningEffort != "" {
				req.ReasoningEffort = mc.ReasoningEffort
			}
			break
		}
	}
	req.Model = apiModel
	return provider.CompleteStream(ctx, req)
}

// Models returns all available models across all providers.
func (m *Manager) Models(ctx context.Context) ([]ModelConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ModelConfig, len(m.models))
	copy(result, m.models)
	return result, nil
}

// activeModel returns the active model. Caller must hold at least RLock.
func (m *Manager) activeModel() string {
	return m.active
}

// rebuildModelListLocked rebuilds the cached model list. Caller must hold m.mu.
func (m *Manager) rebuildModelListLocked() {
	var all []ModelConfig
	seen := make(map[string]int) // model name → index in all

	for provName, prov := range m.providers {
		models, err := prov.Models(context.Background())
		if err != nil {
			continue
		}
		for _, mc := range models {
			qualified := provName + "/" + mc.Name
			mc.Provider = provName

			if idx, ok := seen[mc.Name]; ok {
				// Name collision — disambiguate both.
				existing := all[idx]
				existing.Name = existing.Provider + "/" + existing.Name
				all[idx] = existing
				mc.Name = qualified
			} else {
				seen[mc.Name] = len(all)
			}
			all = append(all, mc)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Provider != all[j].Provider {
			return all[i].Provider < all[j].Provider
		}
		return all[i].Name < all[j].Name
	})
	m.models = all
}
