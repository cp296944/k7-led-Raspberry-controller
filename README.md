# K7 Pi Wi‑Fi Controller

A **24/7 controller for the Noo‑Psyche K7 Pro / K7 Mini aquarium light that runs
on a Raspberry Pi**, is reachable from your whole home LAN, updates itself over
the air, and speaks Traditional Chinese.

<div align="center">

![License](https://img.shields.io/badge/license-MIT-blue)
![Platform](https://img.shields.io/badge/platform-Raspberry%20Pi%20(arm64)-c51a4a)
![Fork of](https://img.shields.io/badge/fork%20of-bitbarista%2Fk7--led--controller-0369a1)

</div>

> Independent community project. Not affiliated with or endorsed by Noo‑Psyche.
> Built on **[bitbarista/k7‑led‑controller](https://github.com/bitbarista/k7-led-controller)**
> (MIT) — the ESP32 firmware, the desktop PC Bridge, the Android app and the
> shared web UI all still live in this repo, unchanged.

---

## Why a Raspberry Pi

The upstream **ESP32 controller** joins the lamp's own Wi‑Fi AP as its only
network — which strands it on the lamp's private `192.168.4.x` island,
unreachable from your home LAN. The K7's **LAN mode** (lamp joins your router)
is widely reported as flaky.

A Raspberry Pi has **two network interfaces**, so it sits on both at once:

```
   K7 Pro AP                Raspberry Pi                 home router / LAN
 ┌───────────┐   wlan0    ┌────────────────┐   eth0    ┌──────────────────┐
 │192.168.4.1│◄──Wi-Fi────│ k7-pi-bridge    │◄──cable──►│ any browser · HA │
 │  :8266    │ (like a phone)│  :80  :8266   │           │  on your LAN     │
 └───────────┘             └────────────────┘           └──────────────────┘
```

- **wlan0 → the lamp's AP** — the Pi is the only client, so the lamp stays in
  the mode that actually works. (`wlan0` is hardened to never carry a default
  route: if the LAN cable is unplugged the Pi loses internet rather than
  black‑holing everything through the lamp.)
- **eth0 → your LAN** — wired, rock‑solid.
- Open **`http://<pi-hostname>/`** from any device on your network.
- If the Pi is off, the lamp keeps running the last **native schedule** it was
  given, so a Pi outage never means a dark tank.

---

## What this adds on top of upstream

| | |
|---|---|
| **Always‑on engine on the Pi** | The full lighting engine (`arduino/src/Effects.cpp` ported to Go) runs as a systemd service: 24‑slot schedule interpolation, Feed / Maintenance timed overrides, Lunar (synodic + moonrise‑tracked), Siesta, Acclimation, Seasonal Shift — all the ESP32's runtime features, none of the ESP32 needed. **Smooth Ramp toggles whether the engine drives the lamp live (~10‑min cadence) or the lamp runs the pushed schedule on its own** while the engine stays dormant. |
| **Over‑the‑air updates** | Tag a release on GitHub → the Pi verifies it (SHA‑256), swaps it in atomically, and self‑restarts, with automatic rollback if the new build won't stay healthy. A **"Check for updates"** button and an **Auto** toggle live in the top bar (Auto is **off** by default). Applying prompts for confirmation, and `POST /api/update/apply` requires `{"confirm":true,"tag":"<exact target>"}` so a bare or replayed request can't trigger an update. Click the **version chip** for the full release history. |
| **Traditional‑Chinese UI** | A conservative overlay translates the basic UI text (Save, Read, Push, Apply, …); proper nouns are left alone. **中/EN** switch in the top bar. |
| **Per‑lamp profile storage** | Saved profiles are keyed by the lamp's MAC (`data/profiles/<lamp>/`) — swapping or running two lamps never mixes them, and an OTA update never touches them. Existing profiles migrate automatically. |
| **Live spectrum value table** | An always‑open grid under the chart: type an exact % per hour per channel and the chart follows live; drag the chart and the numbers follow. Plus ±1h rotate and ±1% power nudge per channel. |
| **Explicit apply** | Nothing reaches the lamp until you press **⬆ Push** — the master slider and Shift no longer auto‑write. The Push button shows a pulsing marker when there are unsent edits. |
| **Wi‑Fi signal indicator** | Live RSSI / quality to the lamp in the top bar, colour‑coded. |
| **Raw 8266 proxy** | `:8266` on the LAN side forwards straight to the lamp, so the desktop PC Bridge or any protocol tool can drive it through the Pi. |
| **Home Assistant** *(planned)* | A REST surface + a small `custom_components/k7_lamp` integration. |

The upstream file tree (`pc-bridge/`, `shared-ui/`, `arduino/`) is **never
edited** — everything above is additive, in `pi-bridge/`, so this fork stays
mergeable with upstream.

---

## Exactly what diverges from upstream

For the upstream author and anyone evaluating this fork — the complete list of
what is different, kept current with every release.

### Upstream files modified

| File | Change |
|---|---|
| `README.md` | Rewritten for the Pi variant (this file). The only edited upstream file. |

`pc-bridge/`, `shared-ui/`, `arduino/`, `android/` and the upstream `tools/`
scripts are **byte-for-byte upstream**. `git diff upstream/master -- pc-bridge shared-ui arduino` is empty.

### Files added (additive — no merge conflicts)

| Path | What |
|---|---|
| `pi-bridge/` | The entire Pi controller: own Go module, zero third-party dependencies. |
| `.github/workflows/pi-bridge.yml` | Cross-compiles `linux/arm64`, publishes a GitHub Release with `SHA256SUMS` on each `pi-v*` tag. |
| `tools/parity_check.py` | Diffs endpoint JSON between two controllers (ESP32 vs pi-bridge). |
| `tools/check_k7tcp_sync.py` | CI guard: the lamp-protocol code vendored into `pi-bridge/` still matches `pc-bridge/internal/k7tcp/`. |
| `tools/check_httpapi_sync.py` | Advisory drift report for the vendored HTTP server. |
| `homeassistant/k7_lamp/` *(planned)* | Home Assistant custom integration. |

### Vendored upstream code (copied, not imported — Go's `internal/` rule)

| Source (upstream) | Copy (in `pi-bridge/`) | Delta |
|---|---|---|
| `pc-bridge/internal/k7tcp/client.go` | `pi-bridge/internal/k7tcp/client.go` | One line: `net.JoinHostPort` in `connect()` instead of `fmt.Sprintf("%s:%d")`, to silence a Go 1.27 vet warning and handle IPv6. `check_k7tcp_sync.py` applies the same transform to the upstream file before diffing, so real drift is still caught. |
| `pc-bridge/internal/bridge/server.go` | `pi-bridge/internal/httpapi/server.go` | +~70 lines, header-documented: `New(Options)` injects identity + all 18 capability flags (upstream hard-codes both); added getters (`StateSnapshot`, `Device`, …) for the always-on engine. |
| `arduino/src/Effects.cpp`, `Moon.cpp` | `pi-bridge/internal/engine/` | Ported C++ → Go, math-for-math, with golden-vector tests. |
| `arduino/src/Presets.h` | `pi-bridge/internal/httpapi/presets.json` | Generated by upstream's own `tools/generate_pc_bridge_presets.py`. |

### Behaviour differences the user sees

- **Smooth Ramp is the "who drives the lamp" switch.** Off (default): **⬆ Push**
  sends the whole 24-slot schedule to the lamp once (0x1007) with every effect
  folded in as a snapshot for today — acclimation, seasonal shift, tracked
  lunar, siesta, master — and the engine then goes dormant; the lamp runs the
  schedule itself. On: the engine drives the lamp live, recomputing the
  interpolated output every ~10 minutes and pushing on change. Either way a
  Feed/Maintenance timer still works, and the lamp is handed back its own
  schedule when the timer ends.
- **Read** pulls the schedule from the lamp (live `readAll`) on this platform too
  — upstream only does that for `pc-bridge`.
- **Push is explicit** — the master slider and Day-shift stage changes locally and
  only reach the lamp on **⬆ Push** (upstream auto-pushes each change).
- **Day-shift actually moves the Base schedule.** The `◀ ▶` Shift buttons
  rotate the real 24 rows on the Base chart, in place — the curve visibly moves,
  the chart stays on Base (upstream only bumps a counter the Base view never
  renders). The `+Nh` readout is a running total that resets to `+0h` after
  Push. Nothing is sent to the server as a separate shift parameter, so there is
  no double-shift.
- **Spectrum value table** sits open under the chart (not collapsed) so the
  drag chart and the exact %-per-hour grid are visible together, columns
  aligned with the header.
- **Hourly gridlines** on the schedule chart (upstream rules only every 4h,
  where its labels are) — easier to read a time off the curve.
- **"Checks" panel is live** — `/api/warnings/status` reports real conditions
  (clock not set, lamp unreachable, weak Wi-Fi to the lamp, an all-zero schedule
  that would leave the tank dark, a failed write). Upstream `pc-bridge` never
  implemented the endpoint, so the panel was always empty.
- **Today's lamp-write counter** in the top bar — `auto` (engine) vs `manual`
  (your Push / Preview), reset at local midnight, so you can see how much the
  Pi is talking to the lamp.
- All 18 capability flags are advertised `true`, so the shared UI shows every
  control (upstream `pc-bridge` hides 9).

### Platform scope

- Builds and ships **`linux/arm64` only** — Raspberry Pi 3B / 3B+ / 4 / 5 /
  Zero 2 W / CM3+ on a 64-bit OS. 32-bit models (Pi 1, Pi 2, Zero / Zero W) are
  out of scope.

---

## Install on a Raspberry Pi

Debian (Bookworm / Trixie), **arm64**, dual‑homed: `eth0` on your LAN, `wlan0`
joined to the lamp's AP (`K7_Pro…`, PSK `12345678`).

```bash
git clone https://github.com/cp296944/k7-led-Raspberry-controller
cd k7-led-Raspberry-controller
sudo pi-bridge/deploy/install.sh          # bootstraps the binary from the latest release
sudo pi-bridge/deploy/setup-network.sh    # wlan0 never-default hardening (optional, recommended)
```

Then open `http://<pi-hostname>/` from any device on your LAN. From then on the
service updates itself from this repo's GitHub Releases (via the top‑bar button;
Auto is off by default).

`pi-bridge/deploy/uninstall.sh` removes it (keeps `data/` unless `--purge`).
See **[pi-bridge/README.md](pi-bridge/README.md)** and
**[pi-bridge/docs/DESIGN.md](pi-bridge/docs/DESIGN.md)** for the internals, and
**[pi-bridge/PROGRESS.md](pi-bridge/PROGRESS.md)** for live status.

---

## Features (shared web UI)

Everything the upstream shared UI offers works here — the Pi simply advertises
every capability as available:

- Read the current schedule and mode from the lamp; edit the 24‑hour schedule
  on a drag‑and‑drop chart or the **live value table**
- Additive colour‑preview strip; **Effective Today** view; **Right Now** output
  bars backed by the engine's real computed output; schedule‑aware checks
- Built‑in preset library (Fish Only, LPS/SPS/Mixed/Soft Reef, Acclimation,
  Shallow SPS, Dino Suppression, …); named profiles saved per‑lamp
- Master brightness + per‑channel brightness‑cap sliders; per‑channel
  visibility toggles; **type‑exact** value entry; **Day‑shift** to slide the
  whole photoperiod
- Manual mode with live preview
- **Smooth Ramp** — on: the Pi drives the lamp live, recomputing the
  interpolated output every ~10 min and pushing on change. Off (default): the
  Pi pushes the full schedule once and the lamp runs it itself
- **Feed mode** — timed white boost, 1–100 % / 1–60 min
- **Maintenance mode** — timed balanced inspection light, 1–100 % / 1–180 min
- **Lunar** — royal‑blue over the 29.5‑day synodic cycle, fixed or
  moonrise‑tracked window, night clamp, schedule‑aware cutoff
- **Siesta** — midday dimming window
- **Acclimation** — start dimmer, recover over N days
- **Seasonal Shift** — move the photoperiod earlier/later across the year
- Preset export/import, community profiles, backup export/import
- K7 Mini (3 channels) and K7 Pro (6 channels)

---

## Other ways to run it (from upstream, still in this repo)

| Variant | Platform | Always‑on |
|---|---|---|
| **ESP32‑S3 controller** | phone / browser | yes — but stranded on the lamp's AP |
| **PC Bridge** | Windows, Linux | no — schedule push only |
| **Android app** | Android | no — push + Feed/Maintenance widget |

Upstream setup and flashing guides:
**[bitbarista.github.io/k7-led-controller/guide.html](https://bitbarista.github.io/k7-led-controller/guide.html)**.

---

## Notes

- The lamp accepts one TCP connection at a time and has no locking; the engine,
  the web API and the proxy share a single mutex so they never collide.
- The Noo‑Psyche lamp firmware is closed, so we can't verify how it manages
  flash write‑wear for the continuous luminance commands. Smooth Ramp writes
  more often — it's **off by default** here for that reason.
- Applying a change takes ~1 s (a full TCP round‑trip); rapid changes are
  coalesced to the latest value. This is the lamp protocol, not a bug.
- `master` branch is protected (no force‑push, no deletion).

## Credit & licence

MIT, same as upstream. Enormous credit to
**[bitbarista](https://github.com/bitbarista)** for the reverse‑engineered
protocol, the shared UI, and the whole original project — if it helps your reef,
[support them on Ko‑fi](https://ko-fi.com/bitbarista).
