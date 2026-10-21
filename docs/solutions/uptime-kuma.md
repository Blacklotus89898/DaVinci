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

## uptime-kuma 2.x config-as-code with uptime-kuma-api 1.2.1 (three compat shims)

### Context
uptime-kuma has no declarative config. Homelab provisions it from `tools/uptime-kuma/monitors.yaml` via `tools/uptime-kuma/provision.py` (idempotent; creates admin from the sealed `monitoring/uptime-kuma-admin` Secret on a fresh instance; `KUMA_USERNAME`/`KUMA_PASSWORD` override for a UI-created admin). The `uptime-kuma-api` 1.2.1 library targets 1.x; against **2.1.2** three things break:

### Breakages and fixes
1. `add` fails: `SQLITE_CONSTRAINT: NOT NULL constraint failed: monitor.conditions` — 2.x added a NOT NULL `conditions` column. Fix: build the payload with `api._build_monitor_data(...)`, then `data["conditions"] = []` and `api._call("add", data)`. **Must be a list**, not the string `"[]"` — the server JSON-encodes it again and DNS monitors then fail with `conditions.forEach is not a function`.
2. `save_status_page` -> `KeyError: 'incident'` — the library reads a key 2.x renamed. Fix: `cfg = api._call("getStatusPage", slug)["config"]`, `api._build_status_page_data(slug=..., id=cfg["id"], ...)`, `api._call("saveStatusPage", data)`.
3. First call on a freshly booted instance -> `socketio.exceptions.TimeoutError` — the socket accepts before handlers are ready. Fix: retry `need_setup()` with backoff (12 x 5s).
- The library's validator only accepts status-code buckets (`"200-299"`, `"300-399"`, or single codes) — `"200-399"` raises ValueError.

### Other 2.x facts
- `UPTIME_KUMA_DB_TYPE=sqlite` env pre-answers the first-run "setup database" wizard (declarative, rebuild-safe).
- Default history retention is 180d — set `keepDataPeriodDays: 14` on small PVCs.
- Trust a private CA for TLS-verified HTTP monitors with `NODE_EXTRA_CA_CERTS` (Homelab: cert-manager Certificate from `lab-ca` in the same ns; its secret's `ca.crt` is the root).

### Verification method
Rehearse on a throwaway pod (`kubectl run kuma-rehearsal --image=louislam/uptime-kuma:2.1.2 --env=UPTIME_KUMA_DB_TYPE=sqlite`, exposed via a temp NodePort — k3s port-forward is flaky): fresh run -> 58 created; re-run -> 58 unchanged. Delete pod + svc afterwards.
