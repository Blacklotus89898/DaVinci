# cert manager sync unknown

## cert-manager app stuck sync=Unknown (helm values schema error)

### Symptom
ArgoCD shows the `cert-manager` Application `Sync status: Unknown` — no conditions, no error surfaced in UI summary. App health Missing/Unknown.

### Root cause
`apps/cert-manager/values.yaml` had a top-level `revisionHistoryLimit: 3`. The jetstack helm chart's values JSON schema rejects unknown top-level keys, which breaks ArgoCD's helm manifest generation. When generation fails, ArgoCD can't compute comparison → status Unknown (it does not render the helm error prominently).

### Diagnosis
- `kubectl -n argocd get app cert-manager -o jsonpath='{.status.conditions}'` empty, sync Unknown.
- Local generation repro: `helm template cert-manager charts.jetstack.io/cert-manager --version 1.21.0 -f values.yaml` → schema validation error naming the offending key. (Or check app conditions after hard refresh.)

### Fix
Remove the schema-invalid key from the app's values file, commit, push; ArgoCD auto-syncs. Fixed in commit `4705da3` (2026-09-28).

### Prevention
Validate values against the chart schema before committing: `helm template` locally, or `helm lint`. Any time an ArgoCD helm app shows sync Unknown with empty diff, suspect manifest-generation failure (bad values key, bad chart ref) rather than cluster drift.
