# argo rollouts workloadref canary

## Canary for Helm-chart services under ArgoCD (workloadRef adoption)

Problem: canary-deploy a service managed by a **Helm chart under ArgoCD**, where ArgoCD (any version to date) has **no helm post-rendering** — `spec.sources[].helm.postRenderer` does not exist (argoproj/argo-cd#3698, still open; Flux's HelmRelease postRenderers is a different product). Writing the field anyway fails silently: the Application CRD prunes unknown fields, the sync "succeeds", and the app sits OutOfSync forever.

### Solution: workloadRef adoption (documented Deployment→Rollout migration)

Do NOT convert the chart's Deployment. Keep the chart as the single pod-template source and let a plain Rollout manifest **adopt** it:

- Rollout manifest lives OUTSIDE `apps/` (e.g. `services/<svc>/rollout.yaml`) — see root-app hazard below — and is added as a second `path:` source on the Application.
- `spec.workloadRef: {apiVersion: apps/v1, kind: Deployment, name: <release>, scaleDown: onsuccess}` + the chart's own selector labels. The chart Service then fronts Rollout pods unchanged.
- A pod-template change in the chart (image, env, probes) bumps the Deployment's generation; the Rollout tracks `status.workloadObservedGeneration` and runs the canary steps. No mesh needed: `setWeight: 50` with `replicas: 2` = a 1:1 pod split (maxSurge 1, maxUnavailable 0).
- Canary gate: `analysis:` with a `clusterScope: true` ClusterAnalysisTemplate (bare `templateName` resolves NAMESPACED AnalysisTemplates only — without clusterScope the Rollout goes Degraded/InvalidSpec "AnalysisTemplate not found"). `progressDeadlineAbort: true` makes a failed rollout auto-rollback instead of sticking Degraded.
- Fresh Rollout creation skips canary steps (straight to 100%); the first dance happens on the NEXT template change.

### Traps hit (each cost real debugging)

1. **Root app-of-apps recursive ownership**: root Application watching `path: apps` with `directory.recurse: true` claims EVERY yaml under apps/. The Rollout under `apps/<svc>/manifests/` got double-managed; root's operation health-waited on the Degraded Rollout and **deadlocked all later auto-syncs** (ArgoCD skips new syncs while an operation runs; there is no default op deadline — observed stuck 40+ min). Fix: `git mv` the manifest to `services/`, point the app's path source there.
   - Terminate a deadlocked op via REST: `DELETE /api/v1/applications/<name>/operation` (NOT `POST .../terminate-operation` — that 404s; TerminateOperation is mapped to DELETE on the operation resource). Needs `Content-Type: application/json` + `-d '{}'` even on DELETE, else 415 "Invalid content type". Token from `POST /api/v1/session`.
2. **`ignoreDifferences` does not stop APPLYs**: with `ignoreDifferences: /spec/replicas` on the Deployment, syncs still applied `spec.replicas: 2` from the chart render, scaling the adopted shell back up mid-canary. `RespectIgnoreDifferences=true` makes apply use the live value — but see trap 3 for why the real fix is different.
3. **`scaleDown: onsuccess` never re-triggers**: it fires once when the rollout first becomes healthy (adoption). If anything else later scales the Deployment up, the controller does NOT scale it back down (verified: left at 2 for days, controller log silent).
   **Final structure (recommended)**: chart values `replicas: 0` — the Deployment is a pod-less template shell, git-enforced; serving replicas live on the Rollout's own `spec.replicas`. No ignoreDifferences needed; syncs can never resurrect the shell.

### Verified E2E (2026-10-13, backstage)

Pod-template change → Deployment gen 49 → `setWeight 50` (stable 2→1, canary 1) → background AnalysisRun Successful 6/6 (avg probe_success ≥ 0.9 from vmsingle — the front-door SLO probe doubles as the canary gate) → 5m pause → promote; portal HTTP 200 throughout. Steady state: Deployment 0 replicas, 2 Rollout pods, root + app Synced/Healthy.

Also: petal is INELIGIBLE for this pattern — single SQLite writer on an RWO PVC with Recreate strategy (canary needs ≥2 independent pods).
