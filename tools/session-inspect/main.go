// wal-server serves an HTML viewer for WALMemory session files.
// Start it from the project root:
//
//	go run ./tools/session-inspect [--dir .dolphin/sessions] [--addr :9090]
//
// Then open http://localhost:9090 in a browser.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/memory"
	"dolphin/internal/types"

	"github.com/tidwall/wal"
)

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

	// HTML viewer page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlPage))
	})

	// API: list sessions
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

	// API: read session entries
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
			t := m.Text()
			if len(t) > 200 {
				t = t[:200] + "..."
			}
			previews[i] = map[string]any{"role": string(m.Role), "text": t}
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

// firstUserInput reads the first user message from a WAL directory.
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
			t := msg.Text()
			if len(t) > 30 {
				return t[:30] + "..."
			}
			return t
		}
		// Stop at first non-user non-msg entry.
		if msg.Role != "" && msg.Role != types.RoleSystem {
			return ""
		}
	}
	return ""
}

// dirSize returns the total size of all files in a directory.
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

const htmlPage = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Session Inspect</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font:13px/1.5 -apple-system,sans-serif;background:#1a1a2e;color:#e0e0e0;display:flex;height:100vh}
.sidebar{width:300px;background:#16213e;border-right:1px solid #0f3460;overflow-y:auto;padding:10px;flex-shrink:0}
.sidebar h2{color:#e94560;margin-bottom:4px;font-size:15px}
.sidebar .hint{color:#555;font-size:10px;margin-bottom:10px}
.sidebar .loading{color:#666;font-size:12px}
.card{background:#1a1a40;border:1px solid #0f3460;border-radius:5px;margin-bottom:6px;padding:8px 10px;cursor:pointer}
.card:hover{background:#1e1e50;border-color:#e94560}
.card .title{color:#e94560;font-weight:600;font-size:12px}
.card .meta{display:flex;gap:12px;margin-top:3px;flex-wrap:wrap}
.card .meta span{color:#777;font-size:10px}
.card .meta b{color:#ccc}
.main{flex:1;overflow-y:auto;padding:16px}
.tab-bar{display:flex;gap:3px;margin-bottom:12px;border-bottom:1px solid #0f3460;padding-bottom:8px}
.tab-bar button{background:0;border:1px solid #0f3460;color:#888;padding:4px 12px;border-radius:3px 3px 0 0;cursor:pointer;font-size:12px}
.tab-bar button.active{background:#e94560;border-color:#e94560;color:#fff}
.entry{border-left:3px solid #333;padding:6px 10px;margin:4px 0;font-size:12px}
.entry.msg-user{border-left-color:#e94560}
.entry.msg-assistant{border-left-color:#16a085}
.entry.msg-system{border-left-color:#f39c12}
.entry.msg-tool{border-left-color:#8e44ad}
.entry.msg{border-left-color:#16a085}
.entry.compact{border-left-color:#e67e22;background:#1a1a10}
.entry.turn{border-left-color:#e94560;background:#1a1010}
.entry .kind{font-size:10px;color:#666;margin-bottom:2px;text-transform:uppercase}
.entry .body{white-space:pre-wrap;word-break:break-word}
.entry .thinking{background:#111;color:#888;padding:4px 8px;margin-top:4px;border-radius:3px;font-size:11px;font-style:italic}
.meta-line{display:flex;gap:12px;margin:2px 0;flex-wrap:wrap}
.meta-line span{font-size:10px;color:#777}
.compact-block{background:#2c3e50;padding:8px 12px;border-radius:4px;margin:8px 0;font-size:12px}
.compact-block .range{color:#e67e22}
.empty{text-align:center;color:#555;padding:40px;font-size:13px}
pre{background:#0d1117;padding:12px;border-radius:4px;overflow:auto;font-size:12px;line-height:1.4;max-height:80vh}
.layout{display:flex;gap:12px;height:calc(100vh - 50px)}
.col-left{flex:1;overflow-y:auto;padding-right:6px;border-right:1px solid #0f3460}
.col-right{flex:1;overflow-y:auto;padding-left:6px}
.entry.turn.selected{border-left-color:#fff;background:#1a1a40;border-left-width:4px}
.entry.turn:hover{border-left-color:#e94560}
.turn-list{padding:0 0 0 8px;margin:-2px 0 6px;display:none}
.turn-btn{display:block;padding:3px 8px;margin:2px 0;background:#0d1117;border:1px solid #0f3460;border-radius:3px;color:#888;font-size:11px;cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.turn-btn:hover{color:#e0e0e0;border-color:#e94560}
.turn-btn.sel{background:#e94560;color:#fff;border-color:#e94560}
</style>
</head>
<body>
<div class="sidebar" id="sidebar">
  <h2>🐬 Session Inspect</h2>
  <p class="hint">session.type = wal</p>
  <div id="sessionList"><div class="loading">加载中...</div></div>
</div>
<div class="main" id="main">
  <div class="tab-bar" id="tabs"></div>
  <div id="content"><div class="empty">选择左侧 session 查看时间线</div></div>
</div>
<script>
let sessions = [];
let activeSid = null;
let entries = [];
let selectedTurnIdx = -1;

async function init() {
  var el = document.getElementById('sessionList');
  try {
    var res = await fetch('/api/sessions');
    if (!res.ok) throw new Error(res.status+' '+res.statusText);
    sessions = await res.json();
  } catch(e) {
    el.innerHTML = '<div class="empty">❌ 加载失败: ' + esc(e.message) + '</div>';
    return;
  }
  if (!sessions.length) {
    el.innerHTML = '<div class="empty">无 .wal 文件</div>';
    return;
  }
  sessions.sort(function(a,b){ return (b.mtime||0) - (a.mtime||0); });
  renderSidebar();
}
init();

function renderSidebar() {
  var el = document.getElementById('sessionList');
  el.innerHTML = sessions.map(function(s){
    return '<div class="card" onclick="openSession(\''+s.id+'\')"><div class="title">'+esc(s.id)+'</div><div class="meta"><span>'+fmtSize(s.size)+'</span></div></div>' +
      '<div id="turns-'+s.id+'" class="turn-list"></div>';
  }).join('');
}

async function openSession(sid) {
  activeSid = sid;
  selectedTurnIdx = -1;
  entries = [];
  try {
    var res = await fetch('/api/session/' + sid);
    if (!res.ok) throw new Error(res.status);
    entries = await res.json();
  } catch(e) { return; }

  var turns = entries.filter(function(e){ return e.type === 'turn'; });
  var turnEl = document.getElementById('turns-'+sid);
  var html = '';
  for (var i = 0; i < turns.length; i++) {
    var t = turns[i]; var d = t.data || {};
    html += '<div class="turn-btn" onclick="event.stopPropagation();showTurn('+i+')" id="tbtn-'+sid+'-'+i+'">T'+(i+1)+': '+esc((d.Input||'').slice(0,30))+'</div>';
  }
  if (turnEl) {
    turnEl.innerHTML = html || '<div class="empty" style="font-size:10px;color:#555;padding:4px">无 turn mark</div>';
    turnEl.style.display = 'block';
  }
  // Default: show last turn.
  if (turns.length > 0) showTurn(turns.length-1);
  else document.getElementById('content').innerHTML = '<div class="empty">无 turn mark</div>';
}

function showTurn(idx) {
  var turns = entries.filter(function(e){ return e.type === 'turn'; });
  if (idx < 0 || idx >= turns.length) return;
  selectedTurnIdx = idx;
  var t = turns[idx];
  var d = t.data || {};

  // Highlight.
  document.querySelectorAll('.turn-btn').forEach(function(b){ b.classList.remove('sel'); });
  var btn = document.getElementById('tbtn-'+activeSid+'-'+idx);
  if (btn) btn.classList.add('sel');

  // Build timeline up to this turn.
  var msgs = rebuildMessages(t.seq);

  // Diff: this turn vs previous turn (or vs empty for first turn).
  var prevSeq = idx > 0 ? turns[idx-1].seq : 0;
  var msgsA = prevSeq ? rebuildMessages(prevSeq) : [];
  var diff = diffMessages(msgsA, msgs);

  // System prompt.
  var sys = d.SystemPrompt || '';

  var el = document.getElementById('content');
  var html = '<div class="layout"><div class="col-left">';
  // Turn header.
  html += '<h3 style="color:#e94560;margin-bottom:2px">T'+(idx+1)+': '+esc(d.Input||'')+'</h3>';
  html += '<div class="meta-line" style="margin-bottom:4px"><span>id:'+esc(d.TurnID||'?')+'</span><span>model:'+esc(d.ModelName||'?')+'</span><span>in:'+(d.InTokens||0)+'</span><span>out:'+(d.OutTokens||0)+'</span><span>rounds:'+(d.Rounds||0)+'</span></div>';
  if (sys) html += '<div class="thinking" style="margin-bottom:8px;max-height:60px;overflow-y:auto;font-size:10px">📋 '+esc(sys.slice(0,200))+'</div>';

  // Messages.
  for (var i = 0; i < msgs.length; i++) {
    var m = msgs[i]; var r = (m.role||'?').toLowerCase();
    var cls = 'msg', icon = '💬';
    if (r==='system'){cls='msg-system';icon='⚙️'}
    else if(r==='tool'){cls='msg-tool';icon='🔧'}
    else if(r==='assistant'){cls='msg-assistant';icon='🤖'}
    else if(r==='user'){cls='msg-user';icon='👤'}
    html += '<div class="entry '+cls+'"><div class="kind">'+icon+' '+esc(m.role||'?')+'</div><div class="body" style="font-size:11px">'+esc((m.text||'').slice(0,200))+'</div></div>';
  }
  html += '</div>'; // col-left

  // Right column: diff (only when there's a previous turn).
  html += '<div class="col-right">';
  if (idx > 0) {
    html += '<h3 style="color:#e94560;margin-bottom:8px">📊 T'+idx+'→T'+(idx+1)+'</h3>';
    html += '<div style="color:#888;font-size:11px;margin-bottom:4px">'+msgsA.length+' → '+msgs.length+' msgs</div>';
    html += diff.map(function(d){
      if (d.cls==='same') return '<div class="entry msg"><div class="body" style="color:#666;font-size:10px">'+esc(d.text).slice(0,120)+'</div></div>';
      return '<div class="entry" style="border-left-color:#e74c3c;background:#1c1010"><div class="kind">'+esc(d.role)+'</div><div class="body"><span style="background:#c0392b33;display:block;padding:2px 4px;font-size:10px">− '+esc(d.old).slice(0,150)+'</span><span style="background:#27ae6033;display:block;padding:2px 4px;font-size:10px">+ '+esc(d.nue).slice(0,150)+'</span></div></div>';
    }).join('');
  } else {
    html += '<div class="empty" style="margin-top:40px">首轮对话<br><span style="font-size:10px;color:#555">选中 T2 可对比 T1→T2 的变化</span></div>';
  }
  html += '</div></div>';
  el.innerHTML = html;
}

function rebuildMessages(toSeq) {
  var msgs = [];
  var cpIdx = -1;
  for (var i = entries.length-1; i >= 0; i--) {
    if (entries[i].type === 'compact' && entries[i].seq <= toSeq) { cpIdx = i; break; }
  }
  if (cpIdx >= 0) {
    var cp = entries[cpIdx];
    if (cp.data && cp.data.messages) {
      msgs = cp.data.messages.map(function(m){ return {role:m.role,text:m.text}; });
    }
  }
  var startIdx = cpIdx >= 0 ? cpIdx + 1 : 0;
  for (var j = startIdx; j < entries.length; j++) {
    if (entries[j].seq > toSeq) break;
    if (entries[j].type === 'msg' && entries[j].data) {
      msgs.push({role:entries[j].data.role, text:entries[j].data.text});
    }
  }
  return msgs;
}

function diffMessages(a, b) {
  var out = [];
  var maxLen = Math.max(a.length, b.length);
  for (var i = 0; i < maxLen; i++) {
    var ta = i < a.length ? a[i].text : '';
    var tb = i < b.length ? b[i].text : '';
    var role = i < a.length ? a[i].role : (i < b.length ? b[i].role : '');
    if (ta === tb) out.push({cls:'same', role:role, text:ta||'(empty)'});
    else out.push({cls:'diff', role:role, old:ta||'(gone)', nue:tb||'(new)'});
  }
  return out;
}

function esc(s) { if (!s) return ''; return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function fmtSize(b) { return b > 1e6 ? (b/1e6).toFixed(1)+'MB' : b > 1e3 ? (b/1e3).toFixed(1)+'KB' : b+'B'; }</script>
</body>
</html>`
