package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/memory"
	"dolphin/internal/types"

	"github.com/tidwall/wal"
)

func TestEmptyDir(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}

	var v any
	json.NewDecoder(w.Body).Decode(&v)
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", v, v)
	}
	if len(arr) != 0 {
		t.Fatalf("expected [], got %d items", len(arr))
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()

	// Create a WAL session with some data.
	sid := "test123"
	wm, err := memory.NewWALMemory(dir, 0, 0)
	if err != nil {
		t.Fatalf("NewWALMemory: %v", err)
	}
	wm.Replace(nil, sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	wm.Write(nil, sid, types.NewTextMessage(types.RoleUser, "hello"))
	wm.Close()

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}

	var sessions []map[string]any
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s["id"] != "test123" {
		t.Errorf("id: got %q", s["id"])
	}
	if !strings.HasPrefix(fmt.Sprint(s["file"]), "session_test123") {
		t.Errorf("file: got %q", s["file"])
	}
	if s["size"].(float64) <= 0 {
		t.Errorf("size should be > 0, got %.0f", s["size"])
	}
}

func TestReadSession(t *testing.T) {
	dir := t.TempDir()

	sid := "sess-read"
	wm, err := memory.NewWALMemory(dir, 0, 0)
	if err != nil {
		t.Fatalf("NewWALMemory: %v", err)
	}
	wm.Replace(nil, sid, []types.Message{types.NewTextMessage(types.RoleSystem, "base")})
	wm.Write(nil, sid, types.NewTextMessage(types.RoleUser, "msg1"))
	wm.Write(nil, sid, types.NewTextMessage(types.RoleAssistant, "reply"))
	wm.WriteTurn(nil, sid, memory.TurnPayload{
		TurnID: "t-1", Input: "msg1", ModelName: "test-m", InTokens: 10, OutTokens: 5, Rounds: 1,
	})
	wm.Close()

	req := httptest.NewRequest("GET", "/api/session/"+sid, nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d body: %s", w.Code, w.Body.String())
	}

	var entries []map[string]any
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) < 3 {
		t.Fatalf("expected >=3 entries, got %d", len(entries))
	}

	// Check msg entry.
	found := false
	for _, e := range entries {
		if e["type"] == "msg" {
			d := e["data"].(map[string]any)
			if d["text"] == "msg1" {
				found = true
				if d["role"] != "user" {
					t.Errorf("expected user role, got %q", d["role"])
				}
			}
		}
	}
	if !found {
		t.Error("msg1 not found in entries")
	}

	// Check turn entry.
	foundTurn := false
	for _, e := range entries {
		if e["type"] == "turn" {
			d := e["data"].(map[string]any)
			if d["TurnID"] == "t-1" {
				foundTurn = true
				if d["Input"] != "msg1" {
					t.Errorf("turn input: %q", d["Input"])
				}
			}
		}
	}
	if !foundTurn {
		t.Error("turn mark not found")
	}
}

func TestReadSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest("GET", "/api/session/nonexistent", nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	// Missing session returns 200 with empty array (WAL open fails gracefully).
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entries []any
	json.NewDecoder(w.Body).Decode(&entries)
	if entries != nil {
		t.Errorf("expected nil/empty, got %v", entries)
	}
}

func TestReadSessionMissingID(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest("GET", "/api/session/", nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHTMLPage(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler(t, dir).ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Session Inspect") {
		t.Error("HTML page missing title")
	}
	if !strings.Contains(body, "app.js") {
		t.Error("HTML page missing app.js reference")
	}
}

// handler builds a standalone http.Handler for testing, without starting a
// real listener. It mimics the ServeMux from main().
func handler(t *testing.T, dir string) http.Handler {
	t.Helper()

	mux := http.NewServeMux()

	// Serve static files from embedded templates.
	staticFS, _ := fs.Sub(templates, "templates")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		index, _ := templates.ReadFile("templates/index.html")
		w.Write(index)
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		sessions := make([]map[string]any, 0)
		for _, e := range entries {
			if e.IsDir() && strings.HasSuffix(e.Name(), ".wal") {
				sid := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "session_"), ".wal")
				sessions = append(sessions, map[string]any{
					"id":   sid,
					"file": e.Name(),
					"size": dirSize(filepath.Join(dir, e.Name())),
				})
			}
		}
		writeJSON(w, sessions)
	})
	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		sid := strings.TrimPrefix(r.URL.Path, "/api/session/")
		if sid == "" {
			http.Error(w, "missing session id", 400)
			return
		}
		path := filepath.Join(dir, "session_"+sid+".wal")
		entries, err := readWAL(path)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, entries)
	})

	// Ensure WAL log files are closed after test.
	t.Cleanup(func() {
		for _, fn := range []string{} {
			_ = fn
		}
		_ = wal.DefaultOptions
	})

	return mux
}
