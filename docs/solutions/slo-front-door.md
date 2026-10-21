# slo front door

## Front-door SLOs (blackbox-exporter + VMProbe + burn-rate alerts)

Front-door SLO monitoring: blackbox-exporter probes the six in-cluster service endpoints, `VmRule front-door-slo` computes a 99%/30d availability error budget with multiwindow burn-rate alerts paging via ntfy. Landed 2026-10-08 (commits dba7544, f562051, 2acb572; docs ed6b6a9).

### What exists where
- `infrastructure/blackbox-exporter/` — plain-manifest app (no chart): Deployment (v0.28.0 pinned, ClusterIP svc :9115, 10m/32Mi), ConfigMap (module `http_2xx`, ip4 preferred, follow_redirects for the /login 302s), `VMProbe front-doors`, `VmRule front-door-slo`.
- Targets (verified live): petal, backstage:7007, kavita-service:5000, linkding-svc:9090, argocd-server, victoria-metrics-grafana — all in-cluster DNS. **Excluded by design**: authentik (postgres parked at replicas=0 pending WAL recovery), audiobookshelf (app Suspended).
- No gateway-api exists yet → the services ARE the front doors (decision log 2026-10-08). External reachability stays uptime-kuma's job. Re-target at HTTPRoutes when a gateway lands.

### SLO design
- Objective: 99% availability / 30d. Recordings `slo:frontdoor:error_ratio_rate{5m,30m,1h,6h}` = `1 - avg by (instance) (avg_over_time(probe_success[W]))` (probe_success is a gauge, not a counter).
- Alerts: fast burn 14.4x (1h AND 5m) → critical 2m; slow burn 6x (6h AND 30m) → warning 15m; hard down `min_over_time(probe_success[2m]) == 0` → critical; p99 latency `quantile_over_time(0.99, probe_duration_seconds[10m]) > 1` → warning 10m. All route to ntfy via the default vmalertmanager route (severity none/info blackholed upstream).

### Alert triage
1. **SloFrontDoorDown** (critical): the instance label names the dead service. `kubectl -n <ns> get pods,svc` + `kubectl -n argocd get app <app>` first — most causes are a failed GitOps sync or a crashed pod, not the network.
2. **SloFrontDoorFastBurn** (critical): >14.4x budget consumption — the service is badly broken RIGHT NOW (for 2m+). Treat as page-worthy even if it flaps.
3. **SloFrontDoorSlowBurn** (warning): sustained 6x burn — a slow leak (restart loops, intermittent 5xx). Check `probe_duration_seconds` trend in Grafana before it becomes a fast burn.
4. **SloFrontDoorLatencyHigh** (warning): p99 >1s for 10m. Genuine slowness or a probe-path change (e.g. a redirect target got slow).
- Verify state: `kubectl get --raw "/api/v1/namespaces/monitoring/services/vmsingle-vmstack:8428/proxy/api/v1/query?query=probe_success"` (expect 6 series = 1; instance label = target URL).

### Gotchas that cost two CI rounds (2026-10-08)
- **VM operator CR spellings differ from prometheus-operator muscle memory**: `vmProberSpec` (NOT `vmProber`), `interval:` (NOT `scrapeInterval`), `targets.staticConfig.targets` (object wrapping the array). API group `operator.victoriametrics.com/v1beta1`.
- **The datreeio CRDs-catalog really validates VM CRs**: `Validate PR` kubeconform passes `-ignore-missing-schemas` but the second `-schema-location` (datreeio catalog, lowercase kind filenames) carries real operator.victoriametrics.com schemas — VM CRs are NOT skipped, and `-strict` enforces them. The catalog is OLDER than the live CRD: it rejects the newer `targets.static` alias (`additionalProperties: false`), so use `staticConfig` which both accept.
- Local replica of the CI check (kubeconform.exe v0.8.0): `find services/ infrastructure/ platform/ -name '*.yaml' | xargs kubeconform.exe -strict -summary -ignore-missing-schemas -schema-location default -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'` — 85/85 valid.
- Wiring is automatic: vmstack vmalert/vmagent run `selectAllByDefault: true`, so any-namespace VmRule/VMProbe is picked up with no selector config. Status condition: `vmstack.monitoring.vmagent.victoriametrics.com/Applied` / `...vmalert...` reason `ConfigParsedAndApplied`.

### Related
- solutions/ntfy-alert-pipeline.md (the paging path these alerts ride)
- solutions/uptime-kuma.md (external prober — complementary scope)
