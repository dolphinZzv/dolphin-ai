package transport

import (
	"dolphin/internal/config"
)

// Factory creates a Transport from the global configuration.
type Factory func(cfg *config.Config) (Transport, error)

var factories = map[string]Factory{}

// Register registers a transport factory by name. Called from init() in sub-packages.
func Register(name string, f Factory) {
	factories[name] = f
}

// Factories returns a snapshot of all registered transport factories.
func Factories() map[string]Factory {
	out := make(map[string]Factory, len(factories))
	for k, v := range factories {
		out[k] = v
	}
	return out
}
