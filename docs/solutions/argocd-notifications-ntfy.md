# argocd notifications ntfy

## ArgoCD notifications to ntfy (global subscription)

### What this is

ArgoCD (argo-helm `notifications:` block in `apps/argocd/values.yaml`) pages on **on-sync-failed** and **on-health-degraded** via a webhook straight to in-cluster ntfy (`http://ntfy.monitoring.svc.cluster.local`, POST `/`, JSON body, topic `homelab-alerts`). Global subscription — every Application alerts, no per-app annotations needed. E2E-verified 2026-10-07: failed op at rev d400924 → message on topic at 03:00:41Z.

### Symptom (why nothing sent — two silent bugs)

1. **No log at all, no send**: `subscriptions:` in `argocd-notifications-cm` used the per-app annotation style with a `destinations:` key. The engine's global-cm parser (`DefaultSubscription`, notifications-engine `pkg/subscriptions/global.go`) unmarshals ONLY `recipients` / `triggers` / `selector` — `destinations:` is **silently dropped**, leaving zero recipients. Trigger matches, nothing sends, no log line.
2. **`notification service 'webhook.ntfy' is not supported`**: recipients were written `webhook.ntfy`. `ParseConfig` (notifications-engine `pkg/api/config.go`) splits the cm key `service.webhook.ntfy` on `.` → serviceType=`webhook`, **name=`ntfy`** (last segment). The service registers as plain `ntfy`; a recipient `webhook.ntfy` misses the registry.

### Fix (both in `apps/argocd/values.yaml`)

```yaml
notifications:
  subscriptions:          # global-cm style: recipients, NOT destinations
    - recipients:
        - ntfy            # bare service name, NOT webhook.ntfy
      triggers:
        - on-sync-failed
        - on-health-degraded
```

Recipient strings are `service:recipient` pairs split on `:` — for a webhook with no channel the whole string is just the service name. Commits: ab97247 (recipients format), 033ff6a (bare `ntfy`).

### Working config shape (for reference)

- `notifiers.service.webhook.ntfy` → url `http://ntfy.monitoring.svc.cluster.local`, header `Content-Type: application/json`
- triggers: each value is a **LIST of conditions**; `when:`/`for:`/`oncePer:` are siblings, not part of the expression. on-sync-failed: `app.status.operationState.phase in ['Failed','Error']`, oncePer `app.status.operationState.syncResult.revision`; on-health-degraded: health `Degraded` `for: 5m`, oncePer `app.status.sync.revision`
- templates: `webhook.ntfy.method: POST`, `path: /`, body is one JSON line: topic/title/message/tags/click; `{{.context.argocdUrl}}` feeds the click URL

### JSON-safety rule for ntfy templates

Interpolate ONLY enum-safe values (app name, phase, sync status, revision). Never `operationState.message` or any free text — a quote/backslash breaks the JSON and ntfy rejects the publish.

### How to E2E-verify (throwaway app pattern)

1. App pointing at a path whose manifest targets a namespace that is deliberately never created (`CreateNamespace=false`) → every sync fails `NamespaceNotFound`.
2. Bump a label in the failing manifest **per retry** — fresh revision ⇒ fresh `oncePer` value ⇒ the dedup marker from a previously-failed send doesn't block the retry (after a failed send the controller still writes the `notified.notifications.argoproj.io` marker keyed `<rev>:<trigger>:<cond-hash>:<recipient>` — a new revision or a changed recipient produces a new key).
3. Expect the op to sit `Running` through ArgoCD's auto-sync retry ladder ("Retrying attempt #N at …", ~exponential) before it terminates `Failed` and the trigger fires. Don't panic at the wait.
4. Verify three ways: controller log `Sending notification … to '{ntfy }'` with no following error; the `notified` annotation carries the new rev key; ntfy cache `GET /homelab-alerts/json?poll=1&since=30m` shows the message.
5. Delete the app dir from git (root app prunes it) when done.

### Reading the controller log correctly

`Found phase 'Running', SyncStatus is assumed to be up-to-date` = non-terminal op phase, notification evaluation skipped — normal mid-operation. `Trigger … FAILED` lines are the normal negative case for healthy apps, not errors. Debug logLevel (`notifications.logLevel: debug`) is only worth enabling while diagnosing; revert after.
