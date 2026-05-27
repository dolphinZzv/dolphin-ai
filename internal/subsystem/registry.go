// Package subsystem provides a unified registration mechanism for pluggable
// subsystems. Each subsystem can inject context into the LLM system prompt
// and register LLM-available tools via the global registry.
//
// Usage:
//
//	import "dolphin/internal/subsystem"
//
//	// At startup:
//	subsystem.Register(myProvider)
//
//	// In context builder:
//	md := subsystem.ContextMD()
//
//	// In tool registration:
//	for _, td := range subsystem.ToolDefs() { ... }
package subsystem

import (
	"context"
	"encoding/json"
	"sync"

	"dolphin/internal/mcp"
)

// ToolDef describes a tool that a subsystem provides to the LLM.
type ToolDef struct {
	Name          string
	Description   string
	Schema        map[string]any
	Handler       func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error)
	SelfEvolution bool // only registered when cfg.Flags.SelfEvolution is true
}

// Provider is the interface each subsystem must implement.
type Provider interface {
	// Name returns the unique subsystem name (e.g. "workflow").
	Name() string

	// ContextMD returns a complete markdown section (including heading) to inject
	// into the LLM system prompt. Return "" to skip injection.
	ContextMD() string

	// ToolDefs returns the list of tools the subsystem provides to the LLM.
	ToolDefs() []ToolDef
}

var (
	mu        sync.RWMutex
	providers []Provider
)

// ProviderWithSpec is an optional extension to Provider that contributes
// commands to the unified command registry instead of (or in addition to)
// returning ToolDefs. CommandSpecs returns a slice where each element is
// a *registry.CommandSpec (cast at the call site to avoid import cycle).
type ProviderWithSpec interface {
	Provider
	CommandSpecs() []any
}

// Providers returns a snapshot of all registered providers.
func Providers() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, len(providers))
	copy(out, providers)
	return out
}

// Register adds a provider to the global registry. Called at startup.
// Panics if a provider with the same name is already registered.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	for _, existing := range providers {
		if existing.Name() == p.Name() {
			panic("subsystem already registered: " + p.Name())
		}
	}
	providers = append(providers, p)
}

// ContextMD aggregates ContextMD from all registered providers.
// Sections are separated by two newlines.
func ContextMD() string {
	mu.RLock()
	defer mu.RUnlock()

	var parts []string
	for _, p := range providers {
		if md := p.ContextMD(); md != "" {
			parts = append(parts, md)
		}
	}
	if len(parts) == 0 {
		return ""
	}

	result := make([]byte, 0, 256)
	for i, part := range parts {
		if i > 0 {
			result = append(result, '\n', '\n')
		}
		result = append(result, part...)
	}
	return string(result)
}

// ToolDefs aggregates ToolDefs from all registered providers.
func ToolDefs() []ToolDef {
	mu.RLock()
	defer mu.RUnlock()

	var total int
	for _, p := range providers {
		total += len(p.ToolDefs())
	}
	if total == 0 {
		return nil
	}

	defs := make([]ToolDef, 0, total)
	for _, p := range providers {
		defs = append(defs, p.ToolDefs()...)
	}
	return defs
}
