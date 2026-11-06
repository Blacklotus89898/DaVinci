# istio cni crashloop bursts

## istio-cni-node crash-loop bursts on k3s-worker-01 (with ztunnel NotReady)

### Symptom
`istio-cni-node` on k3s-worker-01 has a high restart count (134 by 2026-11-05, against 1 for debian's copy). The restarts are not a steady trickle. They come in **crash-loop bursts of about 11/h** (CrashLoopBackOff cadence), and **ztunnel on the same node is NotReady for exactly the same intervals** without restarting. Alerts fire as warnings: `KubePodCrashLooping` + `KubePodNotReady` for both pods.

Bursts seen (UTC): 10-07 01Z (6, with ztunnel 6), **10-28 21Z–10-29 04Z (~82)**, **10-30 09–12Z (~40)**, plus about 1 per node boot (10-09, 10-12, 10-17, 11-05).

### What was ruled out (2026-11-05 investigation)
- Node resources: worker disk 79–81%, no filesystem went read-only, load ≤4.6, memory fine.
- Control plane: `up` for apiserver, kubelet, istiod and ztunnel metrics endpoints stayed 1 throughout.
- No liveness probe on install-cni, so the restarts are the process **exiting on its own**. Kubelet isn't killing it.
- Nothing else in the cluster restarted during the 10-30 burst.

### Root cause: unconfirmed, the logs aged out
- OpenObserve keeps only about 3 days (`doc_time_min` 11-03).
- The worker's journal for the boot of 10-28 14:37 EDT through 11-04 is missing: that boot ended in the 11-04 power cut.
- The bursts cluster around worker boots and the 10-28 pre-outage work (runbooks/power-outage.md). Leading hypothesis: the cni-agent ↔ ztunnel node socket handshake failing after a boot. Not verified.

### Do this the next time the alert fires (capture before it ages out)
```
P=$(kubectl -n istio-system get pod -l k8s-app=istio-cni-node --field-selector spec.nodeName=k3s-worker-01 -o name)
kubectl -n istio-system logs $P --previous > cni-prev.log
kubectl -n istio-system logs -l app=ztunnel --since=1h --prefix > ztunnel.log
kubectl -n istio-system get events --field-selector involvedObject.name=${P#pod/}
```
Look for the last lines before exit, e.g. `ztunnel` socket connect errors, `failed to reserve container name` (a containerd name collision has been seen in events), or iptables reconcile warnings.

### Useful queries
- Restart timeline: `increase(kube_pod_container_status_restarts_total{pod=~"istio-cni-node.*|ztunnel.*"}[1h]) > 0` over 40d.
- Coupling check: `kube_pod_status_ready{namespace="istio-system",condition="true"}` (cni and ztunnel on the worker drop together).
