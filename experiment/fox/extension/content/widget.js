// Fox Floating Widget — self-destructing
// Silent: small dot  |  Teach: expanded with label + hint input
(function () {
  'use strict';

  if (document.getElementById('__fox_widget__')) return;

  // ─── lifecycle ──────────────────────────────────────────────────
  let destroyed = false, extId = null;
  function alive() { if (destroyed) return false; try { return !!(chrome.runtime && chrome.runtime.id); } catch { return false; } }

  function destroy(msg) {
    if (destroyed) return;
    destroyed = true;
    console.debug('[fox widget] self-destruct:', msg);
    try { chrome.storage.onChanged.removeListener(onStorageChange); } catch {}
    const el = document.getElementById('__fox_widget__');
    if (el) el.remove();
    const st = document.getElementById('__fox_style__');
    if (st) st.remove();
  }

  setInterval(() => {
    if (!alive()) { destroy('context dead'); return; }
    const id = chrome.runtime.id;
    if (extId && extId !== id) { destroy('extension reloaded'); return; }
    extId = id;
  }, 3000);

  // ─── style (注入到 head, 不在 widget 内部) ──────────────────────
  const style = document.createElement('style');
  style.id = '__fox_style__';
  style.textContent = `
    #__fox_widget__{all:initial}
    .fw{display:inline-block;position:fixed;bottom:16px;right:16px;z-index:2147483647;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif;font-size:12px;box-shadow:0 4px 20px rgba(0,0,0,.3);border-radius:10px;background:#1a1a2e;color:#ddd;min-width:44px;max-width:280px;user-select:none;transition:all .2s}
    .fw.fw-collapsed{min-width:44px;max-width:44px;border-radius:22px;cursor:pointer}
    .fw.fw-collapsed:hover{transform:scale(1.08)}
    .fw.fw-silent{opacity:.7}
    .fw.fw-dragging{transition:none}
    .fw-dot-wrap{display:flex;align-items:center;justify-content:center;width:44px;height:44px;flex-shrink:0}
    .fw-dot{width:12px;height:12px;border-radius:50%;background:#f44336;animation:fw-pulse 1.5s infinite}
    .fw.fw-paused .fw-dot{background:#ff9800;animation:none}
    @keyframes fw-pulse{0%,to{opacity:1}50%{opacity:.3}}
    .fw-body{display:none;padding:10px 12px;flex-direction:column;gap:6px}
    .fw:not(.fw-collapsed) .fw-body{display:flex}
    .fw:not(.fw-collapsed) .fw-dot-wrap{display:none}
    .fw-label{font-weight:600;color:#fff;font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .fw-meta{font-size:11px;color:#888;display:flex;gap:12px}
    .fw-hint-row{display:flex;gap:6px}
    .fw-hint-input{flex:1;padding:5px 8px;border:1px solid #444;border-radius:5px;font-size:11px;color:#ddd;background:#2a2a3e}
    .fw-hint-input::placeholder{color:#666}
    .fw-hint-input:focus{border-color:#e65100}
    .fw-hint-btn{padding:5px 10px;border:none;border-radius:5px;background:#e65100;color:#fff;cursor:pointer;font-size:11px;font-weight:500}
    .fw-hint-btn:hover{background:#bf360c}
    .fw-actions{display:flex;gap:6px}
    .fw-actions button{flex:1;padding:6px 0;border:none;border-radius:5px;cursor:pointer;font-size:11px;font-weight:500;text-align:center;color:#ccc;transition:background .15s}
    .fw-btn-pause:hover{background:#2a2a3e}
    .fw-btn-done{background:#c62828!important;color:#fff!important}
    .fw-btn-done:hover{background:#b71c1c!important}
  `;
  document.head.appendChild(style);

  // ─── DOM (纯结构, 无 style 标签) ────────────────────────────────
  const el = document.createElement('div');
  el.id = '__fox_widget__';
  el.innerHTML = `
    <div class="fw fw-collapsed fw-silent" id="__fw__">
      <div class="fw-dot-wrap"><div class="fw-dot"></div></div>
      <div class="fw-body">
        <div class="fw-label" id="__fwl__">Recording...</div>
        <div class="fw-meta">
          <span id="__fwc__"></span><span id="__fwm__"></span>
        </div>
        <div class="fw-hint-row">
          <input class="fw-hint-input" id="__fwh__" placeholder="Hint: this is the login step...">
          <button class="fw-hint-btn" id="__fwhb__">Save</button>
        </div>
        <div class="fw-actions">
          <button class="fw-btn-pause" id="__fwp__">Pause</button>
          <button class="fw-btn-done" id="__fwd__">Done</button>
        </div>
      </div>
    </div>`;
  document.body.appendChild(el);

  const fw = document.getElementById('__fw__');
  const labelEl = document.getElementById('__fwl__');
  const countEl = document.getElementById('__fwc__');
  const modeEl = document.getElementById('__fwm__');
  const hintInput = document.getElementById('__fwh__');
  const hintBtn = document.getElementById('__fwhb__');
  const pauseBtn = document.getElementById('__fwp__');
  const doneBtn = document.getElementById('__fwd__');

  // ─── state ──────────────────────────────────────────────────────
  let visible = false, sessionLabel = '', isTeach = false, paused = false;

  function updateUI() {
    if (destroyed || !visible) { el.style.display = 'none'; return; }
    el.style.display = 'block';
    labelEl.textContent = isTeach ? sessionLabel : 'Silent recording';
    paused ? fw.classList.add('fw-paused') : fw.classList.remove('fw-paused');
    pauseBtn.textContent = paused ? 'Resume' : 'Pause';
    isTeach ? fw.classList.remove('fw-silent') : fw.classList.add('fw-silent');
    modeEl.textContent = isTeach ? 'Teach' : 'Silent';
    if (isTeach) fw.classList.remove('fw-collapsed');
    else setTimeout(() => { if (!destroyed && visible && !isTeach) fw.classList.add('fw-collapsed'); }, 2000);
  }

  // ─── drag ──────────────────────────────────────────────────────
  let dragging = false, ox = 0, oy = 0;
  fw.addEventListener('mousedown', (e) => {
    if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;
    dragging = true; fw.classList.add('fw-dragging');
    ox = e.clientX - fw.getBoundingClientRect().left;
    oy = e.clientY - fw.getBoundingClientRect().top;
  });
  document.addEventListener('mousemove', (e) => { if (!dragging) return; fw.style.right = 'auto'; fw.style.bottom = 'auto'; fw.style.left = (e.clientX - ox) + 'px'; fw.style.top = (e.clientY - oy) + 'px'; });
  document.addEventListener('mouseup', () => { dragging = false; fw.classList.remove('fw-dragging'); });

  fw.addEventListener('dblclick', (e) => {
    if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;
    fw.classList.toggle('fw-collapsed');
  });

  // ─── buttons ────────────────────────────────────────────────────
  pauseBtn.addEventListener('click', (e) => {
    e.stopPropagation(); if (!alive()) return;
    try { paused ? chrome.runtime.sendMessage({ action: 'resume' }) : chrome.runtime.sendMessage({ action: 'pause' }); } catch {}
  });
  doneBtn.addEventListener('click', (e) => {
    e.stopPropagation(); if (!alive()) return;
    try { chrome.runtime.sendMessage({ action: 'stop' }); } catch {}
  });
  hintBtn.addEventListener('click', (e) => {
    e.stopPropagation(); if (!alive()) return;
    const text = hintInput.value.trim(); if (!text) return;
    try { chrome.storage.local.set({ __fox_hint: { text, ts: Date.now() } }); } catch {}
    hintInput.value = ''; hintBtn.textContent = 'OK';
    setTimeout(() => { hintBtn.textContent = 'Save'; }, 1000);
  });
  hintInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') hintBtn.click(); e.stopPropagation(); });

  // ─── storage listener ───────────────────────────────────────────
  function onStorageChange(changes) {
    if (destroyed || !alive()) { destroy('storage change after death'); return; }
    if (changes._broadcast) {
      const msg = changes._broadcast.newValue; if (!msg) return;
      switch (msg.type) {
        case 'start': visible = true; sessionLabel = msg.label || ''; isTeach = !!msg.label; paused = false; updateUI(); break;
        case 'pause': paused = true; updateUI(); break;
        case 'resume': paused = false; updateUI(); break;
        case 'session_end': visible = false; updateUI(); break;
        case 'step_update': countEl.textContent = (msg.count || 0) + ' steps'; break;
      }
    }
  }
  chrome.storage.onChanged.addListener(onStorageChange);

  // ─── init ───────────────────────────────────────────────────────
  if (alive()) {
    try {
      chrome.storage.local.get(['active_session', 'active_label'], (data) => {
        if (destroyed || !alive() || chrome.runtime.lastError) return;
        if (data.active_session) { visible = true; sessionLabel = data.active_label || ''; isTeach = !!sessionLabel; updateUI(); }
      });
    } catch {}
  }
})();
