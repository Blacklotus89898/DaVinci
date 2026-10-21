# postmortem 2026 10 01 authentik wal

## Postmortem 2026-10-01: authentik WAL + root wedge

### authentik postgres torn-WAL crashloop + 6-day app-of-apps freeze (root wedge)

- **Severity**: Sev2 — authentik unavailable (pre-OIDC cutover, so no auth path broke); hidden Sev2: **all new ArgoCD child-app creation frozen for ~6 days**; hidden Sev3: disk bleed on the PVC host.
- **Window**: 2026-10-01 ~00:40Z → 2026-10-07 (mitigated; full service restore still pending WAL reset).
- **Blast radius**: authentik server/worker/postgres down; root app-of-apps op stuck → every NEW child app after authentik (alphabetically) never got created/synced. Existing running workloads unaffected. Backstage canary, heartbeat, OpenCost and other Phase-3 work queued behind the invisible freeze.
- **Status**: mitigated (postgres parked at `replicas: 0` in git, commit c46e963); **open** — one-shot pg_resetwal Job staged at `services/authentik/resetwal-job.yaml`, committing it was denied pending user approval.

### Timeline (UTC, approximate where noted)
- 2026-10-01 00:40:27Z — postgres last clean shutdown (later established from WAL state: data files consistent from this point).
- 2026-10-01 (approx, later that day) — next postgres start aborts: **torn checkpoint at 0/3767E568**, PANIC on every subsequent start. Crashloop begins.
- 2026-10-01 (day) — authentik GitOps adoption completes (e8df1f6 → 29ff239); the app enters the root app-of-apps as a first-class child.
- 2026-10-01 → 10-07 — root op health-gates on Application/authentik which can never go Healthy (postgres CrashLoop) → root op stuck Running; because apps are created sequentially, **no later child apps get created**. The stuck op had no UI Terminate button (spec.operation had vanished — zombie op; runbooks/argocd-app-of-apps-wedge.md).
- Each restart attempt wrote a **~152MB core dump into the PVC** — sustained disk bleed on the nearly-full single host.
- 2026-10-07 — user explicitly authorized the non-cascade delete; finalizer-strip patch executed → pending deletion completed, root re-created the authentik app, root op **Succeeded**. (Throwaway of the day: the patch alone completed a deletion that had been pending.)
- 2026-10-08 — postgres scaled 1→0 via git (c46e963) stopping the core-dump bleed; root cause confirmed (torn checkpoint, data files consistent); pg_resetwal Job staged, commit denied → parked.

### Root cause
Uncontrolled postgres shutdown left a **torn WAL checkpoint** (checkpoint record at 0/3767E568 was never completed); postgres refuses to start past it (PANIC) and self-restart only re-hits it. Underlying contributor: postgres on a local-path PV with no backup (accepted posture since 2026-09-29) — the DB was one unclean stop away from this at all times. The freeze was a SECOND, independent failure: ArgoCD app-of-apps sequential health-gating turned one Degraded child into a fleet-wide creation freeze.

### What amplified it
- CrashLoop core dumps (~152MB each) into the PVC on a disk-constrained host.
- The zombie root op (Running with no spec.operation) had no UI escape; delete-with-finalizer-strip was the only reset.
- Detection lag ~6 days: nothing alerted on "root op stuck" or "app creation frozen" — the daily audit surfaced cluster symptoms, not this mechanism.

### What went well
- PVCs and authentik data preserved through the adoption + wedge (no data destroyed by remediation).
- Non-cascade delete fully reset the wedge; root auto-healed the child app.
- Parking postgres at replicas 0 via git stopped the disk bleed without touching the PV.

### Action items
- [x] owner: claude — park postgres, stop core-dump bleed (c46e963) — done 2026-10-08
- [x] owner: claude — wedge mechanics documented (runbooks/argocd-app-of-apps-wedge.md) — done
- [ ] owner: user — approve/commit the resetwal Job (or run the `!` git commands) — **blocked on approval** (standing denial: not to be retried verbatim)
- [ ] owner: claude — after resetwal: restore replicas, verify 4 deployments healthy, then authentik OIDC wiring (roadmap Phase 2)
- [ ] owner: user|claude — detection gap: alert on PVC growth spike and on CrashLoopBackOff > 1h (candidate vmalert rules; not yet written)

### Lessons that generalize
1. **One Degraded child can freeze an entire app-of-apps** — check root op health before debugging "why won't my new app appear". Sync-wave "-1" jumps the gate for critical children (used by heartbeat).
2. **Crashlooping stateful pods on disk-constrained hosts are a disk incident**, not just an availability incident — size/locate core dumps or park the workload fast.
3. GitOps adoption of a crashlooping workload imports its crashloop into the control plane — adopt, then stabilize, or gate adoption on health.
