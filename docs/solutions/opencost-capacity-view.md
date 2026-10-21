# opencost capacity view

## OpenCost capacity view on VictoriaMetrics (no second Prometheus)

### OpenCost capacity view on VictoriaMetrics (no second Prometheus)

**Symptom/Task:** add a cost/capacity allocation view (roadmap Phase 3) without a second metrics store on a disk-constrained cluster.

### What's deployed (2026-10-13, commit 84ece3b)

`apps/opencost/` — opencost chart **2.5.32** (app 1.121.3), ArgoCD Application in namespace `monitoring`, automated+selfHeal, SSA. UI: `http://192.168.0.108:30330`. Verified: Synced/Healthy, pod 2/2 containers ready, `up` query against vmsingle succeeds, API returns real allocation (17 namespaces, ~$1.38/6h).

### Chart traps (each cost real debugging time)

- **One pod, NOT two Deployments**: exporter + UI are two *containers in the same pod* (plus an in-process MCP server in the exporter — `MCP_SERVER_ENABLED`/`MCP_HTTP_PORT`, not a sidecar). Label `app.kubernetes.io/name=opencost`.
- **Exactly ONE prometheus datasource**: chart render fails with `Only use one of the prometheus setups: internal, external, or amp` if you set `prometheus.external.enabled: true` without `prometheus.internal.enabled: false`. CI does NOT catch this (validate-pr only lints the Application CR) — **pre-flight chart values with `helm template` locally** before pushing any new chart app.
- **`service.nodePort` applies ONLY to the http-ui port** when `type: NodePort`: API 9003 + MCP 8081 get *auto-assigned* nodePorts (currently 31189/31744 — can change if the Service is recreated). Helm logs a cosmetic coalesce warning (`cannot overwrite table with non table for opencost.service.nodePort`) — the render is correct.
- **Sizing**: monitoring-ns LimitRange caps containers at 512Mi. Chart-default 1Gi limits are rejected at admission. Working values: exporter 128Mi req / 512Mi lim, UI 64Mi / 256Mi. Probes on both containers come enabled by default.

### Datasource wiring

vmsingle serves the Prometheus query API at its **root**: `http://vmsingle-vmstack.monitoring.svc.cluster.local:8428` (same URL as the Grafana datasource). OpenCost needs kube-state-metrics + node-exporter series — both already scraped by vmagent. No PVC anywhere.

### Pricing (bare metal)

`customPricing.enabled: true` renders a git-owned ConfigMap (`custom-pricing-model`, env `CONFIG_PATH=/tmp/custom-config`) — **UI-side pricing edits are in-memory only**; change `costModel` in `apps/opencost/values.yaml`. Units: CPU $/core-hour, RAM $/GB-hour, GPU $/unit-hour, storage $/GB-month, egress $/GB. Current: CPU 0.032, RAM 0.0045, storage 0.01, egress 0 (shadow-cloud on-demand rates).

### Known benign log noise

Every ~7m the exporter logs `ERR ComputeCostData: ... kubecost_pod_network_ingress_bytes_total ... EOF` + `WRN continuing despite metrics errors`. That metric is produced by **Kubecost's network-costs daemonset**, which we don't run (egress is priced $0). Non-fatal by design — do not chase it. (Separately, vmsingle occasionally resets connections on heavy queries — vmalert has shown the same `connection reset by peer` since before OpenCost existed.)

### Useful checks

```bash
kubectl -n monitoring logs deploy/opencost -c opencost | grep -i prometheus | head
# API (port 9003 auto-NodePort; or port-forward svc/opencost 9003):
curl -s "http://192.168.0.108:31189/allocation/compute?window=6h&aggregate=namespace&accumulate=true"
# response: data[0] is a dict {namespace -> allocation}, not a list
```
