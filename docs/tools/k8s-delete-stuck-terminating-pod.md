# Force-Delete a Stuck Terminating Pod

Use when a pod hangs in `Terminating` past its grace period (usually a finalizer or kubelet issue). Skips graceful shutdown — only run once you've confirmed the workload is safe to hard-kill.

```bash
kubectl delete pod <pod> -n <ns> --grace-period=0 --force
```
