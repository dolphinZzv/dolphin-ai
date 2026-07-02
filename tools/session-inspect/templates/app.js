let sessions = [];
let activeSid = null;
let entries = [];

async function init() {
  const el = document.getElementById('sessionList');
  try {
    const res = await fetch('/api/sessions');
    if (!res.ok) throw new Error(res.status);
    sessions = await res.json();
  } catch (e) {
    el.innerHTML = `<div class="empty-state">❌ 加载失败: ${esc(e.message)}<p>go run ./tools/session-inspect</p></div>`;
    return;
  }
  if (!sessions.length) {
    el.innerHTML = '<div class="empty-state">暂无 WAL 文件<p>session.type = wal</p></div>';
    return;
  }
  sessions.sort((a, b) => (b.mtime || 0) - (a.mtime || 0));
  renderSidebar();
}
init();

function renderSidebar() {
  document.getElementById('sessionList').innerHTML = sessions.map(s => {
    const preview = s.firstInput ? esc(s.firstInput) : '';
    return `<div class="session-card" onclick="openSession('${s.id}')">
      <div class="id">${esc(s.id)}</div>
      ${preview ? `<div class="preview">${preview}</div>` : ''}
      <div class="meta"><span>${fmtSize(s.size)}</span></div>
    </div><div id="turns-${s.id}" class="turn-list"></div>`;
  }).join('');
}

async function openSession(sid) {
  activeSid = sid;
  entries = [];
  try {
    const res = await fetch('/api/session/' + sid);
    if (!res.ok) throw new Error(res.status);
    entries = await res.json();
  } catch (e) { return; }

  const turns = entries.filter(e => e.type === 'turn');
  const turnEl = document.getElementById('turns-' + sid);
  turnEl.innerHTML = turns.map((t, i) => {
    const d = t.data || {};
    return `<div class="turn-btn" onclick="event.stopPropagation();showTurn(${i})" id="tbtn-${sid}-${i}">
      T${i+1}: ${esc((d.Input || '').slice(0, 30))}</div>`;
  }).join('') || '<div style="font-size:10px;color:#94a3b8;padding:2px 4px">无 turn mark</div>';
  turnEl.style.display = 'block';

  if (turns.length > 0) showTurn(turns.length - 1);
  else document.getElementById('main').innerHTML = '<div class="empty-state">无 turn mark<p>需要至少一次对话</p></div>';
}

function showTurn(idx) {
  const turns = entries.filter(e => e.type === 'turn');
  if (idx < 0 || idx >= turns.length) return;

  // Highlight selected button.
  document.querySelectorAll('.turn-btn').forEach(b => b.classList.remove('sel'));
  const btn = document.getElementById('tbtn-' + activeSid + '-' + idx);
  if (btn) btn.classList.add('sel');

  const t = turns[idx];
  const d = t.data || {};
  const msgs = rebuildMessages(t.seq);

  // Diff: this turn vs previous.
  const prevSeq = idx > 0 ? turns[idx - 1].seq : 0;
  const msgsA = prevSeq ? rebuildMessages(prevSeq) : [];
  const diff = diffMessages(msgsA, msgs);

  const el = document.getElementById('main');
  el.innerHTML = `<div class="layout">
    <div class="col-left">
      <div class="turn-header">
        <h3>T${idx + 1}: ${esc(d.Input || '')}</h3>
        <div class="stats">
          <span>id: ${esc(d.TurnID || '?')}</span>
          <span>model: ${esc(d.ModelName || '?')}</span>
          <span>in: ${d.InTokens || 0}</span>
          <span>out: ${d.OutTokens || 0}</span>
          <span>rounds: ${d.Rounds || 0}</span>
        </div>
      </div>
      ${d.SystemPrompt ? `<div class="sys-prompt">📋 ${esc(d.SystemPrompt.slice(0, 200))}</div>` : ''}
      ${msgs.map(m => entryHTML(m)).join('')}
    </div>
    <div class="col-right">
      ${idx > 0
        ? `<div class="diff-header">📊 T${idx} → T${idx + 1}</div>
           <div class="diff-count">${msgsA.length} msgs → ${msgs.length} msgs</div>
           ${diff.map(diffEntryHTML).join('')}`
        : `<div class="empty-state">首轮对话<p>选中 T2 可查看 T1→T2 的变化</p></div>`
      }
    </div>
  </div>`;
}

function entryHTML(m) {
  const r = (m.role || '?').toLowerCase();
  let cls = 'assistant';
  if (r === 'user') cls = 'user';
  else if (r === 'system') cls = 'system';
  else if (r === 'tool') cls = 'tool';
  return `<div class="entry ${cls}">
    <div class="role">${esc(m.role || '?')}</div>
    <div class="body">${esc((m.text || '').slice(0, 300))}</div>
  </div>`;
}

function diffEntryHTML(d) {
  if (d.cls === 'same') {
    return `<div class="diff-same">${esc(d.text).slice(0, 120)}</div>`;
  }
  return `<div class="diff-changed">
    <div class="role">${esc(d.role)}</div>
    <div class="old">− ${esc(d.old).slice(0, 200)}</div>
    <div class="new">+ ${esc(d.nue).slice(0, 200)}</div>
  </div>`;
}

function rebuildMessages(toSeq) {
  const msgs = [];
  let cpIdx = -1;
  for (let i = entries.length - 1; i >= 0; i--) {
    if (entries[i].type === 'compact' && entries[i].seq <= toSeq) { cpIdx = i; break; }
  }
  if (cpIdx >= 0) {
    const cp = entries[cpIdx];
    if (cp.data && cp.data.messages) {
      msgs.push(...cp.data.messages.map(m => ({ role: m.role, text: m.text })));
    }
  }
  const startIdx = cpIdx >= 0 ? cpIdx + 1 : 0;
  for (let j = startIdx; j < entries.length; j++) {
    if (entries[j].seq > toSeq) break;
    if (entries[j].type === 'msg' && entries[j].data) {
      msgs.push({ role: entries[j].data.role, text: entries[j].data.text });
    }
  }
  return msgs;
}

function diffMessages(a, b) {
  const out = [];
  const maxLen = Math.max(a.length, b.length);
  for (let i = 0; i < maxLen; i++) {
    const ta = i < a.length ? a[i].text : '';
    const tb = i < b.length ? b[i].text : '';
    const role = i < a.length ? a[i].role : (i < b.length ? b[i].role : '');
    if (ta === tb) out.push({ cls: 'same', role, text: ta || '(empty)' });
    else out.push({ cls: 'diff', role, old: ta || '(gone)', nue: tb || '(new)' });
  }
  return out;
}

function esc(s) {
  if (!s) return '';
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function fmtSize(b) {
  return b > 1e6 ? (b / 1e6).toFixed(1) + ' MB' : b > 1e3 ? (b / 1e3).toFixed(1) + ' KB' : b + ' B';
}
