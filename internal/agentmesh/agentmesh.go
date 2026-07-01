package agentmesh

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"

	"dolphin/internal/event"
	"dolphin/internal/tool"
)

// AgentMesh is the multi-agent collaboration layer entry point.
//
// Phase 1 implements the sync Delegate path: route → negotiate → rate-limit →
// circuit-break → send → retry/fallback. Async, streaming, spawner, gossip and
// tool federation are later phases.
type AgentMesh struct {
	cfg     AgentConfig
	card    AgentCard // this agent's own card
	registry *Registry
	router   *Router
	metrics  *Metrics

	mu          sync.RWMutex
	clients     map[string]*A2AClient // addr → client
	breakers    map[string]*CircuitBreaker // addr → breaker
	limiter     *RateLimiter
	links       map[string]*SessionLink // child_session_id → link
	linkMu      sync.RWMutex
	eventBus    *event.Bus
	logger      *zap.Logger

	tasks *taskManager // async delegations (Phase 2)
	bgCtx context.Context
	spawner *Spawner // dynamic child-process spawner (Phase 2)
	toolReg *tool.Registry // for ToolMount registration (Phase 4)

	now func() time.Time
}

// NewAgentMesh builds an AgentMesh from config. eventBus may be nil.
func NewAgentMesh(cfg AgentConfig, eventBus *event.Bus, logger *zap.Logger) *AgentMesh {
	if logger == nil {
		logger = zap.NewNop()
	}
	reg := NewRegistry(cfg.Local, cfg.Remote, logger)
	router := NewRouter(reg, cfg.Fallback, logger)
	rl := NewRateLimiter(cfg.RateLimit.SendPerAgent, cfg.RateLimit.SendBurst)
	selfCard := AgentCard{
		Name:         cfg.Name,
		Addr:         cfg.ListenAddr,
		Capabilities: cfg.Capabilities,
		Status:       AgentRunning,
		ProtoVersion: localProto,
	}
	reg.Upsert(selfCard)
	m := &AgentMesh{
		cfg:      cfg,
		card:     selfCard,
		registry: reg,
		router:   router,
		metrics:  initMetrics(),
		clients:  map[string]*A2AClient{},
		breakers: map[string]*CircuitBreaker{},
		limiter:  rl,
		links:    map[string]*SessionLink{},
		eventBus: eventBus,
		logger:   logger,
		tasks:    newTaskManager(),
		bgCtx:    context.Background(),
		now:      func() time.Time { return time.Now() },
	}
	return m
}

// Enabled reports whether agent mesh is turned on.
func (m *AgentMesh) Enabled() bool { return m.cfg.Enabled }

// SetSpawner attaches a dynamic child-process spawner. Only call this when
// cfg.Spawner.Enabled is true and the parent has a dolphin binary path.
func (m *AgentMesh) SetSpawner(sp *Spawner) { m.spawner = sp }

// Spawner returns the attached spawner (nil if none).
func (m *AgentMesh) Spawner() *Spawner { return m.spawner }

// SetToolRegistry attaches the local tool registry, used by MountTools to
// register remote agents' tools as named sources (tool federation).
func (m *AgentMesh) SetToolRegistry(r *tool.Registry) { m.toolReg = r }

// Card returns this agent's own card.
func (m *AgentMesh) Card() AgentCard { return m.card }

// Registry exposes the underlying registry (for testing and tooling).
func (m *AgentMesh) Registry() *Registry { return m.registry }

// Register adds an agent to the registry.
func (m *AgentMesh) Register(card AgentCard) error {
	m.registry.Upsert(card)
	return nil
}

// Deregister removes an agent from the registry.
func (m *AgentMesh) Deregister(name string) error {
	m.registry.Deregister(name)
	return nil
}

// ListAgents returns all known agents.
func (m *AgentMesh) ListAgents() []AgentCard { return m.registry.List() }

// clientFor returns a cached A2AClient for the addr, creating + negotiating if
// necessary.
func (m *AgentMesh) clientFor(ctx context.Context, addr string) (*A2AClient, error) {
	m.mu.RLock()
	c, ok := m.clients[addr]
	m.mu.RUnlock()
	if ok {
		return c, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// double-check
	if c, ok := m.clients[addr]; ok {
		return c, nil
	}
	c, err := newClient(addr, m.cfg.TLS, m.logger)
	if err != nil {
		return nil, err
	}
	if err := c.Negotiate(ctx); err != nil {
		return nil, err
	}
	m.clients[addr] = c
	return c, nil
}

// newClient builds an A2AClient, using mTLS when configured.
func newClient(addr string, tlsConf TLSConfig, logger *zap.Logger) (*A2AClient, error) {
	if tlsConf.Enabled {
		return NewA2AClientWithTLS(addr, &tlsConf, logger)
	}
	return NewA2AClient(addr, logger), nil
}

// breakerFor returns the per-agent circuit breaker.
func (m *AgentMesh) breakerFor(addr string) *CircuitBreaker {
	m.mu.Lock()
	defer m.mu.Unlock()
	cb, ok := m.breakers[addr]
	if !ok {
		cb = NewCircuitBreaker(m.cfg.CircuitBreaker).withClock(m.now)
		m.breakers[addr] = cb
	}
	return cb
}

// Delegate synchronously delegates a task to a peer agent.
//
// Flow: route → (rate-limit) → (circuit-break) → send → retry → fallback.
// Returns the DelegateResult, or a *DelegateError on failure.
func (m *AgentMesh) Delegate(ctx context.Context, payload DelegatePayload) (*DelegateResult, error) {
	if !m.cfg.Enabled {
		return nil, &DelegateError{Code: ErrInternal, Message: "agent mesh disabled"}
	}
	if err := validatePayload(payload); err != nil {
		return nil, err
	}
	// depth check
	if m.cfg.MaxDelegationDepth > 0 && payload.DelegationDepth >= m.cfg.MaxDelegationDepth {
		return nil, &DelegateError{
			Code: ErrDepthExceeded,
			Message: fmt.Sprintf("delegation depth %d >= max %d", payload.DelegationDepth, m.cfg.MaxDelegationDepth),
		}
	}

	candidates, err := m.router.Route(payload)
	if err != nil {
		return nil, err
	}

	// Open a tracing span around the whole delegation (Phase 2). The trace
	// context propagates to the peer via A2AClient HTTP headers.
	ctx, span := m.startDelegateSpan(ctx, payload, candidates[0])
	defer span.End()

	// Apply timeout from payload or default.
	timeout := m.cfg.TaskTimeout
	if payload.Timeout != "" {
		if d, perr := time.ParseDuration(payload.Timeout); perr == nil {
			timeout = d
		}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var lastErr error
	for i, cand := range candidates {
		result, dErr := m.delegateTo(ctx, cand, payload)
		if dErr == nil {
			recordSpanResult(span, result)
			return result, nil
		}
		lastErr = dErr
		recordSpanError(span, dErr)
		// Fallback: only continue to next candidate if fallback is enabled and
		// the error is not retryable-as-is (retry already happened inside
		// delegateTo). ErrPermission / ErrBadPayload / ErrDepthExceeded are
		// not worth retrying on another agent.
		if !m.cfg.Fallback.Enabled {
			break
		}
		if i == len(candidates)-1 {
			break
		}
		if !shouldFallback(dErr) {
			break
		}
		m.logger.Warn("agentmesh: falling back to next candidate",
			zap.String("failed", cand.Name),
			zap.String("next", candidates[i+1].Name),
			zap.String("err", dErr.Error()),
		)
	}
	if lastErr == nil {
		lastErr = &DelegateError{Code: ErrInternal, Message: "no candidate"}
	}
	return nil, lastErr
}

// shouldFallback reports whether a failure warrants trying the next candidate.
func shouldFallback(err error) bool {
	var dErr *DelegateError
	if !errors.As(err, &dErr) {
		return true // unknown error → try fallback
	}
	switch dErr.Code {
	case ErrPermission, ErrBadPayload, ErrDepthExceeded:
		return false
	}
	return true
}

// delegateTo sends the payload to a single candidate, with retry + circuit
// breaker. It records breaker outcomes and respects rate limiting.
func (m *AgentMesh) delegateTo(ctx context.Context, cand AgentCard, payload DelegatePayload) (*DelegateResult, error) {
	delegateStart := m.now()
	addr := cand.Addr
	cb := m.breakerFor(addr)

	// Circuit breaker gate (OPEN short-circuits).
	if !cb.Allow() {
		return nil, &DelegateError{
			Code: ErrAgentUnavail, Message: "circuit breaker open",
			Agent: cand.Name,
		}
	}

	// Rate limit (sender side).
	if !m.limiter.Allow(addr) {
		return nil, &DelegateError{
			Code: ErrRateLimited, Message: "too many requests, retry shortly",
			Agent: cand.Name,
		}
	}

	client, err := m.clientFor(ctx, addr)
	if err != nil {
		cb.RecordFailure()
		return nil, &DelegateError{
			Code: ErrAgentUnavail, Message: "negotiate failed",
			Agent: cand.Name, Cause: err.Error(),
		}
	}

	// Retry loop.
	retry := m.cfg.Retry
	maxRetries := max(retry.MaxRetries, 0)
	backoff := retry.Backoff
	if backoff <= 0 {
		backoff = 1 * time.Second
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Re-check breaker before retrying.
			if !cb.Allow() {
				return nil, &DelegateError{
					Code: ErrAgentUnavail, Message: "circuit breaker opened during retry",
					Agent: cand.Name,
				}
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, &DelegateError{Code: ErrTimeout, Message: "cancelled during backoff", Agent: cand.Name}
			}
			backoff *= 2
			if retry.MaxBackoff > 0 && backoff > retry.MaxBackoff {
				backoff = retry.MaxBackoff
			}
		}

		result, err := m.sendOnce(ctx, client, cand, payload)
		if err == nil {
			cb.RecordSuccess()
			m.recordLink(payload, cand, result)
			m.metrics.recordDelegate(m.card.Addr, cand.Addr, string(result.Status), time.Since(delegateStart).Seconds(), payload.DelegationDepth)
			return result, nil
		}
		lastErr = err
		// Map context deadline to timeout.
		if errors.Is(err, context.DeadlineExceeded) {
			lastErr = &DelegateError{Code: ErrTimeout, Message: "delegate timed out", Agent: cand.Name}
		}
		// Record every failure to the breaker (per design: retries reflect
		// peer health).
		cb.RecordFailure()

		if !isRetryable(lastErr, retry.RetryOn) {
			break
		}
		m.logger.Warn("agentmesh: delegate attempt failed, retrying",
			zap.String("agent", cand.Name),
			zap.Int("attempt", attempt+1),
			zap.Error(lastErr),
		)
	}
	return nil, lastErr
}

// sendOnce performs a single tasks/send call.
func (m *AgentMesh) sendOnce(ctx context.Context, client *A2AClient, cand AgentCard, payload DelegatePayload) (*DelegateResult, error) {
	m.publishEvent(event.Type("agent.delegate.sent"), payload.ParentSessionID, map[string]any{
		"to": cand.Name, "depth": payload.DelegationDepth,
	})
	result, err := client.SendTask(ctx, payload)
	if err != nil {
		return nil, err
	}
	m.publishEvent(event.Type("agent.delegate.received"), payload.ParentSessionID, map[string]any{
		"from": cand.Name, "status": string(result.Status),
	})
	return result, nil
}

// isRetryable reports whether err should be retried.
func isRetryable(err error, retryOn []ErrorCode) bool {
	var dErr *DelegateError
	if !errors.As(err, &dErr) {
		return false
	}
	if len(retryOn) == 0 {
		return dErr.Code.IsRetryable()
	}
	return slices.Contains(retryOn, dErr.Code)
}

// recordLink stores the parent→child session relationship on success.
func (m *AgentMesh) recordLink(payload DelegatePayload, cand AgentCard, result *DelegateResult) {
	if result == nil || result.TaskID == "" {
		return
	}
	childID := payload.ChildSessionID
	if childID == "" {
		childID = payload.ParentSessionID + ".dlg." + result.TaskID
	}
	link := &SessionLink{
		ChildSessionID:  childID,
		ParentSessionID: payload.ParentSessionID,
		ChildAgent:      cand.Addr,
		Status:          result.Status,
		CreatedAt:       m.now(),
	}
	m.linkMu.Lock()
	m.links[childID] = link
	m.linkMu.Unlock()
}

// GetLink returns a recorded session link (observability).
func (m *AgentMesh) GetLink(childSessionID string) (*SessionLink, bool) {
	m.linkMu.RLock()
	defer m.linkMu.RUnlock()
	l, ok := m.links[childSessionID]
	if !ok {
		return nil, false
	}
	return l, true
}

// publishEvent publishes to the event bus if one is configured.
func (m *AgentMesh) publishEvent(t event.Type, sessionID string, payload map[string]any) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(context.Background(), event.Event{
		Type:      t,
		Timestamp: m.now(),
		SessionID: sessionID,
		Payload:   payload,
	})
}

// DelegateAsync delegates a task without blocking. It returns a task id
// immediately; poll with GetResult, interrupt with Cancel. Under the hood the
// peer agent is called synchronously and the async-ness is tracked locally
// (Phase 2). The task is auto-GC'd 5 minutes after completion.
func (m *AgentMesh) DelegateAsync(ctx context.Context, payload DelegatePayload) (string, error) {
	if !m.cfg.Enabled {
		return "", &DelegateError{Code: ErrInternal, Message: "agent mesh disabled"}
	}
	if err := validatePayload(payload); err != nil {
		return "", err
	}
	// Resolve the target up front so async errors surface early (e.g. no
	// candidate). The actual execution runs in background.
	candidates, err := m.router.Route(payload)
	if err != nil {
		return "", err
	}
	childName := candidates[0].Name
	run := func(runCtx context.Context) (*DelegateResult, error) {
		return m.delegateTo(runCtx, candidates[0], payload)
	}
	id := m.tasks.start(m.bgCtx, run, payload.ParentSessionID, childName)
	m.publishEvent(event.Type("agent.delegate.async"), payload.ParentSessionID, map[string]any{
		"task_id": id, "to": childName,
	})
	return id, nil
}

// GetResult polls an async delegation. Returns (result, false, nil) while the
// task is still running; (result, true, nil) when done (result may be nil if
// the task errored — check err). Returns (nil, true, err) if the task is
// unknown/expired.
func (m *AgentMesh) GetResult(ctx context.Context, taskID string) (*DelegateResult, bool, error) {
	st, ok := m.tasks.get(taskID)
	if !ok {
		return nil, true, &DelegateError{Code: ErrAgentNotFound, Message: "unknown task id"}
	}
	select {
	case <-st.done:
		return st.result, true, st.err
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		return nil, false, nil // still running
	}
}

// Cancel interrupts an async delegation. Returns true if a cancellation was
// signaled; false if the task is unknown or already complete.
//
// Note: this cancels the delegator-side context. The peer agent's in-flight
// work is only interrupted if it observes the tasks/cancel RPC (Phase 2:
// wired via A2AClient.CancelTask on a best-effort basis).
func (m *AgentMesh) Cancel(taskID string) error {
	if !m.tasks.cancel(taskID) {
		return &DelegateError{Code: ErrCancelled, Message: "task not running or unknown"}
	}
	// Best-effort: tell the peer to cancel too. We don't know the peer addr
	// from the task id alone in Phase 2; the delegator-side cancel is the
	// primary mechanism.
	return nil
}

// Tasks returns the list of tracked async task ids (observability).
func (m *AgentMesh) Tasks() []string { return m.tasks.list() }

// validatePayload is the shared pre-flight check used by Delegate and
// DelegateAsync.
func validatePayload(payload DelegatePayload) error {
	if payload.Task == "" {
		return &DelegateError{Code: ErrBadPayload, Message: "task is required"}
	}
	if payload.ParentSessionID == "" {
		return &DelegateError{Code: ErrBadPayload, Message: "parent_session_id is required"}
	}
	return nil
}

// Shutdown closes all cached clients. Idempotent.
func (m *AgentMesh) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A2AClient has no Close in Phase 1 (it uses a shared http.Client); this
	// is a placeholder for future connection pooling cleanup.
	m.clients = map[string]*A2AClient{}
	return nil
}

// newMessageID generates a unique message ID (xid).
func newMessageID() string { return xid.New().String() }
