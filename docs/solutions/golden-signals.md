# golden signals

## Golden-signal and pod-kill alerts (ContainerOOMKilled, ContainerMemoryNearLimit, ContainerMemoryLeakPredicted, ContainerRestartingOften, ServiceErrorRateHigh, ServiceLatencyHigh)

Rules: `infrastructure/victoria-metrics/golden-signals-rules.yaml`. Dashboard: Grafana → **Service health** (uid `service-health`, `golden-signals-dashboard.yaml`). Warning alerts push to ntfy `homelab-alerts`, and Warden attaches an evidence bundle. `ContainerRestarted` is `info` and stays in Grafana.

### Where each signal comes from
- **Traffic, errors and latency** come from the Istio gateway's Envoy: `istio_requests_total` and `istio_request_duration_milliseconds_bucket` with `reporter="source"`, per `destination_service_name`. The mesh is ambient, so calls inside the cluster don't appear there; only front-door traffic does. Most of that traffic is blackbox probes (about 0.04 req/s per service), which is why every rule has a minimum request count.
- **Saturation** is container CPU and memory as a share of their limits, plus node CPU, memory and disk.
- **Uptime** is `probe_success` from the VMProbes `gateway-front-doors` and `front-doors`.

### Triage
- **ContainerOOMKilled or ContainerMemoryNearLimit:** first check whether memory grows over days (a leak) or sits flat near the cap (the limit is too small). Use the "Memory trend vs limit" panel. If it's flat, raise `resources.limits.memory` in git. For precedents, see `runbooks/cert-manager-cainjector-oom.md` (limit too small) and `solutions/limitrange-max-vs-metrics-store.md` (a LimitRange max capping the container).
- **ContainerMemoryLeakPredicted:** the 6h linear trend reaches the limit within 6h, and usage is already above 75%. Restarting buys time; the real fix is finding what grew.
- **ContainerRestartingOften:** 3 or more restarts in an hour. Run `kubectl -n <ns> logs <pod> -c <container> --previous` soon, because the previous container's logs disappear after the next restart. Then check the events (`kubectl -n <ns> get events --field-selector involvedObject.name=<pod>`).
- **ServiceErrorRateHigh:** more than 10% 5xx with at least 20 requests in 10 minutes. Grafana sometimes returns 5xx when a long 30-day query times out, which is user-driven and not an outage.
- **ServiceLatencyHigh:** p95 above 3s for 15 minutes. Grafana is excluded, because its p95 measures user queries (up to 9s). Warden's `/map` normally peaks around 2.3s. Check CPU throttling in the Saturation panel first.

### Calibration (2026-11-12, replayed over 7 days before merging)
Firings that would have occurred in that week: OOM 0, near-limit 0, leak 0, restart burst 1 (petal git-sync, 5 restarts in an hour), 5xx about 1 (Grafana), latency 0.

**Close calls:** linkding was at 89% of its memory limit (staging at 87%). Raising its limit prevents the first page.

**Gotchas when writing these queries:** aggregate both sides with `max by (namespace,pod,container)` before dividing cAdvisor by kube-state-metrics; raw series carry different extra labels and match nothing. Also, `kubectl get --raw` hides PromQL error bodies; port-forward vmsingle and use `curl --data-urlencode` to see them.
