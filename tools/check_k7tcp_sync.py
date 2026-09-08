#!/usr/bin/env python3
"""Fail if pi-bridge's vendored copy of k7tcp has drifted from the upstream
pc-bridge source.

pi-bridge/ is a sibling Go module and cannot import pc-bridge/internal/*, so
pc-bridge/internal/k7tcp/client.go is copied verbatim into
pi-bridge/internal/k7tcp/client.go under a provenance header. This check keeps
the two byte-identical (below the header) and is wired into CI.

Re-sync with:  python3 tools/check_k7tcp_sync.py --fix
"""
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UPSTREAM = ROOT / "pc-bridge" / "internal" / "k7tcp" / "client.go"
VENDORED = ROOT / "pi-bridge" / "internal" / "k7tcp" / "client.go"
SHA_FILE = ROOT / "pi-bridge" / "internal" / "k7tcp" / "UPSTREAM_SHA"

# The vendored file keeps a header block above `package k7tcp`; everything from
# that line onward must match upstream exactly.
ANCHOR = "package k7tcp"


# The one deliberate change pi-bridge applies to the vendored copy (see the
# file header). We transform the *upstream* body the same way before diffing,
# so any OTHER drift still trips the check.
KNOWN_PATCHES = [
    (
        '\taddr := fmt.Sprintf("%s:%d", c.Host, c.Port)',
        "\taddr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))",
    ),
    ('\t"net"\n\t"strings"', '\t"net"\n\t"strconv"\n\t"strings"'),
]


def body(path: Path, apply_patches: bool = False) -> str:
    text = path.read_text(encoding="utf-8")
    idx = text.find(ANCHOR)
    if idx < 0:
        sys.exit(f"{path}: no `{ANCHOR}` line found")
    out = text[idx:]
    if apply_patches:
        for old, new in KNOWN_PATCHES:
            out = out.replace(old, new)
    return out


def header(path: Path) -> str:
    text = path.read_text(encoding="utf-8")
    idx = text.find(ANCHOR)
    return text[:idx] if idx > 0 else ""


def git_sha(ref: str) -> str:
    try:
        return subprocess.check_output(
            ["git", "-C", str(ROOT), "rev-parse", ref], text=True
        ).strip()
    except Exception:
        return ""


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--fix", action="store_true", help="overwrite the vendored copy from upstream")
    args = ap.parse_args()

    if not UPSTREAM.exists():
        sys.exit(f"upstream file missing: {UPSTREAM}")

    up = body(UPSTREAM, apply_patches=True)

    if args.fix:
        hdr = header(VENDORED) or (
            "// VENDORED, DO NOT EDIT BY HAND — see tools/check_k7tcp_sync.py\n\n"
        )
        VENDORED.write_text(hdr + up, encoding="utf-8", newline="\n")
        sha = git_sha("HEAD")
        if sha:
            SHA_FILE.write_text(sha + "\n", encoding="utf-8", newline="\n")
        print(f"synced {VENDORED.relative_to(ROOT)} from upstream")
        return 0

    if not VENDORED.exists():
        sys.exit(f"vendored file missing: {VENDORED}  (run with --fix)")

    if body(VENDORED) != up:
        print("DRIFT: pi-bridge/internal/k7tcp/client.go differs from upstream", file=sys.stderr)
        print("  re-sync with:  python3 tools/check_k7tcp_sync.py --fix", file=sys.stderr)
        return 1

    print("k7tcp vendored copy is in sync with upstream")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
