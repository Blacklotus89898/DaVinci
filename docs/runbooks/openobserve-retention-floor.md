# openobserve retention floor

## OpenObserve retention below 3 days panics at boot

### Symptom

After lowering `ZO_COMPACT_DATA_RETENTION_DAYS` below `3`, the OpenObserve pod CrashLoopBackOffs immediately after a StatefulSet rollout; logs show:

```
thread 'main' panicked at src/config/src/config.rs:1893:9:
compact config error: Data retention is not allowed to be less than 3 days.
```

(Seen on openobserve v0.14.6 in this cluster, 2026-10-15, when a 1-day retention was requested.)

### Root cause

OpenObserve validates compactor retention at startup and hard-panics below a 3-day floor. The env var is accepted by k8s; the binary rejects it — so the failure surfaces only after the pod re-creates.

### Fix

Set the value to `3` (the binary's minimum) in `infrastructure/openobserve/statefulset.yaml`:

```yaml
- name: ZO_COMPACT_DATA_RETENTION_DAYS
  value: "3"
```

Commit → push → ArgoCD sync; the pod recovers on the next rollout.

### Prevention

When trimming retention on a nearly-full node, check the component's documented/env-enforced minimum BEFORE pushing the change (grep the upstream config source or release notes for a floor), not after the crashloop. Metrics retention is a separate knob (`vmsingle` `retentionPeriod`, no such floor).
