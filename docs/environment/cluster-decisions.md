# cluster decisions

## arc-controller is kept by decision (not prunable)

### Decision (user, 2026-09-28)
**arc-controller is intentionally retained** even though its ArgoCD Application shows `OutOfSync/Suspended`. The arc-runners apps are Healthy and in use. Do not flag it for pruning in audits; do not "clean it up" without an explicit user request.

### Context
The daily audit previously listed arc-controller under "consider pruning" because of its Suspended/OutOfSync app status. That status is a known, accepted state, not an anomaly.

## Jenkins and ARC retired (2026-10-23) — supersedes "arc-controller is kept"

### Decision (user, 2026-10-23)
Jenkins and ARC (actions-runner-controller) are **retired**: removed from the cluster, kept in git. CI = GitHub-hosted Actions for repo CI + Petal for cluster pipelines. Homelab ADR 0010 (`docs/adr/0010-retire-jenkins-and-arc.md`). This **supersedes** the 2026-09-28 "arc-controller is kept by decision" entry above. Do not re-add either without the user asking.

### Where things are
- Application definitions: `archive/apps/{jenkins,jenkins-infra,arc-controller,arc-runners,arc-runners-infra}/` (outside the root app-of-apps' `apps/` recursion). `infrastructure/{jenkins,arc-runners}/` stay in place.
- Last commit with both running: tag `retired/jenkins-arc-2026-10-23`.
- Reactivation runbooks: `docs/runbooks/reactivate-jenkins.md`, `docs/runbooks/reactivate-arc.md` (Homelab repo).

### Gotchas found while retiring
- The **heartbeat dead-man switch borrowed ARC's PAT** (`arc-runner-secret`, ns arc-runners). It was moved first to its own ns `heartbeat` with SealedSecret `heartbeat-github-pat` (commit 6f07dd7). Removing ARC before that would have silently killed the only out-of-cluster alert path.
- The ARC controller was "Suspended" because its Deployment had been **`kubectl rollout pause`d out-of-band** and its Application had `automated.enabled=false` set live. Neither was in git, so syncs never undid them. Fix: `enabled: true` stated explicitly in git (commit 1e4fcbd) plus `kubectl rollout resume`. The controller has to be running when the scale set is deleted, because its finalizers deregister the scale set from GitHub.
- Petal's `petal-trigger.yaml` uses `runs-on: homelab-runner`. It's dormant (`PETAL_ENABLED` unset) and needs a LAN runner if it's ever enabled.
