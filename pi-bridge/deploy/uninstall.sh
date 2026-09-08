#!/usr/bin/env bash
# Remove the K7 Pi Bridge. Leaves /opt/k7-pi-bridge/data (config, profiles,
# backups) unless --purge is given. Never touches networking or sshd.
set -euo pipefail
ROOT=/opt/k7-pi-bridge
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }

PURGE=0
[[ "${1:-}" == "--purge" ]] && PURGE=1

systemctl disable --now k7-pi-bridge.service 2>/dev/null || true
rm -f /etc/systemd/system/k7-pi-bridge.service \
      /etc/systemd/system/k7-pi-bridge-rollback.service \
      /etc/polkit-1/rules.d/49-k7-pi-bridge.rules
systemctl daemon-reload

if [[ $PURGE -eq 1 ]]; then
  rm -rf "$ROOT"
  userdel k7bridge 2>/dev/null || true
  echo "purged $ROOT and the k7bridge user"
else
  rm -rf "$ROOT/releases" "$ROOT/current" "$ROOT/current.tmp" "$ROOT/rollback.sh" "$ROOT/state"
  echo "removed binaries; kept $ROOT/data (use --purge to remove everything)"
fi
