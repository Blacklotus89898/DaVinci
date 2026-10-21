# uptime kuma

## ArgoCD app permanently OutOfSync on StatefulSet (wildcard ignoreDifferences)

### Symptom
New plain-manifest ArgoCD app wrapping a StatefulSet (uptime-kuma) deploys fine — pod Running, `kubectl diff` against rendered manifests shows ZERO differences — but the app sits `OutOfSync/Healthy` forever, diff on the StatefulSet.

### Root cause
The Application's `ignoreDifferences` used **wildcard jsonPointers**:
```yaml
jsonPointers:
  - /spec/updateStrategy
  - /spec/volumeClaimTemplates/*/apiVersion
  - /spec/volumeClaimTemplates/*/kind
```
The deployed ArgoCD version does NOT honor `*` wildcards in ignoreDifferences jsonPointers, so the known STS+SSA defaulting diff (apiVersion/kind injected inside `volumeClaimTemplates` by the API server) leaked through as a permanent diff.

### Fix
Use numeric-index pointers (copied verbatim from `apps/openobserve/application.yaml`, which stays Synced on this cluster with identical SSA settings):
```yaml
ignoreDifferences:
  - group: apps
    kind: StatefulSet
    jsonPointers:
      - /spec/updateStrategy/type
      - /spec/volumeClaimTemplates/0/apiVersion
      - /spec/volumeClaimTemplates/0/kind
```
Commit 0a9b80d (2026-09-29). Also note `/spec/updateStrategy/type` (leaf field) instead of the whole `/spec/updateStrategy` subtree.

### Prevention
- When copying house patterns, copy them EXACTLY — the control app (openobserve) existed because it worked; "improving" the pointer style reintroduced the bug.
- Diagnosis ladder that cracked it: (1) `kubectl -n argocd get app <name> -o json` → which resource is OutOfSync + comparedTo revision; (2) `kubectl diff -k <dir>` → if clean, the diff is ArgoCD-side (ignore/normalize), not real state drift; (3) diff against a known-Synced control app using the same pattern.

## Uptime-Kuma deployment (external prober)

### What's deployed
Uptime-Kuma 2.1.2 (pinned; 2.x is the maintained line, 1.x EOL Oct 2025) as a plain-manifest ArgoCD app:
- `infrastructure/uptime-kuma/`: StatefulSet (1 replica, 50m/128Mi → 500m/512Mi per monitoring-ns LimitRange, probes on `/` :3001) + NodePort Service.
- `apps/uptime-kuma/application.yaml`: directory app, ns `monitoring`, sync-wave -1.
- Storage: `volumeClaimTemplates` data → 1Gi local-path PVC `data-uptime-kuma-0` (SQLite + history; expected real use ~100Mi).
- UI: **http://192.168.0.108:30320** (NodePort 30320 → 3001). Verified HTTP 200 from LAN.
- Fresh image pull happened at deploy (roadmap's "image already on worker" assumption was stale 1.x); old 1.x image remains prunable via the node image-prune tool.

### First-run setup is UI work (no manifest changes)
1. Browse http://192.168.0.108:30320 → create admin account (local to kuma's SQLite, not tied to ArgoCD/authentik).
2. Add Status Pages later if wanted.

### Suggested monitor list (NodePort inventory, all on 192.168.0.108)
argocd 31991, grafana 30300, ntfy 30310, uptime-kuma 30320 (self), authentik 30080, backstage 30900, jenkins 30808, petal 30990, kavita 30050, linkding 30090, audiobookshelf 30030, openobserve 30500 — HTTP monitors, ~60s interval is plenty on a LAN.
Plus one TCP monitor to `192.168.0.112:6443` as the cluster-alive / control-plane probe.

### Notifications
Kuma has a **native ntfy notification provider** — set it to `http://ntfy.monitoring.svc:80` (in-cluster) topic `homelab-alerts` (or a separate topic like `uptime` if you want page-on-down separate from metrics alerts; the phone already subscribes via http://192.168.0.108:30310).

### Known gap (Phase 2 candidate)
The whole alert path (kuma → ntfy → phone) runs in-cluster; if the cluster/node dies, nothing pages. An external dead-man switch (e.g. healthchecks.io pinged by a kuma monitor or cron, or kuma's own heartbeat to an external service) is the roadmap-level fix.

### Gotcha solved here
See the "ArgoCD app permanently OutOfSync on StatefulSet (wildcard ignoreDifferences)" section above — same file.
