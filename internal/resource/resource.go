// Package resource provides system resource monitoring with threshold-based
// event emission. It tracks CPU, memory, disk, and network bandwidth usage
// and emits events when usage crosses 20%/40%/60%/80% thresholds.
package resource

import (
	"dolphin/internal/config"
	"dolphin/internal/event"
)

// ResourceType identifies a monitored resource.
type ResourceType string

const (
	TypeCPU     ResourceType = "resource:cpu"
	TypeMemory  ResourceType = "resource:memory"
	TypeDisk    ResourceType = "resource:disk"
	TypeNetwork ResourceType = "resource:network"
)

// Direction indicates whether usage crossed a threshold going up or down.
type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

// DefaultThresholds is the default threshold list when none are configured.
var DefaultThresholds = []float64{20, 40, 60, 80}

// bracketIndex returns the index into thresholds for a given usage percentage.
// Returns -1 if below the first threshold. thresholds must be sorted ascending.
func bracketIndex(pct float64, thresholds []float64) int {
	for i, t := range thresholds {
		if pct < t {
			return i - 1
		}
	}
	return len(thresholds) - 1
}

// thresholdAt returns the threshold value at the given bracket index.
// Returns 0 for bracket -1 (below all thresholds).
func thresholdAt(idx int, thresholds []float64) float64 {
	if idx < 0 || idx >= len(thresholds) {
		return 0
	}
	return thresholds[idx]
}

// ToEventType converts a ResourceType to the corresponding event.Type.
func (rt ResourceType) ToEventType() event.Type {
	return event.Type(string(rt))
}

// ResourceEventData builds the common event data map for a resource event.
func ResourceEventData(resource ResourceType, threshold float64, direction Direction, current float64, detail map[string]any) map[string]any {
	data := map[string]any{
		"resource":  string(resource),
		"threshold": threshold,
		"direction": string(direction),
		"current":   current,
	}
	for k, v := range detail {
		data[k] = v
	}
	return data
}

// Config configures the resource monitor.
type Config struct {
	Enabled      bool
	Interval     string
	DiskPaths    []string
	MaxBandwidth uint64
	Thresholds   []float64 // percentage thresholds, must be sorted ascending; defaults to [20, 40, 60, 80]
}

// ConfigFrom converts a config.ResourceConfig to resource.Config.
// This decouples the resource package from direct config struct dependencies
// while allowing conversion at the wiring layer.
func ConfigFrom(cfg config.ResourceConfig) Config {
	paths := cfg.DiskPaths
	if len(paths) == 0 {
		paths = []string{"/"}
	}
	bw := cfg.MaxBandwidth
	if bw == 0 {
		bw = 125_000_000 // 1 Gbps
	}
	thresholds := cfg.Thresholds
	if len(thresholds) == 0 {
		thresholds = DefaultThresholds
	}
	return Config{
		Enabled:      cfg.Enabled,
		Interval:     cfg.Interval,
		DiskPaths:    paths,
		MaxBandwidth: bw,
		Thresholds:   thresholds,
	}
}
