# ntfy alert pipeline

## Alertmanager → ntfy push pipeline (homelab-alerts)

### What's deployed (2026-09-29, roadmap Phase 1 item 2)

Alert pipeline: **vmalert (evaluates VMRule CRs) → vmalertmanager (routes) → ntfy → phone push.**

- **ntfy**: `binwiederhier/ntfy:v2.28.0` (official image, pinned), plain-manifest app `ntfy` (infrastructure/ntfy + apps/ntfy) in `monitoring` ns, NodePort **30310** → http://192.168.0.108:30310. No PVC (emptyDir cache, 12h) — disk constraint. No auth configured: topics open on LAN (accept for 192.168.0.0/24).
- **Phone**: ntfy app → server http://192.168.0.108:30310 → topic `homelab-alerts`.
- **Routing** (apps/victoria-metrics/values.yaml `alertmanager.config`): default receiver ntfy (webhook POST, raw JSON body as message — v1, formatting bridge deferred to Phase 2), `max_alerts: 3`, `send_resolved: true`, group_wait 30s / group_interval 5m / repeat 4h.
- **Blackholed** (no integrations): Watchdog, severity=none (InfoInhibitor), severity=info (CPUThrottlingHigh), KubeMemoryOvercommit (structural on this cluster), namespace=arc-systems (arc-controller deliberately Suspended → chronic replica mismatch).
- **k3s false-positive class**: `kubeScheduler.enabled: false` + `kubeControllerManager.enabled: false` — k3s embeds both; standalone-pod scrapes can never see targets and KubeSchedulerDown/KubeControllerManagerDown fire forever.

### End-to-end test (the proof)

```bash
# 1. inject a synthetic alert straight into alertmanager
kubectl -n monitoring port-forward svc/vmalertmanager-vmstack 19093:9093 &
curl -X POST http://localhost:19093/api/v2/alerts -H 'Content-Type: application/json' \
  -d '[{"labels":{"alertname":"ClaudeWiringTest","severity":"warning","namespace":"monitoring"},"annotations":{"summary":"e2e test"},"startsAt":"2026-09-29T04:24:09Z"}]'
# 2. after group_wait (30s), read the topic
kubectl -n monitoring port-forward svc/ntfy 18080:80 &
curl 'http://localhost:18080/homelab-alerts/json?poll=1&since=10m'
```
Verified 2026-09-29: ClaudeWiringTest arrived on homelab-alerts with title "Homelab alert".

### Gotcha: values.yaml changes to the victoria-metrics app can look "stuck" for minutes

Pushed values.yaml changes DO auto-apply, but with real latency (ArgoCD refresh interval + operator applying the generated config secret — observed several minutes). During that window the app shows "Synced Healthy" while the live config still has the old values, which reads exactly like a failed apply. Verified 2026-09-29: a test push applied itself within ~7 min, no manual sync involved.

- Check what WOULD render before assuming failure: `argocd app manifests victoria-metrics | grep <marker>`
- Live vmalertmanager config: gzipped in secret `vmalertmanager-vmstack-config` (key `alertmanager.yaml.gz`) — `kubectl -n monitoring get secret vmalertmanager-vmstack-config -o jsonpath='{.data.alertmanager\.yaml\.gz}' | base64 -d | gunzip -c`
- `argocd app sync victoria-metrics` just skips the wait (and recovers genuinely stuck/failed operations: `argocd app terminate-op` first if one hangs — solutions/victoria-metrics-stack.md symptom 3).

### Alert hygiene model going forward

- Push channel (ntfy) = warning/critical only. info/none/structural → blackhole routes (each with a comment saying WHY).
- Every VM defaultRule alert carries `runbook_url` → runbooks.prometheus-operator.dev. Custom rules should link OUR KB runbook paths instead (ongoing, applies as custom rules get written).
- Adding a blackhole: add a route under `alertmanager.config.route.routes`, keep `- name: blackhole` receiver defined, commit, push, verify the decoded secret (above).

## Publishers on topic homelab-alerts

### Publishers on topic homelab-alerts (2026-10-07: three now)

1. **vmalertmanager** (metrics alerts: OOMKill, disk >80%, node NotReady …) — max 3 notifications per alert, send_resolved; structural false-positives blackholed.
2. **uptime-kuma** `:30320` — external prober for NodePorts + control-plane TCP 6443.
3. **ArgoCD notifications** — on-sync-failed / on-health-degraded, global subscription, webhook straight to ntfy (see `solutions/argocd-notifications-ntfy.md`). E2E-verified 2026-10-07.

One phone subscription to `homelab-alerts` covers all three. ArgoCD messages are distinguishable by title prefix `ArgoCD sync failed:` / `ArgoCD degraded:` and the `argocd` tag.
