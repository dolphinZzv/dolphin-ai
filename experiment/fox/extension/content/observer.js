// Fox Content Observer
// DOM 事件委托 + Port 连接 + 本地缓冲 + auth 协作 + 自我销毁

(function () {
  'use strict';

  // ─── lifecycle ──────────────────────────────────────────────────
  let destroyed = false, extId = null;
  function alive() { if (destroyed) return false; try { return !!(chrome.runtime && chrome.runtime.id); } catch { return false; } }

  function destroy(msg) {
    if (destroyed) return;
    destroyed = true;
    console.debug('[fox observer] self-destruct:', msg);
    recording = false; sessionId = null; port = null;

    // 卸掉所有 DOM listener
    document.removeEventListener('click', handleClick, true);
    document.removeEventListener('dblclick', handleClick, true);
    document.removeEventListener('input', handleInput, true);
    document.removeEventListener('change', handleChange, true);
    document.removeEventListener('scroll', handleScrollOr, { capture: true, passive: true });
    document.removeEventListener('keydown', handleKeydown, true);
    document.removeEventListener('focus', handleFocus, true);
    document.removeEventListener('blur', handleFocus, true);
    document.removeEventListener('submit', handleSubmit, true);
    document.removeEventListener('copy', handleCopy, true);
    document.removeEventListener('paste', handlePaste, true);
    window.removeEventListener('beforeunload', handleBeforeUnload);
    window.removeEventListener('pagehide', handlePageHide);
    window.removeEventListener('pageshow', handlePageShow);
    window.removeEventListener('load', handleWindowLoad);
    try { chrome.storage.onChanged.removeListener(onStorageChange); } catch {}
  }

  // 定时检查扩展是否被重载
  setInterval(() => {
    if (!alive()) { destroy('context dead'); return; }
    const id = chrome.runtime.id;
    if (extId && extId !== id) { destroy('extension reloaded'); return; }
    extId = id;
  }, 3000);

  // ─── constants ──────────────────────────────────────────────────
  const THROTTLE_INPUT_MS = 300;
  const THROTTLE_SCROLL_MS = 500;

  // ─── state ──────────────────────────────────────────────────────
  let port = null;
  let sessionId = null;
  let recording = false;
  let paused = false;
  let tabSeq = 0;
  let lastInputTime = 0;
  let lastScrollTime = 0;
  let lastPageLoadTime = 0;

  // ─── Port ───────────────────────────────────────────────────────
  function handlePortMessage(msg) {
    if (destroyed || !alive()) { destroy('port msg after death'); return; }
    switch (msg.type) {
      case 'start':
        sessionId = msg.session_id;
        recording = true; paused = false; tabSeq = 0;
        pageLoadIfRecording();
        break;
      case 'pause':   paused = true; break;
      case 'resume':  paused = false; break;
      case 'session_end': recording = false; sessionId = null; paused = false; break;
      case 'idle':    recording = false; break;
    }
  }

  function connect() {
    if (destroyed || !alive()) return;
    try {
      port = chrome.runtime.connect({ name: 'fox-events' });
      port.onMessage.addListener(handlePortMessage);
      port.onDisconnect.addListener(() => {
        port = null;
        if (destroyed) return;
        if (!alive()) { destroy('disconnect while dead'); return; }
        const err = chrome.runtime.lastError?.message || '';
        if (err.includes('back/forward cache')) return;   // BFCache, pageshow 会处理
        if (err.includes('context invalidated')) { destroy('context invalidated'); return; }
        // 正常断开 → 500ms 后重连
        setTimeout(() => { if (!destroyed && alive()) { connect(); checkRecordingState(); } }, 500);
      });
    } catch (e) {
      if (e.message?.includes('context invalidated') || e.message?.includes('Extension context')) { destroy('connect: context invalidated'); return; }
      if (!destroyed) setTimeout(connect, 1000);
    }
  }
  connect();

  function checkRecordingState() {
    if (destroyed || !alive()) return;
    try {
      chrome.storage.local.get(['active_session'], (data) => {
        if (destroyed || !alive()) return;
        if (chrome.runtime.lastError) return;
        if (data.active_session && !recording) {
          sessionId = data.active_session;
          recording = true; paused = false;
          pageLoadIfRecording();
        }
      });
    } catch {}
  }

  // ─── storage listener ───────────────────────────────────────────
  function onStorageChange(changes) {
    if (destroyed || !alive()) { destroy('storage change after death'); return; }
    if (changes._broadcast) {
      const msg = changes._broadcast.newValue;
      if (!msg) return;
      switch (msg.type) {
        case 'pause': paused = true; break;
        case 'resume': paused = false; break;
        case 'session_end': recording = false; sessionId = null; paused = false; break;
        case 'start':
          sessionId = msg.session_id; recording = true; paused = false; tabSeq = 0;
          pageLoadIfRecording();
          break;
      }
    }
    if (changes.__fox_hint) {
      const hint = changes.__fox_hint.newValue;
      if (!hint || !isRecording()) return;
      sendEvent('annotation', { text: hint.text });
      try { chrome.storage.local.remove('__fox_hint'); } catch {}
    }
  }
  chrome.storage.onChanged.addListener(onStorageChange);

  // ─── helpers ────────────────────────────────────────────────────
  function isRecording() { return !destroyed && recording && !paused && sessionId && alive(); }

  function sendEvent(type, payload) {
    if (!isRecording()) return;
    tabSeq++;
    const event = {
      s: sessionId, seq: tabSeq,
      ts: new Date().toISOString(), tab: 0,
      domain: location.hostname, path: location.pathname,
      type, p: JSON.stringify(payload)
    };
    try {
      if (port && alive()) port.postMessage({ type: 'event', event });
      if (chrome.runtime.lastError) {
        const m = chrome.runtime.lastError.message || '';
        if (m.includes('context invalidated') || m.includes('message channel')) { port = null; }
      }
    } catch (e) {
      if (e.message?.includes('context invalidated')) { destroy('sendEvent context invalidated'); }
    }
  }

  function pageLoadIfRecording() {
    if (!isRecording()) return;
    const now = Date.now();
    if (now - lastPageLoadTime < 500) return;  // 防重复
    lastPageLoadTime = now;
    sendEvent('page.load', { url_path: location.pathname, is_spa: false, navigation_source: 'full_load' });
    const auth = window.__fox?.detectAuthPage?.();
    if (auth?.isAuth && auth.confidence >= 0.8 && port && alive()) {
      try { port.postMessage({ type: 'auth_detected', auth: { ...auth, tab_id: 0 } }); } catch {}
    }
    detectPageType();
  }

  // ─── event handlers ─────────────────────────────────────────────
  function handleClick(e) {
    if (!isRecording()) return;
    const info = window.__fox?.extractFromEvent?.(e);
    if (!info) return;
    sendEvent(e.type, {
      el: info.element_tag,
      text: (e.target.textContent || '').trim().slice(0, 100) || '',
      sel: info.css_selector, m: info.css_selector_method,
      alts: info.alternatives || [],
      shadow_path: info.shadow_path,
      pos: { x: e.clientX, y: e.clientY }
    });
  }

  function handleInput(e) {
    if (!isRecording()) return;
    if (e.target.matches?.('input[type="password"]')) return;
    const now = Date.now();
    if (now - lastInputTime < THROTTLE_INPUT_MS) return;
    lastInputTime = now;
    const info = window.__fox?.generateSelector?.(e.target);
    if (!info) return;
    sendEvent('input', {
      el: e.target.tagName.toLowerCase(), input_type: e.target.type || 'text',
      sel: info.css_selector, m: info.css_selector_method,
      value_length: e.target.value?.length || 0
    });
  }

  function handleChange(e) {
    if (!isRecording()) return;
    const info = window.__fox?.generateSelector?.(e.target);
    if (!info) return;
    const payload = { el: e.target.tagName.toLowerCase(), sel: info.css_selector, m: info.css_selector_method };
    if (e.target.tagName === 'SELECT') payload.selected_option_text = e.target.selectedOptions?.[0]?.textContent?.trim?.().slice(0, 100) || '';
    else if (e.target.type === 'checkbox' || e.target.type === 'radio') payload.checked = e.target.checked;
    sendEvent('change', payload);
  }

  function handleScrollOr() {
    if (!isRecording()) return;
    const now = Date.now();
    if (now - lastScrollTime < THROTTLE_SCROLL_MS) return;
    lastScrollTime = now;
    const pct = Math.round((window.scrollY / (Math.max(document.documentElement.scrollHeight - window.innerHeight, 1))) * 100) || 0;
    sendEvent('scroll', { scroll_pct: Math.min(pct, 100) });
  }

  function handleKeydown(e) {
    if (!isRecording()) return;
    if (!e.ctrlKey && !e.metaKey && !e.altKey) return;
    const parts = [];
    if (e.ctrlKey) parts.push('Ctrl'); if (e.metaKey) parts.push('Cmd');
    if (e.altKey) parts.push('Alt'); if (e.shiftKey) parts.push('Shift');
    parts.push(e.key);
    sendEvent('keydown', { key_combo: parts.join('+') });
  }

  function handleFocus(e) {
    if (!isRecording()) return;
    const info = window.__fox?.generateSelector?.(e.target);
    if (!info) return;
    sendEvent(e.type, { el: e.target.tagName.toLowerCase(), sel: info.css_selector, m: info.css_selector_method });
  }

  function handleSubmit(e) {
    if (!isRecording()) return;
    sendEvent('submit', { form_action_domain: location.hostname, field_count: e.target.querySelectorAll('input, select, textarea').length });
  }

  function handleCopy(e) {
    if (!isRecording()) return;
    sendEvent('copy', { source_tag: e.target.tagName?.toLowerCase() || '', content_length: document.getSelection()?.toString().length || 0 });
  }

  function handlePaste(e) {
    if (!isRecording()) return;
    sendEvent('paste', { target_tag: e.target.tagName?.toLowerCase() || '', content_length: (e.clipboardData?.getData('text') || '').length });
  }

  function handleBeforeUnload() { if (isRecording()) sendEvent('page.unload', { time_spent_ms: Math.round(performance.now()) }); }
  function handlePageHide(e) { if (e.persisted) port = null; }
  function handlePageShow(e) { if (e.persisted) { connect(); checkRecordingState(); } }
  function handleWindowLoad() { pageLoadIfRecording(); }

  // ─── SPA nav callback ───────────────────────────────────────────
  window.__fox = window.__fox || {};
  window.__fox.onSPANavigation = function (nav) {
    if (!isRecording()) return;
    sendEvent(nav.event_type, nav.payload);
    const auth = window.__fox?.detectAuthPage?.();
    if (auth?.isAuth && auth.confidence >= 0.8 && port && alive()) {
      try { port.postMessage({ type: 'auth_detected', auth: { ...auth, tab_id: 0 } }); } catch {}
    }
  };

  // ─── page type detection ────────────────────────────────────────
  function detectPageType() {
    const canvases = document.querySelectorAll('canvas');
    for (const c of canvases) {
      const rect = c.getBoundingClientRect();
      const vpArea = window.innerWidth * window.innerHeight;
      if ((rect.width * rect.height) / vpArea > 0.5) {
        if (c.getContext('webgl') || c.getContext('webgl2')) {
          sendEvent('page.type_detected', { page_type: 'canvas', strategy: 'screenshot' });
          return;
        }
      }
    }
  }

  // ─── bind events ────────────────────────────────────────────────
  document.addEventListener('click', handleClick, true);
  document.addEventListener('dblclick', handleClick, true);
  document.addEventListener('input', handleInput, true);
  document.addEventListener('change', handleChange, true);
  document.addEventListener('scroll', handleScrollOr, { capture: true, passive: true });
  document.addEventListener('keydown', handleKeydown, true);
  document.addEventListener('focus', handleFocus, true);
  document.addEventListener('blur', handleFocus, true);
  document.addEventListener('submit', handleSubmit, true);
  document.addEventListener('copy', handleCopy, true);
  document.addEventListener('paste', handlePaste, true);
  window.addEventListener('beforeunload', handleBeforeUnload);
  window.addEventListener('pagehide', handlePageHide);
  window.addEventListener('pageshow', handlePageShow);
  window.addEventListener('load', handleWindowLoad);

  // ─── init ───────────────────────────────────────────────────────
  if (document.readyState === 'complete' || document.readyState === 'interactive') {
    setTimeout(() => { if (!destroyed && alive()) checkRecordingState(); }, 100);
  }
})();
