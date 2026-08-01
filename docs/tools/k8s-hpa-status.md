# Check HPA Scaling Status

Inspect why an HPA isn't scaling: current/target metrics, replica bounds, and metrics-server health.

```bash
kubectl describe hpa <hpa-name> -n <namespace>
kubectl top pods -n <namespace>
kubectl get deploy metrics-server -n kube-system
```
