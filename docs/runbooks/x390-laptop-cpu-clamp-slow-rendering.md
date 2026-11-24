# x390 laptop cpu clamp slow rendering

## X390 laptop 'everything slow / RAM capped': battery CPU clamp + Firefox memory

## Laptop feels capped: CPU clamped ~1 GHz on battery + Firefox memory pressure

### Symptom
Firefox rendering stutters, "like RAM is capped". Browsing slow since ~2026-11-14.

### Root causes (diagnosed 2026-11-16)
1. **CPU frequency clamped to ~1 GHz while on battery** (verified: 4 busy-threads → max core freq 1000 MHz of 4100 MHz possible). Trigger: on AC=0 (battery 63%, discharging), TLP active with default `PLATFORM_PROFILE_ON_BAT` → ACPI platform_profile = `low-power`. Battery health 84% (47.9/57 Wh) may contribute (ThinkPad EC can clamp on weak cells). NOT RAPL (25 W long term = normal), NOT thermald (inactive), no kernel throttle events, temps 38-41 °C.
2. **Firefox memory pressure**: 7.4 GB total RAM; Firefox up 8+ days (since Nov 7) with ~10 content processes ≈ 6 GB, one runaway tab process (PID 334323, ~1 GB, 45-107% CPU, NO network sockets = compute-bound JS/rendering). Committed_AS 12.7 GiB > CommitLimit 11.4 GiB → 939 MB in zram swap → tab-switch/rendering latency.
3. Historical: WG full-tunnel auto-connected Nov 14-15 (see solutions/arch-laptop-network-vpn-dns.md) — already fixed via autoconnect=no.

### Fixes
- Plug in AC → clamp lifts (verify: `grep MHz /proc/cpuinfo` under load should show >2 GHz).
- Restart Firefox (or `about:processes` → find high-CPU tab by PID, close it). Frees ~4-6 GB.
- Permanent battery fix (needs sudo): `/etc/tlp.conf` → `PLATFORM_PROFILE_ON_BAT=balanced` (optionally `CPU_ENERGY_PERF_POLICY_ON_BAT=balance_performance`), then `sudo systemctl restart tlp`.
- If clamp persists on AC: suspect battery current delivery (PROCHOT) — watch `journalctl -k`, consider battery replacement.

### Identification commands
```bash
grep MHz /proc/cpuinfo | awk '{print $4}' | sort -n   # freq floor/ceiling
cat /sys/firmware/acpi/platform_profile               # low-power = suspect
for i in 1 2 3 4; do (yes>/dev/null &); done; sleep 1.5; grep MHz /proc/cpuinfo | sort -rn | head -4; pkill yes
ps aux --sort=-%mem | head -12                        # firefox content procs
ss -tnp | grep firefox                                # per-pid sockets
```

### Status (2026-11-24)
- Re-diagnosed: same clamp (~800 MHz on battery; `platform_profile=low-power`, EPP=`power` from `/etc/tlp.conf`).
- **User `blacklotus` is NOT in sudoers** (in `wheel`, but wheel isn't enabled in sudoers) → `sudo` fails; `!`-prefixed commands in Claude Code have no TTY either. Use `run0 <cmd>` (polkit, own password) or `su -c` in a real terminal.
- Fix script (TLP battery → `balanced` / `balance_power`, plus `Restart=on-failure` drop-ins for `reflector` and `archlinux-keyring-wkd-sync`, which fail at boot on DNS-not-ready): apply with `run0 bash perf-fix.sh`. Verify: `cat /sys/firmware/acpi/platform_profile` on battery → `balanced`.
