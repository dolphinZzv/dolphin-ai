// Fox Auth Page Detector
// 多信号综合检测: URL 路径 + 密码字段 + login form + OAuth 按钮 + 页面标题

(function () {
  'use strict';

  const AUTH_URL_PATTERNS = [
    '/login', '/signin', '/sign-in', '/sign_in',
    '/auth', '/authenticate', '/oauth', '/authorize',
    '/sso', '/saml', '/openid',
    '/two-factor', '/2fa', '/mfa', '/verify',
    '/register', '/signup', '/sign-up',
    '/reset-password', '/forgot-password',
    '/challenge', '/consent'
  ];

  const AUTH_TITLE_WORDS = ['login', 'sign in', 'authentication', 'log in'];

  const OAUTH_SELECTORS = [
    'a[href*="accounts.google.com"]',
    'a[href*="github.com/login/oauth"]',
    'a[href*="login.microsoftonline.com"]',
    'button:has-text("Sign in with")'
  ];

  function detectAuthPage() {
    const signals = [];
    const url = location.pathname.toLowerCase();

    // Signal 1: URL
    for (const p of AUTH_URL_PATTERNS) {
      if (url.includes(p)) {
        signals.push({ source: 'url', pattern: p, confidence: 0.9 });
        break;
      }
    }

    // Signal 2: password fields
    const pwFields = document.querySelectorAll('input[type="password"]');
    if (pwFields.length > 0) {
      signals.push({ source: 'dom', pattern: 'password_field', confidence: 0.85 });
    }

    // Signal 3: login form structure
    if (pwFields.length > 0) {
      const textInputs = document.querySelectorAll(
        'input[type="text"], input[type="email"], input:not([type])'
      );
      if (textInputs.length > 0) {
        for (const pw of pwFields) {
          const form = pw.closest('form');
          if (form && form.querySelector('input[type="text"], input[type="email"]')) {
            signals.push({ source: 'dom', pattern: 'login_form', confidence: 0.95 });
            break;
          }
        }
      }
    }

    // Signal 4: OAuth buttons
    try {
      let oauthCount = 0;
      for (const s of OAUTH_SELECTORS) {
        try {
          oauthCount += document.querySelectorAll(s).length;
        } catch {}
      }
      if (oauthCount > 0) {
        signals.push({ source: 'dom', pattern: 'oauth_buttons', confidence: 0.8 });
      }
    } catch {}

    // Signal 5: page title
    const title = document.title.toLowerCase();
    for (const w of AUTH_TITLE_WORDS) {
      if (title.includes(w)) {
        signals.push({ source: 'title', pattern: w, confidence: 0.7 });
        break;
      }
    }

    if (signals.length === 0) return { isAuth: false, confidence: 0, signals: [] };

    // 单独 OAuth 按钮不足以触发 — 需要结合 password_field 或 auth URL
    const hasForm = signals.some(s => s.pattern === 'login_form' || s.pattern === 'password_field');
    const hasURL = signals.some(s => s.source === 'url');
    const onlyOAuth = signals.every(s => s.pattern === 'oauth_buttons');

    // OAuth 按钮单独出现(如 google.com 顶部 Sign in 链接) → 不算登录页
    if (onlyOAuth && !hasForm && !hasURL) {
      return { isAuth: false, confidence: 0, signals };
    }

    let maxConf = Math.max(...signals.map(s => s.confidence));
    const combined = (hasForm && hasURL) ? Math.min(maxConf + 0.1, 1.0) : maxConf;

    return {
      isAuth: combined >= 0.75,
      confidence: combined,
      signals,
      domain: location.hostname
    };
  }

  // ─── export ────────────────────────────────────────────────────
  window.__fox = window.__fox || {};
  window.__fox.detectAuthPage = detectAuthPage;
})();
