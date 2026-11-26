# authentik warden console setup

## authentik gotchas: Warden console forward auth, app passwords, recovery keys (2025.2.4)

Found while setting up the Warden console (Homelab ADR 0011 amendments 2026-11-24 (2) and 2026-11-25) on 2026-11-25. Setup steps live in Homelab `services/warden/docs/index.md`.

### Proxy provider has no signing-key field → empty JWKS
- Symptom: `/application/o/warden-console/jwks/` returns `{}`; Warden says "not signed in" (it only accepts RS256/ES256 via JWKS).
- Cause: in 2025.2 the proxy provider UI and API expose no `signing_key`, so tokens are HS256 with the client secret.
- Fix (the model has the field):
```
kubectl -n default exec deploy/authentik-worker -- ak shell -c "from authentik.providers.proxy.models import ProxyProvider as P; from authentik.crypto.models import CertificateKeyPair as K; p=P.objects.get(name='warden-console'); p.signing_key=K.objects.get(name='warden-console-signing'); p.save()"
```

### Login redirect goes to http://192.168.0.112:30080 (plain HTTP)
- Cause: the **embedded** outpost builds the redirect from its config `authentik_host`; `authentik_host_browser` is ignored for it.
- Fix: embedded outpost config `authentik_host: https://auth.lab.home` (takes effect within seconds, no restart).

### Terminal client (HTTP Basic + app password) needs two things
- Gateway: Istio ext_authz `includeRequestHeadersInCheck: [cookie, authorization]` (apps/istio/values.yaml).
- Provider: **Intercept header authentication** on (only on `warden-console`).
- The outpost exchanges Basic creds via `/application/o/token/` (client_credentials). A wrong password shows only as `invalid_grant` in authentik-server logs and a 302 to login; a first success can be served from the outpost's cache, so test more than once.
- Check: `warden-k9s whoami` (Warden `contrib/k9s`).

### `ak create_recovery_key <N> <user>`: N is YEARS, not hours
- `ak create_recovery_key 1 akadmin` makes a link valid for **one year**. Delete leftovers after use: Directory → Tokens (intent `recovery`), or `Token.objects.filter(intent='recovery').delete()` in `ak shell`.
- The admin user is `akadmin` (the only real user).

### Windows curl and the lab CA
- Windows curl (Schannel) fails `CERT_TRUST_REVOCATION_STATUS_UNKNOWN` on `*.lab.home` because the Homelab Root CA has no CRL. Use `--ssl-no-revoke --cacert lab-root-ca.crt`. Public CA cert: `kubectl -n cert-manager get secret lab-root-ca -o jsonpath='{.data.ca\.crt}' | base64 -d`.
