/* pi-bridge overlay — injected by the Pi into the unmodified upstream UI.
 *
 *   1. optional zh-Hant translation of basic UI text (exact dictionary matches
 *      only; proper nouns untouched). Toggle with the 中/EN button, remembered
 *      in localStorage.
 *   2. an "Updates" widget wired to /api/update/status + /api/update/apply.
 *
 * Deliberately conservative: it only replaces text that exactly matches a
 * dictionary key, so it can't garble anything it doesn't recognise.
 */
(function () {
  'use strict';
  var LS_LANG = 'k7pi-lang';
  var dict = {};
  var lang = localStorage.getItem(LS_LANG) || 'zh-Hant';

  function t(s) {
    var key = s.trim();
    if (!key) return s;
    var hit = dict[key];
    if (!hit) return s;
    // preserve surrounding whitespace
    return s.replace(key, hit);
  }

  function translateNode(node) {
    if (lang !== 'zh-Hant') return;
    if (node.nodeType === 3) { // text
      var v = node.nodeValue;
      if (v && v.trim() && dict[v.trim()]) node.nodeValue = t(v);
      return;
    }
    if (node.nodeType !== 1) return;
    var tag = node.tagName;
    if (tag === 'SCRIPT' || tag === 'STYLE' || tag === 'CANVAS') return;
    ['title', 'placeholder', 'aria-label'].forEach(function (a) {
      var val = node.getAttribute && node.getAttribute(a);
      if (val && dict[val.trim()]) node.setAttribute(a, t(val));
    });
    for (var c = node.firstChild; c; c = c.nextSibling) translateNode(c);
  }

  function retranslateAll() { translateNode(document.body); }

  // ---- updates widget -------------------------------------------------------
  function el(tag, props, kids) {
    var e = document.createElement(tag);
    Object.assign(e, props || {});
    (kids || []).forEach(function (k) { e.appendChild(typeof k === 'string' ? document.createTextNode(k) : k); });
    return e;
  }

  function mountBar() {
    if (document.getElementById('k7pi-bar')) return;
    var bar = el('div', { id: 'k7pi-bar' });
    bar.style.cssText =
      'position:fixed;right:10px;bottom:10px;z-index:99999;display:flex;gap:6px;' +
      'align-items:center;font:12px/1.4 system-ui,sans-serif;background:rgba(20,22,28,.92);' +
      'color:#e8e8e8;border:1px solid #3a3f4b;border-radius:8px;padding:6px 8px;box-shadow:0 4px 16px rgba(0,0,0,.35)';

    var langBtn = el('button', { textContent: lang === 'zh-Hant' ? 'EN' : '中' });
    styleBtn(langBtn);
    langBtn.onclick = function () {
      lang = lang === 'zh-Hant' ? 'en' : 'zh-Hant';
      localStorage.setItem(LS_LANG, lang);
      location.reload();
    };

    var updBtn = el('button', { textContent: dict['Check for updates'] || 'Check for updates' });
    styleBtn(updBtn);
    var status = el('span', { id: 'k7pi-upd', textContent: '' });
    status.style.opacity = '.85';

    updBtn.onclick = function () {
      status.textContent = '…';
      fetch('/api/update/status').then(function (r) { return r.json(); }).then(function (d) {
        if (d.error) { status.textContent = '✗ ' + d.error; return; }
        if (d.up_to_date) { status.textContent = (dict['Up to date'] || 'Up to date') + ' · ' + d.current; return; }
        status.textContent = (dict['Update available'] || 'Update available') + ': ' + d.available;
        var go = el('button', { textContent: dict['Update now'] || 'Update now' });
        styleBtn(go); go.style.borderColor = '#4a7';
        go.onclick = function () {
          go.disabled = true;
          status.textContent = dict['Restarting…'] || 'Updating…';
          fetch('/api/update/apply', { method: 'POST' }).then(function () {
            var tries = 0;
            var iv = setInterval(function () {
              tries++;
              fetch('/api/version').then(function (r) { return r.json(); }).then(function (v) {
                if (v.version === d.available) { clearInterval(iv); location.reload(); }
              }).catch(function () {});
              if (tries > 40) clearInterval(iv);
            }, 2000);
          });
        };
        bar.appendChild(go);
      }).catch(function (e) { status.textContent = '✗ ' + e; });
    };

    bar.appendChild(langBtn);
    bar.appendChild(updBtn);
    bar.appendChild(status);
    document.body.appendChild(bar);
  }

  function styleBtn(b) {
    b.style.cssText =
      'background:#2a2e38;color:#e8e8e8;border:1px solid #444a58;border-radius:6px;' +
      'padding:3px 8px;cursor:pointer;font:inherit';
  }

  // ---- boot ---------------------------------------------------------------
  fetch('/pi/dict-zh-Hant.json')
    .then(function (r) { return r.json(); })
    .then(function (d) { delete d._note; dict = d; })
    .catch(function () {})
    .finally(function () {
      var start = function () {
        retranslateAll();
        mountBar();
        new MutationObserver(function (muts) {
          muts.forEach(function (m) {
            m.addedNodes && m.addedNodes.forEach(function (n) { translateNode(n); });
            if (m.type === 'characterData') translateNode(m.target);
          });
        }).observe(document.body, { childList: true, subtree: true, characterData: true });
      };
      if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
      else start();
    });
})();
