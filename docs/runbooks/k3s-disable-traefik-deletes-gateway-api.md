# k3s disable traefik deletes gateway api

## Disabling k3s traefik deletes the Gateway API CRDs (and every Gateway/HTTPRoute)

### Symptom
After adding `disable: [traefik]` to `/etc/rancher/k3s/config.yaml` and restarting k3s, every `*.lab.home` URL got **connection refused**, including the old gateway NodePort (:30328). The istio-gateway pod was Running with endpoints, but `pilot-agent request GET listeners` showed only 15090/15021 (no 80/443). `kubectl -n istio-system get gateway lab` returned NotFound.

### Root cause
k3s runs two packaged charts, `traefik` and `traefik-crd`. **`traefik-crd` also owns the standard-channel Gateway API CRDs** (gateways, httproutes, gatewayclasses, grpcroutes, referencegrants, backendtlspolicies). With traefik disabled, k3s runs `helm-delete-traefik-crd`, which deletes those CRDs, and Kubernetes then deletes every Gateway and HTTPRoute. The experimental-channel CRDs (tcproutes, tlsroutes, udproutes, listenersets) survived, because the ArgoCD `gateway-api` app installed them.

ArgoCD's `gateway-api` app re-created the CRDs within about a minute (check `kubectl get crd | grep gateway.networking`: the creationTimestamp equals the restart time). The `gateways` app then **deadlocked** on the re-create:
- the HTTPRoutes were in the default wave and waited to become Healthy;
- an HTTPRoute is only Healthy once a parent Gateway accepts it;
- the Gateway was in sync-wave 4, so it was never applied;
- the operation sat at "waiting for healthy state of HTTPRoute/... and 11 more", pinned to the old revision.

### Fix
1. Put the HTTPRoutes after the Gateway: `argocd.argoproj.io/sync-wave: "5"` on every route (Homelab commit 198a0fa).
2. Terminate the stuck operation (ArgoCD UI → Terminate, or `argocd app terminate-op gateways`), then sync.
3. Verify: `kubectl -n istio-system get gateway lab` shows Programmed, and `curl https://argocd.lab.home/` returns 200.

### Prevention
- Before disabling traefik on any k3s cluster, make sure something else owns the Gateway API CRDs. Here that was the ArgoCD `gateway-api` app, and its selfHeal is what saved the CRDs. Ideally take ownership *before* the restart, so the objects never cascade-delete.
- Always order Gateway API routes after their Gateway in ArgoCD waves, or a fresh create can deadlock.
- A rebuild starts k3s with traefik already disabled (bootstrap/nodes/server/k3s-config.yaml), so this only bites the migration.
