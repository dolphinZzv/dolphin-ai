// Fox Popup — Silent / Teach dual-mode

const SW = () => chrome.runtime;

// ─── DOM refs ─────────────────────────────────────────────────────
const statusDot = document.getElementById('statusDot');
const statusText = document.getElementById('statusText');
const eventCount = document.getElementById('eventCount');
const domainCount = document.getElementById('domainCount');
const sessionList = document.getElementById('sessionList');
const copyBtn = document.getElementById('copyPrompt');
const recordingBadge = document.getElementById('recordingBadge');

// tabs
const tabSilent = document.getElementById('tabSilent');
const tabTeach = document.getElementById('tabTeach');
const silentSection = document.getElementById('silentSection');
const teachSection = document.getElementById('teachSection');

// silent
const btnSilentStart = document.getElementById('btnSilentStart');
const btnSilentStop = document.getElementById('btnSilentStop');

// teach
const labelInput = document.getElementById('labelInput');
const btnTeachStart = document.getElementById('btnTeachStart');
const btnTeachStop = document.getElementById('btnTeachStop');

let currentMode = 'silent';
let currentStatus = { recording: false, sessionId: null, serverOnline: false, label: '' };

// ─── mode tabs ────────────────────────────────────────────────────
function setMode(mode) {
  currentMode = mode;
  tabSilent.classList.toggle('active', mode === 'silent');
  tabTeach.classList.toggle('active', mode === 'teach');
  silentSection.classList.toggle('show', mode === 'silent');
  teachSection.classList.toggle('show', mode === 'teach');
}

tabSilent.onclick = () => { if (!currentStatus.recording) setMode('silent'); };
tabTeach.onclick = () => { if (!currentStatus.recording) setMode('teach'); };

// ─── init ─────────────────────────────────────────────────────────
async function refresh() {
  const resp = await SW().sendMessage({ action: 'getStatus' });
  currentStatus = resp;
  renderControls();
  renderSessions();
  checkServer();
}

function renderControls() {
  const { recording, serverOnline, label, mode: activeMode } = currentStatus;

  if (recording) {
    // 录制中 — 禁用模式切换，显示当前活跃模式
    const isTeach = !!label;
    setMode(isTeach ? 'teach' : 'silent');

    btnSilentStart.disabled = true;
    btnSilentStop.disabled = !isTeach; // teach 模式下 silent stop 不可用
    btnTeachStart.disabled = true;
    btnTeachStop.disabled = !isTeach;
    labelInput.disabled = true;

    if (isTeach) {
      btnTeachStart.textContent = '● Demo';
      btnTeachStop.disabled = false;
      labelInput.value = label;
      recordingBadge.textContent = '📹 Teaching: ' + label;
    } else {
      btnSilentStart.textContent = '● Recording';
      btnSilentStop.disabled = false;
      recordingBadge.textContent = '🕶 Silent recording...';
    }
    recordingBadge.classList.add('show');
    statusDot.className = 'status-dot recording';
    statusText.textContent = 'Recording...';
  } else {
    recordingBadge.classList.remove('show');
    btnSilentStart.disabled = !serverOnline;
    btnSilentStop.disabled = true;
    btnSilentStart.textContent = '▶ Record';

    btnTeachStart.disabled = !serverOnline;
    btnTeachStop.disabled = true;
    btnTeachStart.textContent = '▶ Demo';
    labelInput.disabled = false;

    statusDot.className = serverOnline ? 'status-dot online' : 'status-dot offline';
    statusText.textContent = serverOnline ? 'Server: connected' : 'Server: offline';
  }

  eventCount.textContent = currentStatus.eventCount || 0;
  domainCount.textContent = currentStatus.domains?.length || 0;
}

async function checkServer() {
  const resp = await SW().sendMessage({ action: 'getServerStatus' });
  if (!currentStatus.recording) {
    statusDot.className = resp.online ? 'status-dot online' : 'status-dot offline';
    statusText.textContent = resp.online ? 'Server: connected' : 'Server: offline';
    btnSilentStart.disabled = !resp.online;
    btnTeachStart.disabled = !resp.online;
  }
}

// ─── sessions ─────────────────────────────────────────────────────
async function renderSessions() {
  const sessions = await SW().sendMessage({ action: 'getSessions' }) || [];
  if (sessions.length === 0) {
    sessionList.innerHTML = '<div class="empty">No sessions yet</div>';
    return;
  }
  sessionList.innerHTML = sessions.map(s => {
    const label = s.label || (s.domains || ['unknown']).join(', ');
    const tag = s.label ? '<span class="tag teach">teach</span>' : '<span class="tag silent">silent</span>';
    return `
      <div class="session-item">
        <div class="session-info">
          <div class="session-label">${esc(label)} ${tag}</div>
          <div class="session-meta">${fmtDate(s.started_at)} · ${s.event_count || 0} steps · ${s.status}</div>
        </div>
        <div class="session-actions">
          <button class="icon-btn" data-id="${s.id}" data-action="copy">📋</button>
          <button class="icon-btn danger" data-id="${s.id}" data-action="delete">✕</button>
        </div>
      </div>
    `;
  }).join('');

  sessionList.querySelectorAll('[data-action="copy"]').forEach(b => {
    b.onclick = () => copySessionPrompt(b.dataset.id);
  });
  sessionList.querySelectorAll('[data-action="delete"]').forEach(b => {
    b.onclick = () => deleteSession(b.dataset.id);
  });
}

async function copySessionPrompt(id) {
  const data = await SW().sendMessage({ action: 'getSession', sessionId: id });
  if (!data) return;
  const s = data.session || {};
  const label = s.label || (s.domains || ['unknown']).join(', ');

  const prompt = `I recorded a browser demo: "${label}". Analyze it and teach me this skill.\n\nFile: {data-dir}/${id}.jsonl\n\nFormat: JSON Lines. The session is labeled "${label}".\n\nRequirements:\n1. Extract the exact operation sequence and generalize it\n2. Create a skill named after "${label}" with clear step-by-step instructions\n3. Include CSS selectors from the recording data\n4. Set enabled: false - I will review first\n5. If there are annotation markers, use them to segment the workflow`;

  await navigator.clipboard.writeText(prompt);
  copyBtn.textContent = '✓ Copied!';
  setTimeout(() => { copyBtn.textContent = '📋 Copy Agent Prompt'; }, 2000);
}

async function deleteSession(id) {
  if (!confirm('Delete this session?')) return;
  await SW().sendMessage({ action: 'deleteSession', sessionId: id });
  renderSessions();
}

// ─── buttons ──────────────────────────────────────────────────────
btnSilentStart.onclick = async () => {
  const res = await SW().sendMessage({ action: 'start', label: '' });
  if (res.error) { alert('Error: ' + res.error); return; }
  refresh();
};

btnSilentStop.onclick = async () => {
  await SW().sendMessage({ action: 'stop' });
  refresh();
};

btnTeachStart.onclick = async () => {
  const label = labelInput.value.trim();
  if (!label) { labelInput.focus(); return; }
  const res = await SW().sendMessage({ action: 'start', label });
  if (res.error) { alert('Error: ' + res.error); return; }
  refresh();
};

btnTeachStop.onclick = async () => {
  await SW().sendMessage({ action: 'stop' });
  labelInput.value = '';
  refresh();
};

// ─── copy prompt ──────────────────────────────────────────────────
copyBtn.onclick = async () => {
  const sessions = await SW().sendMessage({ action: 'getSessions' }) || [];
  const last = sessions[0];
  if (!last) { copyBtn.textContent = 'No sessions yet'; return; }
  const label = last.label || (last.domains || ['unknown']).join(', ');
  const prompt = `I recorded a browser demo: "${label}". Analyze it and teach me this skill.\n\nFile: {data-dir}/${last.id}.jsonl\n\nFormat: JSON Lines. The session is labeled "${label}".\n\nRequirements:\n1. Extract the exact operation sequence and generalize it\n2. Create a skill named after "${label}" with clear step-by-step instructions\n3. Include CSS selectors from the recording data\n4. Set enabled: false - I will review first\n5. If there are annotation markers, use them to segment the workflow`;
  await navigator.clipboard.writeText(prompt);
  copyBtn.textContent = '✓ Copied!';
  setTimeout(() => { copyBtn.textContent = '📋 Copy Agent Prompt'; }, 2000);
};

// ─── options ──────────────────────────────────────────────────────
document.getElementById('openOptions').onclick = () => chrome.runtime.openOptionsPage();

// ─── helpers ──────────────────────────────────────────────────────
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
function fmtDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  const diff = Date.now() - d;
  if (diff < 86400000) return 'Today ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  if (diff < 172800000) return 'Yesterday';
  return d.toLocaleDateString();
}

refresh();
setInterval(refresh, 2000);
