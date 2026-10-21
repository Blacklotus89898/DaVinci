# authentik gitops adoption

## Adopting live Portainer deployments into GitOps (authentik, 2026-10-01)

### Adopting live Portainer deployments into GitOps (authentik, 2026-10-01)

Adopted 107-day-old Portainer-created authentik stack (4 Deployments, 3 Services, 2 PVCs, NodePort 30080) into the Homelab GitOps repo in place, preserving PVCs/NodePort/admin login. Commits: e8df1f6 (manifests), 102439f + ca69f00 (cutover), 29ff239 (SSA restore).

### Adoption pattern (in-place, zero data loss)
1. Survey live state exactly: names, selectors (immutable on Deployments), labels, PVC names/sizes/storageClass, NodePorts, running digests (`kubectl get pods -o custom-columns=...imageID`), measured resources (`kubectl top`).
2. Write git manifests matching live verbatim EXCEPT intended deltas: digest-pinned images, resources, empirically-tested probes, env creds moved from inline `value:` to `valueFrom:`/`envFrom` (sealed via kubeseal; strict scope; never display plaintext — pipe kubectl→python→kubeseal).
3. Probe empiricism: authentik worker readiness = exec `ak healthcheck` (celery ping fails exit 2); server httpGet `/-/health/ready/` + `/-/health/live/` (verified 200 via one-shot curlimages/curl pod). Don't chain probe outputs with `&&`.
4. Place under `services/<name>/` + `apps/<name>/application.yaml`; CreateNamespace=false, automated prune+selfHeal, ServerSideApply=true.

### Trap: SSA merges env by name — inline value + git valueFrom = invalid forever
**Symptom:** sync fails deterministically on adopted Deployments: `env[N].valueFrom: Invalid value: "": may not be specified when 'value' is not empty`. Deployments with NO env (redis) roll fine — the tell.
**Root cause:** live objects carry inline `value:` creds owned by the Portainer field-manager; ServerSideApply list-merge (by env name) combines them with git's `valueFrom:` refs into one invalid entry. SSA cannot remove another manager's fields; retries never succeed.
**Fix (git-only, no live mutation):** one-shot cutover syncOptions `Replace=true` + `ApplyOutOfSyncOnly=true` on the Application — ArgoCD PUTs only the OutOfSync Deployments wholesale (PUT has no merge; stale fields vanish); ApplyOutOfSyncOnly shields already-Synced Services/PVCs from PUT (Service PUT without clusterIP is invalid). After first Synced, revert to `ServerSideApply=true`. Pods roll with surge — near-zero downtime; data safe on PVCs.
**Denied alternative:** kubectl delete of the live deployments was auto-mode-classifier DENIED (git now claims the objects → not "orphaned cleanup"). The Replace cutover via git is the sanctioned equivalent.

### Trap: ArgoCD will not auto-retry a failed sync against the same revision
**Symptom:** app OutOfSync, `op=Failed`, reconciliations continue, no new operation for many minutes. Controller log: `Skipping auto-sync: failed previous sync attempt to [sha] and will not retry for [sha]`.
**Fix:** move HEAD (any meaningful commit — e.g. the ROADMAP progress note) or manual sync. A hard-refresh annotation alone does NOT re-arm it.
**Refresh nudge:** `kubectl -n argocd annotate app <name> argocd.argoproj.io/refresh=hard --overwrite` (also works on root to speed app-of-apps spec pickup; root otherwise refreshes ~3 min).

### End-to-end verification checklist
4/4 pods ready 0 restarts on new ReplicaSets; env shows valueFrom + envFrom only; health 200 in-cluster and via NodePort; `argocd app` Synced/Healthy; sealed secret unsealed (`kubectl -n default get secret authentik-credentials`); CI green on all commits.
