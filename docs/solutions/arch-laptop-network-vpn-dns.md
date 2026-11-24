# arch laptop network vpn dns

## Arch laptop (ThinkPad X390): VPN/DNS architecture and the full-tunnel slowdown

## Arch laptop — ThinkPad X390 (20NYS3Y600), 7.4 GB RAM, hostname archlinux

### Network layout (as of 2026-11-12)
- **Home WiFi** `TP-Link_29DE` / `TP-Link_29DE_5G`: DNS pinned to Pi-hole `192.168.0.53` (`ipv4.ignore-auto-dns yes`). Router DNS was 192.168.0.1 before.
- **VPN profile `Arch`** (WireGuard, NM-managed): endpoint `137.175.216.219:51820`, laptop `10.0.0.4/32`, peer allowed-ips=0.0.0.0/0 (**full tunnel**). DNS set to 1.1.1.1 only (was 192.168.0.53 + 1.1.1.1 → caused DNS blackhole: resolved bound .53 queries to the wg interface, unreachable through the tunnel exit).
- **`connection.autoconnect = no` on Arch** — set 2026-11-16 after the tunnel auto-connected Nov 14 16:40 and stayed up ~30 h, routing ALL traffic through the slow VPS → "laptop so much slower". Turn VPN on manually only.
- McGill WiFi (`wpa.mcgill.ca`, `eduroam`): DHCP DNS (132.206.44.21/132.216.44.21), no Pi-hole — by design.

### Known broken / open threads
- **WG handshake to 137.175.216.219 failed 2026-11-12 at home** (tunnel up, no traffic; endpoint pings ~5-16 ms). VPS SSH port 22 unreachable (filtered) — server-side peer keys/port/firewall need checking via provider panel. User WANTS tunnel→Pi-hole DNS for all domains (incl. .home) once tunnel works; requires VPS to route 192.168.0.0/24 back home.
- **k3s worker 192.168.0.108 SSH host key changed** (now SHA256:Phaoz3I5LtU7KBdSH5n+ocYxwD7M8aXUREXbUwtB9w8), MAC bc:24:11:8f:cb:ee (still a Proxmox VM). User says they did NOT recreate it. Old key in known_hosts line 20. Identity unconfirmed — root password not provided yet.
- **DNS *.home doesn't resolve at McGill** — only via Pi-hole at home or through tunnel (once tunnel works).
- Proxmox root is password-only auth (no key); sshpass not installed; sudo needs password too.
