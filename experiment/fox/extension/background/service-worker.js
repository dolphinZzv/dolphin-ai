// Fox Background Service Worker
// - Long-lived Port 管理 (content script → SW)
// - Ring Buffer + HTTP 批量推送
// - 录制状态机 + 恢复

const SERVER = 'http://127.0.0.1:9200';
const FLUSH_INTERVAL_MS = 500;
const FLUSH_BATCH_SIZE = 50;
const RING_SIZE = 1000;
const RETRY_BACKOFF = [1000, 2000, 4000, 8000, 16000, 30000];

// ─── state ────────────────────────────────────────────────────────
let currentSession = null;
let currentLabel = '';
let ring = newRingBuffer(RING_SIZE);
let stepCount = 0;
let batchSeq = 0;
let flushTimer = null;
let retryIndex = 0;
let retryTimer = null;
let serverOnline = false;
let authPaused = false;       // 跟踪 auth 暂停状态

// ─── init ─────────────────────────────────────────────────────────
async function init() {
  // recover from chrome.storage
  const data = await chrome.storage.local.get(['active_session', 'last_batch_seq']);
  if (data.active_session) {
    currentSession = data.active_session;
    batchSeq = data.last_batch_seq || 0;
  }
  checkServer();
  setupPortListener();
}

// ─── ring buffer ──────────────────────────────────────────────────
function newRingBuffer(size) {
  const buf = new Array(size);
  let head = 0, tail = 0, count = 0;
  return {
    push(e) {
      buf[tail] = e; tail = (tail + 1) % size;
      if (count < size) count++; else head = (head + 1) % size;
    },
    popAll() {
      if (count === 0) return [];
      const out = [];
      while (count > 0) { out.push(buf[head]); head = (head + 1) % size; count--; }
      return out;
    },
    get count() { return count; },
    clear() { head = tail = count = 0; }
  };
}

// ─── Port 管理 ─────────────────────────────────────────────────────
function setupPortListener() {
  chrome.runtime.onConnect.addListener((port) => {
    if (port.name !== 'fox-events') return;

    // 向新连接的 CS 发送当前状态
    if (currentSession) {
      port.postMessage({ type: 'start', session_id: currentSession });
    } else {
      port.postMessage({ type: 'idle' });
    }

    port.onMessage.addListener((msg) => {
      handleContentMessage(msg, port);
    });

    port.onDisconnect.addListener(() => {
      // Port 断开 — 如果还有其它 tab，SW 仍存活
    });
  });
}

function handleContentMessage(msg, port) {
  switch (msg.type) {
    case 'event':
      if (currentSession) {
        ring.push(msg.event);
        stepCount++;
        // 每 5 步广播一次给 widget
        if (stepCount % 5 === 0) {
          chrome.storage.local.set({ _broadcast: { type: 'step_update', count: stepCount, _ts: Date.now() } });
        }
        if (ring.count >= FLUSH_BATCH_SIZE) flush();
      }
      break;

    case 'page_meta':
      // page.load 时更新域名信息
      break;

    case 'auth_detected':
      if (currentSession && msg.auth.isAuth) {
        // 广播 pause 到所有 CS
        broadcastToCS({ type: 'pause', reason: 'auth_page' });
        ring.push({
          s: currentSession, seq: 0, ts: new Date().toISOString(),
          tab: msg.auth.tab_id || 0, domain: msg.auth.domain || '',
          path: '', type: 'recording.auto_paused',
          p: JSON.stringify({ reason: 'auth_page', domain: msg.auth.domain })
        });
        if (ring.count >= FLUSH_BATCH_SIZE) flush();
      } else if (currentSession && !msg.auth.isAuth) {
        broadcastToCS({ type: 'resume' });
        ring.push({
          s: currentSession, seq: 0, ts: new Date().toISOString(),
          tab: msg.auth.tab_id || 0, domain: msg.auth.domain || '',
          path: '', type: 'recording.resumed',
          p: JSON.stringify({ reason: 'left_auth_page' })
        });
        if (ring.count >= FLUSH_BATCH_SIZE) flush();
      }
      break;
  }
}

function broadcastToCS(msg) {
  // 仅用于 pause/resume/session_end 控制信号
  // start 信号只走 Port (避免重复)
  if (msg.type === 'start') return;
  chrome.storage.local.set({ _broadcast: { ...msg, _ts: Date.now() } });
}

// ─── HTTP 推送 ────────────────────────────────────────────────────
async function flush() {
  const events = ring.popAll();
  if (events.length === 0) return;

  batchSeq++;
  const batch = { events, batch_seq: batchSeq };

  try {
    const resp = await fetch(`${SERVER}/api/session/${currentSession}/events`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(batch)
    });

    if (resp.ok) {
      retryIndex = 0;
      serverOnline = true;
      updateBadge('●', '#4caf50');
    } else {
      throw new Error(`HTTP ${resp.status}`);
    }
  } catch (e) {
    // 推送失败 — 事件放回 ring (在头部)
    for (let i = events.length - 1; i >= 0; i--) {
      ring.push(events[i]); // 注意顺序
    }
    batchSeq--;
    serverOnline = false;
    updateBadge('⚠', '#ff9800');
    scheduleRetry();
  }
}

function scheduleRetry() {
  if (retryTimer) return;
  const delay = RETRY_BACKOFF[Math.min(retryIndex, RETRY_BACKOFF.length - 1)];
  retryIndex++;
  retryTimer = setTimeout(() => {
    retryTimer = null;
    if (ring.count > 0) flush();
  }, delay);
}

async function checkServer() {
  try {
    const resp = await fetch(`${SERVER}/api/health`);
    serverOnline = resp.ok;
    updateBadge(serverOnline ? '●' : '○', serverOnline ? '#4caf50' : '#9e9e9e');
  } catch {
    serverOnline = false;
    updateBadge('○', '#9e9e9e');
  }
}

function updateBadge(text, color) {
  chrome.action.setBadgeText({ text });
  chrome.action.setBadgeBackgroundColor({ color });
}

// ─── API ──────────────────────────────────────────────────────────
async function startSession(label) {
  if (currentSession) return { error: 'already recording' };

  const sessionId = crypto.randomUUID();
  const ts = new Date().toISOString();
  currentLabel = label || '';

  try {
    const resp = await fetch(`${SERVER}/api/session/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        session_id: sessionId,
        timestamp: ts,
        label: currentLabel,
        chrome_info: {
          version: navigator.userAgent,
          os: navigator.platform
        },
        extension_version: chrome.runtime.getManifest().version
      })
    });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

    currentSession = sessionId;
    batchSeq = 0;
    retryIndex = 0;
    stepCount = 0;
    ring.clear();

    await chrome.storage.local.set({
      active_session: sessionId,
      active_label: currentLabel,
      last_batch_seq: 0
    });

    // 启动 flush 定时器
    flushTimer = setInterval(() => { if (ring.count > 0) flush(); }, FLUSH_INTERVAL_MS);

    // 广播 start (含 label)
    broadcastToCS({ type: 'start', session_id: sessionId, label: currentLabel });
    updateBadge('●', '#f44336');

    return { session_id: sessionId };
  } catch (e) {
    return { error: e.message };
  }
}

async function pauseSession() {
  if (!currentSession) return { error: 'not recording' };
  broadcastToCS({ type: 'pause', reason: 'user' });
  return { paused: true };
}

async function resumeSession() {
  if (!currentSession) return { error: 'not recording' };
  broadcastToCS({ type: 'resume' });
  return { resumed: true };
}

async function stopSession() {
  if (!currentSession) return { error: 'not recording' };

  if (ring.count > 0) await flush();
  clearInterval(flushTimer);
  flushTimer = null;

  const ts = new Date().toISOString();
  try {
    await fetch(`${SERVER}/api/session/${currentSession}/end`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ timestamp: ts })
    });
  } catch {}
  // ignore error — server 侧 recover 会标记

  const sid = currentSession;
  const label = currentLabel;
  currentSession = null;
  currentLabel = '';
  stepCount = 0;
  retryIndex = 0;

  await chrome.storage.local.remove('active_session');
  await chrome.storage.local.remove('active_label');
  broadcastToCS({ type: 'session_end' });
  updateBadge('●', '#4caf50');

  return { session_id: sid, label };
}

async function getSessions() {
  try {
    const resp = await fetch(`${SERVER}/api/sessions`);
    if (!resp.ok) return [];
    return await resp.json();
  } catch {
    return [];
  }
}

async function getSession(id) {
  try {
    const resp = await fetch(`${SERVER}/api/session/${id}`);
    if (!resp.ok) return null;
    return await resp.json();
  } catch {
    return null;
  }
}

async function deleteSession(id) {
  try {
    const resp = await fetch(`${SERVER}/api/session/${id}/delete`, { method: 'POST' });
    return resp.ok;
  } catch {
    return false;
  }
}

// ─── message handlers ─────────────────────────────────────────────
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  switch (msg.action) {
    case 'getStatus':
      sendResponse({
        recording: !!currentSession,
        sessionId: currentSession,
        label: currentLabel,
        serverOnline,
        eventCount: stepCount,
        batchSeq
      });
      break;

    case 'start':
      startSession(msg.label || '').then(sendResponse);
      return true; // async

    case 'pause':
      pauseSession().then(sendResponse);
      return true;

    case 'resume':
      resumeSession().then(sendResponse);
      return true;

    case 'stop':
      stopSession().then(sendResponse);
      return true;

    case 'getSessions':
      getSessions().then(sendResponse);
      return true;

    case 'getSession':
      getSession(msg.sessionId).then(sendResponse);
      return true;

    case 'deleteSession':
      deleteSession(msg.sessionId).then(sendResponse);
      return true;

    case 'getServerStatus':
      sendResponse({ online: serverOnline });
      break;
  }
});

// ─── cross-origin / fast-redirect tracking ───────────────────────
// ─── cross-origin / fast-redirect / window.open tracking ──────────
// (1) 快速 302 跳转: CS 来不及注入 → SW 用 webNavigation 生成 nav 事件
//     与 CS 的 page.load 互补: SW 保证不丢, CS 补 DOM 细节
chrome.webNavigation.onCommitted.addListener((details) => {
  if (!currentSession) return;
  if (details.frameId !== 0) return;               // main frame only
  if (details.transitionType === 'reload') return;  // 刷新已有 CS page.load

  try {
    const u = new URL(details.url);
    ring.push({
      s: currentSession, seq: 0,
      ts: new Date(details.timeStamp).toISOString(),
      tab: details.tabId, domain: u.hostname, path: u.pathname,
      type: 'nav.redirect',
      p: JSON.stringify({ url_path: u.pathname, transition: details.transitionType })
    });
    stepCount++;
    if (ring.count >= FLUSH_BATCH_SIZE) flush();
  } catch {}
});

// (2) window.open() / target=_blank: SW 追踪弹窗 tab
//     CS 注入到弹窗后也会独立发 page.load — 两条互补
chrome.tabs.onCreated.addListener((tab) => {
  if (!currentSession) return;
  if (tab.openerTabId) {
    // 延迟 200ms 等 pendingUrl 就绪
    setTimeout(() => {
      if (!currentSession) return;
      try {
        // 二次获取: pendingUrl 可能已完成解析
        chrome.tabs.get(tab.id, (t) => {
          if (chrome.runtime.lastError || !t || !currentSession) return;
          const u = t.pendingUrl || t.url;
          if (!u) return;
          const parsed = new URL(u);
          ring.push({
            s: currentSession, seq: 0,
            ts: new Date().toISOString(),
            tab: t.id, domain: parsed.hostname, path: parsed.pathname,
            type: 'tab.created',
            p: JSON.stringify({ opener_tab_id: tab.openerTabId })
          });
          stepCount++;
          if (ring.count >= FLUSH_BATCH_SIZE) flush();
        });
      } catch {}
    }, 200);
  }
});

// ─── startup ──────────────────────────────────────────────────────
init();
