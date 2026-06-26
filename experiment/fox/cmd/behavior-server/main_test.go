package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── helpers ──────────────────────────────────────────────────────

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewServer(dir, 50, 7)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s, dir
}

func startSession(t *testing.T, s *Server) string {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/start", toBody(map[string]any{
		"session_id":  "test-sess-1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"chrome_info": map[string]string{"version": "130", "os": "macOS"},
	}))
	s.handleSessionStart(w, r)
	if w.Code != 201 {
		t.Fatalf("start session: %d %s", w.Code, w.Body.String())
	}
	return "test-sess-1"
}

func sendEvents(t *testing.T, s *Server, sessionID string, count int, domain string) {
	t.Helper()
	for batch := 1; batch <= (count+49)/50; batch++ {
		n := min(50, count-(batch-1)*50)
		events := make([]Event, n)
		for i := 0; i < n; i++ {
			events[i] = Event{
				S:      sessionID,
				Seq:    int64((batch-1)*50 + i + 1),
				TS:     time.Now().UTC().Format(time.RFC3339),
				Domain: domain,
				Path:   "/test/page",
				Type:   "click",
				P:      json.RawMessage(`{"el":"button","text":"Test","sel":"button.test","m":"class"}`),
			}
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/session/"+sessionID+"/events", toBody(map[string]any{
			"events":    events,
			"batch_seq": batch,
		}))
		s.handleEvents(w, r)
		if w.Code != 200 {
			t.Fatalf("send events batch %d: %d %s", batch, w.Code, w.Body.String())
		}
	}
}

func endSession(t *testing.T, s *Server, sessionID string) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/"+sessionID+"/end", toBody(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}))
	s.handleSessionEnd(w, r)
	if w.Code != 200 {
		t.Fatalf("end session: %d %s", w.Code, w.Body.String())
	}
}

func toBody(v any) *strings.Reader {
	b, _ := json.Marshal(v)
	return strings.NewReader(string(b))
}

// ─── tests ────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil)
	s.handleHealth(w, r)

	if w.Code != 200 {
		t.Fatalf("health: %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

func TestFullSessionLifecycle(t *testing.T) {
	s, dir := newTestServer(t)

	// 1. Start
	startSession(t, s)

	// verify .meta.json created
	metaPath := filepath.Join(dir, "test-sess-1.meta.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("meta.json not created")
	}

	// verify .jsonl created
	jsonlPath := filepath.Join(dir, "test-sess-1.jsonl")
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		t.Fatal("jsonl not created")
	}

	// 2. Send 80 events (2 batches)
	sendEvents(t, s, "test-sess-1", 80, "github.com")

	// verify .jsonl has 80 lines
	lines, domains := countLines(jsonlPath)
	if lines != 80 {
		t.Errorf("jsonl lines = %d, want 80", lines)
	}
	if len(domains) != 1 || domains[0] != "github.com" {
		t.Errorf("domains = %v, want [github.com]", domains)
	}

	// 3. End session
	endSession(t, s, "test-sess-1")

	// verify meta status=completed
	data, _ := os.ReadFile(metaPath)
	var meta SessionMeta
	json.Unmarshal(data, &meta)
	if meta.Status != "completed" {
		t.Errorf("status = %s, want completed", meta.Status)
	}
	if meta.EventCount != 80 {
		t.Errorf("event_count = %d, want 80", meta.EventCount)
	}
	if meta.EndedAt == "" {
		t.Error("ended_at not set")
	}

	// 4. Get session
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/session/test-sess-1", nil)
	s.handleSessionGet(w, r)
	if w.Code != 200 {
		t.Fatalf("get session: %d", w.Code)
	}
	var getResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &getResp)
	events := getResp["events"].([]any)
	if len(events) != 80 {
		t.Errorf("get events count = %d, want 80", len(events))
	}
}

func TestSessionList(t *testing.T) {
	s, _ := newTestServer(t)

	// create 3 sessions
	for i := 1; i <= 3; i++ {
		sid := fmt.Sprintf("sess-%d", i)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/session/start", toBody(map[string]any{
			"session_id": sid,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		}))
		s.handleSessionStart(w, r)
	}

	// list
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	s.handleSessionsList(w, r)
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}

	var sessions []SessionMeta
	json.Unmarshal(w.Body.Bytes(), &sessions)
	if len(sessions) != 3 {
		t.Errorf("sessions = %d, want 3", len(sessions))
	}
}

func TestDeleteSession(t *testing.T) {
	s, dir := newTestServer(t)
	startSession(t, s)
	sendEvents(t, s, "test-sess-1", 10, "example.com")

	// delete
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/test-sess-1/delete", nil)
	s.handleSessionDelete(w, r)
	if w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}

	// verify files removed
	metaPath := filepath.Join(dir, "test-sess-1.meta.json")
	jsonlPath := filepath.Join(dir, "test-sess-1.jsonl")
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("meta.json not deleted")
	}
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Error("jsonl not deleted")
	}
}

func TestIdempotentBatchSeq(t *testing.T) {
	s, dir := newTestServer(t)
	startSession(t, s)

	// send batch 1
	events := []Event{{
		S: "test-sess-1", Seq: 1, TS: time.Now().UTC().Format(time.RFC3339),
		Domain: "github.com", Path: "/", Type: "click",
		P: json.RawMessage(`{}`),
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/test-sess-1/events", toBody(map[string]any{
		"events": events, "batch_seq": 1,
	}))
	s.handleEvents(w, r)

	// send batch 1 again (duplicate)
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/session/test-sess-1/events", toBody(map[string]any{
		"events": events, "batch_seq": 1,
	}))
	s.handleEvents(w2, r2)

	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["received"].(float64) != 0 {
		t.Errorf("duplicate batch: received=%v, want 0", resp["received"])
	}

	// verify only 1 line written
	lines, _ := countLines(filepath.Join(dir, "test-sess-1.jsonl"))
	if lines != 1 {
		t.Errorf("lines = %d, want 1 (idempotent)", lines)
	}
}

func TestConcurrentWrites(t *testing.T) {
	s, dir := newTestServer(t)
	startSession(t, s)

	// shared batch_seq counter + mutex (mimics SW single-thread batch assignment)
	var seqMu sync.Mutex
	nextSeq := 0

	// 4 goroutines each sending 25 events concurrently
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				seqMu.Lock()
				nextSeq++
				batch := nextSeq
				seqMu.Unlock()

				ev := Event{
					S: "test-sess-1", Seq: int64(batch),
					TS:     time.Now().UTC().Format(time.RFC3339),
					Domain: "github.com", Path: "/", Type: "click",
					P: json.RawMessage(`{}`),
				}
				w := httptest.NewRecorder()
				r := httptest.NewRequest("POST", "/api/session/test-sess-1/events", toBody(map[string]any{
					"events": []Event{ev}, "batch_seq": batch,
				}))
				s.handleEvents(w, r)
				if w.Code != 200 {
					t.Errorf("goroutine %d event %d: HTTP %d", gid, i, w.Code)
				}
			}
		}(g)
	}
	wg.Wait()

	// verify all 100 events written
	lines, _ := countLines(filepath.Join(dir, "test-sess-1.jsonl"))
	if lines != 100 {
		t.Errorf("concurrent lines = %d, want 100", lines)
	}

	// verify no corruption — every line is valid JSON
	f, _ := os.Open(filepath.Join(dir, "test-sess-1.jsonl"))
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("corrupt line: %s (err: %v)", line[:min(80, len(line))], err)
		}
	}
	_ = scanner.Err()
}

func TestRecoveryOnRestart(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewServer(dir, 50, 7)

	// create 2 sessions, end only 1
	startSession(t, s1)
	sendEvents(t, s1, "test-sess-1", 10, "github.com")
	endSession(t, s1, "test-sess-1")

	// session 2 — started but not ended (simulate crash)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/start", toBody(map[string]any{
		"session_id": "test-sess-2",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}))
	s1.handleSessionStart(w, r)
	sendEvents(t, s1, "test-sess-2", 20, "jira.example.com")

	// "crash" — create new server
	s2, _ := NewServer(dir, 50, 7)

	// session 1: should be completed
	meta1Path := filepath.Join(dir, "test-sess-1.meta.json")
	data, _ := os.ReadFile(meta1Path)
	var meta1 SessionMeta
	json.Unmarshal(data, &meta1)
	if meta1.Status != "completed" {
		t.Errorf("sess-1 status = %s, want completed", meta1.Status)
	}
	if meta1.EventCount != 10 {
		t.Errorf("sess-1 event_count = %d, want 10", meta1.EventCount)
	}

	// session 2: should be aborted
	meta2Path := filepath.Join(dir, "test-sess-2.meta.json")
	data2, _ := os.ReadFile(meta2Path)
	var meta2 SessionMeta
	json.Unmarshal(data2, &meta2)
	if meta2.Status != "aborted" {
		t.Errorf("sess-2 status = %s, want aborted", meta2.Status)
	}
	if meta2.EventCount != 20 {
		t.Errorf("sess-2 event_count = %d, want 20", meta2.EventCount)
	}

	// server should have no open writers for aborted session
	s2.mu.RLock()
	_, hasWriter := s2.writers["test-sess-2"]
	s2.mu.RUnlock()
	if hasWriter {
		t.Error("aborted session should not have open writer")
	}
}

func TestStartWithoutSessionID(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/start", toBody(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}))
	s.handleSessionStart(w, r)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestEventsForUnknownSession(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/nonexistent/events", toBody(map[string]any{
		"events":    []Event{},
		"batch_seq": 1,
	}))
	s.handleEvents(w, r)
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestJSONLFileContent(t *testing.T) {
	s, dir := newTestServer(t)
	startSession(t, s)

	// send single event with complex payload
	payload := json.RawMessage(`{"el":"button","text":"Create PR","sel":"[data-testid=\"pr-btn\"]","m":"data-testid","alts":[{"s":"button.new-pr","m":"class","st":0.4}]}`)
	ev := Event{
		S: "test-sess-1", Seq: 1,
		TS:     "2026-06-26T10:30:00Z",
		Domain: "github.com",
		Path:   "/dolphin/pulls",
		Type:   "click",
		Tab:    42,
		P:      payload,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session/test-sess-1/events", toBody(map[string]any{
		"events": []Event{ev}, "batch_seq": 1,
	}))
	s.handleEvents(w, r)

	// read the .jsonl file
	jsonlPath := filepath.Join(dir, "test-sess-1.jsonl")
	f, _ := os.Open(jsonlPath)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	line := scanner.Text()
	_ = scanner.Err()

	// parse and verify
	var parsed Event
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if parsed.S != "test-sess-1" {
		t.Errorf("s = %s", parsed.S)
	}
	if parsed.Type != "click" {
		t.Errorf("type = %s", parsed.Type)
	}
	if parsed.Domain != "github.com" {
		t.Errorf("domain = %s", parsed.Domain)
	}
	if parsed.Tab != 42 {
		t.Errorf("tab = %d", parsed.Tab)
	}
	// verify payload round-trips
	var p struct {
		El   string `json:"el"`
		Text string `json:"text"`
		Sel  string `json:"sel"`
	}
	json.Unmarshal(parsed.P, &p)
	if p.El != "button" {
		t.Errorf("payload.el = %s", p.El)
	}
	if p.Text != "Create PR" {
		t.Errorf("payload.text = %s", p.Text)
	}
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewServer(dir, 2, 365) // max 2 sessions, 365 day retention

	// create 3 sessions
	for i := 1; i <= 3; i++ {
		sid := fmt.Sprintf("sess-%d", i)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/session/start", toBody(map[string]any{
			"session_id": sid,
			"timestamp":  time.Now().Add(-time.Duration(4-i) * 24 * time.Hour).UTC().Format(time.RFC3339),
		}))
		s.handleSessionStart(w, r)
		sendEvents(t, s, sid, 5, "github.com")
		endSession(t, s, sid)
	}

	// run cleanup
	s.cleanup()

	// should keep 2 newest
	entries, _ := os.ReadDir(dir)
	metaCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".meta.json") {
			metaCount++
		}
	}
	if metaCount != 2 {
		t.Errorf("after cleanup: %d meta files, want 2", metaCount)
	}
}
