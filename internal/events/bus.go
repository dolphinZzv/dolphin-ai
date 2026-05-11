package events

import (
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
)

type Event struct {
	Type   EventType
	Payload interface{}
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

func NewBus() *Bus {
	return &Bus{
		handlers: make(map[EventType][]Handler),
	}
}

func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[eventbus] handler panic for %s: %v", event.Type, r)
				}
			}()
			handler(event)
		}(h)
	}
}

// PublishSync publishes synchronously — used in tests for deterministic ordering.
func (b *Bus) PublishSync(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}
