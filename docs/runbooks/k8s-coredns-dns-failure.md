# In-Cluster DNS Resolution Failing

## Symptom
Pods get `could not resolve host` / `dial tcp: lookup <service> ... no such host` when calling other in-cluster services or external hosts. Intermittent or total.

## Root Cause
CoreDNS pods are down, overloaded (dropped UDP packets under load), or a NetworkPolicy is blocking traffic to port 53. Occasionally the default `ndots:5` causes excess lookups that overwhelm CoreDNS under load.

## Fix
```bash
# check CoreDNS pods are healthy
kubectl get pods -n kube-system -l k8s-app=kube-dns
kubectl logs -n kube-system -l k8s-app=kube-dns --tail=100

# test resolution from a debug pod
kubectl run dnsdebug --rm -it --image=busybox --restart=Never -- nslookup kubernetes.default

# check CoreDNS resource usage — a common cause is throttling under load
kubectl top pods -n kube-system -l k8s-app=kube-dns

# restart CoreDNS if it's wedged
kubectl rollout restart deployment/coredns -n kube-system
```

## Prevention
- Run CoreDNS with a `HorizontalPodAutoscaler` or enough replicas for cluster size — 2 static replicas is often too few above a few hundred nodes.
- Enable the `NodeLocal DNSCache` add-on to cut CoreDNS query volume.
- Alert on CoreDNS pod restarts and dropped/latency metrics, not just on downstream app errors.
