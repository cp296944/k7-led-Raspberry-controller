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

    // auto-update toggle (persisted server-side)
    var autoLbl = el('label', { title: 'Auto-apply updates' });
    autoLbl_style(autoLbl);
    var autoCb = el('input', { type: 'checkbox' });
    autoCb.onchange = function () {
      fetch('/api/update/config', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ auto_update: autoCb.checked })
      }).catch(function () {});
    };
    autoLbl.appendChild(autoCb);
    autoLbl.appendChild(document.createTextNode(dict['Auto'] || 'Auto'));
    fetch('/api/update/status').then(function (r) { return r.json(); })
      .then(function (d) { autoCb.checked = !!d.auto_update; }).catch(function () {});

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
    wrap.appendChild(autoLbl);
    wrap.appendChild(status);
    wrap.appendChild(sel);
    anchor.parentNode.insertBefore(wrap, anchor.nextSibling);
    return true;
  }
  function autoLbl_style(l) {
    l.style.cssText = 'display:inline-flex;gap:3px;align-items:center;font-size:0.76rem;color:var(--muted,#8a95a3);cursor:pointer';
  }

  // ---- version chip -> changelog popup --------------------------------
  function mountVersionChangelog() {
    var chip = document.getElementById('versionChip');
    if (!chip || chip.dataset.k7pi) return;
    chip.dataset.k7pi = '1';
    chip.style.cursor = 'pointer';
    chip.title = (dict['View release history'] || '查看更新歷程');
    chip.addEventListener('click', openChangelog);
  }
  function openChangelog() {
    if (document.getElementById('k7pi-changelog')) return;
    var back = el('div', { id: 'k7pi-changelog' });
    back.style.cssText = 'position:fixed;inset:0;z-index:100000;background:rgba(0,0,0,.55);display:flex;align-items:flex-start;justify-content:center;padding:6vh 12px';
    back.onclick = function (e) { if (e.target === back) back.remove(); };
    var panel = el('div');
    panel.style.cssText = 'background:var(--surface,#1c2229);color:var(--text,#e7ecf1);border:1px solid var(--border,#2c343d);border-radius:10px;max-width:680px;width:100%;max-height:82vh;overflow:auto;box-shadow:0 12px 40px rgba(0,0,0,.5)';
    var head = el('div'); head.style.cssText = 'position:sticky;top:0;background:inherit;display:flex;justify-content:space-between;align-items:center;padding:12px 16px;border-bottom:1px solid var(--border,#2c343d);font-weight:600';
    head.appendChild(document.createTextNode(dict['Release history'] || '更新歷程'));
    var x = el('button', { type: 'button', textContent: '✕' }); styleBtn(x); x.onclick = function () { back.remove(); };
    head.appendChild(x);
    var listEl = el('div'); listEl.style.cssText = 'padding:8px 16px 18px';
    listEl.textContent = '…';
    panel.appendChild(head); panel.appendChild(listEl); back.appendChild(panel);
    document.body.appendChild(back);

    fetch('/api/update/history').then(function (r) { return r.json(); }).then(function (d) {
      listEl.textContent = '';
      var cur = d.current;
      (d.releases || []).forEach(function (rel) {
        var item = el('div'); item.style.cssText = 'padding:10px 0;border-bottom:1px solid var(--border,#2c343d)';
        var t = el('div'); t.style.cssText = 'font-weight:600;font-size:0.92rem;display:flex;gap:8px;align-items:center';
        t.appendChild(document.createTextNode(rel.tag));
        if (rel.tag === cur) { var b = el('span', { textContent: dict['installed'] || '目前' }); b.style.cssText = 'font-size:0.72rem;background:#2e7d32;color:#fff;border-radius:4px;padding:1px 6px'; t.appendChild(b); }
        if (rel.prerelease) { var p = el('span', { textContent: 'pre' }); p.style.cssText = 'font-size:0.72rem;color:var(--muted,#93a1af)'; t.appendChild(p); }
        var when = el('span', { textContent: (rel.published_at || '').slice(0, 10) }); when.style.cssText = 'font-size:0.75rem;color:var(--muted,#93a1af);margin-left:auto';
        t.appendChild(when);
        var notes = el('pre', { textContent: (rel.notes || '').trim() || '—' });
        notes.style.cssText = 'white-space:pre-wrap;font:0.8rem/1.45 ui-monospace,monospace;color:var(--muted,#b5c0cc);margin:6px 0 0;max-height:200px;overflow:auto';
        item.appendChild(t); item.appendChild(notes);
        listEl.appendChild(item);
      });
      if (!listEl.children.length) listEl.textContent = dict['No history available'] || '沒有可用的歷程';
    }).catch(function (e) { listEl.textContent = '✗ ' + e; });
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
        var msg = (dict['Update to {tag} now? The service will restart.'] ||
                   '要現在更新到 {tag} 嗎?服務會重新啟動。').replace('{tag}', d.available);
        if (!window.confirm(msg)) return;
        go.disabled = true;
        status.textContent = dict['Restarting…'] || 'Updating…';
        fetch('/api/update/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true, tag: d.available })
        }).then(function (r) { return r.json().catch(function () { return {}; }); })
          .then(function (res) {
            if (res && res.error) { go.disabled = false; status.textContent = '✗ ' + res.error; return; }
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
        // The rotation is already baked into the rows we just pushed, so clear
        // the running "shifted by" readout.
        if (fn === 'pushSchedule') {
          var sv = document.getElementById('shiftVal');
          if (sv) { sv.setAttribute('data-k7pi', '0'); sv.textContent = '+0h'; }
        }
        return r;
      };
    });
    if (typeof window.onModeToggle === 'function') {
      var om = window.onModeToggle;
      window.onModeToggle = function () { var r = om.apply(this, arguments); setDirty(true); return r; };
    }
    // ── Read fix ──────────────────────────────────────────────────────────
    // Upstream's readFromDevice() only does a live lamp readAll (0x1008) when
    // the platform is pc_bridge; on pi-bridge it just reloads the local
    // /api/state cache, so the two Read buttons never show what the lamp
    // actually has stored. Prepend a real /api/lamp/read so Read means "pull
    // the schedule from the lamp" here too. (Same behaviour as pc-bridge Read:
    // it overwrites unsaved chart edits — that is expected.)
    if (typeof window.readFromDevice === 'function' && !window.readFromDevice._k7pi) {
      var oRead = window.readFromDevice;
      window.readFromDevice = async function () {
        try {
          if (typeof window.setBusy === 'function') window.setBusy(dict['Reading from lamp…'] || 'Reading from lamp…');
          await window.api('GET', '/api/lamp/read');
        } catch (e) { /* fall through — oRead surfaces the error via /api/state */ }
        return oRead.apply(this, arguments);
      };
      window.readFromDevice._k7pi = true;
    }
    // ── Shift: rotate the Base schedule for real ──────────────────────────
    // Upstream's changeShift only bumps a `dayShift` counter that the Base chart
    // never renders (so it "does nothing"), and our old wrap forced the chart to
    // "Effective Today" to fake a preview. Instead: rotate the actual 24 rows on
    // the Base chart now. `k7pi-pending-shift` on #shiftVal is just a running
    // readout; it resets to +0h after Push. dayShift stays 0 so the server does
    // NOT rotate a second time.
    if (typeof window.changeShift === 'function' && !window.changeShift._k7pi) {
      window.changeShift = function (delta) {
        delta = delta || 0;
        rotateScheduleHours(delta);
        var sv = document.getElementById('shiftVal');
        if (sv) {
          var n = (parseInt(sv.getAttribute('data-k7pi') || '0', 10) + delta);
          n = ((n % 24) + 24) % 24;
          sv.setAttribute('data-k7pi', String(n));
          sv.textContent = n === 0 ? '+0h' : (n <= 12 ? '+' + n + 'h' : '-' + (24 - n) + 'h');
        }
        setDirty(true);
        toast(dict['Schedule shifted — press ⬆ Push to send'] ||
              '排程已平移,按 ⬆ Push 送到燈');
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

  // ---- Wi-Fi signal indicator (in the topbar) ---------------------------
  function mountWifi() {
    var hdr = document.getElementById('k7pi-hdr');
    if (!hdr || document.getElementById('k7pi-wifi')) return;
    var w = el('span', { id: 'k7pi-wifi', title: 'Wi-Fi to lamp' });
    w.style.cssText = 'font-size:0.78rem;padding:2px 6px;border-radius:5px;border:1px solid var(--border,#444a58);white-space:nowrap';
    hdr.appendChild(w);
    var poll = function () {
      fetch('/api/wifi/signal').then(function (r) { return r.json(); }).then(function (d) {
        var q = d.quality, r = d.rssi_dbm;
        if (q == null) { w.textContent = '📶 —'; w.style.color = 'var(--muted,#8a95a3)'; return; }
        w.textContent = '📶 ' + q + '%' + (r != null ? ' (' + r + 'dBm)' : '');
        w.style.color = q >= 55 ? '#4caf50' : q >= 35 ? '#e0a53a' : '#e05a5a';
        w.title = 'Wi-Fi to lamp · RSSI ' + r + ' dBm' + (d.tx_bitrate_mbps ? ' · ' + d.tx_bitrate_mbps + ' Mbps' : '') +
                  (d.lamp && d.lamp.ok ? ' · lamp OK' : ' · lamp: no recent contact');
      }).catch(function () {});
    };
    poll();
    setInterval(poll, 15000);
  }

  // ---- today's lamp-write counter (in the topbar) ----------------------
  // Auto  = writes the always-on engine made (smooth ramp / feed / maintenance).
  // Manual = writes you triggered (Push / Preview / re-arm). Resets at midnight.
  function mountWrites() {
    var hdr = document.getElementById('k7pi-hdr');
    if (!hdr || document.getElementById('k7pi-writes')) return;
    var s = el('span', { id: 'k7pi-writes' });
    s.style.cssText = 'font-size:0.78rem;padding:2px 6px;border-radius:5px;border:1px solid var(--border,#444a58);white-space:nowrap';
    hdr.appendChild(s);
    var poll = function () {
      fetch('/api/output/status').then(function (r) { return r.json(); }).then(function (d) {
        var w = d.writes_today || {};
        var a = w.auto || 0, m = w.manual || 0;
        var autoL = dict['auto'] || '自動', manL = dict['manual'] || '手動', todayL = dict['today'] || '今日';
        s.textContent = '📤 ' + todayL + ' ' + autoL + ' ' + a + ' · ' + manL + ' ' + m;
        s.style.color = (a + m) >= 200 ? '#e0a53a' : 'var(--muted,#8a95a3)';
        s.title = (dict['Lamp writes since local midnight'] || '本地午夜起對燈的寫入次數') +
                  (d.live ? ' · ' + (dict['engine live-driving'] || 'engine 即時驅動中') : '');
      }).catch(function () {});
    };
    poll();
    setInterval(poll, 15000);
  }

  // ---- FEAT-A: 光譜數值表 (live-linked with the chart) -------------------
  // The grid mirrors chart.data.datasets: pick a profile / drag a point / press
  // Read and the numbers follow; type a number and the chart follows, live.
  // Writes go through the page's own dragData.onDrag so scheduleBase stays
  // authoritative for Push.
  var GRID_TITLE = function () { return dict['Spectrum value table'] || '光譜數值表'; };

  // Upstream declares `chart` with `let` inside a classic <script>, so it is
  // NOT a property of window. Reach the live instance through Chart.js's own
  // registry instead (canvas id = schedChart, Chart.js v4).
  function liveChart() {
    if (window.chart) return window.chart;
    try { return (window.Chart && window.Chart.getChart && window.Chart.getChart('schedChart')) || null; }
    catch (e) { return null; }
  }

  function chartCols() {
    var c = liveChart();
    if (!c || !c.data || !c.data.datasets) return [];
    var seen = {}, out = [];
    c.data.datasets.forEach(function (ds, i) {
      if (ds && ds.channelKey && !seen[ds.channelKey] &&
        String(ds.label || '').indexOf('full moon') < 0) {
        seen[ds.channelKey] = 1;
        out.push({ key: ds.channelKey, ds: i, label: ds.label || ds.channelKey });
      }
    });
    return out;
  }
  function gridCellVal(dsIndex, h) {
    try { return Math.round(liveChart().data.datasets[dsIndex].data[h] || 0); } catch (e) { return 0; }
  }
  function gridWrite(dsIndex, h, v) {
    v = clampv(v);
    var c = liveChart();
    var cfgD = c && (c._dragDataConfig || (c.options.plugins.dragData));
    if (cfgD && typeof cfgD.onDrag === 'function') {
      // this updates schedule[] AND scheduleBase[] the way a real drag would
      if (typeof window.setChartMode === 'function') window.setChartMode('base');
      cfgD.onDrag(null, dsIndex, h, v);
    } else if (c) {
      c.data.datasets[dsIndex].data[h] = v;
    }
    if (c) c.update('none');
    if (typeof window.updateColorStrip === 'function') window.updateColorStrip();
  }

  // Rotate the WHOLE schedule (all 6 channels) by `delta` hours, in place, on the
  // Base chart — new[h] = old[h - delta]. Writes through the page's own
  // dragData.onDrag so scheduleBase (what Push sends) really moves. This is what
  // the "Shift" ◀▶ buttons do now: no mode-switch, the Base curve visibly moves.
  function rotateScheduleHours(delta) {
    delta = ((Math.round(delta) % 24) + 24) % 24;
    if (!delta) return;
    var c = liveChart();
    if (!c) return;
    var cols = chartCols();
    if (!cols.length) return;
    if (typeof window.setChartMode === 'function') window.setChartMode('base');
    var cur = [];
    for (var h = 0; h < 24; h++) { cur[h] = []; for (var k = 0; k < cols.length; k++) cur[h][k] = gridCellVal(cols[k].ds, h); }
    var cfgD = c._dragDataConfig || (c.options.plugins && c.options.plugins.dragData);
    for (var h2 = 0; h2 < 24; h2++) {
      var srcH = ((h2 - delta) % 24 + 24) % 24;
      for (var k2 = 0; k2 < cols.length; k2++) {
        var v = cur[srcH][k2];
        if (cfgD && typeof cfgD.onDrag === 'function') cfgD.onDrag(null, cols[k2].ds, h2, v);
        else c.data.datasets[cols[k2].ds].data[h2] = v;
      }
    }
    if (typeof window.applyMaster === 'function') { try { window.applyMaster(); } catch (e) {} }
    if (typeof window.updateChart === 'function') { try { window.updateChart(); } catch (e) {} }
    else c.update('none');
    if (typeof window.updateColorStrip === 'function') window.updateColorStrip();
    if (typeof window.updateNowBars === 'function') window.updateNowBars();
  }

  // Draw an hourly gridline on the schedule chart (upstream only rules every 4h,
  // where its labels are). Pure draw-time plugin — no mutation of chart.options,
  // which in Chart.js v4 triggers a proxy-setter recursion.
  var hourGridPlugin = {
    id: 'k7piHourGrid',
    beforeDatasetsDraw: function (chart) {
      var xs = chart.scales && chart.scales.x, ys = chart.scales && chart.scales.y;
      if (!xs || !ys) return;
      var ctx = chart.ctx;
      ctx.save();
      var light = document.documentElement.getAttribute('data-theme') === 'light';
      ctx.strokeStyle = light ? 'rgba(0,0,0,0.06)' : 'rgba(255,255,255,0.055)';
      ctx.lineWidth = 1;
      for (var h = 1; h < 24; h++) {
        if (h % 4 === 0) continue; // the chart already rules these
        var x = xs.getPixelForValue(h);
        ctx.beginPath();
        ctx.moveTo(x, ys.top);
        ctx.lineTo(x, ys.bottom);
        ctx.stroke();
      }
      ctx.restore();
    }
  };
  function tuneChartAxis() {
    var c = liveChart();
    if (!c || c._k7piAxis) return;
    if (!window.Chart || !window.Chart.register) return;
    try { window.Chart.register(hourGridPlugin); } catch (e) { return; }
    c._k7piAxis = true;
    try { c.update('none'); } catch (e) {}
  }

  function mountValueTable() {
    if (document.getElementById('k7pi-grid')) return;
    var anchor = document.getElementById('autoPanel') || document.querySelector('.chart-canvas-wrap');
    if (!anchor || !liveChart()) return;

    // Always open — the user wants the chart and the value table side by side
    // for tuning, no collapse.
    var box = el('div', { id: 'k7pi-grid' });
    box.style.cssText = 'margin-top:10px;border:1px solid var(--border,#2c343d);border-radius:8px;background:var(--surface,#1c2229);overflow:hidden';
    var head = el('div');
    head.style.cssText = 'background:var(--surface2,#262e37);color:var(--text,#e7ecf1);padding:9px 12px;font:inherit;font-weight:600';
    head.textContent = GRID_TITLE();
    var body = el('div'); body.style.cssText = 'padding:10px 12px 14px';
    buildGrid(body);
    setInterval(function () { body._sync && body._sync(); }, 400);
    box.appendChild(head); box.appendChild(body);
    anchor.appendChild(box);
  }

  function buildGrid(body) {
    var cols = chartCols();
    var wrap = el('div');
    var tbl = el('table'); tbl.style.cssText = 'border-collapse:collapse;font-size:0.8rem;width:100%;table-layout:fixed';
    var thead = el('tr');
    var hc = cell('th', 'h'); hc.style.width = '52px'; thead.appendChild(hc);
    cols.forEach(function (c) {
      var th = cell('th', c.label);
      th.style.whiteSpace = 'nowrap';
      thead.appendChild(th);
    });
    tbl.appendChild(thead);
    var inputs = [];
    for (var h = 0; h < 24; h++) {
      var tr = el('tr'); inputs[h] = [];
      tr.appendChild(cell('td', (h < 10 ? '0' : '') + h + ':00'));
      (function (h) {
        cols.forEach(function (col, k) {
          var inp = el('input', { type: 'number', min: 0, max: 100, value: gridCellVal(col.ds, h) });
          inp.style.cssText = 'width:46px;background:var(--surface2,#262e37);border:1px solid var(--border,#2c343d);color:var(--text,#e7ecf1);border-radius:4px;padding:2px 4px;text-align:center;font:inherit';
          var commit = function () { gridWrite(col.ds, h, parseInt(inp.value, 10) || 0); setDirty(true); };
          inp.addEventListener('change', commit);
          inp.addEventListener('input', function () { clearTimeout(inp._t); inp._t = setTimeout(commit, 250); });
          inputs[h][k] = inp;
          var td = el('td'); td.style.cssText = 'padding:2px;text-align:center'; td.appendChild(inp); tr.appendChild(td);
        });
      })(h);
      tbl.appendChild(tr);
    }
    // live pull: chart -> grid (skip while a cell is focused)
    body._sync = function () {
      var c2 = chartCols();
      // Channel set changed (device switch / hidden channels). Rebuild — but
      // asynchronously, so a chart caught mid-update can't recurse us to death.
      if (c2.length !== cols.length) {
        if (!body._rebuilding) {
          body._rebuilding = true;
          setTimeout(function () { body._rebuilding = false; body.innerHTML = ''; buildGrid(body); }, 60);
        }
        return;
      }
      for (var h = 0; h < 24; h++) for (var k = 0; k < cols.length; k++) {
        var inp = inputs[h][k];
        if (document.activeElement === inp) continue;
        var v = String(gridCellVal(cols[k].ds, h));
        if (inp.value !== v) inp.value = v;
      }
    };

    var perChan = el('div'); perChan.style.cssText = 'display:flex;gap:10px;flex-wrap:wrap;margin:8px 0;font-size:0.8rem';
    var chk = {};
    cols.forEach(function (col) {
      var l = el('label'); l.style.cssText = 'display:inline-flex;gap:3px;align-items:center;cursor:pointer';
      var cb = el('input', { type: 'checkbox', checked: true }); chk[col.key] = cb;
      l.appendChild(cb); l.appendChild(document.createTextNode(col.label)); perChan.appendChild(l);
    });
    var xform = function (fn) {
      var cur = [];
      for (var h = 0; h < 24; h++) { cur[h] = []; for (var k = 0; k < cols.length; k++) cur[h][k] = gridCellVal(cols[k].ds, h); }
      for (var h2 = 0; h2 < 24; h2++) for (var k2 = 0; k2 < cols.length; k2++) {
        if (!chk[cols[k2].key].checked) continue;
        gridWrite(cols[k2].ds, h2, fn(cur, h2, k2));
      }
      setDirty(true); body._sync();
    };

    var barRow = el('div'); barRow.style.cssText = 'display:flex;gap:6px;flex-wrap:wrap;margin-top:8px';
    var mk = function (t, fn) { var b = el('button', { type: 'button', textContent: t }); styleBtn(b); b.onclick = fn; return b; };
    barRow.appendChild(mk(dict['Load from device'] || '從裝置讀取', function () {
      if (window.readFromDevice) window.readFromDevice();
    }));
    barRow.appendChild(mk('⟲ -1h', function () { xform(function (cur, h, k) { return cur[(h + 1) % 24][k]; }); }));
    barRow.appendChild(mk('⟳ +1h', function () { xform(function (cur, h, k) { return cur[(h + 23) % 24][k]; }); }));
    barRow.appendChild(mk('－1%', function () { xform(function (cur, h, k) { return cur[h][k] - 1; }); }));
    barRow.appendChild(mk('＋1%', function () { xform(function (cur, h, k) { return cur[h][k] + 1; }); }));

    body.appendChild(el('div', { textContent: dict['Edits sync with the chart live; press ⬆ Push to send.'] || '編輯與上方圖表即時連動;按 ⬆ Push 才送到燈。' }, []));
    body.lastChild.style.cssText = 'font-size:0.78rem;color:var(--muted,#93a1af);margin-bottom:8px';
    var scroll = el('div'); scroll.style.cssText = 'max-height:340px;overflow:auto'; scroll.appendChild(tbl);
    wrap.appendChild(scroll); wrap.appendChild(perChan); wrap.appendChild(barRow);
    body.appendChild(wrap);
    body._sync();
  }
  function cell(tag, txt) {
    var c = el(tag, { textContent: txt });
    c.style.cssText = 'padding:3px 6px;border-bottom:1px solid var(--border,#2c343d);color:var(--muted,#93a1af);text-align:center';
    return c;
  }
  function clampv(v) { return v < 0 ? 0 : v > 100 ? 100 : v; }

  // ---- boot ---------------------------------------------------------------
  fetch('/pi/dict-zh-Hant.json')
    .then(function (r) { return r.json(); })
    .then(function (d) { delete d._note; dict = d; })
    .catch(function () {})
    .finally(function () {
      // Each step is independent: a slow/absent chart must not stop the header
      // controls, and a transient Chart.js hiccup must not stop the value table.
      // Keep retrying every step until it has taken hold (or ~15s elapses).
      var steps = [retranslateAll, mountHeaderControls, mountWifi, mountWrites,
                   tuneChartAxis, mountValueTable, mountVersionChangelog, installExplicitApply];
      var run = function () {
        steps.forEach(function (fn) { try { fn(); } catch (e) { /* retry next tick */ } });
      };
      var start = function () {
        run();
        var tries = 0;
        var iv = setInterval(function () {
          run();
          var settled = installExplicitApply.done &&
                        document.getElementById('k7pi-grid') &&
                        (liveChart() ? liveChart()._k7piAxis : true);
          if (settled || ++tries > 60) clearInterval(iv);
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
