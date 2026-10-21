# limitrange max vs metrics store

## LimitRange max capping a stateful store at its own working set

### LimitRange max capping a stateful store at its own working set

### Symptom
`vmsingle` (VictoriaMetrics single-binary, monitoring ns) OOMKilled ~9x/day — 187 restarts over 3 weeks, exit 137, no memory growth trend (not a leak). vmalert's SLO recording rules (`avg_over_time(...[30d])`, 6–9s queries) correlated with kill times.

### Root cause
The namespace LimitRange `monitoring-limits` set `max.memory: 512Mi` for ALL containers in `monitoring`. vmsingle idles at ~507Mi — the cap sat **at** its own steady-state working set, so any query that touched the 30d window tipped it over. The 200Mi request also under-reported reality to the scheduler.

A LimitRange max is a blunt instrument next to a stateful store: the cap must clear the store's *loaded* working set (data + cache + query scratch), not its request or its empty-cache idle.

### Fix (git-path, commit e6a8857)
- `platform/observability/limitrange.yaml`: max memory 512Mi → 2Gi (ns default 256Mi still bounds everything that doesn't ask for more).
- `apps/victoria-metrics/values.yaml`: vmsingle requests 200Mi → 512Mi, limits 512Mi → 2Gi.
- Verified live: new pod 0 restarts @ 507Mi/2Gi, both ArgoCD apps Synced/Healthy.

### Prevention
- When a namespaced LimitRange `max` exists, audit it against **actual** `kubectl top` working sets of the heavy containers in that ns, plus their worst query path — not against requests.
- Recording rules over long windows (`[30d]`) are a hidden load generator on the *database* side, not just vmalert: the query hits vmsingle's cache/scratch. Sizing vmsingle without the rule load explains "mystery" OOMs.
- Ordering note: if the pod rolls before the LimitRange widens, admission rejects the 2Gi limit and ArgoCD retries (5x backoff) — self-healing once both apps sync.
