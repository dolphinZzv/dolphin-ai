//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pandaServer   = "http://127.0.0.1:8080"
	pandaAccount  = "dolphin"
	pandaPassword = "dolphin..*"
)

type loginRes struct {
	UserID string `json:"user_id"`
	Token  string `json:"token"`
}

type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func login(t *testing.T) string {
	t.Helper()
	body := fmt.Sprintf(`{"account":"%s","password":"%s"}`, pandaAccount, pandaPassword)
	resp, err := http.Post(pandaServer+"/api/v1/users/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal("login request:", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var env apiResp
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal("parse login response:", err)
	}
	if env.Code != 0 {
		t.Fatalf("login failed: code=%d msg=%s", env.Code, env.Msg)
	}
	var r loginRes
	if err := json.Unmarshal(env.Data, &r); err != nil {
		t.Fatal("parse login data:", err)
	}
	t.Logf("login success: user_id=%s", r.UserID)
	return r.Token
}

func TestIntegration_FileUpload_Image(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}
	token := login(t)

	// Create minimal valid PNG (1x1 pixel)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0x60, 0x60, 0x00, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xE5, 0x27, 0xDE, 0xFC,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44,
		0xAE, 0x42, 0x60, 0x82,
	}
	tmpFile := filepath.Join(t.TempDir(), "test_1px.png")
	if err := os.WriteFile(tmpFile, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	s := &pandaSource{serverURL: pandaServer}
	result, err := s.uploadFile(context.Background(), token, tmpFile)
	if err != nil {
		t.Fatalf("uploadFile failed: %v", err)
	}
	t.Logf("Upload result: file_id=%s url=%s size=%d name=%s width=%d height=%d",
		result.FileID, result.URL, result.Size, result.Name, result.Width, result.Height)

	if result.FileID == "" {
		t.Fatal("expected non-empty file_id")
	}
	if result.URL == "" {
		t.Fatal("expected non-empty url")
	}
	if result.Name != "test_1px.png" {
		t.Fatalf("expected 'test_1px.png', got '%s'", result.Name)
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Fatal("expected image dimensions > 0")
	}
}

func TestIntegration_FileUpload_TextFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}
	token := login(t)

	tmpFile := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(tmpFile, []byte("Hello, panda-ai!"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &pandaSource{serverURL: pandaServer}
	result, err := s.uploadFile(context.Background(), token, tmpFile)
	if err != nil {
		t.Fatalf("uploadFile failed: %v", err)
	}
	t.Logf("Upload result: file_id=%s url=%s size=%d name=%s",
		result.FileID, result.URL, result.Size, result.Name)

	if result.FileID == "" {
		t.Fatal("expected non-empty file_id")
	}
	if result.URL == "" {
		t.Fatal("expected non-empty url")
	}
	if result.Name != "hello.txt" {
		t.Fatalf("expected 'hello.txt', got '%s'", result.Name)
	}
}

func TestIntegration_FileUpload_Audio(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in short mode")
	}
	token := login(t)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, make([]byte, 512), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &pandaSource{serverURL: pandaServer}
	result, err := s.uploadFile(context.Background(), token, tmpFile)
	if err != nil {
		t.Fatalf("uploadFile failed: %v", err)
	}
	t.Logf("Upload result: file_id=%s url=%s size=%d name=%s",
		result.FileID, result.URL, result.Size, result.Name)
}
