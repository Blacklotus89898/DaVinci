# HPA Not Scaling Despite High Load

## Symptom
CPU/memory or request latency is clearly elevated, but `kubectl get hpa` shows the replica count unchanged, or `TARGETS` column shows `<unknown>`.

## Root Cause
- `metrics-server` isn't installed or is unreachable — HPA has no data to act on (`<unknown>/80%`)
- Pods don't have resource `requests` set — HPA's percentage-of-request calculation has no denominator, so it can't compute a target
- HPA is within its stabilization window (default 5 min scale-down, 0s scale-up in newer versions) and is intentionally holding
- `maxReplicas` already reached — check current vs max
- Custom/external metrics HPA (via Prometheus adapter) — the adapter pod is down or the metric name/query is wrong
- Behavior policy limits how many pods can be added per period (`scaleUp.policies`), scaling looks "stuck" but is actually rate-limited

## Fix
```bash
# Check HPA status and current/target/replica counts
kubectl get hpa <hpa-name> -n <namespace>
kubectl describe hpa <hpa-name> -n <namespace>

# Confirm metrics-server is running and serving data
kubectl get deploy metrics-server -n kube-system
kubectl top pods -n <namespace>
# If this errors, metrics-server is the problem

# Confirm the target Deployment's pods have resource requests set
kubectl get deploy <deployment-name> -n <namespace> -o jsonpath='{.spec.template.spec.containers[*].resources}'

# For custom-metrics HPAs, check the adapter and raw metric
kubectl get pods -n monitoring | grep prometheus-adapter
kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1" | jq .

# Check for scaling events in HPA's condition history
kubectl get hpa <hpa-name> -n <namespace> -o yaml | grep -A10 conditions
```

## Prevention
Always set CPU/memory `requests` on every container — HPA (and the scheduler) depend on them. Monitor `metrics-server` and any custom-metrics adapter as first-class dependencies with their own alerts, since HPA fails silently (shows `<unknown>`) rather than erroring loudly when they're down.
