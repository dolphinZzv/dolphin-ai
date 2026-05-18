package resource

import (
	"context"
	"sync"
	"testing"
	"time"

	"dolphin/internal/event"
)

// mockSampler implements Sampler for testing.
type mockSampler struct {
	mu           sync.Mutex
	cpu          float64
	memory       float64
	disk         map[string]float64
	netRx, netTx float64
	cpuErr       error
	memErr       error
	diskErr      error
	netErr       error
	closed       bool
}

func newMockSampler() *mockSampler {
	return &mockSampler{
		disk: make(map[string]float64),
	}
}

func (m *mockSampler) CPU() (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cpu, m.cpuErr
}

func (m *mockSampler) Memory() (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.memory, m.memErr
}

func (m *mockSampler) Disk(path string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.disk[path]
	return v, m.diskErr
}

func (m *mockSampler) Network() (float64, float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.netRx, m.netTx, m.netErr
}

func (m *mockSampler) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func (m *mockSampler) setCPU(pct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cpu = pct
}

func (m *mockSampler) setMemory(pct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory = pct
}

func (m *mockSampler) setDisk(path string, pct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disk[path] = pct
}

// collector uses a channel to synchronize on events.
type collector struct {
	mu     sync.Mutex
	events []event.Event
	ch     chan struct{} // signaled on each add
}

func newCollector() *collector {
	return &collector{ch: make(chan struct{}, 100)}
}

func (c *collector) add(evt event.Event) {
	c.mu.Lock()
	c.events = append(c.events, evt)
	c.mu.Unlock()
	c.ch <- struct{}{}
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *collector) waitFor(min int, timeout time.Duration) bool {
	for c.count() < min {
		select {
		case <-c.ch:
		case <-time.After(timeout):
			return false
		}
	}
	return true
}

func (c *collector) hasEvent(match func(event.Event) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, evt := range c.events {
		if match(evt) {
			return true
		}
	}
	return false
}

func TestMonitorCPUThresholdUp(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeCPU.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{DiskPaths: []string{"/"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	// CPU at 10% stays below all thresholds → no event
	mock.setCPU(10)
	m.sampleAndCheck(ctx)

	// CPU at 30% crosses 20 (up)
	mock.setCPU(30)
	m.sampleAndCheck(ctx)

	if !col.waitFor(1, time.Second) {
		t.Fatal("expected at least 1 event crossing 20% threshold up")
	}

	evt := col.events[len(col.events)-1]
	if v, _ := evt.Data["threshold"].(float64); v != 20 {
		t.Errorf("threshold = %v, want 20", v)
	}
	if v, _ := evt.Data["direction"].(string); v != "up" {
		t.Errorf("direction = %q, want 'up'", v)
	}
}

func TestMonitorCPUThresholdDown(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeCPU.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{DiskPaths: []string{"/"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	// CPU at 70% enters bracket 2 (60-79), emits up@60 event
	mock.setCPU(70)
	m.sampleAndCheck(ctx)

	// CPU at 50% crosses 60 (down)
	mock.setCPU(50)
	m.sampleAndCheck(ctx)

	// Wait for at least 2 events (up@60 + down@60)
	if !col.waitFor(2, time.Second) {
		t.Fatalf("expected 2 events (up + down), got %d", col.count())
	}

	found := col.hasEvent(func(evt event.Event) bool {
		return evt.Data["direction"] == "down" && evt.Data["threshold"] == float64(60)
	})
	if !found {
		t.Error("expected down event at threshold 60, not found")
	}
}

func TestMonitorNoChangeWithinBracket(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeCPU.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{DiskPaths: []string{"/"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	// First sample at 25% enters bracket 0 → emits crossing event for 20 (up)
	mock.setCPU(25)
	m.sampleAndCheck(ctx)
	col.waitFor(1, time.Second)

	// Same bracket at 35% → no crossing
	mock.setCPU(35)
	m.sampleAndCheck(ctx)

	// Wait a bit to see if any extra events arrive
	time.Sleep(100 * time.Millisecond)
	before := col.count()

	// Still same bracket at 33% → no crossing
	mock.setCPU(33)
	m.sampleAndCheck(ctx)

	if col.waitFor(before+1, 300*time.Millisecond) {
		t.Errorf("expected no event when staying in same bracket, got %d events", col.count()-before)
	}
}

func TestMonitorMemoryCrossing(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeMemory.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{DiskPaths: []string{"/"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	// Memory jumps from 10% to 85% → crosses multiple thresholds
	mock.setMemory(10)
	m.sampleAndCheck(ctx)
	before := col.count()

	mock.setMemory(85)
	m.sampleAndCheck(ctx)

	if !col.waitFor(before+1, time.Second) {
		t.Error("expected events when memory jumps multiple thresholds")
	}
}

func TestMonitorDiskCrossing(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeDisk.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{DiskPaths: []string{"/", "/data"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	mock.setDisk("/", 15)
	mock.setDisk("/data", 15)
	m.sampleAndCheck(ctx)
	before := col.count()

	mock.setDisk("/", 50)
	m.sampleAndCheck(ctx)

	if !col.waitFor(before+1, time.Second) {
		t.Error("expected disk crossing events")
	}

	hasPath := col.hasEvent(func(evt event.Event) bool {
		p, ok := evt.Data["path"]
		return ok && p == "/"
	})
	if !hasPath {
		t.Error("expected disk event to contain 'path' in Data")
	}
}

func TestMonitorNetworkCrossing(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()
	bus.On(TypeNetwork.ToEventType(), func(ctx context.Context, evt event.Event) {
		col.add(evt)
	})

	mock := newMockSampler()
	m := New(Config{MaxBandwidth: 100_000_000}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	// 10 MB/s = 10%
	mock.netRx = 10_000_000
	mock.netTx = 5_000_000
	m.sampleAndCheck(ctx)
	before := col.count()

	// 50 MB/s = 50%
	mock.netRx = 50_000_000
	mock.netTx = 30_000_000
	m.sampleAndCheck(ctx)

	if !col.waitFor(before+1, time.Second) {
		t.Error("expected network crossing events")
	}
}

func TestMonitorSampleErrors(t *testing.T) {
	bus := event.NewEventBus(64)
	col := newCollector()

	for _, et := range []event.Type{TypeCPU.ToEventType(), TypeMemory.ToEventType(), TypeDisk.ToEventType(), TypeNetwork.ToEventType()} {
		bus.On(et, func(ctx context.Context, evt event.Event) {
			col.add(evt)
		})
	}

	mock := newMockSampler()
	mock.cpuErr = errTest
	mock.memErr = errTest
	mock.diskErr = errTest
	mock.netErr = errTest

	m := New(Config{DiskPaths: []string{"/"}}, bus)
	m.SetSampler(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.Close()

	m.sampleAndCheck(ctx)

	if col.waitFor(1, 200*time.Millisecond) {
		t.Errorf("expected 0 events on sampler errors, got %d", col.count())
	}
}

func TestMonitorClose(t *testing.T) {
	mock := newMockSampler()
	m := New(Config{}, event.NewEventBus(64))
	m.SetSampler(mock)

	m.Close()
	if !mock.closed {
		t.Error("expected sampler to be closed")
	}
	// Double close should not panic
	m.Close()
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"30s", 30 * time.Second},
		{"1m", time.Minute},
		{"5s", 5 * time.Second},
		{"", 0},
		{" 10s ", 10 * time.Second},
		{"invalid", 30 * time.Second},
	}
	for _, tt := range tests {
		got := parseInterval(tt.input)
		if got != tt.want {
			t.Errorf("parseInterval(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
