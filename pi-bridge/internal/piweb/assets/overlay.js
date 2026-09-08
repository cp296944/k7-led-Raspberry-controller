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

  var LANGS = [
    { code: 'zh-Hant', label: '繁體中文' },
    { code: 'en', label: 'English' }
  ];

  // Header controls live in the upstream .topbar, right after the version chip.
  function mountHeaderControls() {
    var old = document.getElementById('k7pi-bar');
    if (old) old.remove();
    if (document.getElementById('k7pi-hdr')) return true;
    var bar = document.querySelector('.topbar');
    var anchor = document.getElementById('versionChip') || (bar && bar.querySelector('h1'));
    if (!bar || !anchor) return false;

    var wrap = el('span', { id: 'k7pi-hdr' });
    wrap.style.cssText = 'display:inline-flex;gap:6px;align-items:center';

    // update button + inline status
    var updBtn = el('button', { type: 'button', textContent: dict['Check for updates'] || 'Check for updates' });
    styleBtn(updBtn);
    var status = el('span', { id: 'k7pi-upd' });
    status.style.cssText = 'font-size:0.78rem;color:var(--muted,#8a95a3)';
    updBtn.onclick = function () { checkUpdate(status, wrap); };

    // language dropdown
    var sel = el('select', { id: 'k7pi-lang', title: 'Language / 語言' });
    sel.style.cssText =
      'background:var(--surface2,#2a2e38);border:1px solid var(--border,#444a58);color:var(--text,#e8e8e8);' +
      'border-radius:6px;padding:3px 6px;font-size:0.8rem;font-family:inherit;cursor:pointer';
    LANGS.forEach(function (l) {
      var o = el('option', { value: l.code, textContent: l.label });
      if (l.code === lang) o.selected = true;
      sel.appendChild(o);
    });
    sel.onchange = function () {
      localStorage.setItem(LS_LANG, sel.value);
      location.reload();
    };

    wrap.appendChild(updBtn);
    wrap.appendChild(status);
    wrap.appendChild(sel);
    anchor.parentNode.insertBefore(wrap, anchor.nextSibling);
    return true;
  }

  function checkUpdate(status, wrap) {
    status.textContent = '…';
    fetch('/api/update/status').then(function (r) { return r.json(); }).then(function (d) {
      if (d.error) { status.textContent = '✗ ' + d.error; return; }
      if (d.up_to_date) { status.textContent = (dict['Up to date'] || 'Up to date') + ' · ' + d.current; return; }
      status.textContent = (dict['Update available'] || 'Update available') + ': ' + d.available;
      if (wrap.querySelector('.k7pi-go')) return;
      var go = el('button', { type: 'button', textContent: dict['Update now'] || 'Update now' });
      go.className = 'k7pi-go';
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
      wrap.appendChild(go);
    }).catch(function (e) { status.textContent = '✗ ' + e; });
  }

  function styleBtn(b) {
    b.style.cssText =
      'background:var(--surface2,#2a2e38);color:var(--text,#e8e8e8);border:1px solid var(--border,#444a58);' +
      'border-radius:6px;padding:3px 9px;cursor:pointer;font:inherit;font-size:0.8rem;line-height:1.4';
  }

  // ---- explicit-apply: nothing reaches the lamp until the user hits Push ----
  // Upstream auto-pushes to the lamp on every master-slider / shift change.
  // We neuter that: staged changes stay local (chart + pi-bridge store) and are
  // only sent to the lamp by the explicit "Push" / "Apply" button.
  function curMaster() {
    var s = document.getElementById('masterSlider');
    return s ? parseInt(s.value, 10) || 100 : 100;
  }
  function setDirty(on) {
    var pb = document.getElementById('pushBtn');
    if (!pb) return;
    pb.classList.toggle('k7pi-dirty', !!on);
    if (on && !pb.dataset.k7piBase) pb.dataset.k7piBase = pb.textContent;
    if (pb.dataset.k7piBase) pb.textContent = on ? '● ' + pb.dataset.k7piBase : pb.dataset.k7piBase;
  }
  function installExplicitApply() {
    if (installExplicitApply.done || !window.api) return;
    // once per session hint
    var hinted = sessionStorage.getItem('k7pi-hinted');

    if (typeof window._autoPushMaster === 'function') {
      window._autoPushMaster = async function () {
        setDirty(true);
        if (!hinted) {
          toast(dict['Changes staged — press Push to send to the lamp'] ||
                '改動已暫存,按「⬆ Push」才會送到燈');
          sessionStorage.setItem('k7pi-hinted', '1');
          hinted = '1';
        }
      };
    }
    ['pushSchedule', 'saveManual'].forEach(function (fn) {
      var orig = window[fn];
      if (typeof orig !== 'function') return;
      window[fn] = async function () {
        try { await window.api('POST', '/api/master', { value: curMaster() }); } catch (e) {}
        var r = await orig.apply(this, arguments);
        setDirty(false);
        return r;
      };
    });
    if (typeof window.onModeToggle === 'function') {
      var om = window.onModeToggle;
      window.onModeToggle = function () { var r = om.apply(this, arguments); setDirty(true); return r; };
    }
    // ── Shift fix ──────────────────────────────────────────────────────────
    // Upstream's "Effective Today" curve applies seasonal shift, siesta and
    // lunar — but NOT the manual "時段平移" (dayShift). So +2h and +4h render
    // identically. We wrap chartEffectiveValueAtMins to also rotate the lookup
    // by the current shift (read from the #shiftVal label), matching the
    // server-side row rotation piweb does on /api/push.
    if (typeof window.chartEffectiveValueAtMins === 'function' && !window.chartEffectiveValueAtMins._k7pi) {
      var oCEV = window.chartEffectiveValueAtMins;
      window.chartEffectiveValueAtMins = function (mins, ci) {
        var lbl = document.getElementById('shiftVal');
        var sh = lbl ? (parseInt(lbl.textContent, 10) || 0) : 0; // "+2h" -> 2
        var w = window._wrapMins || function (m) { return ((m % 1440) + 1440) % 1440; };
        return oCEV(w(mins - sh * 60), ci);
      };
      window.chartEffectiveValueAtMins._k7pi = true;
    }
    if (typeof window.changeShift === 'function' && !window.changeShift._k7pi) {
      var ocs = window.changeShift;
      window.changeShift = function () {
        var r = ocs.apply(this, arguments);
        try {
          if (typeof window.setChartMode === 'function') window.setChartMode('effective');
          else if (typeof window.updateChart === 'function') window.updateChart();
        } catch (e) {}
        setDirty(true);
        toast(dict['Shift preview — press Push to apply to the lamp'] ||
              '時段平移:圖表已切到「今日實際」預覽,按 ⬆ Push 才會套用到燈');
        return r;
      };
      window.changeShift._k7pi = true;
    }
    var style = document.createElement('style');
    style.textContent =
      '.k7pi-dirty{outline:2px solid #e0a53a !important;outline-offset:1px;animation:k7pipulse 1.6s ease-in-out infinite}' +
      '@keyframes k7pipulse{50%{outline-color:#f4c96b}}' +
      '.k7pi-toast{position:fixed;left:50%;bottom:64px;transform:translateX(-50%);z-index:99999;' +
      'background:#2a2e38;color:#fff;border:1px solid #4a4f5c;border-radius:8px;padding:8px 14px;' +
      'font:13px/1.4 system-ui,sans-serif;box-shadow:0 6px 20px rgba(0,0,0,.4);max-width:80vw}';
    document.head.appendChild(style);
    installExplicitApply.done = true;
  }
  function toast(msg) {
    var d = document.createElement('div');
    d.className = 'k7pi-toast';
    d.textContent = msg;
    document.body.appendChild(d);
    setTimeout(function () { d.style.opacity = '0'; d.style.transition = 'opacity .4s'; }, 3500);
    setTimeout(function () { d.remove(); }, 4000);
  }

  // ---- boot ---------------------------------------------------------------
  fetch('/pi/dict-zh-Hant.json')
    .then(function (r) { return r.json(); })
    .then(function (d) { delete d._note; dict = d; })
    .catch(function () {})
    .finally(function () {
      var start = function () {
        retranslateAll();
        mountHeaderControls();
        installExplicitApply();
        // the page's own script may define api() slightly after us
        var tries = 0;
        var iv = setInterval(function () {
          installExplicitApply();
          mountHeaderControls();
          if (installExplicitApply.done || ++tries > 40) clearInterval(iv);
        }, 250);
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
