package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"dolphin/internal/types"

	"go.uber.org/zap"
)

func (s *dingtalkSource) executeFileUpload(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	s.logger.Info("FILE_UPLOAD tool called", zap.String("file_path", args.FilePath))
	token, err := getAccessToken(ctx, s.clientID, s.clientSecret)
	if err != nil {
		return &types.ToolResult{Content: "failed to get DingTalk access token: " + err.Error(), IsError: true}, nil
	}

	mediaID, err := uploadMedia(ctx, token, args.FilePath)
	if err != nil {
		return &types.ToolResult{Content: "failed to upload file to DingTalk: " + err.Error(), IsError: true}, nil
	}

	fileName := filepath.Base(args.FilePath)
	ext := strings.ToLower(filepath.Ext(fileName))
	mediaType := mediaTypeForExt(ext)

	if mediaType == "image" {
		snippet := fmt.Sprintf("\n![%s](%s)\n", fileName, mediaID)
		return &types.ToolResult{
			Content: fmt.Sprintf("Image uploaded successfully.\n- media_id: %s\n\nInclude this markdown in your reply to show it in the group chat:\n%s", mediaID, snippet),
		}, nil
	}

	cid := s.conversationIDFn()
	if cid == "" {
		return &types.ToolResult{Content: "file uploaded but no conversation ID available to send it to the group", IsError: true}, nil
	}
	if err := sendFileMessage(ctx, token, cid, mediaID, fileName); err != nil {
		return &types.ToolResult{Content: "file uploaded but failed to send to group: " + err.Error(), IsError: true}, nil
	}

	return &types.ToolResult{
		Content: fmt.Sprintf("File sent to the group.\n- name: %s\n- media_id: %s\n- type: %s\n\nThe file has been sent as a native file message. Mention it briefly in your markdown reply so the group knows about it.", fileName, mediaID, mediaType),
	}, nil
}
