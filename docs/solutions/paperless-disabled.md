# paperless disabled

## Paperless disabled (scaled to zero) — crashloop root cause and revival path

### Symptom
`paperless-5cc5456869-*` pod in CrashLoopBackOff for 21 days (172 restarts). Crash log: `django.core.exceptions.ImproperlyConfigured: PAPERLESS_SECRET_KEY is not set or is the default 'change-me' value`.

### Root cause
The paperless stack (paperless + postgres + redis, namespace `paperless`) was deployed **out-of-band ~105 days ago and never adopted into GitOps** — no `apps/paperless/`, no ArgoCD Application. Deployment image was `ghcr.io/paperless-ngx/paperless-ngx:latest`; when a pod reschedule pulled a newer image 2026-09-07ish, the new version enforced `PAPERLESS_SECRET_KEY`, which was never set.

### Decision (2026-09-28)
User confirmed the service is unused. Stack **disabled, not deleted**:
- `kubectl -n paperless scale deployment paperless postgres redis --replicas=0`
- Backstage catalog lifecycle flipped to `deprecated` (commit `d57b13f`, Homelab repo)
- **Retained**: all 6 PVCs (paperless-media 10Gi, paperless-data/consume/export 2Gi, postgres 5Gi, redis 1Gi), services, NodePort 30070, traefik ingress `paperless.local`

### Re-enable procedure
1. `kubectl -n paperless scale deployment postgres redis --replicas=1` — wait for Running.
2. BEFORE scaling paperless up, fix the secret: generate `python3 -c "import secrets; print(secrets.token_urlsafe(64))"`, seal via kubeseal against the sealed-secrets controller, add as env `PAPERLESS_SECRET_KEY` (secretKeyRef) on the deployment.
3. Pin the image off `:latest` (running digest was `sha256:5fa76604a81d...e74e79`) to avoid repeat breakage.
4. `kubectl -n paperless scale deployment paperless --replicas=1`.
5. Ideally adopt the whole stack into `apps/paperless/` (Application + manifests) so it is GitOps-managed like everything else.

### Prevention
Never run `:latest` tags on workloads; adopt all live deployments into ArgoCD (drift audit: only the paperless ns was unmanaged).
