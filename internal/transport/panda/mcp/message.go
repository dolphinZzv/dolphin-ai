package mcp

import (
	"context"
	"encoding/json"

	"dolphin/internal/types"

	"go.uber.org/zap"
)

func (s *pandaSource) executeMessage(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if args.Content == "" {
		return &types.ToolResult{Content: "content is required", IsError: true}, nil
	}

	s.logger.Info("MESSAGE tool called", zap.Int("content_length", len(args.Content)))
	if err := s.writeFn(ctx, args.Content); err != nil {
		return &types.ToolResult{Content: "failed to send message: " + err.Error(), IsError: true}, nil
	}

	return &types.ToolResult{Content: "Message sent successfully."}, nil
}
