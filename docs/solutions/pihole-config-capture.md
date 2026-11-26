# pihole config capture

## Capturing Pi-hole v6 config into a public repo without leaking secrets

### Context
Homelab `hosts/pihole/capture.sh` (Homelab #84, #85) reads Pi-hole CT 107 over one ssh to the Proxmox host (password, run from a real terminal, not a `!` shell) and writes `pihole.toml`, `adlists.sql`, `domainlist.sql`, `groups.txt`, `clients.txt`, `version.txt`, `lxc-107.conf`.

### Secrets to strip
- `pihole.toml`: `webserver.api.pwhash`, `app_pwhash`, `totp_secret`. Redact on the Proxmox host with sed so they never reach the laptop.
- **`pct config <ct>` `description:` (Proxmox Notes)**: the Pi-hole LXC installer (community helper script) writes "Your Admin Webpage login password is XXXXXXXX" into the container Notes in plain text. A hash-pattern guard does NOT catch it. Drop the `description:` line, and guard for plaintext `password is X` / `password: X` as well as `$BALLOON`/argon/bcrypt hashes, 64-hex strings and `-----BEGIN`.
- Teleporter backups carry the password hash: never commit them.

### Gotchas
- Lists from gravity.db via `pihole-FTL sqlite3 -batch /etc/pihole/gravity.db "<query>"` (embedded sqlite, no separate sqlite3 needed). Emit `INSERT OR IGNORE` statements so restore is a replay; header comments in .sql files must be `--`, not `#`.
- When testing a script with stub `ssh`/`pct` on Windows Git Bash, put the stub dir on PATH as `/c/...`, not `C:/...`: the colon splits PATH and the real ssh runs.

### Follow-up
If the installer password in the Notes is still the live admin password, change it (`pihole setpassword`) and clear the Notes in the Proxmox UI.
