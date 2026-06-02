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
)

// httpClient with a sensible timeout for DingTalk API calls.
var httpClient = &http.Client{Timeout: 30 * time.Second}

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

func sendMessage(ctx context.Context, token, chatID, content, msgType string) error {
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": msgType,
	}
	switch msgType {
	case "text":
		body["text"] = map[string]string{"content": content}
	default:
		body["msgtype"] = "markdown"
		body["markdown"] = map[string]string{
			"title": "Dolphin",
			"text":  content,
		}
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
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode response: %s (raw: %s)", err, string(respBody))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("api error: %s (code %d)", result.ErrMsg, result.ErrCode)
	}

	return nil
}

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
