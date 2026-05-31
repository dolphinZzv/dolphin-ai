package event

import (
	"context"
	"sync"
	"time"
)

// Type is an event type string.
type Type string

const (
	EventPipelineStart        Type = "pipeline.start"
	EventPipelineShutdown     Type = "pipeline.shutdown"
	EventTurnStart            Type = "turn.start"
	EventTurnComplete         Type = "turn.complete"
	EventTurnError            Type = "turn.error"
	EventTurnInterrupt        Type = "turn.interrupt"
	EventLLMStart             Type = "llm.start"
	EventLLMComplete          Type = "llm.complete"
	EventLLMError             Type = "llm.error"
	EventLLMRetry             Type = "llm.retry"
	EventToolAssembly         Type = "tool.assembly"
	EventToolStart            Type = "tool.start"
	EventToolComplete         Type = "tool.complete"
	EventToolError            Type = "tool.error"
	EventMemoryWriteStart     Type = "memory.write.start"
	EventMemoryWriteComplete  Type = "memory.write.complete"
	EventContextStart         Type = "context.start"
	EventContextComplete      Type = "context.complete"
	EventContextBuildStart    Type = "context.build.start"
	EventContextBuildComplete Type = "context.build.complete"
)

// Event is the universal event structure.
type Event struct {
	Type      Type
	Timestamp time.Time
	SessionID string
	Payload   map[string]any
}

// Handler processes events.
type Handler func(ctx context.Context, e Event)

// Bus is a pub-sub event bus.
type Bus struct {
	mu       sync.Mutex
	handlers []Handler
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Publish(ctx context.Context, e Event) {
	b.mu.Lock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.Unlock()

	for _, h := range handlers {
		h(ctx, e)
	}
}

func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	b.handlers = append(b.handlers, h)
	b.mu.Unlock()
}
