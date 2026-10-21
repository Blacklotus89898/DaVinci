# dead man switch paging hygiene

## Dead-man switch: false "cluster dark" verdicts and rotting page issues

Postmortem of the Homelab heartbeat/watchdog system (2026-10-08 → 2026-10-20). A health sweep concluded "beats dead 4 days AND watchdog not paging" — both conclusions were wrong or incomplete, and the real defects were elsewhere.

### Symptom (what the sweep saw)
- No heartbeat commits on the GitHub API `/commits` listing → "beats stopped 4 days ago".
- Heartbeat CronJob jobs `Complete 1/1` in ~6s → "silent push failure".
- Watchdog workflow runs all `success` → "watchdog not paging".

### Root causes (three separate traps)
1. **Wrong-branch freshness check.** Beats push to an **orphan `heartbeat` branch**, never `main`. `/repos/X/Y/commits` defaults to the default branch, so a beat check there reads "dead" forever. Always pass `?sha=heartbeat`. The branch was actually fresh at the exact :07/:37 cadence.
2. **Green watchdog runs ≠ healthy.** The watchdog's dedupe design (comment on the existing open page instead of stacking new issues) exits 0 — so `success` runs continued all through a real 7-day outage while paging hourly into issue comments nobody read. The paging state lives in the **open "Cluster dark" issue**, not in run conclusions. Check `issues?state=open` for pages, and issue comments for the "Still dark: age Nm" trail.
3. **No recovery-close.** When the beat came back (PAT re-sealed), the page issue stayed open 12 more days. A stale page makes the next real outage read as old noise.

### The actual incident
Launched 2026-10-08T01:47Z, one beat landed, then the sealed PAT could not push → 7 days dark. Watchdog paged correctly within hours of its own GH_REPO fix landing. `7b3f9bd fix: re-seal arc-runner PAT with corrected classic token` (10-15) restored beats.

### Exact fixes
- Watchdog workflow gained a `Close page issue on recovery` step: when the beat is fresh and a `Cluster dark` issue is open, `gh issue close <n> --comment "Beat recovered: ..."`. Verified live via `workflow_dispatch` (run closed issue #7 itself; step success, open-page step skipped).
- Freshness checks: `curl api.github.com/repos/Blacklotus89898/Homelab/commits?sha=heartbeat&per_page=3`.

### Prevention
- E2E-verifying a recurring system on its **first** occurrence proves nothing — the heartbeat was "verified" on beat #1 and broke until beat #2. Verify the *cadence* (second occurrence).
- Watchdogs that page via issues must close those issues on recovery (symmetric open/close), or pages decay into noise.
- In-cluster beat script already exits 1 on push failure (job shows Failed) — job `Complete` + stale branch should trigger a re-read of which branch you queried.
