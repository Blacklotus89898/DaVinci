# backstage reading allow port

## Backstage $text placeholder NotAllowedError despite host in reading.allow

### Symptom
Catalog logs warn on every refresh: `Placeholder $text could not read location http://192.168.0.108:31991/swagger.json, NotAllowedError ... add it to backend.reading.allow`. This happens even though `backend.reading.allow` already contains `- host: 192.168.0.108`. The API entity pages show no definition.

### Root cause
`reading.allow[].host` is matched against the URL's host **including the port**. A bare IP or hostname only allows the default port, 80 or 443. This silently broke `argocd-api` and `openobserve-api` from 2026-10-14 to 2026-11-06.

A second bug sat behind the first one. OpenObserve v0.14.6 serves its OpenAPI spec at `/api-doc/openapi.json`, not `/api-json`, which returns 404. Its Swagger UI is at `/swagger/`.

### Fix (Homelab PR #27)
In `apps/backstage/values.yaml`:
```yaml
reading:
  allow:
    - host: raw.githubusercontent.com
    - host: 192.168.0.108:30500 # openobserve
    - host: 192.168.0.108:31991 # argocd
```
In `catalog/apis.yaml`, set the openobserve `$text` to `http://192.168.0.108:30500/api-doc/openapi.json`.

### Verify
Test the URL from the pod. A 200 here rules out a wrong path, so any remaining error is the allow-list:
```bash
kubectl -n backstage exec deploy/backstage -- node -e "fetch('http://192.168.0.108:31991/swagger.json').then(r=>console.log(r.status))"
kubectl -n backstage logs deploy/backstage --since=10m | grep NotAllowedError   # expect nothing after rollout
```

### Prevention
Every new NodePort spec URL needs its own `host:port` allow entry.
