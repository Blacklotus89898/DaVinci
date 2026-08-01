# Exec Into a Running Container

```bash
kubectl exec -it <pod> -n <ns> -- /bin/sh
# or, if sh isn't present:
kubectl exec -it <pod> -n <ns> -- /bin/bash
```
