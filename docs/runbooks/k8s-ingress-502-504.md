# Ingress Returning 502/504 Bad Gateway

## Symptom
Requests through the ingress controller (nginx-ingress, ALB, Traefik, etc.) return `502 Bad Gateway` or `504 Gateway Timeout`, while `kubectl get pods` shows the backend pods as `Running`.

## Root Cause
- Backend pod is `Running` but not actually ready (readiness probe missing/misconfigured, so the Service sends traffic to a pod that hasn't finished booting) → 502
- Service `targetPort` doesn't match the container's actual listening port → 502
- Backend takes longer to respond than the ingress controller's proxy timeout (default 60s on nginx-ingress) → 504
- Pod is being OOMKilled or crash-looping mid-request → 502
- Ingress controller can't resolve the Service's Endpoints (no endpoints because label selector on the Service doesn't match pod labels) → 502

## Fix
```bash
# Confirm the Service actually has endpoints
kubectl get endpoints <service-name> -n <namespace>
# Empty ADDRESSES column = selector mismatch or no ready pods

# Compare Service selector to pod labels
kubectl get svc <service-name> -n <namespace> -o jsonpath='{.spec.selector}'
kubectl get pods -n <namespace> --show-labels

# Check readiness probe status
kubectl describe pod <pod-name> -n <namespace> | grep -A5 Readiness

# Verify targetPort matches the container's listening port
kubectl get svc <service-name> -n <namespace> -o yaml | grep -A3 ports
kubectl exec <pod-name> -n <namespace> -- netstat -tlnp 2>/dev/null || ss -tlnp

# Tail ingress controller logs for the specific upstream error
kubectl logs -n ingress-nginx deploy/ingress-nginx-controller --tail=100 | grep <hostname>

# If timeout-related (504), raise proxy timeout via annotation (nginx-ingress example)
kubectl annotate ingress <ingress-name> -n <namespace> \
  nginx.ingress.kubernetes.io/proxy-read-timeout="120" \
  nginx.ingress.kubernetes.io/proxy-send-timeout="120" --overwrite
```

## Prevention
Always set both readiness and liveness probes matching the app's actual health-check endpoint. Set ingress timeout annotations to comfortably exceed the slowest expected backend response, and alert on p99 latency approaching that threshold.
