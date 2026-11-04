# dind mtu tls timeout

## Petal step containers: "TLS handshake timeout" to some hosts (DinD bridge MTU 1500 vs flannel 1450)

### Symptom
A Petal step on the DinD bridge (any step without `docker: true`) fails on some HTTPS hosts and not others. First seen 2026-11-03 in Tulip's backtest pipeline: `Get "https://query1.finance.yahoo.com/...": net/http: TLS handshake timeout`. In the same run, `go build` fetched modules from proxy.golang.org fine.

### Root cause
The pod's `eth0` has **MTU 1450** (flannel VXLAN). DinD's `docker0` defaults to **1500**. Step containers send 1500-byte packets that get dropped on the overlay, and PMTU discovery doesn't recover them. Small TLS handshakes fit under 1450; a large server certificate chain (Yahoo's) doesn't, so the handshake stalls.

Diagnosis, run from the dind container:
```sh
ip link show eth0 | head -1                                    # mtu 1450
docker network inspect bridge -f '{{json .Options}}'           # driver.mtu 1500
docker run --rm curlimages/curl:8.11.1 -s -o /dev/null -m 20 -w '%{http_code}\n' https://query1.finance.yahoo.com/v8/finance/chart/XIU.TO               # 000
docker run --rm --network host curlimages/curl:8.11.1 -s -o /dev/null -m 20 -w '%{http_code}\n' https://query1.finance.yahoo.com/v8/finance/chart/XIU.TO   # 200
```

### Fix
Add `--mtu=1450` to the dind `dockerd` args in Homelab `services/petal/deployment.yaml` (commit fef18af). After the restart, `docker network inspect bridge` shows 1450 and the bridge curl returns 200.

### Prevention / related
- Any Docker-in-Docker inside a k3s pod (ARC runners, Jenkins agents) needs `--mtu` set to the pod network's MTU.
- A step that pipes into `tee` needs `set -o pipefail`, or a failed command reports success. Tulip's backtest pipeline got that fix in commit 6a0f234. Its summary step checks for the expected tables, which is how this failure was caught.
