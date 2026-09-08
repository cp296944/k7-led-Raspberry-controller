#!/usr/bin/env bash
# Harden the Pi's dual-homed networking for the K7 bridge.
#
#   wlan0  -> the lamp's AP: route ONLY 192.168.4.0/24 out of it, and NEVER let
#             it become the default route. If eth0 goes down the Pi then simply
#             has no internet (correct) instead of black-holing everything
#             through the lamp's AP (which has none).
#   eth0   -> left completely alone. This script never touches eth0 or sshd, so
#             it cannot lock you out.
#
# Debian 13 (trixie) Pi OS uses netplan -> NetworkManager. We drop in a small
# netplan override for wlan0 and re-generate; the wifi credentials configured by
# Raspberry Pi Imager stay where they are.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }

WLAN="${1:-wlan0}"
DROPIN=/etc/netplan/90-k7-wlan0-noroute.yaml

echo "==> writing $DROPIN"
cat > "$DROPIN" <<YAML
# Managed by k7-pi-bridge deploy/setup-network.sh — wlan0 carries the lamp
# subnet only and must never be the default route.
network:
  version: 2
  wifis:
    ${WLAN}:
      dhcp4: true
      dhcp4-overrides:
        use-routes: true
        route-metric: 4000
      routes:
        - to: default
          scope: link
          via: 0.0.0.0
          metric: 4000
YAML
chmod 600 "$DROPIN"

# NetworkManager honours ipv4.never-default directly and it survives a
# `netplan apply` better than the yaml route hack above on some versions:
CON="$(nmcli -t -f NAME,DEVICE con show --active | awk -F: -v d="$WLAN" '$2==d{print $1; exit}')"
if [[ -n "${CON:-}" ]]; then
  echo "==> nmcli: $CON ipv4.never-default yes / route-metric 4000"
  nmcli con mod "$CON" ipv4.never-default yes ipv4.route-metric 4000 || true
fi

echo "==> netplan generate"
netplan generate
echo "==> netplan apply"
netplan apply || true
sleep 2

echo
echo "==> result:"
ip route | sed 's/^/    /'
echo
if ip route | grep -qE "^default .*dev ${WLAN}"; then
  echo "    ⚠ ${WLAN} still has a default route — check 'nmcli con show \"$CON\"'"
else
  echo "    ✓ no default route via ${WLAN}"
fi
