# stale series false alerts

## Stale pod series cause false "no activity" alerts after a rollout

### Symptom
A "nothing happened in N hours" alert, like `increase(x[26h]) == 0`, fires after a deploy even though the activity did happen. The alert's `instance`/`pod` label names the **old** pod's IP, not the current one. Example: TulipNoCycle paged on 2026-10-27 at 21:31Z with instance 10.42.1.37. The live pod was 10.42.1.203, and cycles had run at 14:05Z and 19:00Z.

### Root cause
A rollout creates a new series, because the `pod` and `instance` labels change. The replaced pod's series stays inside the long lookbehind window, and its increase is 0. A per-series `== 0` comparison matches that dead series. Joining with `and on()` lets it through. The alert "resolves" only when a time gate closes or the old series ages out of the window.

### Fix
Aggregate before comparing, and keep "no series at all" firing:

```
((sum(increase(metric{kind="..."}[26h])) or vector(0)) == 0)
```

To check, query at the alert time: `increase(...)` per series shows the 0-valued old pod, while `sum(increase(...))` shows the real count. Port-forward with `kubectl -n monitoring port-forward svc/vmsingle-vmstack 18428:8428` and pass `&time=<unix>`.

### Prevention
Any "absence of events" rule over a window longer than a rollout gap must `sum()` (or `sum by` a stable label) first. Per-series `== 0` is only safe for windows shorter than the series' lifetime. Fixed in Homelab 1a5a78d (services/tulip/observability.yaml).
