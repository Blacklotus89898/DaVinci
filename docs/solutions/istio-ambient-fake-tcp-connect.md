# istio ambient fake tcp connect

## Istio ambient: TCP port checks from a mesh-enrolled pod always succeed (false UP)

### Symptom
uptime-kuma (namespace `monitoring`, enrolled in Istio ambient via `istio.io/dataplane-mode: ambient` on the ns) reported **UP** for TCP "port" monitors against hosts that were offline (no ping, no open ports from the LAN). HTTP and DNS monitors from the same pod were correct.

### Root cause
In ambient mode, ztunnel captures **outbound** TCP from enrolled pods and completes the client's TCP handshake locally before (failing to) reach the real destination. A raw `connect()` therefore succeeds to *anything* — offline host, unused IP, closed port. Protocols that need a real response (HTTP status, DNS answer over UDP) are not fooled; bare TCP-connect checks are.

Proof (2026-10-21, Istio 1.30.3, k3s 1.34):
```bash
# from an enrolled pod — all "succeed"
kubectl -n monitoring exec uptime-kuma-0 -- node -e "const s=require('net').connect({host:'192.168.0.250',port:22,timeout:4000},()=>{console.log('CONNECTED');s.destroy()});s.on('error',e=>console.log('ERR '+e.code))"
# -> CONNECTED   (unused IP!)   same for 192.168.0.108:1 (closed port)
# from a non-mesh pod (ns petal) the same connects FAIL
```

### Fix
Take the prober pod out of the mesh with a pod-template label (Homelab commit d9bdd9b, `infrastructure/uptime-kuma/statefulset.yaml`):
```yaml
template:
  metadata:
    labels:
      istio.io/dataplane-mode: none
```
After the restart: offline hosts -> `EHOSTUNREACH`, real ports -> connect. Ambient redirection annotation disappears from the pod.

### Prevention
- Any synthetic prober (uptime-kuma, blackbox `tcp_connect` modules, custom health checkers) must run **outside** the ambient mesh, or use protocols that require an application response.
- blackbox-exporter in `monitoring` is fine today only because it uses `http_2xx` exclusively — do NOT add a `tcp_connect` module there without opting the pod out.
- Related: Homelab ADR 0009 (Istio front door), `tools/uptime-kuma/monitors.yaml`.
