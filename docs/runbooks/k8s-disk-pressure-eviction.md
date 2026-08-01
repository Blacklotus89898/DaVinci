# Node DiskPressure / Pods Being Evicted

## Symptom
Pods on a node get evicted with reason `Evicted`, message mentions `disk pressure` or `ephemeral-storage`. `kubectl describe node` shows condition `DiskPressure: True`.

## Root Cause
Node's available disk (usually `/var/lib/kubelet` or the container runtime's image/layer store) dropped below the kubelet eviction threshold. Common causes: unbounded container logs, accumulated old images/layers, or a workload writing to ephemeral storage without limits.

## Fix
```bash
# confirm the condition
kubectl describe node <node> | grep -A3 DiskPressure

# find what's eating space (via debug pod)
kubectl debug node/<node> -it --image=busybox -- chroot /host du -ahx / | sort -rh | head -20

# clean up unused images on the node's runtime, e.g. for containerd:
crictl rmi --prune

# clear completed/failed pods across the cluster (their logs linger on-disk)
kubectl delete pods -A --field-selector=status.phase=Succeeded
kubectl delete pods -A --field-selector=status.phase=Failed
```

## Prevention
- Set log rotation limits on the container runtime (`containerLogMaxSize`, `containerLogMaxFiles`).
- Set `ephemeral-storage` requests/limits on pods that write temp data.
- Add a CronJob to prune completed/failed pods and unused images on a schedule instead of doing it manually during an incident.
