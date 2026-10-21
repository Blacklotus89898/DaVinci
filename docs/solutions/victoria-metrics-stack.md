# victoria metrics stack

## VictoriaMetrics k8s-stack deployment (monitoring ns)

### What's deployed

- **App**: `victoria-metrics` (chart `vm/victoria-metrics-k8s-stack` 0.95.0, app v1.153.0) + `victoria-metrics-infra` (SealedSecret), both in `apps/` auto-discovered by the root app. Helm release name `victoria-metrics`, **`fullnameOverride: vmstack`**.
- **Namespace**: `monitoring` (istio ambient). Nodes: vmsingle (Deployment, PVC 8Gi cap, retention 30d), vmagent, vmalert, vmalertmanager STS, operator, kube-state-metrics, node-exporter daemonset (2 nodes), grafana (PVC 2Gi, NodePort **30300** → http://192.168.0.108:30300).
- **Grafana admin**: SealedSecret `grafana-admin-credentials` in `infrastructure/victoria-metrics/`. Password: `kubectl -n monitoring get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d`.

### Verified state (2026-09-29)

`count(up)` = 22 with all `up == 1`; ingestion ~6.8k rows/s; worker disk 66%→71% (~2G incl. images — at budget).

### Pitfalls hit during deploy (all cost debugging time — read before touching this stack)

### Symptom 1: vmalertmanager STS never creates pods
`metadata.labels: Invalid value: "vmalertmanager-victoria-metrics-victoria-metrics-k8s-stack-b77667444": must be no more than 63 characters`. The VM operator builds pod labels from CR names, so the chart fullname must stay short.
**Fix**: `fullnameOverride: vmstack` in values (commit 75dcf3b).

### Symptom 2: vmsingle ReplicaSet FailedCreate
`maximum memory usage per Container is 512Mi, but limit is 768Mi` — the `monitoring` ns has a LimitRange (`kubectl -n monitoring get limitrange monitoring-limits`): max 512Mi/container, defaults 256Mi limit / 64Mi request.
**Fix**: keep every container limit ≤512Mi. Also: any chart that ships NO explicit resources (grafana did) silently gets the 256Mi default and gets **OOMKilled** during startup — set explicit resources (commit 9ddbe32, grafana 512Mi).

### Symptom 3: push doesn't roll out — app stuck "Retrying attempt #N"
A first sync operation that can't complete (it was blocked by symptoms 1+2) holds the operation lock; later pushes sit OutOfSync.
**Fix**: `argocd app terminate-op victoria-metrics` (NOT `terminate-operation` in argocd CLI v3), then `argocd app sync <name>` once the blockers are fixed. `argocd app manifests <name> | grep -A4 resources` tells you whether a values change actually rendered before you suspect sync.

### Known residue (do not re-escalate)

Operator self-rotates its admission-webhook TLS cert + caBundles → transient OutOfSync on the app. If it ever oscillates, set `victoria-metrics-operator.admissionWebhooks.enabled: false`.

### Useful commands

```bash
kubectl -n monitoring port-forward svc/vmsingle-vmstack 18428:8428   # then query /api/v1/query
curl 'http://localhost:18428/api/v1/query?query=count(up)'           # VM image has no curl/wget — port-forward from outside
kubectl -n monitoring get pods | grep -iE 'vmstack|victoria|grafana'
argocd app diff victoria-metrics   # caBundle/cert diffs = the known residue
```

## alertmanager→ntfy 400 "attachments not allowed" (SLO paging down)

### Symptom
`vmalertmanager` logs `Notify for alerts failed ... unexpected status code 400: {"code":40014,...,"error":"invalid request: attachments not allowed"}` every group_interval; `AlertmanagerFailedToSendAlerts` (warning) + `AlertmanagerClusterFailedToSendAlerts` (critical) fire; nothing reaches the phone topic `homelab-alerts` even though ntfy itself is up (heartbeat's own wget pushes work — those send `text/plain`).

### Root cause
The receiver was a bare `webhook_configs` URL (`http://ntfy.monitoring.svc/homelab-alerts?title=...`). Alertmanager POSTs its payload with `Content-Type: application/json`; ntfy treats any non-`text/plain` body as an **attachment upload**, and this self-hosted ntfy has no `attachment-cache-dir` → attachments disabled → 400 on every notification. Found 2026-10-14 during the stability audit; paging had been silently down.

### Fix
ntfy (≥ v2.11) ships a built-in `alertmanager` template that parses the webhook payload server-side. One-line change in `apps/victoria-metrics/values.yaml` (commit `42eedfb`):
```yaml
webhook_configs:
  - url: "http://ntfy.monitoring.svc/homelab-alerts?template=alertmanager"
    send_resolved: true
    max_alerts: 3
```
Verified live BEFORE committing by POSTing an alertmanager-shaped JSON from the blackbox-exporter pod (busybox wget):
```bash
kubectl -n monitoring exec deploy/blackbox-exporter -- wget -S -O- \
  --header='Content-Type: application/json' \
  --post-data='{"version":"4","alerts":[{"status":"firing","labels":{"alertname":"Test"},"annotations":{"summary":"safe to ignore"}}]}' \
  'http://ntfy.monitoring.svc/homelab-alerts?template=alertmanager'
```
→ HTTP 200, formatted notification (title `🚨 Alert: <name>`, body with Status/Severity/Summary). Post-deploy: zero send errors in vmalertmanager logs; both Alertmanager*Send* alerts resolved and stayed resolved.

### Gotchas
- Alertmanager logs only FAILURES — after the fix "success" is silence in the logs. Don't gauge it by log noise.
- `AlertmanagerFailedToSendAlerts` resolves SLOWLY on purpose: its expr is `rate(failed[15m]) / rate(total[15m]) > 0.01` — a 15-minute window, so it keeps firing up to 15 min after the last real failure. Read the actual rule expr (vmalert API, below) before assuming a fix didn't work.
- vmalert's own rule state (authoritative, faster than ALERTS in vmsingle): `kubectl -n monitoring exec deploy/blackbox-exporter -- wget -qO- 'http://vmalert-vmstack:8080/api/v1/rules?type=alert'` — same trick for expr strings.
- Query recipe used throughout: `kubectl -n monitoring exec deploy/blackbox-exporter -- wget -qO- 'http://vmsingle-vmstack:8428/prometheus/api/v1/query?query=ALERTS{alertstate="firing"}'` (blackbox pod has wget; vmsingle svc = `vmsingle-vmstack:8428`).
- Same commit blackholed `KubeCPUOvercommit` (requests overcommit is structural on the single worker — matches the pre-existing KubeMemoryOvercommit blackhole; blackhole = no page, alert still visible in Grafana) and set `kubeEtcd.enabled: false`: k3s uses the sqlite datastore, so the chart's headless `vmstack-kube-etcd` Service (kube-system) selects nothing → `ScrapePoolHasNoTargets` fired forever since install.

## Default-rules noise cured at the source (CPUThrottlingHigh + inverted RecordingRulesNoData)

### Symptom
Two alerts firing chronically on a healthy cluster (commit ad532f1, 2026-10-21):
`CPUThrottlingHigh` for exactly two containers (petal `git-sync`, node-exporter), and `RecordingRulesNoData` forever at severity=info.

### Root cause (three separate mechanisms)
1. **git-sync**: 100m CPU limit throttled 47% of CPU periods — git-sync's 30s poll cycle (`--period=30s`) bursts hard (TLS handshake + ref listing + fetch) then idles; a limit sized to the idle state throttles every burst.
2. **node-exporter DaemonSet**: the victoria-metrics-k8s-stack chart sets NO resources on it → the monitoring-ns LimitRange default (250m CPU limit) applies → per-core collectors throttled ~20% of periods, riding the alert's 25% threshold.
3. **RecordingRulesNoData has inverted logic**: it fires BECAUSE the cluster is healthy. The kube-prometheus-general rule `count:up0` = `count(up == 0)` = count of DOWN targets; when everything is up that's an empty set in PromQL → `vmalert_recording_rules_last_evaluation_samples` = 0 → the alert (sum < 1 per group) reads "no data" as broken. Verified live: `count:up1`=23 samples (healthy), `count:up0`=0 samples, alert firing on the 0. It would self-resolve during an outage and re-fire on recovery.

### Fix
- petal deployment: git-sync `limits.cpu` 100m→500m (requests stay 20m — bursts, not steady state).
- vmstack values: explicit `prometheus-node-exporter.resources` (20m/32Mi req, 500m/128Mi lim). NOTE: the values key is the hyphenated `prometheus-node-exporter:` (NOT `nodeExporter:` — that's kube-prometheus-stack's alias, absent here).
- vmstack values: `defaultRules.rules.RecordingRulesNoData.enabled: false` — chart's per-rule disable; real recording-rule breakage is still caught by `RecordingRulesError`/`AlertingRulesError`.

### Prevention
- CPU limits for poll-loop containers (git-sync, exporters) must be sized to the burst, not the idle; check `sum by (container) (rate(container_cpu_cfs_throttled_periods_total[15m])) / sum by (container) (rate(container_cpu_cfs_periods_total[15m]))` before trusting any CPUThrottlingHigh verdict.
- Chart components without explicit resources silently inherit the namespace LimitRange defaults — audit DaemonSets/Deployments from charts, not just hand-written manifests.
- A "no data" alert on a recording rule is ambiguous by design: empty result ≠ broken rule. Before killing such an alert, run the rule's expr manually and check `vmalert_recording_rules_last_evaluation_samples` per rule (label `recording`) — if the zero-sample rule is a `count(...)` of an absent condition, the alert is structurally un-satisfiable on a healthy system.
