# k3s node image prune

## Prune unreferenced container images on a k3s node (privileged pod)

### Purpose
Reclaim disk on k3s nodes by pruning unreferenced container images (needs root — no sudo on worker, so privileged pod trick). **Read `df` before and after.**

### Script
```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: worker-image-prune
  namespace: default
spec:
  nodeName: k3s-worker-01          # target node
  restartPolicy: Never
  containers:
  - name: prune
    image: busybox
    command: ["sh","-c","/usr/local/bin/k3s crictl --runtime-endpoint unix:///run/k3s/containerd/containerd.sock --timeout 60s rmi --prune; df -h / | tail -1"]
    volumeMounts:
    - {name: bin, mountPath: /usr/local/bin, readOnly: true}
    - {name: run, mountPath: /run/k3s}
    - {name: k3s, mountPath: /var/lib/rancher/k3s}
  volumes:
  - name: bin
    hostPath: {path: /usr/local/bin}
  - name: run
    hostPath: {path: /run/k3s}
  - name: k3s
    hostPath: {path: /var/lib/rancher/k3s}
EOF
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded pod/worker-image-prune --timeout=540s
kubectl logs worker-image-prune
kubectl delete pod worker-image-prune --wait=false
```

### Gotchas (learned 2026-09-28)
- **Run it twice.** First pass: each `RemoveImage` that follows heavy deletions can hit crictl's default 7s deadline (`DeadlineExceeded`) while containerd GC churns; pass 2 sweeps them and triggers the GC cascade (3.9G freed total on a 35G-full worker vs 0.9G after pass 1).
- `--timeout 60s` on the crictl call avoids the per-call 7s default.
- `rmi --prune` only removes images with **no containers** — safe for running workloads; scaled-to-zero apps (e.g. paperless) lose their image and re-pull on revival.
- `k3s crictl` requires `/var/lib/rancher/k3s` mounted RW (data-dir lock) and `/run/k3s` (containerd socket); listing images works with `--runtime-endpoint unix:///run/k3s/containerd/containerd.sock` even without the data dir.
- Deleting `mirrored-pause` is harmless: running sandboxes unaffected; kubelet re-pulls (300kB) on next sandbox creation.
