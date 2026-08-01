# Pod Stuck in CrashLoopBackOff

## Symptom
`kubectl get pods` shows `STATUS: CrashLoopBackOff`, restart count climbing, backoff delay increasing between attempts.

## Root Cause
The container's main process exits (crashes, or a startup/liveness probe fails) faster than Kubernetes can consider it healthy. Common causes: bad config/env var, missing dependency (DB not reachable), failing liveness probe, or the app panicking on startup.

## Fix
```bash
# see why it died last time
kubectl logs <pod> -n <ns> --previous --tail=100

# check events for probe failures / config errors
kubectl describe pod <pod> -n <ns>

# if it's a bad rollout, roll back
kubectl rollout undo deployment/<name> -n <ns>
```

## Prevention
- Set `initialDelaySeconds` on liveness/readiness probes generously enough for real startup time.
- Validate config/secrets in CI before deploy so bad env vars never reach the cluster.
- Keep a fast rollback path (`kubectl rollout undo`) as the default first response before deep debugging.
