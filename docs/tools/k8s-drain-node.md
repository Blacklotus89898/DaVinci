# Drain a Node for Maintenance

Safely evict all pods from a node before maintenance, respecting PodDisruptionBudgets. Re-enable scheduling afterward.

```bash
kubectl drain <node> --ignore-daemonsets --delete-emptydir-data
# after maintenance:
kubectl uncordon <node>
```
