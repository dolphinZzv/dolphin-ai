// behavior-server — 独立本地 HTTP server, 接收 Fox Chrome 扩展推送的浏览器行为事件,
// 以 JSON Lines 格式追加写入文件.
//
// 零外部依赖, 仅 Go 标准库.
//
// 用法:
//
//	go run main.go
//	go run main.go --addr :9201 --data-dir ./sessions --retention 7 --fsync-interval 30
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── types ────────────────────────────────────────────────────────

type Event struct {
	S      string          `json:"s"`   // session_id
	Seq    int64           `json:"seq"` // global sequence
	TS     string          `json:"ts"`  // ISO8601
	Tab    int             `json:"tab"`
	Domain string          `json:"domain"`
	Path   string          `json:"path"`
	Type   string          `json:"type"`
	P      json.RawMessage `json:"p"` // payload
}

type SessionMeta struct {
	ID               string          `json:"id"`
	Label            string          `json:"label,omitempty"`
	StartedAt        string          `json:"started_at"`
	EndedAt          string          `json:"ended_at"`
	Status           string          `json:"status"`
	Domains          []string        `json:"domains"`
	EventCount       int             `json:"event_count"`
	ChromeInfo       json.RawMessage `json:"chrome_info,omitempty"`
	ExtensionVersion string          `json:"extension_version,omitempty"`
	Segments         []Segment       `json:"segments,omitempty"`
}

type Segment struct {
	StartSeq int64  `json:"start_seq"`
	EndSeq   int64  `json:"end_seq"`
	Reason   string `json:"reason"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
}

// ─── server ───────────────────────────────────────────────────────

type Server struct {
	dataDir     string
	maxSessions int
	retention   int // days

	mu      sync.RWMutex
	writers map[string]*SessionWriter // session_id → writer
}

type SessionWriter struct {
	mu          sync.Mutex
	fd          *os.File
	meta        SessionMeta
	metaPath    string
	unsynced    int
	lastSync    time.Time
	seenBatches map[int]bool // 幂等去重
}

func NewServer(dataDir string, maxSessions, retention int) (*Server, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dataDir, err)
	}
	s := &Server{
		dataDir:     dataDir,
		maxSessions: maxSessions,
		retention:   retention,
		writers:     make(map[string]*SessionWriter),
	}
	s.recover()
	go s.fsyncLoop()
	go s.cleanupLoop()
	return s, nil
}

// ─── recovery ─────────────────────────────────────────────────────

func (s *Server) recover() {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		jsonlPath := filepath.Join(s.dataDir, e.Name())
		metaPath := filepath.Join(s.dataDir, sessionID+".meta.json")

		meta := SessionMeta{ID: sessionID, Status: "aborted"}
		if data, err := os.ReadFile(metaPath); err == nil {
			json.Unmarshal(data, &meta)
		}

		// count lines
		lineCount, domains := countLines(jsonlPath)
		if meta.EventCount != lineCount {
			meta.EventCount = lineCount
		}
		if len(meta.Domains) == 0 && len(domains) > 0 {
			meta.Domains = domains
		}
		if meta.Status == "recording" || meta.Status == "paused" {
			meta.Status = "aborted"
		}

		if meta.Status == "completed" && meta.EndedAt == "" {
			meta.EndedAt = time.Now().UTC().Format(time.RFC3339)
		}

		s.writeMeta(metaPath, &meta)
		log.Printf("[recover] session=%s status=%s events=%d", sessionID, meta.Status, meta.EventCount)
	}
}

// ─── API handlers ──────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	setCORS(w)
	s.mu.RLock()
	n := len(s.writers)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"sessions_count": n,
	})
}

func (s *Server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		SessionID        string          `json:"session_id"`
		Timestamp        string          `json:"timestamp"`
		Label            string          `json:"label"`
		ChromeInfo       json.RawMessage `json:"chrome_info"`
		ExtensionVersion string          `json:"extension_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required")
		return
	}

	jsonlPath := filepath.Join(s.dataDir, req.SessionID+".jsonl")
	metaPath := filepath.Join(s.dataDir, req.SessionID+".meta.json")

	fd, err := os.OpenFile(jsonlPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open file: "+err.Error())
		return
	}

	meta := SessionMeta{
		ID:               req.SessionID,
		StartedAt:        req.Timestamp,
		Status:           "recording",
		ChromeInfo:       req.ChromeInfo,
		ExtensionVersion: req.ExtensionVersion,
	}
	metaBuf, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, metaBuf, 0o644); err != nil {
		fd.Close()
		writeError(w, http.StatusInternalServerError, "write meta: "+err.Error())
		return
	}

	sw := &SessionWriter{
		fd:       fd,
		meta:     meta,
		metaPath: metaPath,
		lastSync: time.Now(),
	}

	s.mu.Lock()
	s.writers[req.SessionID] = sw
	s.mu.Unlock()

	log.Printf("[session:start] id=%s", req.SessionID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id": req.SessionID,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionID = strings.TrimSuffix(sessionID, "/events")

	var req struct {
		Events   []Event `json:"events"`
		BatchSeq int     `json:"batch_seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	sw := s.getWriter(sessionID)
	if sw == nil {
		writeError(w, http.StatusNotFound, "session not found (start it first)")
		return
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	// 幂等检查: 跟踪已处理的 batch_seq (支持 SW 重启重发)
	if sw.seenBatches == nil {
		sw.seenBatches = make(map[int]bool)
	}
	if req.BatchSeq > 0 && sw.seenBatches[req.BatchSeq] {
		writeJSON(w, http.StatusOK, map[string]any{
			"received":      0,
			"last_sequence": sw.meta.EventCount,
		})
		return
	}

	// 追加写入
	domainSet := map[string]bool{}
	for _, d := range sw.meta.Domains {
		domainSet[d] = true
	}

	var buf strings.Builder
	for _, e := range req.Events {
		line, _ := json.Marshal(e)
		buf.Write(line)
		buf.WriteByte('\n')
		if e.Domain != "" {
			domainSet[e.Domain] = true
		}
	}
	if _, err := sw.fd.WriteString(buf.String()); err != nil {
		writeError(w, http.StatusInternalServerError, "write: "+err.Error())
		return
	}

	sw.meta.EventCount += len(req.Events)
	sw.meta.Domains = keys(domainSet)
	sw.seenBatches[req.BatchSeq] = true
	sw.unsynced += len(req.Events)

	if sw.unsynced >= 200 {
		sw.fd.Sync()
		sw.unsynced = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"received":      len(req.Events),
		"last_sequence": sw.meta.EventCount,
	})
}

func (s *Server) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionID = strings.TrimSuffix(sessionID, "/end")

	var req struct {
		Timestamp string `json:"timestamp"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	sw := s.getWriter(sessionID)
	if sw == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.fd.Sync()
	sw.fd.Close()

	sw.meta.Status = "completed"
	sw.meta.EndedAt = req.Timestamp
	s.writeMeta(sw.metaPath, &sw.meta)

	s.mu.Lock()
	delete(s.writers, sessionID)
	s.mu.Unlock()

	log.Printf("[session:end] id=%s events=%d", sessionID, sw.meta.EventCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":  sessionID,
		"event_count": sw.meta.EventCount,
	})
}

func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")

	metaPath := filepath.Join(s.dataDir, sessionID+".meta.json")
	meta := SessionMeta{ID: sessionID}
	if data, err := os.ReadFile(metaPath); err == nil {
		json.Unmarshal(data, &meta)
	}

	jsonlPath := filepath.Join(s.dataDir, sessionID+".jsonl")
	events := []json.RawMessage{}
	if f, err := os.Open(jsonlPath); err == nil {
		scanner := bufio.NewScanner(f)
		_ = scanner.Err()
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				events = append(events, json.RawMessage(line))
			}
		}
		f.Close()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session": meta,
		"events":  events,
	})
}

func (s *Server) handleSessionsList(w http.ResponseWriter, _ *http.Request) {
	setCORS(w)
	entries, _ := os.ReadDir(s.dataDir)
	results := []SessionMeta{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			continue
		}
		var meta SessionMeta
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		results = append(results, meta)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartedAt > results[j].StartedAt
	})
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "DELETE required")
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionID = strings.TrimSuffix(sessionID, "/delete")

	// close writer if open
	s.mu.Lock()
	if sw, ok := s.writers[sessionID]; ok {
		sw.mu.Lock()
		sw.fd.Close()
		sw.mu.Unlock()
		delete(s.writers, sessionID)
	}
	s.mu.Unlock()

	os.Remove(filepath.Join(s.dataDir, sessionID+".jsonl"))
	os.Remove(filepath.Join(s.dataDir, sessionID+".meta.json"))

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ─── fsync ─────────────────────────────────────────────────────────

func (s *Server) fsyncLoop() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		s.mu.RLock()
		for _, sw := range s.writers {
			sw.mu.Lock()
			if sw.unsynced > 0 {
				sw.fd.Sync()
				sw.unsynced = 0
			}
			sw.mu.Unlock()
		}
		s.mu.RUnlock()
	}
}

// ─── cleanup ──────────────────────────────────────────────────────

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		s.cleanup()
	}
}

func (s *Server) cleanup() {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return
	}
	type sessionInfo struct {
		id        string
		startedAt time.Time
	}
	var sessions []sessionInfo
	cutoff := time.Now().Add(-time.Duration(s.retention) * 24 * time.Hour)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".meta.json")
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			continue
		}
		var meta SessionMeta
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, meta.StartedAt)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionInfo{id: id, startedAt: t})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].startedAt.Before(sessions[j].startedAt)
	})

	removed := 0
	for _, si := range sessions {
		if si.startedAt.Before(cutoff) || len(sessions)-removed > s.maxSessions {
			os.Remove(filepath.Join(s.dataDir, si.id+".jsonl"))
			os.Remove(filepath.Join(s.dataDir, si.id+".meta.json"))
			removed++
		}
	}
	if removed > 0 {
		log.Printf("[cleanup] removed %d sessions", removed)
	}
}

// ─── helpers ──────────────────────────────────────────────────────

func (s *Server) getWriter(sessionID string) *SessionWriter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.writers[sessionID]
}

func (s *Server) writeMeta(path string, meta *SessionMeta) {
	buf, _ := json.MarshalIndent(meta, "", "  ")
	tmp := path + ".tmp"
	os.WriteFile(tmp, buf, 0o644)
	os.Rename(tmp, path) // atomic replace
}

func countLines(path string) (int, []string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer f.Close()

	count := 0
	domainSet := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		_ = scanner.Err()
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		count++
		var e Event
		if json.Unmarshal([]byte(line), &e) == nil && e.Domain != "" {
			domainSet[e.Domain] = true
		}
	}
	return count, keys(domainSet)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─── main ─────────────────────────────────────────────────────────

func main() {
	addr := flag.String("addr", "127.0.0.1:9200", "listen address")
	dataDir := flag.String("data-dir", "experiment/fox/data/sessions", "session data directory")
	maxSessions := flag.Int("max-sessions", 50, "max sessions to retain")
	retentionDays := flag.Int("retention", 7, "retention in days")
	flag.Parse()

	absDir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("abs path: %v", err)
	}

	srv, err := NewServer(absDir, *maxSessions, *retentionDays)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(204)
			return
		}
		srv.handleHealth(w, r)
	})

	mux.HandleFunc("/api/session/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(204)
			return
		}
		srv.handleSessionStart(w, r)
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(204)
			return
		}
		srv.handleSessionsList(w, r)
	})

	// 动态路由: /api/session/{id}
	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(204)
			return
		}

		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/events"):
			srv.handleEvents(w, r)
		case strings.HasSuffix(path, "/end"):
			srv.handleSessionEnd(w, r)
		case strings.HasSuffix(path, "/delete"):
			srv.handleSessionDelete(w, r)
		default:
			// GET /api/session/{id}
			if r.Method == http.MethodGet {
				srv.handleSessionGet(w, r)
			} else {
				writeError(w, http.StatusNotFound, "unknown endpoint")
			}
		}
	})

	log.Printf("behavior-server listening on %s", *addr)
	log.Printf("data dir: %s", absDir)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
