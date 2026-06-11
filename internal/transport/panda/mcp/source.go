package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

// NewFileUploadSource returns a tool source that provides FILE_UPLOAD and MESSAGE
// when the active transport is Panda (checked via context).
func NewFileUploadSource(serverURL string, tokenGetter func() string, writeFn func(ctx context.Context, text string) error, logger *zap.Logger) tool.Executor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &pandaSource{
		serverURL: serverURL,
		token:     tokenGetter,
		writeFn:   writeFn,
		logger:    logger,
	}
}

type pandaSource struct {
	serverURL string
	token     func() string
	writeFn   func(ctx context.Context, text string) error
	logger    *zap.Logger
}

func (s *pandaSource) List(ctx context.Context) ([]types.ToolDef, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "panda" {
		return nil, nil
	}
	return []types.ToolDef{
		{
			Name: "FILE_UPLOAD",
			Description: "Upload a file (image, document, audio, video, archive, etc.) to the panda-ai server. " +
				"For images, include the returned markdown snippet in your reply to show it inline. " +
				"For other file types the tool returns the file URL.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {"type": "string", "description": "Absolute path to the file to upload"}
				},
				"required": ["file_path"]
			}`),
		},
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
	case "FILE_UPLOAD":
		return s.executeFileUpload(ctx, call)
	case "MESSAGE":
		return s.executeMessage(ctx, call)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}
