package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dolphin/internal/tool"
	transport "dolphin/internal/transport"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

// WeWorkClient is the subset of *WeWork needed by MCP tools.
type WeWorkClient interface {
	ProactiveMessage(ctx context.Context, content, msgType string) error
	UploadMedia(ctx context.Context, filePath string) (mediaID, fileName, mediaType string, err error)
	SendMediaMessage(ctx context.Context, mediaID, mediaType string) error
}

type weworkSource struct {
	client WeWorkClient
	botID  string
	secret string
	logger *zap.Logger
}

func NewSource(client WeWorkClient, botID, secret string, logger *zap.Logger) tool.Executor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &weworkSource{client: client, botID: botID, secret: secret, logger: logger}
}

func (s *weworkSource) List(ctx context.Context) ([]types.ToolDef, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "wework" || s.botID == "" || s.secret == "" {
		return nil, nil
	}
	return []types.ToolDef{
		{
			Name: "FILE_UPLOAD",
			Description: "Upload a file (image, document, archive, etc.) to WeWork Smart Bot and send it to the current conversation. " +
				"The file is uploaded via WebSocket and sent directly as a native image/file message. " +
				"For images, mention it in your reply that the image has been sent.",
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
			Description: "Send a text or markdown message to the WeWork chat proactively. " +
				"Use this to notify users, ask questions, or send results without waiting for a reply.",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"content": {"type": "string", "description": "Message content to send"},
					"msgtype": {"type": "string", "description": "Message type: text or markdown (default: markdown)"}
				},
				"required": ["content"]
			}`),
		},
	}, nil
}

func (s *weworkSource) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	switch call.Name {
	case "MESSAGE":
		return s.executeMessage(ctx, call)
	case "FILE_UPLOAD":
		return s.executeFileUpload(ctx, call)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}
