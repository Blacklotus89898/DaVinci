# TLS Certificate Expired / Expiring Soon

## Symptom
Clients see `x509: certificate has expired or is not yet valid`, TLS handshake failures, or browser cert warnings for a service endpoint.

## Root Cause
A cert-manager `Certificate` failed to renew (ACME challenge failure, rate limit, DNS record issue) or a manually-managed cert simply passed its expiry without automated renewal.

## Fix
```bash
# check expiry directly
echo | openssl s_client -connect <host>:443 -servername <host> 2>/dev/null | openssl x509 -noout -dates

# if using cert-manager, check the Certificate/CertificateRequest status
kubectl get certificate -n <ns>
kubectl describe certificate <name> -n <ns>
kubectl get certificaterequest -n <ns>

# force cert-manager to retry issuance
kubectl delete certificaterequest <name> -n <ns>
```

## Prevention
- Alert on certificate expiry at 30/14/3 days out, not just on failure.
- Prefer cert-manager (or equivalent) with automated renewal over manually-issued certs.
- Monitor ACME challenge success rate so a broken DNS-01/HTTP-01 setup is caught before the cert actually expires.
