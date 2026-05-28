package transport

import (
	"errors"

	"dolphin/internal/config"
)

// ConfigReloader is an optional interface that transports and other components
// can implement to receive config change notifications.
//
// OnConfigChange is called before the old actor is stopped. The handler can:
// - Update fields in-place (lock + swap)
// - Return ErrUnchanged if no relevant fields changed (skips actor restart)
// - Return ErrRequiresRestart if the component cannot hot-reload in-place
//
// When ErrRequiresRestart is returned, the ActorGroup stops the old actor and
// starts a new one with the updated config.
type ConfigReloader interface {
	OnConfigChange(oldCfg, newCfg *config.Config) error
}

// ErrUnchanged is returned by OnConfigChange when no relevant fields changed.
// The caller can skip unnecessary work (e.g., actor restart).
var ErrUnchanged = errors.New("config unchanged, no action needed")

// ErrRequiresRestart is returned by OnConfigChange when the component cannot
// hot-reload in-place and must be stopped and re-started.
var ErrRequiresRestart = errors.New("component must be restarted")
