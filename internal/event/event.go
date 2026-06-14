package event

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
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
	EventLLMEmit              Type = "llm.emit"
	EventFileCreate           Type = "file.create"
	EventFileUpdate           Type = "file.update"
	EventFileDelete           Type = "file.delete"

	// Limit events
	EventCheckLLM       Type = "limit.check.llm"
	EventLimitSoftWarn  Type = "limit.soft_warn"
	EventLimitHardBlock Type = "limit.hard_block"

	// Worker events
	EventWorkerPanic Type = "worker.panic"
	// Workflow events
	EventWorkflowStart      Type = "workflow.start"
	EventWorkflowStepChange Type = "workflow.step_change"
	EventWorkflowPaused     Type = "workflow.paused"
	EventWorkflowComplete   Type = "workflow.complete"
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
	logger   *zap.Logger
}

func NewBus() *Bus {
	return &Bus{}
}

// SetLogger attaches a logger to the bus for diagnostics.
func (b *Bus) SetLogger(logger *zap.Logger) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logger = logger
}

func (b *Bus) Publish(ctx context.Context, e Event) {
	b.mu.Lock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	logger := b.logger
	b.mu.Unlock()

	if logger != nil {
		logger.Debug("event published", zap.String("type", string(e.Type)))
	}

	for _, h := range handlers {
		h(ctx, e)
	}
}

func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
	if b.logger != nil {
		b.logger.Info("event subscriber added", zap.Int("total", len(b.handlers)))
	}
}
