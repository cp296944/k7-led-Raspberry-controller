#!/usr/bin/env python3
"""Advisory drift check for pi-bridge's vendored+adapted copy of the pc-bridge
HTTP server.

Unlike k7tcp (byte-identical), httpapi/server.go is deliberately modified — the
diff is documented in its header. This script does NOT enforce equality; it
prints how far the two have drifted (line counts, whether upstream changed since
the recorded SHA) so a maintainer knows when to re-review after
`git merge upstream/master`.

Exit 0 always unless files are missing. Wire as a non-blocking CI step.
"""
from __future__ import annotations
import subprocess, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UP = ROOT / "pc-bridge" / "internal" / "bridge" / "server.go"
VENDORED = ROOT / "pi-bridge" / "internal" / "httpapi" / "server.go"
SHA_FILE = ROOT / "pi-bridge" / "internal" / "httpapi" / "UPSTREAM_SHA"

for p in (UP, VENDORED, SHA_FILE):
    if not p.exists():
        sys.exit(f"missing: {p}")

recorded = SHA_FILE.read_text().strip()
try:
    current = subprocess.check_output(
        ["git", "-C", str(ROOT), "rev-parse", "upstream/master"], text=True
    ).strip()
except Exception:
    current = ""

try:
    changed = subprocess.check_output(
        ["git", "-C", str(ROOT), "log", "--oneline", f"{recorded}..upstream/master",
         "--", "pc-bridge/internal/bridge/server.go"],
        text=True,
    ).strip()
except Exception:
    changed = ""

up_lines = len(UP.read_text().splitlines())
v_lines = len(VENDORED.read_text().splitlines())

print("httpapi vendored-copy drift report")
print(f"  recorded upstream SHA : {recorded}")
print(f"  current  upstream SHA : {current or '(unknown)'}")
print(f"  upstream server.go    : {up_lines} lines")
print(f"  vendored  server.go   : {v_lines} lines (+{v_lines - up_lines} from adaptation)")
if changed:
    print("  ⚠ upstream server.go changed since the recorded SHA:")
    for line in changed.splitlines():
        print(f"      {line}")
    print("  → review the diff and re-apply the header-documented changes, then bump UPSTREAM_SHA")
else:
    print("  ✓ upstream server.go unchanged since the recorded SHA")
