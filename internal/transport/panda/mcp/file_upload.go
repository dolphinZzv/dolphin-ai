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

	"dolphin/internal/types"

	"go.uber.org/zap"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type uploadResp struct {
	FileID string `json:"file_id"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func (s *pandaSource) executeFileUpload(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return &types.ToolResult{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	s.logger.Info("FILE_UPLOAD tool called", zap.String("file_path", args.FilePath))

	token := s.token()
	if token == "" {
		return &types.ToolResult{Content: "not authenticated", IsError: true}, nil
	}

	resp, err := s.uploadFile(ctx, token, args.FilePath)
	if err != nil {
		return &types.ToolResult{Content: "failed to upload file: " + err.Error(), IsError: true}, nil
	}

	fileName := filepath.Base(args.FilePath)
	if resp.Width > 0 && resp.Height > 0 {
		snippet := fmt.Sprintf("\n![%s](%s)\n", fileName, resp.URL)
		return &types.ToolResult{
			Content: fmt.Sprintf("Image uploaded successfully.\n- url: %s\n- size: %d bytes\n- dimensions: %dx%d\n\nInclude this markdown in your reply to show it inline:\n%s", resp.URL, resp.Size, resp.Width, resp.Height, snippet),
		}, nil
	}

	return &types.ToolResult{
		Content: fmt.Sprintf("File uploaded successfully.\n- name: %s\n- url: %s\n- size: %d bytes", fileName, resp.URL, resp.Size),
	}, nil
}

func (s *pandaSource) uploadFile(ctx context.Context, token, filePath string) (*uploadResp, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	fileType := fileTypeForExt(ext)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, fmt.Errorf("copy file: %w", err)
	}

	w.WriteField("file_type", fmt.Sprintf("%d", fileType))
	w.Close()

	serverURL := strings.TrimRight(s.serverURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/files/upload", &buf)
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(respData))
	}

	var envelope apiResponse
	if err := json.Unmarshal(respData, &envelope); err != nil {
		return nil, fmt.Errorf("parse response envelope: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("upload rejected (code %d): %s", envelope.Code, envelope.Msg)
	}

	var result uploadResp
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return nil, fmt.Errorf("parse upload data: %w", err)
	}

	return &result, nil
}

func fileTypeForExt(ext string) int {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return 0 // image
	case ".mp3", ".wav", ".ogg", ".aac", ".m4a", ".amr":
		return 2 // audio
	case ".mp4", ".avi", ".mov", ".wmv", ".flv":
		return 3 // video
	default:
		return 1 // file
	}
}
