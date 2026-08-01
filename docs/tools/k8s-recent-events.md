# Show Recent Cluster Events

Chronological event stream, newest last — good first stop when something broke recently but you don't know what.

```bash
kubectl get events -A --sort-by='.lastTimestamp'
```
