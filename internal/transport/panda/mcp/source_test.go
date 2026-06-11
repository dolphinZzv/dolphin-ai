package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dolphin/internal/transport"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

func TestPandaSource_List_WithPandaContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	tools, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "FILE_UPLOAD" {
		t.Fatalf("expected first tool to be FILE_UPLOAD, got %s", tools[0].Name)
	}
	if tools[1].Name != "MESSAGE" {
		t.Fatalf("expected second tool to be MESSAGE, got %s", tools[1].Name)
	}
	if tools[0].Schema == nil {
		t.Fatal("expected FILE_UPLOAD schema")
	}
	if tools[1].Schema == nil {
		t.Fatal("expected MESSAGE schema")
	}
}

func TestPandaSource_List_WithoutPandaContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	// No transport info
	tools, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tools != nil {
		t.Fatal("expected nil tools when not panda transport")
	}
}

func TestPandaSource_List_WrongTransport(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
	tools, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tools != nil {
		t.Fatal("expected nil tools for non-panda transport")
	}
}

func TestPandaSource_Execute_NoContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	_, err := s.Execute(context.Background(), types.ToolCall{Name: "FILE_UPLOAD"})
	if err == nil {
		t.Fatal("expected error when no panda context")
	}
}

func TestPandaSource_Execute_WrongContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
	_, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD"})
	if err == nil {
		t.Fatal("expected error when wrong transport context")
	}
}

func TestPandaSource_Execute_UnknownTool(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	_, err := s.Execute(ctx, types.ToolCall{Name: "UNKNOWN"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestPandaSource_NilLogger(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, nil)
	if s == nil {
		t.Fatal("expected non-nil source")
	}
}

// --- FILE_UPLOAD tests ---

func TestFileUpload_InvalidArgs(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: `{invalid}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid args")
	}
}

func TestFileUpload_NoToken(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: `{"file_path":"/tmp/test.txt"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty token")
	}
	if result.Content != "not authenticated" {
		t.Fatalf("expected 'not authenticated', got '%s'", result.Content)
	}
}

func TestFileUpload_FileNotFound(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: `{"file_path":"/tmp/nonexistent_xyz.txt"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestFileUpload_Success_Image(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(tmpFile, []byte("fake-png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/upload" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		// Verify auth header
		if r.Header.Get("Authorization") != "Bearer tok123" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		// Verify multipart
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("file_type") != "0" {
			t.Fatalf("expected file_type=0 for image, got %s", r.FormValue("file_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"file_id":"file_1","url":"http://example.com/file_1.png","size":123,"name":"test.png","width":100,"height":200}}`))
	}))
	defer srv.Close()

	s := NewFileUploadSource(srv.URL, func() string { return "tok123" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tmpFile)})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !contains(result.Content, "test.png") {
		t.Fatalf("expected filename in result, got: %s", result.Content)
	}
	if !contains(result.Content, "http://example.com/file_1.png") {
		t.Fatalf("expected url in result, got: %s", result.Content)
	}
	if !contains(result.Content, "100x200") {
		t.Fatalf("expected dimensions in result, got: %s", result.Content)
	}
	if !contains(result.Content, "![test.png]") {
		t.Fatalf("expected markdown snippet in result, got: %s", result.Content)
	}
}

func TestFileUpload_Success_NonImage(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(tmpFile, []byte("fake-pdf-data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/upload" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		r.ParseMultipartForm(10 << 20)
		if r.FormValue("file_type") != "1" {
			t.Fatalf("expected file_type=1 for file, got %s", r.FormValue("file_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"file_id":"file_2","url":"http://example.com/file_2.pdf","size":456,"name":"test.pdf","width":0,"height":0}}`))
	}))
	defer srv.Close()

	s := NewFileUploadSource(srv.URL, func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tmpFile)})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if contains(result.Content, "![" ) {
		t.Fatal("non-image result should not contain markdown image syntax")
	}
}

func TestFileUpload_ServerError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	s := NewFileUploadSource(srv.URL, func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tmpFile)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for server error response")
	}
}

func TestFileUpload_ApiError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":400,"msg":"bad request","data":null}`))
	}))
	defer srv.Close()

	s := NewFileUploadSource(srv.URL, func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tmpFile)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-zero API code")
	}
}

func TestFileUpload_InvalidResponseData(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":"not-an-object"}`))
	}))
	defer srv.Close()

	s := NewFileUploadSource(srv.URL, func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "FILE_UPLOAD", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tmpFile)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid response data")
	}
}

// --- fileTypeForExt tests (via integration with upload handler) ---

func TestFileTypeForExt(t *testing.T) {
	tests := []struct {
		ext      string
		expected int
	}{
		{".png", 0}, {".jpg", 0}, {".jpeg", 0}, {".gif", 0}, {".webp", 0}, {".bmp", 0},
		{".mp3", 2}, {".wav", 2}, {".ogg", 2}, {".aac", 2}, {".m4a", 2}, {".amr", 2},
		{".mp4", 3}, {".avi", 3}, {".mov", 3}, {".wmv", 3}, {".flv", 3},
		{".pdf", 1}, {".doc", 1}, {".zip", 1}, {".txt", 1}, {".exe", 1}, {"", 1},
	}
	for _, tt := range tests {
		got := fileTypeForExt(tt.ext)
		if got != tt.expected {
			t.Errorf("fileTypeForExt(%q) = %d, want %d", tt.ext, got, tt.expected)
		}
	}
}

// --- MESSAGE tests ---

func TestMessage_InvalidArgs(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{invalid}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid args")
	}
}

func TestMessage_EmptyContent(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":""}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty content")
	}
}

func TestMessage_Success(t *testing.T) {
	var got string
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error {
		got = text
		return nil
	}, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hello from mcp"}`})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if got != "hello from mcp" {
		t.Fatalf("expected 'hello from mcp', got '%s'", got)
	}
}

func TestMessage_WriteError(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error {
		return fmt.Errorf("write failed")
	}, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hello"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for write failure")
	}
}

// --- uploadFile with invalid response JSON ---

func TestUploadFile_InvalidResponseJSON(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	s := &pandaSource{
		serverURL: srv.URL,
		token:     func() string { return "tok" },
		writeFn:   func(ctx context.Context, text string) error { return nil },
		logger:    zap.NewNop(),
	}

	_, err := s.uploadFile(context.Background(), "tok", tmpFile)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNewFileUploadSource_Source tests that the returned source implements tool.Executor.
func TestNewFileUploadSource_Source(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, zap.NewNop())
	if _, ok := s.(interface {
		List(ctx context.Context) ([]types.ToolDef, error)
		Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error)
	}); !ok {
		t.Fatal("NewFileUploadSource should return an executor")
	}
}
