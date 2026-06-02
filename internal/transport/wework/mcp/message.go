package mcp

import (
	"context"
	"encoding/json"

	"dolphin/internal/types"

	"go.uber.org/zap"
)

func (s *weworkSource) executeMessage(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
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

	msgType := args.MsgType
	if msgType == "" {
		msgType = "markdown"
	}

	s.logger.Info("MESSAGE tool called", zap.String("msgtype", msgType), zap.Int("content_length", len(args.Content)))
	if err := s.client.ProactiveMessage(ctx, args.Content, msgType); err != nil {
		return &types.ToolResult{Content: "failed to send message: " + err.Error(), IsError: true}, nil
	}

	return &types.ToolResult{Content: "Message sent successfully."}, nil
}
