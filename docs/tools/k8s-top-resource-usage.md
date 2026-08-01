# Top CPU/Memory Consumers

Requires metrics-server installed in-cluster.

```bash
kubectl top pods -A --sort-by=memory
kubectl top nodes
```
