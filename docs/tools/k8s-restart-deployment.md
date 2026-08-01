# Rolling-Restart a Deployment

Rolls all pods without changing the manifest — picks up new ConfigMap/Secret values or clears bad in-memory state.

```bash
kubectl rollout restart deployment/<name> -n <ns>
kubectl rollout status deployment/<name> -n <ns>
```
