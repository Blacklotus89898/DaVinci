# NetworkPolicy Silently Blocking Traffic

## Symptom
A pod can't reach another service (connection timeout, not refused — refusal would mean the app itself rejected it) even though the target pod, Service, and DNS all look healthy. Often appears right after a NetworkPolicy is introduced to a previously open namespace.

## Root Cause
- The moment ANY NetworkPolicy selects a pod (via empty or matching `podSelector`), that pod's traffic becomes default-deny for the policy's `policyTypes` (Ingress/Egress) — any traffic not explicitly allowed by some policy is dropped
- A new "deny-all" or restrictive policy was added to a namespace, but no accompanying "allow" policy covers a legitimate path (e.g., allowing ingress from the ingress-controller namespace, or egress to DNS/kube-system)
- Egress policy blocks DNS (UDP/TCP 53 to kube-system) — looks like "everything is broken" because even name resolution fails, not just the target connection
- Policy's `namespaceSelector`/`podSelector` labels don't match reality (label typo, or the target namespace lacks the expected `kubernetes.io/metadata.name` label some policies key off of)
- CNI plugin doesn't actually enforce NetworkPolicy (e.g., plain Flannel) — policies are silently no-ops, so this isn't the cause if traffic is blocked; conversely if traffic that *should* be blocked isn't, check CNI support first

## Fix
```bash
# List all NetworkPolicies affecting the namespace
kubectl get networkpolicy -n <namespace>
kubectl describe networkpolicy -n <namespace>

# Check exactly which pods a policy selects
kubectl get networkpolicy <policy-name> -n <namespace> -o jsonpath='{.spec.podSelector}'
kubectl get pods -n <namespace> --show-labels

# Test connectivity directly to isolate NetworkPolicy vs. app/DNS issue
kubectl run debug-net --rm -it --image=nicolaka/netshoot -n <namespace> -- \
  sh -c "nc -zv <target-service> <port>"

# Check if DNS egress is blocked (common false lead — looks like app bug)
kubectl run debug-net --rm -it --image=nicolaka/netshoot -n <namespace> -- \
  nslookup kubernetes.default

# Temporarily allow all traffic to a namespace to confirm NetworkPolicy is the cause
cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: debug-allow-all
  namespace: <namespace>
spec:
  podSelector: {}
  ingress:
  - {}
  egress:
  - {}
EOF
# If traffic starts working, a legitimate policy is missing a rule — remove this debug policy after confirming
kubectl delete networkpolicy debug-allow-all -n <namespace>
```

## Prevention
Whenever adding a default-deny policy to a namespace, add the DNS-egress and same-namespace-ingress allow rules in the same change, not as a follow-up — the gap between them is exactly when this incident happens. Document which namespaces need cross-namespace policies (e.g., ingress-controller → app namespaces) before rolling out network segmentation.
