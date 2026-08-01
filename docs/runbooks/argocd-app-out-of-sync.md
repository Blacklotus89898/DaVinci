# ArgoCD App Stuck OutOfSync / Degraded

## Symptom
ArgoCD UI shows an Application as `OutOfSync` even right after a sync, or `Degraded`/`Progressing` indefinitely instead of settling to `Healthy`.

## Root Cause
- A controller (HPA, mutating webhook, admission controller, or another operator) mutates the live resource after ArgoCD applies it (e.g., HPA changes `replicas`, cert-manager injects annotations) — ArgoCD sees a permanent diff between desired and live state
- Health check for a custom resource isn't defined, so ArgoCD can't determine health and reports `Progressing`/`Unknown` forever
- `Degraded` due to an actual rollout failure underneath (CrashLoopBackOff, failed probe) — the sync itself succeeded but the resulting pods are unhealthy
- Sync policy `prune: false` leaves orphaned resources that keep showing as extra/out-of-sync
- Helm/Kustomize output is non-deterministic (e.g., unsorted map keys, generated timestamps) causing a perpetual diff

## Fix
```bash
# See the actual diff ArgoCD is computing
argocd app diff <app-name>

# Check resource-by-resource health/sync status
argocd app get <app-name> --show-operation

# For fields that a controller legitimately owns (e.g., HPA-managed replicas),
# tell ArgoCD to ignore that path
argocd app edit <app-name>  # or via Application manifest:
# spec.ignoreDifferences:
#   - group: apps
#     kind: Deployment
#     jsonPointers: ["/spec/replicas"]

# Check underlying pod health if status is Degraded
kubectl get pods -n <namespace> -l app.kubernetes.io/instance=<app-name>

# Manually trigger a hard refresh (bypasses cache, recomputes diff)
argocd app get <app-name> --hard-refresh

# If truly stuck, force sync (see tools/argocd-force-sync.md)
argocd app sync <app-name> --force
```

## Prevention
Add `spec.ignoreDifferences` for every field a non-ArgoCD controller legitimately mutates (HPA replicas, injected sidecar annotations, admission-webhook-added fields). Define custom health checks (`resource.customizations`) for any CRD ArgoCD manages so `Progressing` doesn't become a permanent state.
