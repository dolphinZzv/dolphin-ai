// Package event provides asynchronous event dispatch for the agent loop.
// Plugins subscribe to events and receive them in background goroutines.
// The EventBus also has built-in JSONL logging and webhook delivery.
package event

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Type identifies an event category.
type Type string

const (
	TypeSessionCreated  Type = "session:created"
	TypeSessionEnded    Type = "session:ended"
	TypeUserMessage     Type = "user:message"
	TypeLLMResponse     Type = "llm:response"
	TypeToolCalled      Type = "tool:called"
	TypeToolCompleted   Type = "tool:completed"
	TypeCompression     Type = "compression"
	TypeError           Type = "error"
	TypeHeartbeat       Type = "heartbeat"
	TypeAgentDispatched Type = "agent:dispatched"
	TypeAgentCompleted  Type = "agent:completed"
	TypeSkillLoaded     Type = "skill:loaded"
	TypeAppStarted      Type = "app:started"
	TypeAppStopped      Type = "app:stopped"

	// Resource monitoring events
	TypeResourceCPU     Type = "resource:cpu"
	TypeResourceMemory  Type = "resource:memory"
	TypeResourceDisk    Type = "resource:disk"
	TypeResourceNetwork Type = "resource:network"

	// Agent lifecycle
	TypeAgentReload Type = "agent:reload"
)

var AllTypes = []Type{
	TypeSessionCreated, TypeSessionEnded,
	TypeUserMessage, TypeLLMResponse,
	TypeToolCalled, TypeToolCompleted,
	TypeCompression, TypeError, TypeHeartbeat,
	TypeAgentDispatched, TypeAgentCompleted, TypeSkillLoaded,
	TypeAppStarted, TypeAppStopped,
	TypeAgentReload,
	TypeResourceCPU, TypeResourceMemory, TypeResourceDisk, TypeResourceNetwork,
}

// Event is an asynchronous notification.
type Event struct {
	Type      Type           `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Turn      int            `json:"turn"`
	Data      map[string]any `json:"data,omitempty"`
}

// Handler receives events. It runs in a background goroutine; must not block
// indefinitely.
type Handler func(ctx context.Context, evt Event)

// handlerEntry wraps a handler with a unique ID for unsubscribe support.
type handlerEntry struct {
	id      string
	handler Handler
}

// EventBus dispatches events to subscribers. It is safe for concurrent use.
type EventBus struct {
	mu       sync.RWMutex
	subs     map[Type][]handlerEntry // exact-type subscribers
	wildcard []handlerEntry          // "*" subscribers

	buffer int

	// Built-in handlers
	logWriter io.Writer
	logMu     sync.Mutex

	webhookURL    string
	webhookEvents map[Type]bool // set of events to POST
	webhookClient *http.Client
}

// NewEventBus creates a new EventBus. bufferSize controls the per-handler
// channel capacity; when full, events are dropped with a warning.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &EventBus{
		subs:          make(map[Type][]handlerEntry),
		buffer:        bufferSize,
		webhookEvents: make(map[Type]bool),
		webhookClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// On subscribes a handler to an event type. Use "*" to receive all events.
// Returns an unsubscribe function that removes the handler.
func (b *EventBus) On(t Type, h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := handlerEntry{id: xid.New().String(), handler: h}

	if t == "*" {
		b.wildcard = append(b.wildcard, entry)
		return func() { b.removeHandler("", entry.id) }
	}
	b.subs[t] = append(b.subs[t], entry)
	return func() { b.removeHandler(t, entry.id) }
}

func (b *EventBus) removeHandler(t Type, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if t == "" {
		for i, e := range b.wildcard {
			if e.id == id {
				b.wildcard = append(b.wildcard[:i], b.wildcard[i+1:]...)
				return
			}
		}
		return
	}

	entries := b.subs[t]
	for i, e := range entries {
		if e.id == id {
			b.subs[t] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

// SetLogWriter enables built-in JSONL logging. Each event is written as one
// JSON line. Safe to call multiple times (replaces previous writer).
func (b *EventBus) SetLogWriter(w io.Writer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logWriter = w
}

// SetWebhook enables built-in webhook delivery. Events matching the filter
// are POSTed as JSON. An empty filter means no events. Use []Type{"*"} for all.
func (b *EventBus) SetWebhook(url string, filter []Type) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.webhookURL = url
	b.webhookEvents = make(map[Type]bool)
	for _, t := range filter {
		if t == "*" {
			for _, at := range AllTypes {
				b.webhookEvents[at] = true
			}
			break
		}
		b.webhookEvents[t] = true
	}
}

// Emit dispatches an event to all matching subscribers. The call is non-blocking.
// If a subscriber's buffer is full, the event is dropped and a warning is logged.
func (b *EventBus) Emit(ctx context.Context, evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	b.mu.RLock()
	handlers := make([]Handler, 0, len(b.subs[evt.Type])+len(b.wildcard))
	for _, e := range b.subs[evt.Type] {
		handlers = append(handlers, e.handler)
	}
	for _, e := range b.wildcard {
		handlers = append(handlers, e.handler)
	}
	logWriter := b.logWriter
	webhookURL := b.webhookURL
	shouldWebhook := b.webhookEvents[evt.Type]
	b.mu.RUnlock()

	// Built-in: JSONL log writer (synchronous, small cost)
	if logWriter != nil {
		b.writeLog(logWriter, evt)
	}

	// Built-in: webhook delivery (async)
	if webhookURL != "" && shouldWebhook {
		go b.sendWebhook(ctx, webhookURL, evt)
	}

	// Dispatch to subscriber handlers
	for _, h := range handlers {
		go b.dispatch(ctx, h, evt)
	}
}

// dispatch sends an event to a single handler with panic recovery.
func (b *EventBus) dispatch(ctx context.Context, h Handler, evt Event) {
	done := make(chan struct{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				zap.S().Warnw("event handler panic recovered", "event", string(evt.Type), "panic", r)
			}
			done <- struct{}{}
		}()
		h(ctx, evt)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		zap.S().Warnw("event handler slow (5s timeout)", "event", string(evt.Type))
	}
}

// writeLog writes an event as one JSON line to the writer.
func (b *EventBus) writeLog(w io.Writer, evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	b.logMu.Lock()
	if _, err := w.Write(data); err != nil {
		b.logMu.Unlock()
		return
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		b.logMu.Unlock()
		return
	}
	b.logMu.Unlock()
}

// sendWebhook POSTs an event as JSON to the webhook URL.
func (b *EventBus) sendWebhook(ctx context.Context, url string, evt Event) {
	body, err := json.Marshal(evt)
	if err != nil {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(1<<uint(attempt)) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := b.webhookClient.Do(req)
		if err != nil {
			zap.S().Debugw("webhook delivery failed (will retry)", "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		zap.S().Debugw("webhook delivery bad status", "attempt", attempt+1, "status", resp.StatusCode)
	}
	zap.S().Warnw("webhook delivery failed after 3 attempts", "url", url)
}
