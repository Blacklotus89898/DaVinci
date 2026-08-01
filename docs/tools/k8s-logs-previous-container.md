# Tail Logs of a Crashed Container

The running container's logs are empty after a crash — the useful output is in the previous instantiation.

```bash
kubectl logs <pod> -n <ns> --previous --tail=200
```
