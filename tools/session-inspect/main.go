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
				sessions = append(sessions, map[string]any{
					"id":   sid,
					"file": e.Name(),
					"size": dirSize(filepath.Join(*dir, e.Name())),
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

	log.Printf("WAL viewer: http://localhost%s (dir=%s)", *addr, *dir)
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
<title>WAL Session Viewer</title>
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
</style>
</head>
<body>
<div class="sidebar" id="sidebar">
  <h2>🐬 WAL Viewer</h2>
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
let activeTab = 'timeline';
let diffA = null, diffB = null;

async function init() {
  var el = document.getElementById('sessionList');
  try {
    var res = await fetch('/api/sessions');
    if (!res.ok) throw new Error(res.status+' '+res.statusText);
    sessions = await res.json();
  } catch(e) {
    el.innerHTML = '<div class="empty">❌ 加载失败: ' + esc(e.message) + '<br><span style="font-size:10px">go run ./tools/session-inspect --dir .dolphin/sessions</span></div>';
    return;
  }
  if (!sessions.length) {
    el.innerHTML = '<div class="empty">无 .wal 文件<br><span style="font-size:10px">session.type 设为 wal 后对话几次再刷新</span></div>';
    return;
  }
  sessions.sort(function(a,b){ return b.size - a.size; });
  renderSidebar();
}
init();

function renderSidebar() {
  const el = document.getElementById('sessionList');
  if (!sessions.length) { el.innerHTML = '<div class="empty">无 .wal 文件</div>'; return; }
  el.innerHTML = sessions.map(s => ` + "`" + `<div class="card" onclick="openSession('${s.id}')">
    <div class="title">${s.id}</div>
    <div class="meta"><span>$(fmtSize(s.size))</span></div>
  </div>` + "`" + `).join('');
}

async function openSession(sid) {
  activeSid = sid;
  entries = [];
  var mainEl = document.getElementById('content');
  mainEl.innerHTML = '<div class="empty">加载中...</div>';
  try {
    var res = await fetch('/api/session/' + sid);
    if (!res.ok) throw new Error(res.status);
    entries = await res.json();
  } catch(e) {
    mainEl.innerHTML = '<div class="empty">❌ 加载失败: ' + esc(e.message) + '</div>';
    return;
  }
  diffA = null; diffB = null;
  document.getElementById('tabs').innerHTML = ` + "`" + `
    <button class="active" onclick="setTab('timeline')">📋 Timeline</button>
    <button onclick="setTab('diff')">📊 Diff</button>
    <button onclick="setTab('raw')">🔍 Raw JSON</button>
  ` + "`" + `;
  setTab('timeline');
}

function setTab(tab) {
  activeTab = tab;
  document.querySelectorAll('.tab-bar button').forEach(b => {
    var label = b.textContent;
    b.classList.toggle('active', (tab==='timeline'&&label.includes('📋'))||(tab==='diff'&&label.includes('📊'))||(tab==='raw'&&label.includes('🔍')));
  });
  renderView();
}

function rebuildMessages(toSeq) {
  // Replay from the last compact up to the given seq.
  var msgs = [];
  // Find the nearest compact before toSeq.
  var cp = null, cpIdx = -1;
  for (var i = entries.length-1; i >= 0; i--) {
    if (entries[i].type === 'compact' && entries[i].seq <= toSeq) {
      cp = entries[i]; cpIdx = i; break;
    }
  }
  if (!cp) return msgs;
  if (cp.data && cp.data.messages) {
    msgs = cp.data.messages.map(function(m){ return {role:m.role,text:m.text}; });
  }
  // Replay msgs after the compact.
  for (var j = cpIdx+1; j < entries.length; j++) {
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
    if (ta === tb) {
      out.push({cls:'same', role:role, text:ta || '(empty)'});
    } else {
      out.push({cls:'diff', role:role, old:ta || '(gone)', nue:tb || '(new)'});
    }
  }
  return out;
}

function renderView() {
  var el = document.getElementById('content');
  if (activeTab === 'raw') {
    el.innerHTML = '<pre>' + JSON.stringify(entries, null, 2) + '</pre>';
    return;
  }
  if (activeTab === 'diff') {
    var turns = entries.filter(function(e){ return e.type === 'turn'; });
    if (turns.length < 2) { el.innerHTML = '<div class="empty">至少需要 2 个 Turn Mark 才能 Diff<br>需要调用方在每轮结束时 WriteTurn()</div>'; return; }
    var h = '<h3 style="color:#e94560;margin-bottom:8px">📊 Diff ' + esc(activeSid) + '</h3>';
    h += '<div style="display:flex;gap:12px;margin-bottom:12px;flex-wrap:wrap">';
    h += '<div><span style="color:#888;font-size:11px">基准:</span><br><select id="selA" style="background:#16213e;color:#e0e0e0;border:1px solid #0f3460;padding:4px;border-radius:3px;max-width:280px">';
    h += turns.map(function(t,i){ return '<option value="'+t.seq+'"' + (i===0?' selected':'')+'>Turn #'+(i+1)+': ' + esc((t.data||{}).Input||'').slice(0,40) + '</option>'; }).join('');
    h += '</select></div>';
    h += '<div><span style="color:#888;font-size:11px">对比:</span><br><select id="selB" style="background:#16213e;color:#e0e0e0;border:1px solid #0f3460;padding:4px;border-radius:3px;max-width:280px">';
    h += turns.map(function(t,i){ return '<option value="'+t.seq+'"' + (i===1?' selected':'')+'>Turn #'+(i+1)+': ' + esc((t.data||{}).Input||'').slice(0,40) + '</option>'; }).join('');
    h += '</select></div>';
    h += '<div><button onclick="runDiff()" style="background:#e94560;color:#fff;border:0;padding:6px 16px;border-radius:3px;cursor:pointer;margin-top:18px">对比</button></div>';
    h += '</div><div id="diffResult"></div>';
    el.innerHTML = h;
    return;
  }
  // Timeline view.
  var html = '<h3 style="color:#e94560;margin-bottom:12px">' + esc(activeSid) + '</h3>';
  let cn = 0;
  for (const e of entries) {
    const ts = new Date(e.ts_ms).toLocaleString('zh-CN');
    switch (e.type) {
      case 'msg': {
        const d = e.data || {};
        const tc = d.tool_calls || 0;
        html += '<div class="entry msg"><div class="kind">💬 ' + esc(d.role||'?') + ' · seq=' + e.seq + ' · ' + ts + (tc?' · 🔧x'+tc:'') + '</div>';
        html += '<div class="body">' + esc(d.text||'') + '</div>';
        if (d.thinking) html += '<div class="thinking">💭 ' + esc(d.thinking) + '</div>';
        html += '</div>';
        break;
      }
      case 'compact': {
        cn++; const d = e.data || {};
        html += '<div class="compact-block">📦 Compact #' + cn + ' · ' + ts + ' · seq=' + e.seq + ' <span class="range">[' + d.src_start + '–' + (d.src_end||'?') + ']</span>';
        html += '<br><span style="color:#999;font-size:10px">' + esc(d.summary||'') + ' · ' + (d.msg_count||0) + ' msgs</span>';
        html += '</div>';
        break;
      }
      case 'turn': {
        const d = e.data || {};
        html += '<div class="entry turn"><div class="kind">⏱ Turn · seq=' + e.seq + ' · ' + ts + '</div>';
        html += '<div class="body">' + esc(d.Input||'') + '</div>';
        html += '<div class="meta-line"><span>id:' + esc(d.TurnID||'?') + '</span><span>model:' + esc(d.ModelName||'?') + '</span><span>in:' + (d.InTokens||0) + '</span><span>out:' + (d.OutTokens||0) + '</span><span>rounds:' + (d.Rounds||0) + '</span></div>';
        html += '</div>';
        break;
      }
    }
  }
  el.innerHTML = html || '<div class="empty">空</div>';
}

function runDiff() {
  var selA = document.getElementById('selA');
  var selB = document.getElementById('selB');
  if (!selA || !selB) return;
  var seqA = parseInt(selA.value), seqB = parseInt(selB.value);
  var msgsA = rebuildMessages(seqA);
  var msgsB = rebuildMessages(seqB);
  var diff = diffMessages(msgsA, msgsB);
  var el = document.getElementById('diffResult');
  el.innerHTML = '<div style="margin-top:12px">基准: ' + msgsA.length + ' msgs → 对比: ' + msgsB.length + ' msgs</div>' + diff.map(function(d){
    if (d.cls === 'same') return '<div class="entry msg"><div class="kind">' + esc(d.role) + '</div><div class="body" style="color:#888">' + esc(d.text) + '</div></div>';
    return '<div class="entry" style="border-left-color:#e74c3c;background:#1c1010"><div class="kind">' + esc(d.role) + '</div><div class="body"><span style="background:#c0392b33;display:block;padding:2px 4px">− ' + esc(d.old) + '</span><span style="background:#27ae6033;display:block;padding:2px 4px">+ ' + esc(d.nue) + '</span></div></div>';
  }).join('');
}

function esc(s) { if (!s) return ''; return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function fmtSize(b) { return b > 1e6 ? (b/1e6).toFixed(1)+'MB' : b > 1e3 ? (b/1e3).toFixed(1)+'KB' : b+'B'; }
</script>
</body>
</html>`
