package agentio

import (
	"context"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"

	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

type Turn struct {
	TurnID      string
	TransportID string
	SessionID   string
	Input       string
	Context     string    // transport-specific context string
	EnqueuedAt  time.Time // when the turn was placed into the queue
}

type TurnResult struct {
	TurnID      string
	TransportID string
	SessionID   string
	Text        string
	Thinking    string
	ToolCall    *types.ToolCall
	ToolResult  *types.ToolResult
	Done        bool
	Error       error
}

type TurnInfo struct {
	TurnID          string
	SessionID       string
	TransportID     string
	Input           string
	StartedAt       time.Time
	CurrentActivity string // e.g. "call search_file"
}

type AgentIO struct {
	queue      chan *Turn
	priority   chan *Turn // priority queue, selected before regular queue
	routes     map[string]transport.IO
	sessionMgr *session.Manager
	signalBus  *signal.Bus
	logger     *zap.Logger
	agentName  string

	// expireAfter, if > 0, prompts the user to start a new session when a
	// turn arrives more than this long after the active session's last
	// activity. 0 disables the check.
	expireAfter time.Duration

	bufMu   sync.Mutex
	buffers map[string]string // partial text for chunk-mode transports

	lastTransportMu sync.RWMutex
	lastTransportID string // most recent transport that sent a turn

	pendingMu sync.Mutex
	pending   []*Turn         // mirror of chan queue for introspection
	cancelled map[string]bool // turn IDs popped before dequeue

	activeTurns map[string]*TurnInfo // worker_id → currently processing turn
	activeMu    sync.RWMutex
}

func NewAgentIO(bufferSize int, mgr *session.Manager, sb *signal.Bus, logger *zap.Logger, agentName string) *AgentIO {
	a := &AgentIO{
		queue:       make(chan *Turn, bufferSize),
		priority:    make(chan *Turn, bufferSize),
		routes:      make(map[string]transport.IO),
		sessionMgr:  mgr,
		signalBus:   sb,
		logger:      logger,
		agentName:   agentName,
		buffers:     make(map[string]string),
		cancelled:   make(map[string]bool),
		activeTurns: make(map[string]*TurnInfo),
	}

	// Listen for session flips to broadcast to all transports
	mgr.OnFliped(func(ctx context.Context, sessionID string) {
		msg := i18n.T("agentio.session_broadcast", sessionID)
		for id, tio := range a.routes {
			if err := tio.Write(ctx, msg); err != nil {
				a.logger.Warn("session flip broadcast failed",
					zap.String("transport_id", id),
					zap.Error(err),
				)
			}
		}
		a.logger.Info("session flipped broadcast",
			zap.String("new_session_id", sessionID),
			zap.Int("transports_notified", len(a.routes)),
		)
	})

	return a
}

func (a *AgentIO) RegisterTransport(id string, tio transport.IO) {
	a.routes[id] = tio
}

func (a *AgentIO) GetTransport(id string) transport.IO {
	return a.routes[id]
}

// SetExpireAfter configures session idle expiry. When a turn arrives more
// than d after the active session's last activity, the user is asked
// (via their transport) whether to start a fresh session. 0 disables.
func (a *AgentIO) SetExpireAfter(d time.Duration) {
	a.expireAfter = d
}

// bindSession picks (or creates / rotates) the session for a turn that
// didn't already carry one. On idle expiry it prompts the user's transport;
// turns without a transport context silently continue the active session.
func (a *AgentIO) bindSession(ctx context.Context, turn *Turn) {
	sess := a.sessionMgr.Active()
	if sess == nil {
		sess = a.sessionMgr.Create(ctx)
		turn.SessionID = sess.ID
		return
	}

	// Decide whether to ask about rotation. Only ask when:
	//   - expiry is enabled,
	//   - the session is genuinely idle past the threshold,
	//   - the turn came in over a known transport that can ask the user.
	if a.expireAfter > 0 && !sess.UpdatedAt.IsZero() &&
		time.Since(sess.UpdatedAt) > a.expireAfter && turn.TransportID != "" {
		if tio := a.routes[turn.TransportID]; tio != nil {
			idle := time.Since(sess.UpdatedAt).Round(time.Second)
			prompt := i18n.T("agentio.session_expired_prompt", idle.String())
			ok, err := tio.Confirm(ctx, prompt)
			if err == nil && ok {
				sess = a.sessionMgr.Create(ctx)
				turn.SessionID = sess.ID
				return
			}
			// err != nil (transport non-interactive) or user said no:
			// fall through and reuse the existing session.
		}
	}

	turn.SessionID = sess.ID
	a.sessionMgr.Touch(sess.ID)
}

func (a *AgentIO) SendTurn(ctx context.Context, turn *Turn) {
	if turn.TurnID == "" {
		turn.TurnID = xid.New().String()
	}

	if turn.TransportID == "" {
		if info := transport.GetInfo(ctx); info != nil {
			turn.TransportID = info.ID
		} else {
			// Fall back to the most recently active transport.
			a.lastTransportMu.RLock()
			turn.TransportID = a.lastTransportID
			a.lastTransportMu.RUnlock()
		}
	}

	// Track this transport as the most recently active.
	if turn.TransportID != "" {
		a.lastTransportMu.Lock()
		a.lastTransportID = turn.TransportID
		a.lastTransportMu.Unlock()
	}

	if turn.SessionID == "" {
		a.bindSession(ctx, turn)
	}

	turn.EnqueuedAt = time.Now()

	a.pendingMu.Lock()
	a.pending = append(a.pending, turn)
	a.pendingMu.Unlock()

	a.queue <- turn
}

func (a *AgentIO) Queue() chan *Turn {
	return a.queue
}

func (a *AgentIO) PriorityQueue() chan *Turn {
	return a.priority
}

// SendTurnPriority enqueues a turn at the front of the pending queue.
func (a *AgentIO) SendTurnPriority(ctx context.Context, turn *Turn) {
	if turn.TurnID == "" {
		turn.TurnID = xid.New().String()
	}

	if turn.TransportID == "" {
		if info := transport.GetInfo(ctx); info != nil {
			turn.TransportID = info.ID
		} else {
			a.lastTransportMu.RLock()
			turn.TransportID = a.lastTransportID
			a.lastTransportMu.RUnlock()
		}
	}

	if turn.TransportID != "" {
		a.lastTransportMu.Lock()
		a.lastTransportID = turn.TransportID
		a.lastTransportMu.Unlock()
	}

	if turn.SessionID == "" {
		a.bindSession(ctx, turn)
	}

	turn.EnqueuedAt = time.Now()

	a.pendingMu.Lock()
	a.pending = append([]*Turn{turn}, a.pending...)
	a.pendingMu.Unlock()

	a.priority <- turn
}

// QueueSnapshot returns a snapshot of the pending turns, the queue capacity,
// and whether the agent loop is currently processing a turn.
func (a *AgentIO) QueueSnapshot() (pending []*Turn, capacity int, processing bool) {
	a.pendingMu.Lock()
	pending = make([]*Turn, len(a.pending))
	copy(pending, a.pending)
	a.pendingMu.Unlock()
	return pending, cap(a.queue), a.Processing()
}

// PopIndex removes the turn at the given 0-based pending index.
// It marks the turn as cancelled so AgentLoop skips it on dequeue.
// Returns the popped turn, or nil if out of bounds.
func (a *AgentIO) PopIndex(index int) *Turn {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if index < 0 || index >= len(a.pending) {
		return nil
	}
	t := a.pending[index]
	a.pending = append(a.pending[:index], a.pending[index+1:]...)
	a.cancelled[t.TurnID] = true
	return t
}

// IsCancelled reports whether the turn was popped before dequeue.
func (a *AgentIO) IsCancelled(turnID string) bool {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	return a.cancelled[turnID]
}

// OnTurnDequeued removes a turn from the pending slice and cleans up cancelled state.
func (a *AgentIO) OnTurnDequeued(turn *Turn) {
	a.pendingMu.Lock()
	for i, t := range a.pending {
		if t.TurnID == turn.TurnID {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			break
		}
	}
	delete(a.cancelled, turn.TurnID)
	a.pendingMu.Unlock()
}

// SetActive records that a worker started processing a turn.
func (a *AgentIO) SetActive(workerID string, turn *Turn) {
	a.activeMu.Lock()
	a.activeTurns[workerID] = &TurnInfo{
		TurnID:      turn.TurnID,
		SessionID:   turn.SessionID,
		TransportID: turn.TransportID,
		Input:       turn.Input,
		StartedAt:   time.Now(),
	}
	a.activeMu.Unlock()
}

// ClearActive removes a worker's active turn record.
func (a *AgentIO) ClearActive(workerID string) {
	a.activeMu.Lock()
	delete(a.activeTurns, workerID)
	a.activeMu.Unlock()
}

// SetWorkerActivity updates the current activity description for a worker's active turn.
func (a *AgentIO) SetWorkerActivity(workerID, activity string) {
	a.activeMu.Lock()
	if t, ok := a.activeTurns[workerID]; ok {
		t.CurrentActivity = activity
	}
	a.activeMu.Unlock()
}

// ActiveSnapshot returns a copy of the current active turns map.
func (a *AgentIO) ActiveSnapshot() map[string]*TurnInfo {
	a.activeMu.RLock()
	defer a.activeMu.RUnlock()
	snap := make(map[string]*TurnInfo, len(a.activeTurns))
	for k, v := range a.activeTurns {
		cp := *v
		snap[k] = &cp
	}
	return snap
}

// Processing returns whether any worker is currently processing a turn.
func (a *AgentIO) Processing() bool {
	a.activeMu.RLock()
	defer a.activeMu.RUnlock()
	return len(a.activeTurns) > 0
}

func (a *AgentIO) OnResult(result *TurnResult) {
	if result.TransportID == "" {
		// System/internal event (e.g. subscription trigger): broadcast to all transports.
		for id := range a.routes {
			a.writeResult(result, id)
		}
		return
	}
	a.writeResult(result, result.TransportID)
}

func (a *AgentIO) writeResult(result *TurnResult, transportID string) {
	tio, ok := a.routes[transportID]
	if !ok {
		a.logger.Warn("OnResult: unknown transport",
			zap.String("transport_id", transportID),
		)
		return
	}

	cap := tio.Capability()

	if result.Thinking != "" {
		if err := tio.WriteThinking(context.Background(), result.Thinking); err != nil {
			a.logger.Error("OnResult WriteThinking failed",
				zap.Error(err),
				zap.String("transport_id", transportID),
			)
		}
	}
	if result.ToolCall != nil {
		if err := tio.WriteToolCall(context.Background(), *result.ToolCall); err != nil {
			a.logger.Error("OnResult WriteToolCall failed",
				zap.Error(err),
				zap.String("transport_id", transportID),
			)
		}
	}
	if result.ToolResult != nil {
		if err := tio.WriteToolResult(context.Background(), *result.ToolResult); err != nil {
			a.logger.Error("OnResult WriteToolResult failed",
				zap.Error(err),
				zap.String("transport_id", transportID),
			)
		}
	}

	if result.Text != "" {
		if cap.Streamable {
			// Streamable: write chunks as they arrive.
			text := result.Text
			if err := tio.Write(context.Background(), text); err != nil {
				a.logger.Error("OnResult write failed",
					zap.Error(err),
					zap.String("transport_id", transportID),
				)
			}
		} else {
			// Chunk mode: buffer all text, write on Done.
			a.bufMu.Lock()
			a.buffers[transportID] += result.Text
			a.bufMu.Unlock()
		}
	}

	if result.Done {
		if !cap.Streamable {
			// Chunk mode: flush buffered content as one complete message (no prompt prefix).
			a.bufMu.Lock()
			buf := a.buffers[transportID]
			delete(a.buffers, transportID)
			a.bufMu.Unlock()

			if buf != "" {
				if err := tio.Write(context.Background(), buf); err != nil {
					a.logger.Error("OnResult write failed",
						zap.Error(err),
						zap.String("transport_id", transportID),
					)
				}
			}
			if result.Error != nil && buf == "" {
				if err := tio.Write(context.Background(), result.Error.Error()); err != nil {
					a.logger.Error("OnResult write failed",
						zap.Error(err),
						zap.String("transport_id", transportID),
					)
				}
			}
		}

		if err := tio.Flush(); err != nil {
			a.logger.Error("OnResult flush failed",
				zap.Error(err),
				zap.String("transport_id", transportID),
			)
		}
	}
}
