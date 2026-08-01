# Node Shows NotReady

## Symptom
`kubectl get nodes` shows a node `STATUS: NotReady`. Pods scheduled there may get stuck `Terminating` or get rescheduled elsewhere after ~5 minutes.

## Root Cause
Kubelet stopped reporting heartbeats — network partition between kubelet and control plane, kubelet crashed/hung, the node ran out of disk (DiskPressure demotes readiness), or the underlying VM/host is down.

## Fix
```bash
# get the specific condition that flipped
kubectl describe node <node> | grep -A10 Conditions

# check kubelet status if you have host access
ssh <node> 'systemctl status kubelet; journalctl -u kubelet -n 100 --no-pager'

# if the node is unrecoverable, cordon + drain and let the ASG/node-pool replace it
kubectl cordon <node>
kubectl drain <node> --ignore-daemonsets --delete-emptydir-data --force
```

## Prevention
- Alert on node NotReady for >2 minutes, not just on pod-level symptoms.
- Use a managed node pool / cluster-autoscaler so unhealthy nodes self-heal by replacement instead of manual SSH debugging.
- Watch node disk usage proactively — DiskPressure is a common silent cause of NotReady.
