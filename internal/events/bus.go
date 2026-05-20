package events

import (
	"fmt"
	"log"
	"sync"
)

type EventType string

const (
	EventIssueCreated         EventType = "issue.created"
	EventIssueStateChanged    EventType = "issue.state_changed"
	EventCommentAdded         EventType = "comment.added"
	EventAgentStatusChanged   EventType = "agent.status_changed"
	EventIssueAssigneeChanged EventType = "issue.assignee_changed"
	EventFeedbackCreated      EventType = "feedback.created"
	EventProposalCreated      EventType = "proposal.created"
	EventProposalStateChanged EventType = "proposal.state_changed"
	EventTaskCreated          EventType = "task.created"
	EventTaskStateChanged     EventType = "task.state_changed"
)

type Event struct {
	Type   EventType
	Payload interface{}
}

type Handler func(Event)

type registeredHandler struct {
	id  string
	fn  func(Event)
}

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]registeredHandler
	nextID   uint64
}

func NewBus() *Bus {
	return &Bus{
		handlers: make(map[EventType][]registeredHandler),
		nextID:   1,
	}
}

// Subscribe registers a handler for the given event type and returns a cancel function.
// Call the cancel function to unregister the handler (e.g., when a WebSocket client disconnects).
func (b *Bus) Subscribe(eventType EventType, handler func(Event)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := fmt.Sprintf("%d", b.nextID)
	b.nextID++
	b.handlers[eventType] = append(b.handlers[eventType], registeredHandler{id: id, fn: handler})

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		handlers := b.handlers[eventType]
		for i, h := range handlers {
			if h.id == id {
				b.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
				return
			}
		}
	}
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(fn func(Event)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[eventbus] handler panic for %s: %v", event.Type, r)
				}
			}()
			fn(event)
		}(h.fn)
	}
}

// PublishSync publishes synchronously — used in tests for deterministic ordering.
func (b *Bus) PublishSync(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		h.fn(event)
	}
}
