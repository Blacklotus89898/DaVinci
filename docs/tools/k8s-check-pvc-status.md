# Check PVC/StorageClass Status

Diagnose a stuck PVC: describe it, list StorageClasses, and check CSI provisioner health.

```bash
kubectl describe pvc <pvc-name> -n <namespace>
kubectl get storageclass
kubectl get pods -n kube-system | grep -i csi
```
