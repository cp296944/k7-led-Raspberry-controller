#!/usr/bin/env python3
"""Compare the HTTP API of two K7 controllers endpoint-by-endpoint.

Point it at the reference (the ESP32 firmware, or the mock) and at pi-bridge;
it GETs every read endpoint on both, normalises volatile fields, and diffs the
JSON. This is the concrete "1:1" burndown/verification tool referenced in
PLAN.md.

    python3 tools/parity_check.py --ref http://192.168.4.1 --new http://k7-pi-wifi-controller.local
    python3 tools/parity_check.py --ref http://127.0.0.1:8788 --new http://127.0.0.1:8787   # both mock-backed

Exit code is the number of endpoints that differ (0 = full parity on the
checked set).
"""
from __future__ import annotations

import argparse
import json
import sys
import urllib.request

# GET endpoints that should return equivalent JSON on any K7 controller.
# capability-gated ones are grouped so a partial implementation reads clearly.
READ_ENDPOINTS = [
    "/api/version",
    "/api/capabilities",
    "/api/config",
    "/api/devices",
    "/api/state",
    "/api/presets",
    "/api/profiles",
    "/api/master",
    "/api/siesta/status",
    "/api/lunar/status",
    "/api/lunar/schedule",
    # capability-gated (ESP32-only until pi-bridge Phase 3):
    "/api/ramp/status",
    "/api/feed/status",
    "/api/maintenance/status",
    "/api/acclimation/status",
    "/api/acclimation/config",
    "/api/seasonal/status",
    "/api/seasonal/config",
    "/api/output/status",
    "/api/time",
    "/api/wifi/signal",
    "/api/warnings/status",
]

# Fields that legitimately differ between hosts/runs — blanked before diffing.
VOLATILE_KEYS = {
    "uptime", "uptime_s", "millis", "now", "timestamp", "time", "date",
    "last_read_at", "last_pushed_at", "last_read", "free_heap", "rssi",
    "signal", "ip", "commit", "build", "wifi", "firmware", "version",
}


def fetch(base: str, path: str) -> tuple[int, object]:
    try:
        with urllib.request.urlopen(base.rstrip("/") + path, timeout=8) as r:
            raw = r.read().decode("utf-8", "replace")
            try:
                return r.status, json.loads(raw)
            except json.JSONDecodeError:
                return r.status, raw
    except urllib.error.HTTPError as e:
        return e.code, None
    except Exception as e:  # noqa: BLE001
        return 0, f"<{type(e).__name__}: {e}>"


def scrub(v):
    if isinstance(v, dict):
        return {k: ("<volatile>" if k in VOLATILE_KEYS else scrub(x)) for k, x in sorted(v.items())}
    if isinstance(v, list):
        return [scrub(x) for x in v]
    return v


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--ref", required=True, help="reference base URL (ESP32 or mock)")
    ap.add_argument("--new", required=True, help="pi-bridge base URL")
    ap.add_argument("--verbose", "-v", action="store_true")
    args = ap.parse_args()

    diffs = 0
    for path in READ_ENDPOINTS:
        rs, rj = fetch(args.ref, path)
        ns, nj = fetch(args.new, path)
        rn, nn = scrub(rj), scrub(nj)
        same = rn == nn and (rs // 100) == (ns // 100)
        mark = "ok  " if same else "DIFF"
        if not same:
            diffs += 1
        print(f"{mark} {path:32s} ref={rs} new={ns}")
        if not same and args.verbose:
            print("      ref:", json.dumps(rn, ensure_ascii=False)[:400])
            print("      new:", json.dumps(nn, ensure_ascii=False)[:400])

    print(f"\n{diffs} endpoint(s) differ out of {len(READ_ENDPOINTS)}")
    return diffs


if __name__ == "__main__":
    raise SystemExit(main())
