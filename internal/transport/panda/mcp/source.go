package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

// NewFileUploadSource returns a tool source that provides tools (e.g. MESSAGE)
// when the active transport is Panda (checked via context).
func NewFileUploadSource(serverURL string, tokenGetter func() string, writeFn func(ctx context.Context, text string) error, writeContentFn func(ctx context.Context, text string, contentType int) error, logger *zap.Logger) tool.Executor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &pandaSource{
		serverURL:      serverURL,
		token:          tokenGetter,
		writeFn:        writeFn,
		writeContentFn: writeContentFn,
		logger:         logger,
	}
}

type pandaSource struct {
	serverURL      string
	token          func() string
	writeFn        func(ctx context.Context, text string) error
	writeContentFn func(ctx context.Context, text string, contentType int) error
	logger         *zap.Logger
}

func (s *pandaSource) List(ctx context.Context) ([]types.ToolDef, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "panda" {
		return nil, nil
	}
	return []types.ToolDef{
		{
			Name: "MESSAGE",
			Description: "Send a text message proactively to the current conversation. " +
				"Use this to notify users, ask questions, or send results without waiting for a reply.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"content": {"type": "string", "description": "Message content to send"}
				},
				"required": ["content"]
			}`),
		},
	}, nil
}

func (s *pandaSource) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "panda" {
		return nil, fmt.Errorf("%s is not available on this transport", call.Name)
	}

	switch call.Name {
	case "MESSAGE":
		return s.executeMessage(ctx, call)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}
