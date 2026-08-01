# Show ArgoCD App Diff and Hard Refresh

See exactly what ArgoCD thinks is out of sync, then force a cache-bypassing recheck.

```bash
argocd app diff <app-name>
argocd app get <app-name> --hard-refresh
```
