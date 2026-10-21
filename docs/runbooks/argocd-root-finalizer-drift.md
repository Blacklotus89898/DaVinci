# argocd root finalizer drift

## root app OutOfSync from stale pre-delete finalizers on child Application

### Symptom

The `root` Application (app-of-apps) stuck `OutOfSync/Healthy`. Drilling into status showed exactly one drifting resource: child `Application/victoria-metrics` (namespace argocd, syncWave -2). My commits had not touched any victoria-metrics file.

### Diagnosis

Diffed live vs git with parsed-field normalization (pop status, annotations, uid, generation, resourceVersion, creationTimestamp; JSON-dump both sorted):

```
kubectl -n argocd get app victoria-metrics -o yaml --show-managed-fields=false
```

Only difference: live metadata.finalizers had two extra entries not in git — `pre-delete-finalizer.argocd.argoproj.io` and `pre-delete-finalizer.argocd.argoproj.io/cleanup`. Cross-checked ALL apps: those two appear on victoria-metrics only (residue from a past deletion attempt). `deletionTimestamp` verified empty before any action.

Windows gotcha in the diff harness: redirecting kubectl output to Git Bash `/tmp` is invisible to Windows-native python (`FileNotFoundError`) — write to a cwd-relative file or fetch via `subprocess.run(['kubectl', ...])`.

### Fix (git-side, per the one rule)

Live-patching the finalizers via kubectl was classifier-denied (correctly — direct Application mutation). The sanctioned fix is teaching root to ignore controller-managed finalizer drift, in `bootstrap/root-app.yaml` (commit fa3e78e, 2026-09-30):

```yaml
spec:
  ignoreDifferences:
    - group: argoproj.io
      kind: Application
      jsonPointers:
        - /metadata/finalizers
```

Then `kubectl apply -f bootstrap/root-app.yaml` (the sanctioned bootstrap-path exception for root itself). Root went Synced/Healthy immediately.

### Notes

- Trade-off: git no longer diffs child-app finalizers at all — intentional; git never intents ArgoCD-internal finalizers. Same pattern family as [[solutions/uptime-kuma]] (jsonPointers, NOT jqPathExpressions).
- The stale finalizers remain harmlessly on the live object (only meaningful at deletion time). If they ever need stripping, that is a user-executed step.
- Root's own spec changes do NOT propagate through ArgoCD — root is updated only by applying `bootstrap/root-app.yaml` (documented one-rule carve-out).
