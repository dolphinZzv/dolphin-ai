package resource

import (
	"context"
	"strings"
	"sync"
	"time"

	"dolphin/internal/event"

	"go.uber.org/zap"
)

// Monitor periodically samples system resources and emits events when usage
// crosses the defined thresholds (20%/40%/60%/80%) in either direction.
type Monitor struct {
	cfg    Config
	sampler Sampler
	events *event.EventBus

	mu        sync.Mutex
	closed    bool

	// Current bracket index for each resource (-1 = below all thresholds)
	cpuBracket     int
	memBracket     int
	diskBrackets   map[string]int
	netBracket     int

	// Network bandwidth percentage requires max_bandwidth config
	maxBandwidth uint64
}

// New creates a new resource monitor. If the sampler is nil, a default Linux
// sampler is created (or a stub on non-Linux platforms).
func New(cfg Config, events *event.EventBus) *Monitor {
	if len(cfg.Thresholds) == 0 {
		cfg.Thresholds = DefaultThresholds
	}
	if cfg.MaxBandwidth == 0 {
		cfg.MaxBandwidth = 125_000_000 // default: 1 Gbps in bytes/sec
	}
	return &Monitor{
		cfg:          cfg,
		sampler:      NewSampler(),
		events:       events,
		cpuBracket:   -1,
		memBracket:   -1,
		diskBrackets: make(map[string]int),
		netBracket:   -1,
		maxBandwidth: cfg.MaxBandwidth,
	}
}

// SetSampler replaces the sampler (used in tests).
func (m *Monitor) SetSampler(s Sampler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sampler != nil {
		m.sampler.Close()
	}
	m.sampler = s
}

// Start begins periodic resource monitoring. It blocks until ctx is cancelled.
// The sampling interval is parsed from cfg.Interval (default 30s).
func (m *Monitor) Start(ctx context.Context) error {
	interval := parseInterval(m.cfg.Interval)
	if interval <= 0 {
		interval = 30 * time.Second
	}

	zap.S().Infow("resource monitor started",
		"interval", interval,
		"thresholds", m.cfg.Thresholds,
		"disk_paths", m.cfg.DiskPaths,
		"max_bandwidth", m.maxBandwidth,
	)

	// Initial sample immediately
	m.sampleAndCheck(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Close()
			return nil
		case <-ticker.C:
			m.sampleAndCheck(ctx)
		}
	}
}

// Close stops the monitor and releases resources.
func (m *Monitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	if m.sampler != nil {
		m.sampler.Close()
	}
}

// sampleAndCheck samples all resources and emits events for threshold crossings.
func (m *Monitor) sampleAndCheck(ctx context.Context) {
	// CPU
	if pct, err := m.sampler.CPU(); err == nil {
		m.checkThreshold(ctx, TypeCPU, pct, &m.cpuBracket, nil)
	} else {
		zap.S().Debugw("resource: cpu sample failed", "error", err)
	}

	// Memory
	if pct, err := m.sampler.Memory(); err == nil {
		m.checkThreshold(ctx, TypeMemory, pct, &m.memBracket, nil)
	} else {
		zap.S().Debugw("resource: memory sample failed", "error", err)
	}

	// Disk (each configured path)
	for _, path := range m.cfg.DiskPaths {
		if path == "" {
			continue
		}
		pct, err := m.sampler.Disk(path)
		if err != nil {
			zap.S().Debugw("resource: disk sample failed", "path", path, "error", err)
			continue
		}
		idx := m.diskIndex(path)
		m.checkThreshold(ctx, TypeDisk, pct, idx, map[string]any{"path": path})
	}

	// Network bandwidth
	if pct, err := m.networkPercent(); err == nil {
		m.checkThreshold(ctx, TypeNetwork, pct, &m.netBracket, nil)
	} else {
		zap.S().Debugw("resource: network sample failed", "error", err)
	}
}

// networkPercent returns the network bandwidth usage as a percentage (0-100)
// of the configured max bandwidth. Uses the higher of rx/tx rate.
func (m *Monitor) networkPercent() (float64, error) {
	rx, tx, err := m.sampler.Network()
	if err != nil {
		return 0, err
	}
	rate := rx
	if tx > rate {
		rate = tx
	}
	if m.maxBandwidth == 0 {
		return 0, nil
	}
	pct := 100.0 * rate / float64(m.maxBandwidth)
	if pct > 100 {
		pct = 100
	}
	return pct, nil
}

// checkThreshold checks if the current percentage crosses a threshold boundary
// and emits an event if so. bracketPtr is a pointer to the stored bracket index.
func (m *Monitor) checkThreshold(ctx context.Context, rtype ResourceType, pct float64, bracketPtr *int, extra map[string]any) {
	if bracketPtr == nil {
		return
	}
	thresholds := m.cfg.Thresholds
	newBracket := bracketIndex(pct, thresholds)
	oldBracket := *bracketPtr

	if newBracket == oldBracket {
		return // no crossing
	}

	// Determine which threshold was crossed and in which direction.
	// If moving up, the crossed threshold is at newBracket index.
	// If moving down, the crossed threshold is at oldBracket index.
	var crossedThreshold float64
	var dir Direction

	if newBracket > oldBracket {
		dir = DirectionUp
		crossedThreshold = thresholdAt(newBracket, thresholds)
	} else {
		dir = DirectionDown
		crossedThreshold = thresholdAt(oldBracket, thresholds)
	}

	// Update stored bracket
	*bracketPtr = newBracket
	detail := make(map[string]any)
	for k, v := range extra {
		detail[k] = v
	}
	detail["bracket_old"] = oldBracket
	detail["bracket_new"] = newBracket

	if m.events != nil && ctx != nil {
		m.events.Emit(ctx, event.Event{
			Type: rtype.ToEventType(),
			Data: ResourceEventData(rtype, crossedThreshold, dir, pct, detail),
		})
		zap.S().Infow("resource threshold crossed",
			"resource", rtype,
			"threshold", crossedThreshold,
			"direction", dir,
			"current", pct,
		)
	}
}

// diskIndex returns the bracket pointer for a disk path, creating it if needed.
func (m *Monitor) diskIndex(path string) *int {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx, ok := m.diskBrackets[path]
	if !ok {
		idx = -1
		m.diskBrackets[path] = idx
	}
	return &idx
}

// parseInterval parses a duration string like "30s", "1m", "5s".
func parseInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		zap.S().Warnw("resource: invalid interval, using default 30s", "value", s, "error", err)
		return 30 * time.Second
	}
	return d
}
