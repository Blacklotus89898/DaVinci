# Pod OOMKilled (Exit Code 137)

## Symptom
Pod restarts repeatedly. `kubectl describe pod` shows `Last State: Terminated, Reason: OOMKilled, Exit Code: 137`.

## Root Cause
Container's memory usage exceeded its configured `resources.limits.memory`, so the kernel OOM-killer terminated it. Common causes: memory leak, a limit set too low for real workload usage, or a traffic/data spike.

## Fix
```bash
# confirm it's OOM, not a crash
kubectl describe pod <pod> -n <ns> | grep -A5 'Last State'

# check actual usage vs. limit
kubectl top pod <pod> -n <ns>

# bump the limit (short-term mitigation)
kubectl set resources deployment/<name> -n <ns> --limits=memory=1Gi
kubectl rollout status deployment/<name> -n <ns>
```

## Prevention
- Set `requests.memory` close to steady-state usage from `kubectl top`, and `limits.memory` with real headroom (not a guess).
- Add a memory-usage alert at 80% of the limit so it pages before the kill, not after.
- If usage grows unbounded over the pod's lifetime, treat it as a leak and profile the app instead of just raising the limit.
