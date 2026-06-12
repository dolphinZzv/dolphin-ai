package agentio

import (
	"context"
	"sync"

	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	"dolphin/internal/types"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

type Turn struct {
	TurnID      string
	TransportID string
	SessionID   string
	Input       string
	Context     string // transport-specific context string
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

type AgentIO struct {
	queue      chan *Turn
	routes     map[string]transport.IO
	sessionMgr *session.Manager
	signalBus  *signal.Bus
	logger     *zap.Logger
	agentName  string
	replied    map[string]bool

	bufMu   sync.Mutex
	buffers map[string]string // partial text for chunk-mode transports

	lastTransportMu sync.RWMutex
	lastTransportID string // most recent transport that sent a turn
}

func NewAgentIO(bufferSize int, mgr *session.Manager, sb *signal.Bus, logger *zap.Logger, agentName string) *AgentIO {
	a := &AgentIO{
		queue:      make(chan *Turn, bufferSize),
		routes:     make(map[string]transport.IO),
		sessionMgr: mgr,
		signalBus:  sb,
		logger:     logger,
		agentName:  agentName,
		replied:    make(map[string]bool),
		buffers:    make(map[string]string),
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
		sess := a.sessionMgr.Active()
		if sess == nil {
			sess = a.sessionMgr.Create(ctx)
		}
		turn.SessionID = sess.ID
	}

	a.queue <- turn
}

func (a *AgentIO) Queue() chan *Turn {
	return a.queue
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
		if tw, ok := tio.(transport.ThinkingWriter); ok {
			if err := tw.WriteThinking(context.Background(), result.Thinking); err != nil {
				a.logger.Error("OnResult WriteThinking failed",
					zap.Error(err),
					zap.String("transport_id", transportID),
				)
			}
		}
	}
	if result.ToolCall != nil {
		if tcw, ok := tio.(transport.ToolCallWriter); ok {
			if err := tcw.WriteToolCall(context.Background(), *result.ToolCall); err != nil {
				a.logger.Error("OnResult WriteToolCall failed",
					zap.Error(err),
					zap.String("transport_id", transportID),
				)
			}
		}
	}
	if result.ToolResult != nil {
		if trw, ok := tio.(transport.ToolResultWriter); ok {
			if err := trw.WriteToolResult(context.Background(), *result.ToolResult); err != nil {
				a.logger.Error("OnResult WriteToolResult failed",
					zap.Error(err),
					zap.String("transport_id", transportID),
				)
			}
		}
	}

	if result.Text != "" {
		if cap.Streamable {
			// Streamable: write chunks as they arrive.
			text := result.Text
			if !a.replied[transportID] {
				text = i18n.T("agentio.reply_prefix", a.agentName) + text
				a.replied[transportID] = true
			}
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

		a.replied[transportID] = false
		if err := tio.Flush(); err != nil {
			a.logger.Error("OnResult flush failed",
				zap.Error(err),
				zap.String("transport_id", transportID),
			)
		}
	}
}
