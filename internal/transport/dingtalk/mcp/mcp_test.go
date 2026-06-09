package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/h2non/gock"
	"go.uber.org/zap"

	"dolphin/internal/transport"
	"dolphin/internal/types"
)

func TestMediaTypeForExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".png", "image"},
		{".jpg", "image"},
		{".jpeg", "image"},
		{".gif", "image"},
		{".webp", "image"},
		{".bmp", "image"},
		{".amr", "voice"},
		{".mp3", "voice"},
		{".wav", "voice"},
		{".ogg", "voice"},
		{".aac", "voice"},
		{".m4a", "voice"},
		{".mp4", "video"},
		{".avi", "video"},
		{".mov", "video"},
		{".wmv", "video"},
		{".flv", "video"},
		{".pdf", "file"},
		{".zip", "file"},
		{".docx", "file"},
		{".unknown", "file"},
		{"", "file"},
	}
	for _, tc := range tests {
		got := mediaTypeForExt(tc.ext)
		if got != tc.want {
			t.Errorf("mediaTypeForExt(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestGetAccessToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			MatchParams(map[string]string{"appkey": "test-key", "appsecret": "test-secret"}).
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "abc123"})

		token, err := getAccessToken(context.Background(), "test-key", "test-secret")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "abc123" {
			t.Errorf("got token %q, want %q", token, "abc123")
		}
	})

	t.Run("api error", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			Reply(200).
			JSON(map[string]any{"errcode": 400, "errmsg": "invalid appkey"})

		_, err := getAccessToken(context.Background(), "bad-key", "bad-secret")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("http error", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			ReplyError(connectionError())

		_, err := getAccessToken(context.Background(), "key", "secret")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestSendMessage(t *testing.T) {
	token := "test-token"

	t.Run("text message", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			MatchHeader("Content-Type", "application/json").
			JSON(map[string]any{
				"chatid":  "chat-1",
				"msgtype": "text",
				"text":    map[string]any{"content": "hello"},
			}).
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok"})

		err := sendMessage(context.Background(), token, "chat-1", "hello", "text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("markdown message", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok"})

		err := sendMessage(context.Background(), token, "chat-2", "**bold**", "markdown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("api error", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			Reply(200).
			JSON(map[string]any{"errcode": 400, "errmsg": "invalid token"})

		err := sendMessage(context.Background(), "bad-token", "chat-3", "hi", "text")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestSendFileMessage(t *testing.T) {
	token := "test-token"

	t.Run("success", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "chatid": "chat-1", "messageId": "msg-1"})

		err := sendFileMessage(context.Background(), token, "chat-1", "media-1", "file.pdf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("api error", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			Reply(200).
			JSON(map[string]any{"errcode": 400, "errmsg": "bad request"})

		err := sendFileMessage(context.Background(), token, "chat-x", "media-x", "bad.pdf")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUploadMedia(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test.png")
		if err := os.WriteFile(filePath, []byte("fake-png-data"), 0644); err != nil {
			t.Fatal(err)
		}

		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/media/upload").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "media_id": "media-uploaded-1"})

		mediaID, err := uploadMedia(context.Background(), "token", filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mediaID != "media-uploaded-1" {
			t.Errorf("got media_id %q, want %q", mediaID, "media-uploaded-1")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := uploadMedia(context.Background(), "token", "/nonexistent/file.png")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("api error", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "doc.pdf")
		if err := os.WriteFile(filePath, []byte("pdf-data"), 0644); err != nil {
			t.Fatal(err)
		}

		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Post("/media/upload").
			Reply(200).
			JSON(map[string]any{"errcode": 400, "errmsg": "upload failed"})

		_, err := uploadMedia(context.Background(), "token", filePath)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

// dingtalkCtx returns a context with dingtalk transport info.
func dingtalkCtx() context.Context {
	return transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
}

func TestList(t *testing.T) {
	t.Run("returns tools when dingtalk transport", func(t *testing.T) {
		s := NewFileUploadSource("client-id", "client-secret", func() string { return "conv-1" }, nil)
		tools, err := s.List(dingtalkCtx())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tools) != 2 {
			t.Fatalf("got %d tools, want 2", len(tools))
		}
		if tools[0].Name != "FILE_UPLOAD" {
			t.Errorf("first tool name = %q, want FILE_UPLOAD", tools[0].Name)
		}
		if tools[1].Name != "MESSAGE" {
			t.Errorf("second tool name = %q, want MESSAGE", tools[1].Name)
		}
	})

	t.Run("returns nil for non-dingtalk transport", func(t *testing.T) {
		ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "stdio"})
		s := NewFileUploadSource("client-id", "client-secret", func() string { return "conv-1" }, nil)
		tools, err := s.List(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tools != nil {
			t.Errorf("expected nil tools for non-dingtalk transport, got %d", len(tools))
		}
	})

	t.Run("returns nil when credentials empty", func(t *testing.T) {
		s := NewFileUploadSource("", "", func() string { return "conv-1" }, nil)
		tools, err := s.List(dingtalkCtx())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tools != nil {
			t.Errorf("expected nil tools when credentials empty, got %d", len(tools))
		}
	})
}

func TestExecute(t *testing.T) {
	t.Run("returns error for non-dingtalk context", func(t *testing.T) {
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, nil)
		ctx := context.Background()
		_, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hi"}`})
		if err == nil {
			t.Fatal("expected error for non-dingtalk context")
		}
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, nil)
		_, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "UNKNOWN", Arguments: "{}"})
		if err == nil {
			t.Fatal("expected error for unknown tool")
		}
	})
}

func TestExecuteMessage(t *testing.T) {
	t.Run("invalid arguments", func(t *testing.T) {
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "MESSAGE", Arguments: `{bad json}`})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError {
			t.Error("expected error result for invalid json")
		}
	})

	t.Run("empty content", func(t *testing.T) {
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "MESSAGE", Arguments: `{"content":""}`})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError {
			t.Error("expected error result for empty content")
		}
	})

	t.Run("no conversation ID", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "tk-1"})

		s := NewFileUploadSource("id", "secret", func() string { return "" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hi"}`})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError {
			t.Error("expected error result when no conversation ID")
		}
	})

	t.Run("token api error", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			Reply(200).
			JSON(map[string]any{"errcode": 400, "errmsg": "bad appkey"})

		s := NewFileUploadSource("bad-id", "bad-secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hi"}`})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError {
			t.Error("expected error result when token request fails")
		}
	})
}

func TestExecuteFileUpload(t *testing.T) {
	t.Run("invalid arguments", func(t *testing.T) {
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "FILE_UPLOAD", Arguments: `{bad}`})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError {
			t.Error("expected error result for invalid json")
		}
	})

	t.Run("token api error then success", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "tk-1"})

		gock.New("https://oapi.dingtalk.com").
			Post("/media/upload").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "media_id": "mid-1"})

		gock.New("https://oapi.dingtalk.com").
			Post("/chat/send").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok"})

		dir := t.TempDir()
		filePath := filepath.Join(dir, "test.pdf")
		if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		args, _ := json.Marshal(map[string]string{"file_path": filePath})
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "FILE_UPLOAD", Arguments: string(args)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil || result.IsError {
			t.Errorf("expected success, got IsError=%v content=%s", result.IsError, result.Content)
		}
	})

	t.Run("image upload returns inline markdown", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(httpClient)
		gock.New("https://oapi.dingtalk.com").
			Get("/gettoken").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "tk-img"})

		gock.New("https://oapi.dingtalk.com").
			Post("/media/upload").
			Reply(200).
			JSON(map[string]any{"errcode": 0, "errmsg": "ok", "media_id": "mid-img"})

		dir := t.TempDir()
		filePath := filepath.Join(dir, "photo.png")
		if err := os.WriteFile(filePath, []byte("png-data"), 0644); err != nil {
			t.Fatal(err)
		}

		args, _ := json.Marshal(map[string]string{"file_path": filePath})
		s := NewFileUploadSource("id", "secret", func() string { return "conv-1" }, zap.NewNop())
		result, err := s.Execute(dingtalkCtx(), types.ToolCall{Name: "FILE_UPLOAD", Arguments: string(args)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil || result.IsError {
			t.Fatalf("expected success, got IsError=%v", result.IsError)
		}
		if len(result.Content) == 0 {
			t.Error("expected non-empty content for image upload")
		}
	})
}

func TestNewFileUploadSource(t *testing.T) {
	s := NewFileUploadSource("id", "secret", func() string { return "conv" }, nil)
	if s == nil {
		t.Fatal("NewFileUploadSource returned nil")
	}

	s2 := NewFileUploadSource("", "", nil, nil)
	if s2 == nil {
		t.Fatal("NewFileUploadSource returned nil for empty params")
	}
}

// connectionError returns a connection error suitable for gock ReplyError.
func connectionError() error {
	return &connErr{}
}

type connErr struct{}

func (e *connErr) Error() string   { return "connection refused" }
func (e *connErr) Timeout() bool   { return false }
func (e *connErr) Temporary() bool { return false }
