package event

import (
	"context"

	"go.uber.org/zap"
)

// NewLogHandler returns a handler that logs events at appropriate levels.
func NewLogHandler(logger *zap.Logger) Handler {
	return func(ctx context.Context, e Event) {
		fields := []zap.Field{zap.String("sessionid", e.SessionID)}
		for k, v := range e.Payload {
			fields = append(fields, zap.Any(k, v))
		}

		switch e.Type {
		case EventLLMError, EventToolError, EventTurnError:
			logger.Error(string(e.Type), fields...)
		case EventLLMRetry:
			logger.Warn(string(e.Type), fields...)
		case EventToolAssembly, EventPipelineStart, EventPipelineShutdown:
			logger.Debug(string(e.Type), fields...)
		default:
			logger.Info(string(e.Type), fields...)
		}
	}
}
