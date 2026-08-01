# Pod Stuck Pending / Unschedulable

## Symptom
`kubectl get pods` shows `STATUS: Pending` indefinitely. `kubectl describe pod` events show `FailedScheduling`.

## Root Cause
The scheduler can't find a node that satisfies the pod's requirements: insufficient CPU/memory across all nodes, a taint with no matching toleration, an unsatisfiable nodeSelector/affinity, or no PV available for a bound PVC.

## Fix
```bash
# read the exact scheduling failure reason
kubectl describe pod <pod> -n <ns> | grep -A10 Events

# check cluster capacity
kubectl describe nodes | grep -A5 'Allocated resources'

# check for taints blocking scheduling
kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints
```

## Prevention
- Keep headroom (e.g. cluster-autoscaler or ~20% free capacity) so a normal deploy doesn't exhaust the cluster.
- Document any custom taints/tolerations so they don't silently orphan new workloads.
- Alert on pending pods older than a few minutes rather than discovering it from a user report.
