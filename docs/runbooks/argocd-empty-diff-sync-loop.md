# argocd empty diff sync loop

## ArgoCD app OutOfSync with empty diff / sync ping-pong on istio webhook configs

### Symptom
The `istio` ArgoCD Application endlessly flapped `OutOfSync` on two resources — `ValidatingWebhookConfiguration/istio-validator-istio-system` and `istiod-default-validator` — with automated sync constantly "fixing" them (`OperationStarted` spam every few minutes). `argocd app diff istio` showed an **empty diff** despite the OutOfSync status.

### Root cause
`pilot-discovery` (istiod) owns two fields on those webhook configs at runtime: `webhooks[0].clientConfig.caBundle` (cert rotation) and `webhooks[0].failurePolicy` (set to `Ignore` for mesh resilience while the chart declares `Fail`). ArgoCD (default field manager comparison) saw the live values differ from helm target → OutOfSync → selfHeal reverted → istiod re-wrote → loop.

The empty diff + stuck OutOfSync combination happens because the CLI diff and the controller comparison normalize differently. **The real field list comes from managedFields** — which `kubectl get` hides by default.

### Diagnosis (the forensics move)
```bash
kubectl -n istio-system get validatingwebhookconfiguration istio-validator-istio-system -o yaml --show-managed-fields
```
Managers section shows `pilot-discovery` owning `/webhooks/0/clientConfig/caBundle` and `/webhooks/0/failurePolicy` — those are the fields to ignore, nothing more.

### Fix
`apps/istio/application.yaml` → `ignoreDifferences` with **jsonPointers** (NOT jqPathExpressions):
```yaml
ignoreDifferences:
  - group: admissionregistration.k8s.io
    kind: ValidatingWebhookConfiguration
    jsonPointers:
      - /webhooks/0/clientConfig/caBundle
      - /webhooks/0/failurePolicy
```
Commits `160c6d7` + `cc8c3ef` (2026-09-28). After: status pinned `Synced/Healthy` verified by 20s sampling over ~6 min, `argocd app diff` empty, webhook configs byte-stable across two istiod reconcile cycles.

### Gotchas
- **jqPathExpressions were unreliable here** under `ServerSideApply=true` (spec accepted, diff stayed stuck). jsonPointers worked. Prefer jsonPointers for array-index fields under SSA.
- **Residue (benign, do not re-escalate):** ArgoCD still logs an instant (<1s) `OperationStarted/Completed` pair every ~5 min for these two webhooks — istiod's periodic rewrite bumps resourceVersion/managedFields, ArgoCD answers with a no-op serverside-apply. Live objects are byte-identical before/after; sync status never leaves Synced. Only worth action if operations start failing or showing real diffs.
- General rule: whenever an ArgoCD app is OutOfSync with an empty diff, dump managedFields with `--show-managed-fields` and diff per-manager ownership; then ignore exactly the fields another controller owns.
