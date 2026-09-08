# K7 Pi Bridge — Build Progress (living handoff)

**If you are a resumed/scheduled session: read this whole file, then
[PLAN.md](PLAN.md) and [DESIGN.md](DESIGN.md). Execute the NEXT unchecked
step(s) only. Commit + push after each. Update this file. If blocked, write the
blocker under "BLOCKED" and stop.**

## Hard rules for any session working this

- Work only on branch `dev/pi-bridge` in `D:\HomeAssistant\K7\k7-led-controller`.
  Never commit to `master`. Never `git push --force`.
- Git identity is repo-local: `cp296944` / `cp296944@gmail.com` (already set).
- `gh` CLI is authed as `cp296944` — pushing works.
- Go: `C:\Program Files\Go\bin\go.exe` (1.27.0). arm64 cross-compile verified.
- The Pi: `ssh -i %USERPROFILE%\.ssh\id_ed25519_k7pi k7pi@192.168.0.149`
  (DHCP-reserved). sudo is NOPASSWD. It is dual-homed: eth0 `192.168.0.149`
  (LAN), wlan0 `192.168.4.2` (K7 AP). **The K7 lamp answers at
  `192.168.4.1:8266` and this was verified end-to-end.** Wi-Fi signal is weak
  right now (Pi far from tank) — user will move it later; do NOT tune timeouts
  around the current bad signal, just make them generous + retrying.
- NEVER touch eth0 / sshd config on the Pi (lock-out risk). wlan0 changes only.
- Do not reflash the user's ESP32 — it is the parity oracle.
- Keep upstream files untouched (`pc-bridge/`, `shared-ui/`, `arduino/`,
  `tools/` except NEW files) so `git merge upstream/master` stays clean.
- The shared UI is capability-driven: "1:1" = all 18 capability flags `true`
  and their endpoints implemented. See PLAN.md §0 for the ledger.

## Key facts already established

- Protocol: TCP `192.168.4.1:8266`, frame `AA A5 <cmd hi> <cmd lo> <data> BB`,
  6 channels 0-255, 24 slots. Reference impl:
  `pc-bridge/internal/k7tcp/client.go` (vendored into `pi-bridge/`).
- `pi-bridge/` cannot import `pc-bridge/internal/*` (Go internal rule, separate
  module). Decision: **vendor** `k7tcp/client.go` into
  `pi-bridge/internal/k7tcp/` with a provenance header + a CI sync check.
- The 21 "free" endpoints in `pc-bridge/internal/bridge/server.go` are
  reimplemented in `pi-bridge` (we need all-caps-true + real scheduler + a
  different store anyway).
- Live K7 Pro readAll decoded: 6ch = 50/50/50/50/50/50, 24 slots, name
  `K7_Pro42113`, device `k7pro`.

---

## Milestone checklist

### Phase 0 — Foundation
- [x] Fork cloned to `D:\HomeAssistant\K7\k7-led-controller`, branch `dev/pi-bridge` pushed, `upstream` remote added
- [x] Actions workflow token perms → write (for OTA releases)
- [x] Go 1.27 verified, arm64 cross-compile verified
- [x] `pi-bridge/` Go module scaffold (go.mod zero-dep, cmd/k7-pi-bridge/main.go serves /healthz + /api/version, internal/{config,version} done, other internal/ dirs stubbed)
- [x] Vendor `k7tcp/client.go` → `pi-bridge/internal/k7tcp/` + provenance header + UPSTREAM_SHA
- [x] `pi-bridge/docs/API.md` — endpoint/capability ledger (compact; per-endpoint shapes filled in as Phase 2/3 implements them)
- [x] `tools/parity_check.py` — diff endpoint JSON between two base URLs
- [x] `tools/check_k7tcp_sync.py` — CI guard that vendored k7tcp matches upstream (passing)
- note: `go vet ./...` trips on the vendored upstream file (IPv6 `%s:%d` nit); CI vets our packages only

### Phase 1 — Pi base + OTA  ✅ DONE (tags `pi-v0.1.0`, `pi-v0.2.0`)
- [x] `internal/config` — JSON file < env < flags, zero-dep (config.toml → config.json)
- [x] minimal daemon: `/api/version`, `/healthz`, `/api/capabilities`, `/api/update/{status,apply}`, slog
- [x] `internal/updater` — GitHub releases API, semver pick, SHA256 verify, atomic symlink swap, restart, ConfirmAfterStart; unit tests
- [x] `.github/workflows/pi-bridge.yml` — check job (sync/vet/test/build) + release job on `pi-v*` (arm64, ldflags stamp, SHA256SUMS+version.json, `gh release create`)
- [x] `pi-bridge/deploy/` — install.sh (idempotent, --binary bootstrap), uninstall.sh, k7-pi-bridge.service (k7bridge user, CAP_NET_BIND_SERVICE, OnFailure), k7-pi-bridge-rollback.service + rollback.sh (loop-guarded), polkit rule
- [x] deployed to Pi (`192.168.0.149:80`), reachable from Windows LAN
- [x] **exit gate PASSED on real hardware:** tagged pi-v0.1.0→pi-v0.2.0, `POST /api/update/apply` → Pi downloaded+verified+swapped+restarted → `/api/version` = pi-v0.2.0. Rollback drill: broken release → crash-loop → systemd OnFailure → rollback.sh reverted to pi-v0.2.0, service healthy.
- NOTE: routing NOT modified (eth0 wins by metric 100<600); wlan0 never-default deferred. Deploy never touches eth0/sshd.
- NOTE: install.sh cosmetic bug — "LAN UI" line prints `http:/…/24` (harmless)

**Pi is currently running pi-v0.2.0 as a systemd service.**

### Phase 2 — Lamp link + read path  ✅ DONE (tag `pi-v0.3.0`)
- [x] `internal/httpapi` — vendored+adapted `pc-bridge/internal/bridge/server.go` (+66 lines, header-documented). Embeds shared-ui, serves `/` + all 21 upstream endpoints. `New(Options)` injects identity + the 18 caps; `DefaultCapabilities()` = 9 true / 9 false; `SetCapability()` for Phase 3.
- [x] `internal/proxy` — raw `:8266` passthrough (one client at a time, idle timeout, optional scheduler gate)
- [x] presets served from vendored `presets.json` (generated from `Presets.h`)
- [x] `tools/check_httpapi_sync.py` — advisory drift report
- [~] `internal/lamp` (mutexed conn) + `internal/store` — DEFERRED to Phase 3: httpapi opens a fresh k7tcp conn per call (same as pc-bridge). Phase 3 introduces `internal/lamp` as the single owner shared by scheduler+httpapi+proxy.
- [x] **exit gate PASSED:** OTA'd Pi to pi-v0.3.0. `/api/lamp/read` via the real Pi service returns the real K7 Pro (name `K7_Pro42113`, its actual 24-slot reef schedule). `/api/preview` + `/api/hand` reach the lamp (tested vs mock). Proxy `:8266` listening. UI serves (`/static/index.html`). Caps 9 true / 9 false.
- NOTE: browser render check of the UI still pending (interrupted); endpoints all verified.

**Pi is currently running pi-v0.3.0.** Releases: pi-v0.1.0, pi-v0.2.0, pi-v0.3.0.
KNOWN NIT: `go vet` can't run on cmd/httpapi (they import vendored k7tcp which has
an upstream `%s:%d` IPv6 printf nit) — CI vets config/version/updater/proxy only.
Candidate upstream PR: `net.JoinHostPort` in k7tcp connect().

### Phase 2.5 — pi-bridge UX layer  ✅ DONE (tag `pi-v0.4.0`)  [user requests]
- [x] `internal/piweb` — one middleware over httpapi: serves `/pi/*`, injects
  `<script src="/pi/overlay.js">` into HTML (upstream files untouched),
  intercepts `/api/profiles*`, rewrites `/api/push` for `schedule_shift_minutes`
- [x] `assets/overlay.js` + `dict-zh-Hant.json` (105 terms) — zh-Hant translation
  of basic UI text (exact-match only, proper nouns left alone; 中/EN toggle) +
  an Updates widget (`/api/update/status` + `/api/update/apply`) + version footer
- [x] `internal/profiles` — per-lamp profile store `data/profiles/<lampID>/*.json`
  (lampID = lamp MAC via `ip neigh`, else lamp name, else default); one-time
  migration from the legacy store.json map; survives OTA. tests.
- [x] FIX #4: `/api/push` now honours `schedule_shift_minutes` (rotates the 24
  rows) — upstream pc-bridge silently ignored it
- [x] getters added to vendored httpapi: `LampName()`, `LegacyProfiles()`
- [x] tests: piweb shift-rotation + HTML injection; profiles per-lamp isolation + migrate
- answers given to user: #4 Shift = photoperiod time-shift (was upstream no-op);
  #5 Base/Effective Today/Play-day chart modes (Effective needs Phase 3 overlays)

### Phase 3 — Always-on engine  (tags `pi-v0.5`..`0.9`)  ← the big port of Effects.cpp + Moon.cpp
- [x] **v0.5.0** `persistent_controller_clock` + `logs` + engine tick + `/api/time` + `/api/output/status` + `/api/wifi/signal` + `/api/logs`  → **11/18**
  - `internal/engine/model.go` — PURE port of Effects.cpp math: interpolate,
    EffectiveSchedule (seasonal+UI shift resample + acclimation scale),
    applySiesta/applyMaster/applyLunar, LunarWindow + clampWindowToNight,
    Compute() = restoreScheduledOutputNow. `moon.go` = Moon.cpp. 8 golden tests.
  - `internal/engine/engine.go` — tick loop (60s + Kick on push/master), diffs
    vs lastSent, `Hand()` on change, tracks OutputStatus. Override hook (feed/maint).
  - `internal/lamp` — single mutexed connection owner, generous timeouts, Health()
  - `internal/ringlog` — bounded log + slog.Handler wrapper
  - `internal/piapi` — middleware: the 4 new endpoints + engine.Provider (reads
    httpapi StateSnapshot). Kicks engine on /api/push, /api/master.
  - vendored httpapi getters added: StateSnapshot(), Device()
  - verified vs mock: push schedule → engine kicked → output `[50,30,...]` sent within 2s
- [ ] v0.6.0 `smooth_ramp` (`/api/ramp/start|stop|status|tick`) — default OFF, engine interval → 2min when active, push-on-change
- [ ] v0.7.0 `feed_mode` + `maintenance_mode` (`/api/feed/*`, `/api/maintenance/*`) — timed engine.Override; buildMaintenanceChannels (MINI/PRO tables in Effects.cpp:79)
- [ ] v0.8.0 `tracked_lunar` — flip flag; LunarWindow already ports trackMoonrise. Add `/api/lunar/*` to piapi? (fixed lunar is in vendored httpapi; tracked just needs the cap on + engine already does moon math)
- [ ] v0.9.0 `acclimation` (`/api/acclimation/config|status`) + `seasonal_daylength` (`/api/seasonal/config|status`) — piapi gets its own small JSON store for these 2 configs; engine.Config already has the fields + math
- [ ] golden-vector tests vs ESP32 `/api/output/status` (needs the ESP32 powered + on the lamp AP — user's bench unit)
- [ ] **exit gate:** 17/18 caps, UI shows every control, all work

NOTE for v0.6-0.9: piapi already has the Provider + engine wiring. Each tag =
add endpoint handlers to piapi + flip the cap in main.go + (for accl/seasonal)
a tiny config store. engine.Config fields + math are ALL already there.
NOTE: `internal/lamp` gate is used by engine + proxy; vendored httpapi still
opens its own per-call conns (brief, low collision risk). Unify if it bites.

### Phase 4 — Setup page + hardening  (tag `pi-v1.0`)
- [ ] `setup_portal` equivalent (lamp SSID/IP settings page, wifi status, factory reset)
- [ ] `/api/warnings/status`, diagnostics page
- [ ] 7-day soak on the Pi
- [ ] `pi-bridge/README.md` + top-level `README.md` rewrite (user asked: explain the project for others)
- [ ] optional: PR `pi-bridge/` back to bitbarista
- [ ] **exit gate: 18/18. `pi-v1.0`.**

### Phase 5 — Home Assistant (tag `pi-v1.1`)
- [ ] `/api/ha/*` REST surface
- [ ] `homeassistant/k7_lamp/` custom integration (light, 6×number, switch, select, buttons, binary_sensor, sensor)
- [ ] install into `D:\HomeAssistant\custom_components\k7_lamp\`

### User-requested features (queue — slot into a tag when reached)
- [x] UX-1 (pi-v0.5.1): move 檢查更新 + language INTO the `.topbar` (after versionChip);
  language is a `<select>` dropdown (LANGS array, easy to add locales). Removes
  the bottom-right floating bar.
- [x] UX-2 (pi-v0.5.1): Shift discoverability — overlay overrides `changeShift`
  to jump the chart to "Effective Today" + toast "按 Push 生效". (Shift math
  already works: `/api/push` with `schedule_shift_minutes` → piweb rotates the
  24 rows. Verified on Pi: +6h moved an 08-16 band to 14-22.) Root cause of
  "光譜不會移動": upstream Base chart mode never renders the shift, and
  explicit-apply (pi-v0.4.1, user-requested) means it needs a Push.
- [ ] **FEAT-A: manual hourly value table** — a "逐時數值表" like the user's own
  `D:\HomeAssistant\K7\K7_QR_Generator\k7profilegeneratoroffline.html`: an
  editable 24-row × 6-channel number grid (type exact %), plus that tool's
  "整體位移工具" (rotate curve ±1h per checked channel, power ±1%). The shared
  UI only has drag-to-edit. Approach: overlay.js injects a collapsible panel
  under the chart that reads/writes the page's schedule via a small bridge
  (needs a hook — `scheduleBase` is a `let`, so add a getter/setter through
  `window` or drive it via `updateChart` + a synthetic drag, OR simplest:
  the panel POSTs its own grid straight to `/api/push`). Target: pi-v0.6.x or
  a dedicated pi-v0.10.
- [ ] **FEAT-B: move working dir** `D:\HomeAssistant\K7\k7-led-controller` →
  `D:\HomeAssistant\K7\K7_Pi_Wifi_Controller`. Must also update: this file's
  "Hard rules" path, the scheduled task prompt
  (`k7-pi-bridge-build-resume/SKILL.md`), `_archive` copy path, any absolute
  paths in scripts. Do at a clean checkpoint (all committed), not mid-feature.

---

## Current state (2026-09-08)
- **PR #1 MERGED** (Phases 0–2.5). Phase 3 work → PR #2 on `dev/pi-bridge`
  (also carries the pi-v0.4.1 UX fix until merged).
- **Pi is running `pi-v0.5.0`** — always-on engine LIVE (tick loop driving the real K7 Pro).
- REAL wlan0 signal measured: RSSI -73 dBm / 54% / 24 Mbps (Pi far from tank; engine copes, last_write_ok true). User will move the Pi closer later.
- Releases: pi-v0.1.0 … pi-v0.5.0. Capabilities: **11 / 18**.
- master branch protected: no force-push, no deletion (no review requirement).
- pi-v0.4.1: explicit-apply — overlay.js neuters upstream's auto-push; master/
  shift/mode edits stay local until the user presses ⬆ Push (dirty indicator).
- Real lamp verified: MAC `4a:55:19:ec:b0:49`, profiles migrated to
  `data/profiles/mac-4a_55_19_ec_b0_49/` (user's `BRS_AB`, `K7_Pro42113`).
- User confirmed the UI renders + works in a browser.

## BLOCKED
_(none)_

## Session log
- 2026-09-07/08 — Phase 0 done (scaffold, vendored k7tcp, config, version,
  API.md, parity tooling).
- 2026-09-08 — **Phase 1 done.** updater + CI + deploy scripts. Deployed to Pi.
  OTA self-update AND rollback both verified on real hardware end-to-end.
  Pi running pi-v0.2.0. Next: **Phase 2** — vendor+adapt the pc-bridge HTTP
  server into `internal/httpapi`, embed shared-ui, `internal/lamp` (mutexed
  conn), `internal/store`, `internal/proxy` (raw :8266), wire the 21 free
  endpoints, flip 9 capability flags true. Exit: UI loads on LAN, Read/Preview/
  manual/push work vs real lamp, parity_check green on those 21.
