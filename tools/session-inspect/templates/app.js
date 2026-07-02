let sessions = [];
let activeSid = null;
let entries = [];
let mdMode = true; // markdown vs raw text toggle

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
      T${i+1}: ${esc(d.Input || '')}</div>`;
  }).join('') || '<div style="font-size:10px;color:#94a3b8;padding:2px 4px">无 turn mark</div>';
  turnEl.style.display = 'block';

  if (turns.length > 0) showTurn(turns.length - 1);
  else document.getElementById('main').innerHTML = '<div class="empty-state">无 turn mark<p>需要至少一次对话</p></div>';
}

function toggleMD() {
  mdMode = !mdMode;
  document.querySelectorAll('.md-toggle').forEach(b => {
    b.textContent = mdMode ? '📝 Markdown' : '📋 Raw';
    b.classList.toggle('active', mdMode);
  });
  if (activeSid) {
    const turns = entries.filter(e => e.type === 'turn');
    for (let i = 0; i < turns.length; i++) {
      const btn = document.getElementById('tbtn-' + activeSid + '-' + i);
      if (btn && btn.classList.contains('sel')) { showTurn(i); break; }
    }
  }
}

function showTurn(idx) {
  const turns = entries.filter(e => e.type === 'turn');
  if (idx < 0 || idx >= turns.length) return;

  document.querySelectorAll('.turn-btn').forEach(b => b.classList.remove('sel'));
  const btn = document.getElementById('tbtn-' + activeSid + '-' + idx);
  if (btn) btn.classList.add('sel');

  const t = turns[idx];
  const d = t.data || {};
  const msgs = rebuildMessagesBetween(0, t.seq);

  // Diff: full message snapshot at T_{idx-1} vs T_{idx}.
  // T_{idx} builds on top of T_{idx-1} by appending new messages — LCS
  // will keep T_{idx-1} messages as "same" and flag T_{idx} additions.
  const prevSeq = idx > 0 ? turns[idx - 1].seq : 0;
  const msgsA = idx > 0 ? rebuildMessagesBetween(0, prevSeq) : [];
  const msgsB = rebuildMessagesBetween(0, t.seq);
  const diff = diffMessages(msgsA, msgsB);

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
          <button class="md-toggle${mdMode ? ' active' : ''}" onclick="toggleMD()" title="Toggle markdown / raw text">${mdMode ? '📝 Markdown' : '📋 Raw'}</button>
        </div>
      </div>
      ${d.SystemPrompt ? `<div class="sys-prompt">📋 ${renderMarkdown(d.SystemPrompt)}</div>` : ''}
      ${msgs.map(m => entryHTML(m)).join('')}
    </div>
    <div class="col-right">
      ${idx > 0
        ? `<div class="diff-header">📊 T${idx} → T${idx + 1}</div>
           <div class="diff-summary">${diffSummary(diff)}</div>
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
  const body = mdMode ? renderMarkdown(m.text || '') : `<pre class="raw-text">${esc(m.text || '')}</pre>`;
  const thinking = m.thinking ? `<div class="thinking">💭 ${mdMode ? renderMarkdown(m.thinking) : esc(m.thinking)}</div>` : '';
  return `<div class="entry ${cls}">
    <div class="role">${esc(m.role || '?')}</div>
    <div class="body md-content">${body}</div>
    ${thinking}
  </div>`;
}

// ---- LCS-based message alignment ----
function alignMessages(a, b) {
  const m = a.length, n = b.length;
  const dp = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] = (a[i - 1].role === b[j - 1].role && a[i - 1].text === b[j - 1].text)
        ? dp[i - 1][j - 1] + 1
        : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }

  const pairs = [];
  let i = m, j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && a[i - 1].role === b[j - 1].role && a[i - 1].text === b[j - 1].text) {
      pairs.unshift({ cls: 'same', role: a[i - 1].role, text: a[i - 1].text });
      i--; j--;
    } else if (i > 0 && j > 0 && a[i - 1].role === b[j - 1].role) {
      pairs.unshift({ cls: 'diff', role: a[i - 1].role, old: a[i - 1].text, nue: b[j - 1].text });
      i--; j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      pairs.unshift({ cls: 'add', role: b[j - 1].role, text: b[j - 1].text });
      j--;
    } else {
      pairs.unshift({ cls: 'del', role: a[i - 1].role, text: a[i - 1].text });
      i--;
    }
  }
  return pairs;
}

function diffMessages(a, b) {
  return alignMessages(a, b);
}

// ---- Diff summary (counts) ----
function diffSummary(diff) {
  const same = diff.filter(d => d.cls === 'same').length;
  const changed = diff.filter(d => d.cls === 'diff').length;
  const added = diff.filter(d => d.cls === 'add').length;
  const deleted = diff.filter(d => d.cls === 'del').length;
  const parts = [];
  if (same) parts.push(`<span class="stat-same">${same} 未变</span>`);
  if (changed) parts.push(`<span class="stat-changed">${changed} 变动</span>`);
  if (added) parts.push(`<span class="stat-add">+${added} 新增</span>`);
  if (deleted) parts.push(`<span class="stat-del">-${deleted} 删除</span>`);
  return parts.join(' ');
}

// ---- Diff entry renderer ----
function diffEntryHTML(d) {
  switch (d.cls) {
    case 'same': {
      const maxLen = 120;
      const truncated = d.text.length > maxLen
        ? esc(d.text.slice(0, maxLen)) + '…'
        : esc(d.text);
      return `<div class="diff-same"><span class="role-tag">${esc(d.role)}</span> <span class="text-muted">${truncated}</span></div>`;
    }
    case 'diff':
      return `<div class="diff-changed">
        <div class="role-tag">${esc(d.role)}</div>
        <div class="diff-pair">
          <div class="diff-old"><span class="diff-label">− old</span><pre>${esc(d.old)}</pre></div>
          <div class="diff-new"><span class="diff-label">+ new</span><pre>${esc(d.nue)}</pre></div>
        </div>
      </div>`;
    case 'add':
      return `<div class="diff-added"><span class="role-tag add">+ ${esc(d.role)}</span> <pre>${esc(d.text)}</pre></div>`;
    case 'del':
      return `<div class="diff-deleted"><span class="role-tag del">− ${esc(d.role)}</span> <pre>${esc(d.text)}</pre></div>`;
    default:
      return '';
  }
}

// ---- markdown rendering (marked.js, CDN) ----
function renderMarkdown(md) {
  if (!md) return '';
  if (typeof marked !== 'undefined' && marked.parse) {
    return marked.parse(md);
  }
  return esc(md).replace(/\n\n/g, '</p><p>').replace(/\n/g, '<br>');
}

// ---- WAL replay logic ----

// rebuildMessagesBetween returns messages with seq in (fromSeq, toSeq].
function rebuildMessagesBetween(fromSeq, toSeq) {
  const msgs = [];
  let cpIdx = -1;
  for (let i = entries.length - 1; i >= 0; i--) {
    if (entries[i].type === 'compact' && entries[i].seq <= toSeq) { cpIdx = i; break; }
  }
  // If there's a compact checkpoint in range, load its snapshot and then
  // filter to messages whose seq > fromSeq (they were added after the baseline).
  if (cpIdx >= 0) {
    const cp = entries[cpIdx];
    if (cp.data && cp.data.messages && cp.seq > fromSeq) {
      msgs.push(...cp.data.messages.map(m => ({ role: m.role, text: m.text, thinking: m.thinking || '' })));
    }
  }
  const startIdx = cpIdx >= 0 ? cpIdx + 1 : 0;
  for (let j = startIdx; j < entries.length; j++) {
    if (entries[j].seq > toSeq) break;
    if (entries[j].seq <= fromSeq) continue;
    if (entries[j].type === 'msg' && entries[j].data) {
      msgs.push({ role: entries[j].data.role, text: entries[j].data.text, thinking: entries[j].data.thinking || '' });
    }
  }
  return msgs;
}

function esc(s) {
  if (!s) return '';
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function fmtSize(b) {
  return b > 1e6 ? (b / 1e6).toFixed(1) + ' MB' : b > 1e3 ? (b / 1e3).toFixed(1) + ' KB' : b + ' B';
}
