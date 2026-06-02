package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dolphin/internal/types"

	"go.uber.org/zap"
)

func (s *weworkSource) executeFileUpload(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var req struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &req); err != nil {
		return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	s.logger.Info("FILE_UPLOAD tool called", zap.String("file_path", req.FilePath))
	mediaID, fileName, mediaType, err := s.client.UploadMedia(ctx, req.FilePath)
	if err != nil {
		return &types.ToolResult{Content: "failed to upload file: " + err.Error(), IsError: true}, nil
	}

	if err := s.client.SendMediaMessage(ctx, mediaID, mediaType); err != nil {
		return &types.ToolResult{
			Content: fmt.Sprintf("File uploaded (media_id: %s) but failed to send: %s", mediaID, err.Error()),
			IsError: true,
		}, nil
	}

	return &types.ToolResult{
		Content: fmt.Sprintf("File sent to the conversation.\n- name: %s\n- media_id: %s\n- type: %s", fileName, mediaID, mediaType),
	}, nil
}
