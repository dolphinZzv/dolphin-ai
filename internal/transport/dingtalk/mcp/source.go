package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

// NewFileUploadSource returns a tool source that provides FILE_UPLOAD and MESSAGE
// when the active transport is DingTalk (checked via context).
func NewFileUploadSource(clientID, clientSecret string, conversationIDFn func() string) tool.Executor {
	return &dingtalkSource{
		clientID:         clientID,
		clientSecret:     clientSecret,
		conversationIDFn: conversationIDFn,
	}
}

type dingtalkSource struct {
	clientID         string
	clientSecret     string
	conversationIDFn func() string
}

func (s *dingtalkSource) List(ctx context.Context) ([]types.ToolDef, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "dingtalk" || s.clientID == "" || s.clientSecret == "" {
		return nil, nil
	}
	return []types.ToolDef{
		{
			Name: "FILE_UPLOAD",
			Description: "Upload a file (image, voice, video, archive, document, etc.) to DingTalk and share it in the group chat. " +
				"For images, include the returned markdown snippet in your reply to show it inline. " +
				"For other file types the tool sends the file directly as a native file message to the group.",
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
			Description: "Send a text or markdown message to the DingTalk group chat proactively. " +
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

func (s *dingtalkSource) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	info := transport.GetInfo(ctx)
	if info == nil || info.ID != "dingtalk" {
		return nil, fmt.Errorf("%s is not available on this transport", call.Name)
	}

	switch call.Name {
	case "MESSAGE":
		return s.executeMessage(ctx, call)
	case "FILE_UPLOAD":
		return s.executeFileUpload(ctx, call)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}
