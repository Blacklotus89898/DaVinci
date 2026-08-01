# cert-manager Certificate Not Issuing

## Symptom
`kubectl get certificate` shows `READY: False` indefinitely, or a Certificate that was working starts failing to renew. Distinct from an already-expired cert — see [[runbooks/tls-certificate-expiry.md]] for that case.

## Root Cause
- ACME HTTP-01 challenge can't be reached: ingress not routing `/.well-known/acme-challenge/*` to the solver pod (common when a WAF or auth layer intercepts it first)
- DNS-01 challenge: DNS provider API credentials expired/wrong, or the TXT record propagation is slower than cert-manager's poll timeout
- Rate limited by the CA (Let's Encrypt's staging vs prod rate limits — check if `letsencrypt-staging` issuer was accidentally used, or prod rate limit hit from repeated failed attempts)
- `ClusterIssuer`/`Issuer` misconfigured (wrong `server` URL, expired ACME account, missing `email`)
- cert-manager webhook pod itself is unavailable, so CRD validation/admission fails silently

## Fix
```bash
# Check Certificate + CertificateRequest + Order + Challenge chain (in order)
kubectl get certificate,certificaterequest,order,challenge -n <namespace>
kubectl describe certificate <cert-name> -n <namespace>
kubectl describe challenge -n <namespace>

# cert-manager controller logs almost always name the exact failure
kubectl logs -n cert-manager deploy/cert-manager --tail=200 | grep -i <cert-name>

# For HTTP-01: confirm the solver pod/ingress path is actually reachable externally
kubectl get pods -n <namespace> | grep cm-acme-http-solver
curl -v http://<domain>/.well-known/acme-challenge/<token>

# For DNS-01: verify the TXT record was actually created
dig +short TXT _acme-challenge.<domain>

# Check which issuer is referenced — staging certs look "issued" but browsers reject them
kubectl get certificate <cert-name> -n <namespace> -o jsonpath='{.spec.issuerRef.name}'

# Force a retry by deleting the stuck CertificateRequest (cert-manager recreates it)
kubectl delete certificaterequest <cr-name> -n <namespace>
```

## Prevention
Point `spec.issuerRef` at the staging ACME server while testing, and only switch to the production issuer once the flow is verified end-to-end — this avoids burning Let's Encrypt's production rate limits (5 duplicate certs/week per exact domain set) on iteration.
