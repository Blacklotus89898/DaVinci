# cluster decisions

## arc-controller is kept by decision (not prunable)

### Decision (user, 2026-09-28)
**arc-controller is intentionally retained** even though its ArgoCD Application shows `OutOfSync/Suspended`. The arc-runners apps are Healthy and in use. Do not flag it for pruning in audits; do not "clean it up" without an explicit user request.

### Context
The daily audit previously listed arc-controller under "consider pruning" because of its Suspended/OutOfSync app status. That status is a known, accepted state, not an anomaly.
