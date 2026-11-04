# heartbeat dead man switch

## Cluster-down dead-man switch (heartbeat branch + GitHub watchdog)

**Status:** LIVE 2026-10-07 (commits aa8297d, 84344e3, 078875e; E2E-verified).

### Problem

Every paging path (vmalertmanager → ntfy, uptime-kuma → ntfy, ArgoCD notifications → ntfy) is in-cluster. If the whole cluster (or its single Proxmox host) dies, nothing pages — the alerting system dies with the thing it monitors. Classic dead-man gap.

### Design — push, not pull

The **cluster pushes** a heartbeat; **GitHub watches**:

- **Cluster side** — `infrastructure/heartbeat/cronjob.yaml`: CronJob `cluster-heartbeat` in `arc-runners`, schedule `7,37 * * * *` (2×/h, off :00/:30). `alpine/git:2.54.0` clones the orphan `heartbeat` branch (depth 1), writes epoch+ISO timestamp to `heartbeat.txt`, commits, pushes. Token = existing `arc-runner-secret` PAT via a git credential helper (token never appears in remote URL or error output). On push failure it alerts the LOCAL ntfy topic ("Heartbeat push FAILED (cluster is up)") — distinguishing "cluster up but can't reach GitHub" from "cluster dark" — then exits 1.
- **GitHub side** — `.github/workflows/heartbeat-watchdog.yml`: schedule `23 * * * *` on a GitHub-HOSTED runner. Clones the heartbeat branch, computes age of last commit; `>7200s` → opens a "Cluster dark? heartbeat stale Nm" issue (permissions: issues:write). Dedup: comments "Still dark" on an existing open issue instead of stacking. GitHub issue notifications are then the page.

Why push-not-pull: an external prober (uptime-kuma watching "can the cluster push?") would need the same GitHub-side watch anyway, plus a second place to store creds. The CronJob reuses the PAT already in `arc-runner-secret`, and GitHub-hosted Actions are free on the public repo, zero cluster footprint (disk constraint).

### Key decisions / gotchas

- **Namespace colocation for the PAT**: SealedSecrets are namespace-scoped. Housing the CronJob in `arc-runners` reuses `arc-runner-secret` — no second copy of the token anywhere.
- **Child Application `sync-wave: "-1"`**: the root app-of-apps health-gates child creation (see runbooks/argocd-app-of-apps-wedge.md); the wave makes the heartbeat app sync first, before any Degraded sibling can block it.
- **2h threshold vs cron jitter**: 2x/h cadence → 4 missed beats before paging. GitHub cron jitter can reach ~30 min; the watchdog is hourly, so worst-case detection is ~3h after true death. Tunable via `env.STALE_SECONDS` in the workflow.
- **60-day inactivity auto-disable**: GitHub disables scheduled workflows on repos with no commit activity for 60 days. This repo is active; if the watchdog ever shows "disabled" in the Actions tab, re-enable + investigate.
- **Invocation bug at birth**: the CronJob shipped with `command: ["/bin/sh","-eu"]` — without `-c` sh treats the script arg as a FILENAME; nothing ran. Fixed in 078875e (runbooks/heartbeat-sh-filename-too-long.md).

### Verify / test procedures

- **Beat is flowing:** `git ls-remote origin heartbeat` — head must advance every ≤30 min.
- **Watchdog ran:** `curl -s "https://api.github.com/repos/Blacklotus89898/Homelab/actions/workflows/heartbeat-watchdog.yml/runs?per_page=3"` — scheduled runs, conclusion success; job summary shows age + verdict.
- **Paging-path drill (safe):** Actions → Heartbeat watchdog → Run workflow → `test_alert=true`. Opens a TEST issue and auto-closes it (notification already proved the page). Does NOT depend on heartbeat freshness.

### Triage on a real "Cluster dark?" page

1. Cluster/host actually down → worker 192.168.0.108, Proxmox host, power. (If the cluster is up, in-cluster ntfy alerts would have been firing too.)
2. Cluster up, heartbeat failing → `kubectl -n arc-runners get jobs,pods` + pod logs. A local ntfy "Heartbeat push FAILED (cluster is up)" = PAT expired or GitHub unreachable (prime suspect: PAT scope/expiry in `arc-runner-secret`).
3. GitHub Actions degraded → check the watchdog's own run history for missed schedules.

Related: [runbooks/argocd-app-of-apps-wedge.md] (root health-gate wedge fixed alongside), solutions/ntfy-alert-pipeline.md, solutions/argocd-notifications-ntfy.md.

## False "Heartbeat push FAILED" during a GitHub incident (2026-11-04)

### Symptom
ntfy `homelab-alerts` showed "⚠️ Heartbeat push FAILED (cluster is up)" at 10:07Z and 10:37Z, yet the `heartbeat` branch has commits for both slots, at :16 instead of the usual :03.

### Root cause
GitHub had a major "Pull Requests, Git Operations and Issues" incident (opened 09:49Z). The first pod's `git push` failed, and the script paged on that first failure. The Job's automatic retry pod (default `backoffLimit` 6) then pushed fine about 10s later. The PAT was not the problem: the same token pushed every other beat.

### Fix (Homelab b2341b7)
- One beat = fresh clone + commit + push, tried **3× over ~80s** (waits 0/20/60s). The alert fires only if all three fail.
- Clone failures are now covered too. Under `sh -eu`, a failed clone used to exit before reaching the alert.
- `backoffLimit: 1`. The default of 6 meant up to 7 pages per beat in a real outage.
- The alert text now says to check https://www.githubstatus.com before the PAT.
- Before shipping, simulated with a fake `git`: 2 failed pushes → exit 0, no alert; persistent failure → 3 attempts, 1 alert, exit 1.

### Triage next time
1. `git log -5 --format=%ci origin/heartbeat`: if every slot has a commit, it was transient.
2. If beats are missing, check githubstatus.com.
3. Only then suspect the PAT (`solutions/pat-rotation-seal.md`).
