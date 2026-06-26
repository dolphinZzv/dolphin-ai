// Fox Options — 设置持久化

const DEFAULTS = {
  serverAddr: 'http://127.0.0.1:9200',
  domainMode: 'blocklist',
  domains: [],
  redactInputs: true,
  stripQueryParams: true,
  textTruncate: 100,
  maxSessions: 50,
  retentionDays: 7
};

let settings = { ...DEFAULTS };

// ─── load ─────────────────────────────────────────────────────────
async function load() {
  const data = await chrome.storage.local.get('fox_settings');
  if (data.fox_settings) {
    settings = { ...DEFAULTS, ...data.fox_settings };
  }
  render();
}

function render() {
  document.getElementById('serverAddr').value = settings.serverAddr;
  document.getElementById('domainMode').value = settings.domainMode;
  document.getElementById('redactInputs').checked = settings.redactInputs;
  document.getElementById('stripQueryParams').checked = settings.stripQueryParams;
  document.getElementById('textTruncate').value = settings.textTruncate;
  document.getElementById('maxSessions').value = settings.maxSessions;
  document.getElementById('retentionDays').value = settings.retentionDays;
  renderDomainList();
}

function renderDomainList() {
  const list = document.getElementById('domainList');
  list.innerHTML = (settings.domains || []).map((d, i) => `
    <div class="domain-row">
      <input type="text" value="${esc(d)}" data-idx="${i}" class="domain-input">
      <button data-idx="${i}" class="domain-remove">✕</button>
    </div>
  `).join('');

  list.querySelectorAll('.domain-remove').forEach(b => {
    b.onclick = () => {
      settings.domains.splice(parseInt(b.dataset.idx), 1);
      renderDomainList();
    };
  });

  list.querySelectorAll('.domain-input').forEach(inp => {
    inp.onchange = () => {
      settings.domains[parseInt(inp.dataset.idx)] = inp.value.trim();
    };
  });
}

document.getElementById('addDomain').onclick = () => {
  settings.domains.push('');
  renderDomainList();
  const inputs = document.querySelectorAll('.domain-input');
  inputs[inputs.length - 1]?.focus();
};

// ─── save ─────────────────────────────────────────────────────────
document.getElementById('saveBtn').onclick = async () => {
  // collect domain values
  document.querySelectorAll('.domain-input').forEach(inp => {
    settings.domains[parseInt(inp.dataset.idx)] = inp.value.trim();
  });
  settings.domains = settings.domains.filter(d => d !== '');

  settings.serverAddr = document.getElementById('serverAddr').value.trim();
  settings.domainMode = document.getElementById('domainMode').value;
  settings.redactInputs = document.getElementById('redactInputs').checked;
  settings.stripQueryParams = document.getElementById('stripQueryParams').checked;
  settings.textTruncate = parseInt(document.getElementById('textTruncate').value) || 100;
  settings.maxSessions = parseInt(document.getElementById('maxSessions').value) || 50;
  settings.retentionDays = parseInt(document.getElementById('retentionDays').value) || 7;

  await chrome.storage.local.set({ fox_settings: settings });
  toast('Settings saved');
};

// ─── clear ────────────────────────────────────────────────────────
document.getElementById('clearBtn').onclick = async () => {
  if (!confirm('Delete ALL recorded session data? This cannot be undone.')) return;
  // 通过 SW 删除 server 端的 sessions
  const sessions = await chrome.runtime.sendMessage({ action: 'getSessions' }) || [];
  for (const s of sessions) {
    await chrome.runtime.sendMessage({ action: 'deleteSession', sessionId: s.id });
  }
  toast('All data cleared');
};

// ─── helpers ──────────────────────────────────────────────────────
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

function toast(msg) {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = 'toast show';
  setTimeout(() => { el.className = 'toast'; }, 2000);
}

// ─── init ─────────────────────────────────────────────────────────
load();
