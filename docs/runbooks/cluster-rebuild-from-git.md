# cluster rebuild from git

## Cluster cold-rebuild from git (restore drill)

### What this drill proves (and what it deliberately is NOT)

Velero/PV backups were **DECLINED by the user 2026-09-29** — DR posture = *this git repo is the source of truth; PV contents accepted non-recoverable.* The quarterly "restore drill" is therefore a **git-rebuild rehearsal**: (1) prove git alone can rebuild the cluster, (2) keep surfacing the short list of things git CANNOT restore before they bite, (3) keep the floating-object list at zero drift.

### Rebuild matrix — what comes back and from where

| Layer | Restored by | Notes |
|---|---|---|
| k3s + core (traefik, coredns, metrics-server, local-path) | fresh `k3s install` defaults | nodes: debian 192.168.0.112 (control-plane), k3s-worker-01 192.168.0.108 |
| ArgoCD root app-of-apps | ONE manual step: `kubectl apply -f bootstrap/root-app.yaml` | everything below then converges automatically |
| Namespaces + LimitRanges + ambient labels | git (`infrastructure/namespaces`, wave -5) | no manual ns creation |
| All 30+ platform/service apps | git (apps/* → root app) | includes all CronJobs (heartbeat, pod-cleanup, kube-bench) |
| GitHub Actions CI (9 jobs) | git (.github/workflows) | repo public, hosted runners |
| **SealedSecrets decryption** | **USER ACTION — the critical gap** | a NEW sealed-secrets controller generates NEW keys → every SealedSecret in git is undecryptable. Restore the old master key or every consumer (ARC PAT, grafana admin, backstage tokens, authentik creds) runs secret-less |
| ArgoCD admin password | auto-generated on rebuild (`argocd admin initial-password`) | current one is git-managed via SealedSecret; fetch silently, never echo |
| PV contents (grafana dashboards, vmsingle metrics, openobserve logs, authentik+backstage postgres, petal SQLite, uptime-kuma monitors, kavita hostPath) | **NOT restored — accepted loss** (decision 2026-09-29) | only the worker disk survives if the worker itself isn't rebuilt |
| Floating objects (below) | nothing — that's the finding | decide adopt-or-delete BEFORE a rebuild forces the choice |

### Sealed-secrets master key (quarterly check + restore procedure)

Backup (USER runs, keep OFF git — it is secret-shaped):
```bash
kubectl -n sealed-secrets get secret -l sealed-secrets-key=true -o yaml > sealed-secrets-master-key.yaml
```
Restore into a rebuilt cluster (before or after the sealed-secrets app syncs; controller picks up existing keys on start):
```bash
kubectl apply -f sealed-secrets-master-key.yaml
kubectl -n sealed-secrets delete pod -l app.kubernetes.io/name=sealed-secrets   # force key reload
```
Quarterly check: the backup file exists, is dated within the last quarter, and the newest controller key's creationTimestamp in-cluster is older than the backup date (else a key rotation happened that the backup missed).

### Quarterly rehearsal procedure (restore leg)

1. **App parity**: git `apps/` dirs vs `kubectl -n argocd get applications`. On Windows, kubectl jsonpath output carries `\r` — pipe through `tr -d '\r'` before `comm`, or every line mismatches. Expected delta: exactly `root` (bootstrap-owned) on the live side.
2. **Floating-object sweep**: `kubectl get deploy,sts,ds,cronjob -A` → anything whose ns/name isn't produced by an Application = floater. Exclude k3s-core (kube-system) and operator-owned children (e.g. otel Collector DaemonSet).
3. **Drift check**: every app Synced (audiobookshelf Suspended-by-design, arc-controller OutOfSync-by-user-decision are the accepted exceptions — see Watchlist in docs/ROADMAP.md).
4. **Master-key check** (above).
5. File results; new floaters → adopt into git or schedule deletion (user intent).

### Drill results — 2026-10-13 (first run)

- **App parity: PASS** — 32 git dirs = 31 live apps + kube-bench (converging at audit time); root is the only bootstrap-owned extra. No app in git missing live, none live missing git.
- **Drift: PASS** — all Synced/Healthy except the two accepted exceptions (arc-controller Suspended user-kept; audiobookshelf Suspended) and authentik Progressing (postgres parked at replicas 0 by design, WAL recovery pending).
- **Floating objects: 5 deployments, NONE in git** (all pre-dating GitOps adoption):
  - `default/juice-shop` (2026-03-28, NodePort 30001 — externally reachable, actively serving)
  - `default/guestbook-ui` (2026-05-11, ArgoCD demo, ClusterIP)
  - `default/simple-rust-api` (2026-05-19, no Service — unreachable)
  - `default/uptime-kuma` (2026-03-26, the OLD deployment; the real STS lives in monitoring — image predates 2.1.2 pin)
  - `portainer/portainer-agent` (2026-03-28 — Portainer-era leftover, still running; **newly surfaced by this drill**, previously undocumented)
  - Plus `paperless/*` (3 deployments, scaled to 0) — documented/accepted (solutions/paperless-disabled.md), not counted.
  - **Dispositions pending user intent**: adopt into git or delete. None will survive a rebuild.
- **Master-key check: NOT DONE — cannot be done by Claude.** The backup file is a USER-held artifact (never in this repo). If no backup exists, a rebuild loses every sealed credential — this is the single highest-impact gap in the current DR posture.

### Rebuild matrix gotchas learned from real incidents

- A Degraded child app alphabetically early **blocks all later child creation** (app-of-apps sequential health-gating) — after a rebuild, if one app is unhealthy, later apps never appear; don't debug the missing app, debug the wedge (see runbooks/argocd-app-of-apps-wedge.md).
- SealedSecrets restore ordering: applying the master key AFTER services synced means their Secrets were empty briefly — re-sync affected apps or bounce pods after the key reload.
- ArgoCD `CreateNamespace=false` apps fail on a fresh cluster until the namespaces app (wave -5) syncs — expected, resolves on retry ladder.
