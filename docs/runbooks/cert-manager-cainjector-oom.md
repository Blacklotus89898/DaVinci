# cert manager cainjector oom

## cert-manager-cainjector OOMKilled every ~22h (exit 137)

### Symptom
`cert-manager-cainjector` pod restarts climbing steadily (33 restarts observed 2026-09-28), `lastState.terminated.reason: OOMKilled`, exit code 137, roughly a ~22h kill cycle. App otherwise Healthy so it hides from app-level health checks; caught via `kubectl -n cert-manager get pods` restart counts in the daily audit.

### Root cause
Chart default memory limit 64Mi is too small for cainjector's cache on this cluster (webhook configs + cert rotations). Slow leak until the cap kills it.

### Fix
`apps/cert-manager/values.yaml`: raise cainjector limit 64Mi → 128Mi (requests 32Mi kept). Commit + push; ArgoCD rolls the deployment. Verified: new pod Running, restarts reset to 0. Fixed in commit `4705da3` (2026-09-28).

### Prevention
The daily audit script flags restart counts > 10 — any pod hitting that deserves `kubectl describe pod` for OOMKilled exit 137 before anything else. Memory limits under 128Mi for controllers that cache cluster state are suspect.
