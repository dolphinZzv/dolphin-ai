package mcp

import (
	"context"
	"encoding/json"

	"dolphin/internal/types"
)

func (s *dingtalkSource) executeMessage(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args struct {
		Content string `json:"content"`
		MsgType string `json:"msgtype"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Content == "" {
		return &types.ToolResult{Content: "content is required", IsError: true}, nil
	}

	token, err := getAccessToken(ctx, s.clientID, s.clientSecret)
	if err != nil {
		return &types.ToolResult{Content: "failed to get DingTalk access token: " + err.Error(), IsError: true}, nil
	}

	cid := s.conversationIDFn()
	if cid == "" {
		return &types.ToolResult{Content: "no conversation ID available to send message", IsError: true}, nil
	}

	msgType := args.MsgType
	if msgType == "" {
		msgType = "markdown"
	}

	if err := sendMessage(ctx, token, cid, args.Content, msgType); err != nil {
		return &types.ToolResult{Content: "failed to send message: " + err.Error(), IsError: true}, nil
	}

	return &types.ToolResult{Content: "Message sent successfully."}, nil
}
