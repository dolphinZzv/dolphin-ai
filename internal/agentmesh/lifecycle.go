package agentmesh

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/event"
)

// LifecycleManager periodically health-checks known remote agents via
// agents/ping and updates their status in the registry. It emits events on
// disconnect/reconnect and powers graceful shutdown.
type LifecycleManager struct {
	mesh      *AgentMesh
	registry  *Registry
	interval  time.Duration // heartbeat interval, default 30s
	timeout   time.Duration // miss threshold, default 3x interval
	logger    *zap.Logger
	eventBus  *event.Bus

	mu     sync.Mutex
	cancel context.CancelFunc
	// missCount tracks consecutive ping failures per agent addr.
	miss map[string]int
}

// NewLifecycleManager builds a LifecycleManager over the mesh's registry.
func NewLifecycleManager(mesh *AgentMesh, interval, timeout time.Duration, bus *event.Bus, logger *zap.Logger) *LifecycleManager {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = 3 * interval
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LifecycleManager{
		mesh:     mesh,
		registry: mesh.Registry(),
		interval: interval,
		timeout:  timeout,
		logger:   logger,
		eventBus: bus,
		miss:     map[string]int{},
	}
}

// Start launches the background heartbeat loop. It is idempotent.
func (l *LifecycleManager) Start(ctx context.Context) {
	l.mu.Lock()
	if l.cancel != nil {
		l.mu.Unlock()
		return
	}
	cctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	l.mu.Unlock()

	go l.loop(cctx)
}

// Stop cancels the heartbeat loop. Idempotent.
func (l *LifecycleManager) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}

// Shutdown performs graceful shutdown: stop heartbeats, then (Phase 4 stub)
// it would send MsgCancel to running tasks, wait a grace period, and kill
// spawned children. Spawner cleanup is handled by the Spawner itself.
func (l *LifecycleManager) Shutdown(ctx context.Context) error {
	l.Stop()
	if l.mesh != nil {
		return l.mesh.Shutdown()
	}
	return nil
}

func (l *LifecycleManager) loop(ctx context.Context) {
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.checkAll(ctx)
		}
	}
}

// checkAll pings every running remote agent and updates status.
func (l *LifecycleManager) checkAll(ctx context.Context) {
	cards := l.registry.ListRunning()
	var wg sync.WaitGroup
	for _, c := range cards {
		wg.Add(1)
		go func(c AgentCard) {
			defer wg.Done()
			l.checkOne(ctx, c)
		}(c)
	}
	wg.Wait()
}

func (l *LifecycleManager) checkOne(ctx context.Context, c AgentCard) {
	if c.Addr == "" || c.Addr == l.mesh.Card().Addr {
		return // skip self and addr-less cards
	}
	client, err := l.mesh.clientFor(ctx, c.Addr)
	if err != nil {
		l.recordMiss(c, "negotiate failed: "+err.Error())
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx); err != nil {
		l.recordMiss(c, err.Error())
		return
	}
	// success → reset miss count, ensure running, emit reconnect if was down.
	l.mu.Lock()
	wasDown := l.miss[c.Addr] >= 3
	l.miss[c.Addr] = 0
	l.mu.Unlock()
	if wasDown {
		l.registry.Upsert(AgentCard{
			Name: c.Name, Addr: c.Addr, Capabilities: c.Capabilities,
			Status: AgentRunning, MaxLoad: c.MaxLoad, ProtoVersion: c.ProtoVersion,
		})
		l.publish("agent.reconnected", c.Name)
	}
}

func (l *LifecycleManager) recordMiss(c AgentCard, cause string) {
	l.mu.Lock()
	l.miss[c.Addr]++
	count := l.miss[c.Addr]
	l.mu.Unlock()
	if count >= 3 {
		// mark unavailable
		l.registry.Upsert(AgentCard{
			Name: c.Name, Addr: c.Addr, Capabilities: c.Capabilities,
			Status: AgentError, MaxLoad: c.MaxLoad, ProtoVersion: c.ProtoVersion,
		})
		l.publish("agent.disconnected", c.Name+" ("+cause+")")
	}
}

func (l *LifecycleManager) publish(t, msg string) {
	if l.eventBus == nil {
		return
	}
	l.eventBus.Publish(context.Background(), event.Event{
		Type:      event.Type(t),
		Timestamp: time.Now(),
		Payload:   map[string]any{"agent": msg},
	})
}
