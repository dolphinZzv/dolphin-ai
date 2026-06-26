// Fox Selector Builder
// 为 DOM 元素生成稳定 CSS 选择器, 按 stability 评分降序排列.
// 策略: data-testid > aria-label > role+name > id > stable-class > nth-of-type

(function () {
  'use strict';

  // ─── patterns ──────────────────────────────────────────────────
  const GENERATED_ID = [
    /^[a-z]-[a-f0-9]{4,}$/i,      // React: "r-abc123"
    /^:[rw]:[a-f0-9]*$/i,          // React: ":r0:", ":r1a:"
    /^[a-f0-9]{8,}$/i,             // hex-only
    /^\d+$/                         // numeric-only
  ];

  const HASH_CLASS = [
    /-[a-f0-9]{4,}$/i,             // ends with "-hex"
    /_[a-f0-9]{4,}$/i,             // ends with "_hex"
    /^css-\d+[a-z]+$/i,            // CSS modules
    /^\w+_[a-zA-Z0-9]{5,}$/         // styled-components
  ];

  function isGeneratedId(id) {
    return GENERATED_ID.some(p => p.test(id));
  }

  function isStableClass(cls) {
    return !HASH_CLASS.some(p => p.test(cls));
  }

  // ─── implicit role ──────────────────────────────────────────────
  function implicitRole(tag) {
    const map = {
      a: 'link', button: 'button', input: 'textbox',
      select: 'combobox', textarea: 'textbox',
      img: 'img', nav: 'navigation', main: 'main',
      form: 'form', table: 'table', h1: 'heading',
      h2: 'heading', h3: 'heading'
    };
    return map[tag] || '';
  }

  function computeAccessibleName(el) {
    return el.getAttribute('aria-label') ||
           el.getAttribute('title') ||
           el.getAttribute('placeholder') ||
           (el.textContent || '').trim().slice(0, 60) ||
           '';
  }

  // ─── main ──────────────────────────────────────────────────────
  function generateSelector(el) {
    if (!el || el === document.documentElement || el === document.body) return null;

    const candidates = [];
    const tag = el.tagName.toLowerCase();

    // 1. data-testid
    const testId = el.closest('[data-testid]')?.getAttribute('data-testid');
    if (testId) {
      candidates.push({ s: `[data-testid="${esc(testId)}"]`, m: 'data-testid', st: 1.0 });
    }

    // 2. aria-label
    const aria = el.getAttribute('aria-label');
    if (aria && aria.length <= 80) {
      candidates.push({ s: `${tag}[aria-label="${esc(aria)}"]`, m: 'aria-label', st: 0.9 });
    }

    // 3. role + name
    const role = el.getAttribute('role') || implicitRole(tag);
    const name = computeAccessibleName(el);
    if (role && name) {
      candidates.push({ s: `[role="${esc(role)}"][aria-label="${esc(name)}"]`, m: 'role+name', st: 0.85 });
    }

    // 4. id
    if (el.id && !isGeneratedId(el.id)) {
      candidates.push({ s: `#${CSS.escape(el.id)}`, m: 'id', st: 0.8 });
    }

    // 5. stable classes
    const classes = Array.from(el.classList).filter(isStableClass);
    if (classes.length > 0) {
      candidates.push({ s: `${tag}.${classes.map(CSS.escape).join('.')}`, m: 'class', st: 0.4 });
    }

    // 6. nth-of-type fallback
    if (candidates.length === 0) {
      candidates.push({ s: buildNthPath(el), m: 'nth-of-type', st: 0.1 });
    }

    // 排序: stability desc, selector length asc
    candidates.sort((a, b) => b.st - a.st || a.s.length - b.s.length);

    const best = candidates[0];
    return {
      css_selector: best.s,
      css_selector_method: best.m,
      alternatives: candidates.slice(1, 4).map(c => ({ s: c.s, m: c.m, st: c.st }))
    };
  }

  function buildNthPath(el) {
    const parts = [];
    let cur = el;
    while (cur && cur !== document.body && cur !== document.documentElement) {
      const parent = cur.parentElement;
      if (!parent) break;
      const siblings = Array.from(parent.children).filter(
        c => c.tagName === cur.tagName
      );
      const idx = siblings.indexOf(cur) + 1;
      parts.unshift(`${cur.tagName.toLowerCase()}:nth-of-type(${idx})`);
      cur = parent;
      if (cur.id && !isGeneratedId(cur.id)) {
        parts.unshift(`#${CSS.escape(cur.id)}`);
        break;
      }
    }
    return parts.join(' > ');
  }

  function esc(s) {
    return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
  }

  // ─── shadow DOM ─────────────────────────────────────────────────
  function extractFromEvent(event) {
    const path = event.composedPath();
    for (const node of path) {
      if (node instanceof Element && node !== document.documentElement && node !== document.body) {
        const sel = generateSelector(node);
        if (sel) {
          // check shadow boundary
          let shadowPath = null;
          if (event.target !== node && event.target.getRootNode() instanceof ShadowRoot) {
            shadowPath = buildShadowPath(event.target);
          }
          return { ...sel, shadow_path: shadowPath, element_tag: node.tagName.toLowerCase() };
        }
      }
      if (node instanceof ShadowRoot) {
        const host = node.host;
        if (host instanceof Element) {
          const sel = generateSelector(host);
          if (sel) return { ...sel, shadow_path: null, element_tag: host.tagName.toLowerCase() };
        }
      }
    }
    return null;
  }

  function buildShadowPath(el) {
    const parts = [];
    let cur = el;
    while (cur) {
      const root = cur.getRootNode();
      if (root instanceof ShadowRoot) {
        const sel = generateSelector(cur)?.css_selector || cur.tagName.toLowerCase();
        parts.unshift(sel);
        cur = root.host;
      } else {
        break;
      }
    }
    return parts.length > 0 ? parts.join(' >>>> ') : null;
  }

  // ─── export ────────────────────────────────────────────────────
  window.__fox = window.__fox || {};
  window.__fox.generateSelector = generateSelector;
  window.__fox.extractFromEvent = extractFromEvent;
  window.__fox.isGeneratedId = isGeneratedId;
})();
