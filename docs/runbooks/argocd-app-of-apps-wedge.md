# argocd app of apps wedge

## Root app-of-apps wedged: one zombie child op blocks ALL new child creation

### Symptom

- Root app (app-of-apps) operation stuck `Running` for days, "waiting on Application/<child> health".
- The named child Application's own operation is a TRUE ZOMBIE: status shows an op in progress, the UI offers NO Terminate button (because `spec.operation` is already gone — only stale `status.operation` remains).
- Net effect: root creates NO new child Applications at all — a brand-new child app (e.g. `heartbeat`) never appears even though git has it. Wedge observed 2026-10-01 → 2026-10-07 (authentik child).

### Root cause (two stacked mechanisms)

1. **Sequential health-gating in app-of-apps**: root's sync processes child Applications in order and gates on each child's health; a Degraded/failing child alphabetically early stalls the whole queue. New child creation is behind that gate.
2. **Zombie child operation**: the child's live op got orphaned (op running while `spec.operation` vanished). ArgoCD won't auto-retry a failed sync against the same revision and the UI can't terminate what has no spec.operation — un-terminable from the UI.

### Fix (what worked, user-authorized 2026-10-07)

Non-cascade delete of the zombie child Application + finalizer strip; root auto-heals by re-creating it:

```bash
# 1. delete the child app WITHOUT cascading (preserves all live resources)
kubectl -n argocd delete app authentik --cascade=orphan
# 2. if it hangs on finalizers, complete the pending deletion explicitly
kubectl -n argocd patch app authentik --type=merge -p '{"metadata":{"finalizers":[]}}'
```

Result (all verified live): root op → Succeeded; root re-created the child from git and synced it to HEAD; nothing was lost (in this case the child's deployments + PVCs were untouched — orphaned, then re-adopted).

Key detail: the finalizer patch ALONE (without a preceding delete) completes a deletion that is pending on finalizers — that was the actual wedge-breaker.

### Prevention / early unblock

- **`sync-wave: "-1"` annotation on a child Application jumps the health gate** — critical children (e.g. heartbeat dead-man switch) sync before any Degraded sibling can block them. Applied in 84344e3.
- Suspect this wedge whenever root is stuck AND a new child app from git never appears. Check the named child's op: `kubectl -n argocd get app <child> -o jsonpath='{.status.operation}'` vs `spec.operation` (spec gone + status running = zombie).
- For a merely-stuck (non-zombie) op: `argocd app terminate-op <app>`; a failed auto-sync against the same sha will NOT auto-retry — move HEAD with a real commit to re-arm.

Related: runbooks/argocd-root-finalizer-drift.md (git-side finalizer ignore on children), solutions/heartbeat-dead-man-switch.md (the child that exposed this).
