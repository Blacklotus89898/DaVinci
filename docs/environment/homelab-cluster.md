# homelab cluster

## k3s Homelab Cluster Topology and NodePort Allocations

### Cluster

Single k3s cluster (v1.34.5+k3s1): control-plane `debian` = 192.168.0.112
(SchedulingDisabled), worker `k3s-worker-01` = 192.168.0.108 (all workloads).
GitOps is app-of-apps ArgoCD from `github.com/Blacklotus89898/Homelab`
(`bootstrap/root-app.yaml` is the entry point). Default StorageClass is
`local-path`; PVCs are RWO, so SQLite-backed services use `strategy: Recreate`.

Monitoring since 2026-09-29: VictoriaMetrics single-binary + vmagent + Grafana
(ns `monitoring`, fullname `vmstack`) — see solutions/victoria-metrics-stack.md.

### NodePort allocations (verified against the cluster 2026-10-07)

Everything is still exposed by raw NodePort — `platform/gateways/` is empty and
Istio (installed, Synced/Healthy) is not used for routing yet. Check this list
before assigning a new port; the Jenkinsfile conflict check only greps literal
`nodePort:` keys and misses Helm-style `nodePortHttp`/`nodePortHttps`.

| Port | Service | Notes |
|---|---|---|
| 30001 | juice-shop | default ns, **not in git** — orphan pending user decision |
| 30030 | audiobookshelf | |
| 30070 | paperless | scaled to zero by decision (see paperless-disabled.md) |
| 30080 | authentik | adopted into GitOps 2026-10-01 (services/authentik/) |
| 30090 | linkding | |
| 30131 / 30233 / 30328 | istio-gateway | installed, unused for routing |
| 30300 | grafana | VictoriaMetrics stack (VM stack chart) |
| 30310 | ntfy | topic `homelab-alerts`: vmalertmanager + ArgoCD notifications + uptime-kuma |
| 30320 | uptime-kuma | external prober (solutions/uptime-kuma.md) |
| 30443 | argocd (https) | |
| 30500 / 32365 | openobserve | logs (OTel), 30500 UI |
| 30808 | jenkins | |
| 30900 | backstage | |
| 30990 | petal | |
| 31991 | argocd (http) | the usual UI entry |

### k3s 1.34 port-forward bug

`kubectl port-forward` is broken on this k3s version. Use the NodePort directly
instead — this is why the ArgoCD UI is documented as
`http://192.168.0.108:31991` rather than a port-forward. (Port-forwarding the
VictoriaMetrics query API `svc/vmsingle-vmstack` also 404s `/proxy/stats`; query
via NodePort-adjacent port-forward with explicit svc port works for VM.)

### Exposure caution

Petal on 30990 serves an API whose mutating routes (`POST /api/runs`, all
`/api/schedules` routes) have no authentication, and it fronts a privileged
DinD pod. Treat NodePort reachability as equivalent to node-level access until
either an auth check or ClusterIP + gateway routing is in place.
