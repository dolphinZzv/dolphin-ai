// Fox SPA Navigation Detector
// 检测 SPA 内的"页面跳转": pushState/popstate/hashchange/poll

(function () {
  'use strict';

  let lastURL = location.href;

  function onNavigation(source, newURL) {
    if (newURL === lastURL) return;
    const prev = lastURL;
    lastURL = newURL;
    const u = new URL(newURL);

    window.__fox?.onSPANavigation?.({
      event_type: 'page.load',
      payload: {
        url_path: u.pathname,
        is_spa: true,
        navigation_source: source,
        previous_url: prev
      }
    });
  }

  // Signal 1: pushState / replaceState
  const _push = history.pushState;
  history.pushState = function (...args) {
    const r = _push.apply(this, args);
    requestAnimationFrame(() => {
      setTimeout(() => onNavigation('pushState', location.href), 100);
    });
    return r;
  };

  const _replace = history.replaceState;
  history.replaceState = function (...args) {
    const r = _replace.apply(this, args);
    requestAnimationFrame(() => {
      setTimeout(() => onNavigation('replaceState', location.href), 100);
    });
    return r;
  };

  // Signal 2: popstate
  window.addEventListener('popstate', () => {
    onNavigation('popstate', location.href);
  });

  // Signal 3: hashchange
  window.addEventListener('hashchange', () => {
    onNavigation('hashchange', location.href);
  });

  // Signal 4: URL poll fallback
  setInterval(() => {
    if (location.href !== lastURL) {
      onNavigation('poll', location.href);
    }
  }, 300);
})();
