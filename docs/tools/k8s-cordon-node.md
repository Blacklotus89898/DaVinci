# Cordon a Node (Stop New Scheduling)

Mark a node unschedulable without evicting existing pods — use when you want to stop new work landing there but don't need an immediate drain.

```bash
kubectl cordon <node>
```
