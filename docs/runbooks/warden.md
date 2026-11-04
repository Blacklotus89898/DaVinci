# warden

## Warden — incident-investigation agent runbook

Warden (github.com/Blacklotus89898/Warden, ns `warden`, Homelab ADR 0011) receives Alertmanager webhooks for warning and critical alerts. For each one it collects read-only evidence (pods, logs, events, ArgoCD apps, nodes, the alert's metric, knowledge-base hits), journals the bundle in SQLite, and pushes a redacted digest to ntfy topic `warden`. It never changes anything in the cluster.

### Look at what it did
```
kubectl -n warden port-forward svc/warden 18080:8080
curl localhost:18080/investigations          # recent bundles
curl localhost:18080/investigations/<id>     # one full evidence bundle
kubectl -n warden logs deploy/warden -c warden
```

### WardenNotifyFailing
Digests aren't reaching ntfy, but bundles are still journaled. Check the ntfy pod in ns `monitoring` and the `notify.error` events. Nothing is lost; read the bundles through the API.

### WardenSkippingAlerts
Each `alert.skipped` event is journaled with a `reason`:
- `daily cap 40 reached`: more than WARDEN_DAILY_CAP alerts today. That's usually a real storm. Fix the storm and don't raise the cap.
- `queue full`: an investigation is stuck (each collector has a 20s timeout). Check the logs.
- `disabled`: WARDEN_ENABLED=false. Every firing alert is skipped, so this alert fires while Warden is switched off. That's expected.
- `dedup store: …`: SQLite error, so check the PVC and disk.
Re-sends of an alert already handled (same fingerprint and startsAt) are dropped silently and never count as skips.

### Off switch
Set `WARDEN_ENABLED: "false"` in services/warden/deployment.yaml, then commit and push. Warden journals each alert as skipped and does nothing else. To remove it from the alert path entirely, delete the `warden` route in apps/victoria-metrics/values.yaml. The route uses `continue: true`, so ntfy paging never depends on Warden.

### Boundary checks
```
kubectl auth can-i get pods/log --as=system:serviceaccount:warden:warden -A    # yes
kubectl auth can-i get secrets  --as=system:serviceaccount:warden:warden -A    # no
kubectl auth can-i create pods/exec --as=system:serviceaccount:warden:warden -A # no
```
Even with a broader RBAC role, Warden's code can only GET the five path patterns in internal/kube (pinned by boundary_test.go).

### Knowledge-base sidecars
Two git-sync sidecars (`kb-davinci`, `kb-homelab`) mirror docs every 10 minutes into an emptyDir. If KB hits are empty, check `kubectl -n warden logs deploy/warden -c kb-davinci`.
