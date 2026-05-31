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

var httpClient = &http.Client{Timeout: 30 * time.Second}

// RegisterTools registers enterprise wechat Bot MCP tools.
// botID and botSecret must be non-empty.
func RegisterTools(reg *tool.Registry, botID, botSecret string) {
	if botID == "" || botSecret == "" {
		return
	}

	reg.RegisterBuiltin("FILE_UPLOAD",
		"Upload a file (image, voice, video, document, archive, etc.) to enterprise wechat and send it to the wework group. "+
			"The file is uploaded to wework and sent directly to the group as a native file message (supports zip, pdf, doc, etc.). "+
			"For images, also include the returned markdown snippet in your reply to show it inline.",
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

			token, err := getAccessToken(ctx, botID, botSecret)
			if err != nil {
				return &types.ToolResult{Content: "failed to get wework access token: " + err.Error(), IsError: true}, nil
			}

			mediaID, err := uploadMedia(ctx, token, req.FilePath)
			if err != nil {
				return &types.ToolResult{Content: "failed to upload file to wework: " + err.Error(), IsError: true}, nil
			}

			fileName := filepath.Base(req.FilePath)
			ext := strings.ToLower(filepath.Ext(fileName))

			// For images, return markdown snippet for inline display.
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp" {
				snippet := fmt.Sprintf("\n![%s](%s)\n", fileName, mediaID)
				if err := sendImageMessage(ctx, token, mediaID); err != nil {
					return &types.ToolResult{
						Content: fmt.Sprintf("Image uploaded.\n- media_id: %s\n\nInclude this markdown in your reply:\n%s", mediaID, snippet),
					}, nil
				}
				return &types.ToolResult{
					Content: fmt.Sprintf("Image uploaded and sent to the wework group.\n- media_id: %s\n\nInclude this markdown in your reply:\n%s", mediaID, snippet),
				}, nil
			}

			// Send as native file message.
			if err := sendFileMessage(ctx, token, mediaID); err != nil {
				return &types.ToolResult{Content: "file uploaded but failed to send to wework group: " + err.Error(), IsError: true}, nil
			}

			return &types.ToolResult{
				Content: fmt.Sprintf("File sent to the wework group.\n- name: %s\n- media_id: %s\n\nThe file has been sent to the enterprise wechat group as a native file message. Mention it briefly in your markdown reply.", fileName, mediaID),
			}, nil
		},
	)
}

func getAccessToken(ctx context.Context, botID, botSecret string) (string, error) {
	body := map[string]string{
		"bot_id":     botID,
		"bot_secret": botSecret,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://qyapi.weixin.qq.com/cgi-bin/bot/gettoken", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var tr struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if tr.ErrCode != 0 {
		return "", fmt.Errorf("api error (code %d): %s", tr.ErrCode, tr.ErrMsg)
	}
	return tr.AccessToken, nil
}

func uploadMedia(ctx context.Context, token, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	uploadType := "file"
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp" {
		uploadType = "image"
	}

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

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/bot/upload?access_token=%s&type=%s", token, uploadType)
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

	var mr struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if mr.ErrCode != 0 {
		return "", fmt.Errorf("api error (code %d): %s", mr.ErrCode, mr.ErrMsg)
	}
	return mr.MediaID, nil
}

func sendFileMessage(ctx context.Context, token, mediaID string) error {
	payload := map[string]any{
		"msgtype": "file",
		"file": map[string]string{
			"media_id": mediaID,
		},
	}
	return callBotAPI(ctx, token, payload)
}

func sendImageMessage(ctx context.Context, token, mediaID string) error {
	payload := map[string]any{
		"msgtype": "image",
		"image": map[string]string{
			"media_id": mediaID,
		},
	}
	return callBotAPI(ctx, token, payload)
}

func callBotAPI(ctx context.Context, token string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/bot/send?access_token=%s", token)
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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("api error: %s (code %d)", result.ErrMsg, result.ErrCode)
	}
	return nil
}
