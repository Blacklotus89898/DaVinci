# PVC Stuck in Pending

## Symptom
`kubectl get pvc` shows a PersistentVolumeClaim stuck in `Pending` and the pod mounting it is stuck in `ContainerCreating` with events like `waiting for a volume to be created` or `no persistent volumes available for this claim`.

## Root Cause
- No StorageClass matches the claim (typo in `storageClassName`, or no default StorageClass set in the cluster)
- Requested size exceeds what the provisioner/backing storage can satisfy
- `WaitForFirstConsumer` binding mode: the PVC won't bind until a pod using it is scheduled, and that pod can't schedule due to zone mismatch (PVC provisioned in zone A, pod scheduled to zone B)
- CSI driver/provisioner pod is crashed or missing (e.g., `ebs-csi-controller`, `pd-csi-driver`)
- Storage backend quota exhausted (cloud provider volume limit per node/account)

## Fix
```bash
# Inspect the claim and its events
kubectl describe pvc <pvc-name> -n <namespace>

# Check what StorageClasses exist and which is default
kubectl get storageclass
kubectl get storageclass -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}'

# Check the CSI provisioner pods are healthy
kubectl get pods -n kube-system | grep -i csi

# If WaitForFirstConsumer + zone mismatch: check node zones vs PVC's chosen zone
kubectl get pv <bound-pv-name> -o jsonpath='{.spec.nodeAffinity}'
kubectl get nodes -L topology.kubernetes.io/zone

# If size/class is wrong, delete and recreate the PVC (data loss if already had one!)
kubectl delete pvc <pvc-name> -n <namespace>
kubectl apply -f pvc.yaml
```

## Prevention
Set an explicit default StorageClass per cluster. For multi-zone clusters, use a StorageClass with `volumeBindingMode: WaitForFirstConsumer` (not `Immediate`) so the PV is provisioned in the same zone as the pod actually gets scheduled to.
