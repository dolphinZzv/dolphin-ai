package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// httpClient with a sensible timeout for DingTalk API calls.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// RegisterTools registers DingTalk-specific MCP tools.
// clientID and clientSecret must be non-empty for registration.
// conversationIDFn returns the latest group conversation ID for chat/send API.
func RegisterTools(reg *tool.Registry, clientID, clientSecret string, conversationIDFn func() string) {
	if clientID == "" || clientSecret == "" {
		return
	}

	reg.RegisterBuiltin("FILE_UPLOAD",
		"Upload a file (image, voice, video, archive, document, etc.) to DingTalk and share it in the group chat. "+
			"For images, include the returned markdown snippet in your reply to show it inline. "+
			"For other file types the tool sends the file directly as a native file message to the group.",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the file to upload"}
			},
			"required": ["file_path"]
		}`),
		func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			var req struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
			}

			token, err := getAccessToken(ctx, clientID, clientSecret)
			if err != nil {
				return &types.ToolResult{Content: "failed to get DingTalk access token: " + err.Error(), IsError: true}, nil
			}

			mediaID, err := uploadMedia(ctx, token, req.FilePath)
			if err != nil {
				return &types.ToolResult{Content: "failed to upload file to DingTalk: " + err.Error(), IsError: true}, nil
			}

			fileName := filepath.Base(req.FilePath)
			ext := strings.ToLower(filepath.Ext(fileName))
			mediaType := mediaTypeForExt(ext)

			if mediaType == "image" {
				snippet := fmt.Sprintf("\n![%s](%s)\n", fileName, mediaID)
				return &types.ToolResult{
					Content: fmt.Sprintf("Image uploaded successfully.\n- media_id: %s\n\nInclude this markdown in your reply to show it in the group chat:\n%s", mediaID, snippet),
				}, nil
			}

			// Send as native file message via chat/send API.
			cid := conversationIDFn()
			if cid == "" {
				return &types.ToolResult{Content: "file uploaded but no conversation ID available to send it to the group", IsError: true}, nil
			}
			if err := sendFileMessage(ctx, token, cid, mediaID, fileName); err != nil {
				return &types.ToolResult{Content: "file uploaded but failed to send to group: " + err.Error(), IsError: true}, nil
			}

			return &types.ToolResult{
				Content: fmt.Sprintf("File sent to the group.\n- name: %s\n- media_id: %s\n- type: %s\n\nThe file has been sent as a native file message. Mention it briefly in your markdown reply so the group knows about it.", fileName, mediaID, mediaType),
			}, nil
		},
	)
}

// sendFileMessage sends a native file message to a group conversation via chat/send API.
func sendFileMessage(ctx context.Context, token, chatID, mediaID, _ string) error {
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": "file",
		"file": map[string]string{
			"media_id": mediaID,
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("https://oapi.dingtalk.com/chat/send?access_token=%s", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		ErrCode   int    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		ChatID    string `json:"chatid"`
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode response: %s (raw: %s)", err, string(respBody))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("api error: %s (code %d, chatid=%s)", result.ErrMsg, result.ErrCode, chatID)
	}

	return nil
}

// getAccessToken obtains a DingTalk access token using client credentials.
func getAccessToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	url := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s", clientID, clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if tr.ErrCode != 0 {
		return "", fmt.Errorf("api error (code %d): %s", tr.ErrCode, tr.ErrMsg)
	}
	return tr.AccessToken, nil
}

type tokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
}

type mediaResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	MediaID string `json:"media_id"`
}

func uploadMedia(ctx context.Context, token, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	mediaType := mediaTypeForExt(strings.ToLower(filepath.Ext(filePath)))

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("media", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("copy file: %w", err)
	}
	w.Close()

	url := fmt.Sprintf("https://oapi.dingtalk.com/media/upload?access_token=%s&type=%s", token, mediaType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	var mr mediaResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if mr.ErrCode != 0 {
		return "", fmt.Errorf("api error (code %d): %s", mr.ErrCode, mr.ErrMsg)
	}
	return mr.MediaID, nil
}

// mediaTypeForExt maps file extensions to DingTalk media upload types.
// Supported types: image, voice, video, file.
func mediaTypeForExt(ext string) string {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".amr", ".mp3", ".wav", ".ogg", ".aac", ".m4a":
		return "voice"
	case ".mp4", ".avi", ".mov", ".wmv", ".flv":
		return "video"
	default:
		return "file"
	}
}
