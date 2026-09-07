# pi-bridge deployment

Target: Raspberry Pi, Debian (Bookworm/Trixie), **arm64**, dual-homed —
`eth0` on the home LAN, `wlan0` joined to the K7 Pro's AP (`K7_Pro42113`).

## Files

| file | role |
|---|---|
| `install.sh` | idempotent installer/updater — user, dirs, binary, systemd, polkit |
| `uninstall.sh` | remove (keeps `data/` unless `--purge`) |
| `k7-pi-bridge.service` | the daemon unit — runs `/opt/k7-pi-bridge/current/k7-pi-bridge` as `k7bridge`, `CAP_NET_BIND_SERVICE` for `:80`, `OnFailure=` rollback |
| `k7-pi-bridge-rollback.service` + `rollback.sh` | repoint `current` at the previous release if a new one crash-loops |
| `49-k7-pi-bridge.rules` | polkit — `k7bridge` may `systemctl restart` **only** its own units (needed after an OTA binary swap) |

## On-disk layout

```
/opt/k7-pi-bridge/
├── releases/pi-v0.1.0/k7-pi-bridge
├── releases/pi-v0.2.0/k7-pi-bridge
├── current            -> releases/pi-v0.2.0     (symlink; ExecStart target)
├── rollback.sh
├── state/{PREVIOUS,CONFIRMED,ROLLED_BACK}
└── data/{config.json, …}                        (never touched by updates)
```

## First install (bootstrap — no GitHub release exists yet)

Cross-build on a dev box and copy it over:

```bash
cd pi-bridge
GOOS=linux GOARCH=arm64 go build -o /tmp/k7-pi-bridge ./cmd/k7-pi-bridge
scp /tmp/k7-pi-bridge  k7pi@<pi>:/tmp/
scp -r deploy          k7pi@<pi>:/tmp/pi-deploy
ssh k7pi@<pi> 'sudo /tmp/pi-deploy/install.sh --binary /tmp/k7-pi-bridge --tag pi-v0.1.0'
```

## Normal updates

Once a `pi-v*` GitHub release exists the daemon self-updates hourly (and via
`POST /api/update/apply`). To pull one manually:

```bash
ssh k7pi@<pi> 'sudo /opt/k7-pi-bridge/data/../. ; sudo bash -c "cd /tmp && curl -fsSLO https://raw.githubusercontent.com/cp296944/k7-led-Raspberry-controller/master/pi-bridge/deploy/install.sh && bash install.sh"'
```

(or re-run `install.sh` from a checkout.)

## Networking — what this does NOT do (by design, v0.1)

It does **not** modify routing. On this Pi `eth0`'s default route already wins
by metric (100 vs wlan0's 600), so LAN/internet traffic goes out `eth0` and only
`192.168.4.0/24` goes out `wlan0`. If `eth0` ever goes down the bridge is
unreachable from the LAN anyway, so the wlan0 fallback route is harmless.

`sshd` and `eth0` are never touched — you cannot be locked out by this installer.

A later phase may add `wlan0` `never-default` hardening + an nftables ruleset if
it proves necessary.
