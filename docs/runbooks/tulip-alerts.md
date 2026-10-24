# tulip alerts

## Tulip alerts — TulipNoCycle, TulipMarketDataUnavailable, TulipKillSwitchArmed

Defined in Homelab `services/tulip/observability.yaml` and built on Tulip's `/metrics` (journal event counts plus the `tulip_kill_switch` and `tulip_suggestions_pending` gauges). All three are warnings and go to ntfy.

**Agent rule (Homelab CLAUDE.md):** while triaging, never read `tulip-approval`, never call `/approve` or `/killswitch`, and never approve or disarm for the operator. The MCP tools (`tulip_status`, `journal_list`, `cycle_run`, …) are the agent's whole surface.

### TulipNoCycle
- **Means:** no `cycle.finished` in 26h on a trading day. Cycles fire at 10:05 and 15:00 Toronto. The check runs Tue–Sat after 21:00 UTC, so weekends can't trigger it. A TSX holiday can; it's rare, so ignore it on those days.
- **Triage:**
  - `kubectl -n tulip get pods`, then `kubectl -n tulip logs deploy/tulip --tail=50`.
  - `calendar.unknown` in the journal means the holiday table doesn't cover the year (`internal/calendar`), and Tulip fails closed. Extend the table.
  - Run one cycle by hand with the MCP tool `cycle_run`.

### TulipMarketDataUnavailable
- **Means:** more than 4 `data.unavailable` events in a day. Yahoo's unofficial chart endpoint is rate-limiting (429), returning nothing, or has changed shape. Tulip fails closed and proposes nothing.
- **Triage:** run `journal_list kind=data.unavailable` and read the `error` field. Then compare with a live check: `curl -A Mozilla/5.0 "https://query1.finance.yahoo.com/v8/finance/chart/XIU.TO?range=5d&interval=1d"`.

### TulipKillSwitchArmed
- **Means:** `tulip_kill_switch == 1`, so nothing will execute. It was armed by Claude Code (`kill_switch_arm`) or the operator; `tulip_status` shows `kill_reason`.
- **Resolution:** the operator alone disarms it, with their passphrase, at `http://192.168.0.108:30995/killswitch`.
