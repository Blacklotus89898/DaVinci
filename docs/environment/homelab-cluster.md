# homelab cluster

## k3s Homelab Cluster Topology and NodePort Allocations

### Cluster

Single k3s cluster. Worker node reachable at `192.168.0.108`. GitOps is
app-of-apps ArgoCD from `github.com/Blacklotus89898/Homelab`
(`bootstrap/root-app.yaml` is the entry point).

Default StorageClass is `local-path`. PVCs are RWO, so any service backed by
SQLite uses `strategy: Recreate` (one writer, one replica).

### NodePort allocations

Everything is still exposed by raw NodePort — `platform/gateways/` is empty and
Istio ingress routing is not yet in place. Check this list before assigning a
new port; the Jenkinsfile conflict check only greps literal `nodePort:` keys and
misses Helm-style `nodePortHttp`/`nodePortHttps`.

| Port | Service |
|---|---|
| 30030 | audiobookshelf |
| 30050 | kavita |
| 30090 | linkding |
| 30131 / 30233 / 30328 | istio-gateway |
| 30443 | argocd (https) |
| 30500 | openobserve |
| 30808 | jenkins |
| 30990 | petal |
| 31991 | argocd (http) |

### k3s 1.34 port-forward bug

`kubectl port-forward` is broken on this k3s version. Use the NodePort directly
instead — this is why the ArgoCD UI is documented as
`http://192.168.0.108:31991` rather than a port-forward.

### Exposure caution

Petal on 30990 serves an API whose mutating routes (`POST /api/runs`, all
`/api/schedules` routes) have no authentication, and it fronts a privileged
DinD pod. Treat NodePort reachability as equivalent to node-level access until
either an auth check or ClusterIP + gateway routing is in place.
