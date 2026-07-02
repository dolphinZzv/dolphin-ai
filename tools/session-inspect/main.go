// session-inspect serves an HTML viewer for WALMemory session files.
// Start it from the project root:
//
//	go run ./tools/session-inspect [--dir .dolphin/sessions] [--addr :9090]
//
// Then open http://localhost:9090 in a browser.
package main

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/memory"
	"dolphin/internal/types"

	"github.com/tidwall/wal"
)

//go:embed templates/*
var templates embed.FS

func init() {
	gob.Register(types.Message{})
	gob.Register(types.ContentPart{})
	gob.Register(types.ToolCall{})
	gob.Register(types.ToolDef{})
	gob.Register(memory.CompactPayload{})
	gob.Register(memory.TurnPayload{})
}

type jsonEntry struct {
	Seq  uint64 `json:"seq"`
	TS   int64  `json:"ts_ms"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

func main() {
	dir := flag.String("dir", ".dolphin/sessions", "WAL session directory")
	addr := flag.String("addr", ":9090", "listen address")
	flag.Parse()

	mux := http.NewServeMux()

	// Static assets (CSS, JS).
	staticFS, _ := fs.Sub(templates, "templates")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// HTML page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		index, _ := templates.ReadFile("templates/index.html")
		w.Write(index)
	})

	// API: list sessions.
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		entries, err := os.ReadDir(*dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		sessions := make([]map[string]any, 0)
		for _, e := range entries {
			if e.IsDir() && strings.HasSuffix(e.Name(), ".wal") {
				sid := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "session_"), ".wal")
				info, _ := e.Info()
				var mtime int64
				if info != nil {
					mtime = info.ModTime().Unix()
				}
				firstInput := firstUserInput(filepath.Join(*dir, e.Name()))
				sessions = append(sessions, map[string]any{
					"id":         sid,
					"file":       e.Name(),
					"size":       dirSize(filepath.Join(*dir, e.Name())),
					"mtime":      mtime,
					"firstInput": firstInput,
				})
			}
		}
		writeJSON(w, sessions)
	})

	// API: read session entries.
	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		sid := strings.TrimPrefix(r.URL.Path, "/api/session/")
		if sid == "" {
			http.Error(w, "missing session id", 400)
			return
		}
		path := filepath.Join(*dir, "session_"+sid+".wal")
		entries, err := readWAL(path)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, entries)
	})

	log.Printf("Session Inspect: http://localhost%s (dir=%s)", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func readWAL(path string) ([]jsonEntry, error) {
	log, err := wal.Open(path, wal.DefaultOptions)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer log.Close()

	lastIdx, _ := log.LastIndex()
	firstIdx, _ := log.FirstIndex()
	if lastIdx == 0 {
		return nil, nil
	}

	var entries []jsonEntry
	for seq := firstIdx; seq <= lastIdx; seq++ {
		data, err := log.Read(seq)
		if err != nil {
			continue
		}
		if len(data) < 9 {
			continue
		}
		ts := int64(binary.BigEndian.Uint64(data[0:8]))
		typ := data[8]
		payload := data[9:]

		je := jsonEntry{Seq: seq, TS: ts / 1e6, Type: typeName(typ)}
		je.Data = decodePayload(typ, payload)
		entries = append(entries, je)
	}
	return entries, nil
}

func typeName(typ byte) string {
	switch typ {
	case 0:
		return "msg"
	case 1:
		return "compact"
	case 2:
		return "turn"
	}
	return fmt.Sprintf("unknown(%d)", typ)
}

func decodePayload(typ byte, data []byte) any {
	r := bytes.NewReader(data)
	dec := gob.NewDecoder(r)
	switch typ {
	case 0:
		var msg types.Message
		if err := dec.Decode(&msg); err != nil {
			return map[string]string{"error": err.Error()}
		}
		return map[string]any{
			"role":       string(msg.Role),
			"text":       msg.Text(),
			"thinking":   msg.Thinking,
			"tool_calls": len(msg.ToolCalls),
		}
	case 1:
		var cp memory.CompactPayload
		if err := dec.Decode(&cp); err != nil {
			return map[string]string{"error": err.Error()}
		}
		previews := make([]map[string]any, len(cp.Messages))
		for i, m := range cp.Messages {
			previews[i] = map[string]any{"role": string(m.Role), "text": m.Text()}
		}
		return map[string]any{
			"src_start": cp.SrcStart,
			"src_end":   cp.SrcEnd,
			"summary":   cp.Summary,
			"msg_count": len(cp.Messages),
			"messages":  previews,
		}
	case 2:
		var tp memory.TurnPayload
		if err := dec.Decode(&tp); err != nil {
			return map[string]string{"error": err.Error()}
		}
		return tp
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func firstUserInput(path string) string {
	log, err := wal.Open(path, wal.DefaultOptions)
	if err != nil {
		return ""
	}
	defer log.Close()
	last, _ := log.LastIndex()
	first, _ := log.FirstIndex()
	for seq := first; seq <= last; seq++ {
		data, err := log.Read(seq)
		if err != nil || len(data) < 9 {
			continue
		}
		if data[8] != 0 {
			continue
		}
		var msg types.Message
		if err := gob.NewDecoder(bytes.NewReader(data[9:])).Decode(&msg); err != nil {
			continue
		}
		if msg.Role == types.RoleUser {
			return msg.Text()
		}
		if msg.Role != "" && msg.Role != types.RoleSystem {
			return ""
		}
	}
	return ""
}

func dirSize(path string) int64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil {
			total += info.Size()
		}
	}
	return total
}
