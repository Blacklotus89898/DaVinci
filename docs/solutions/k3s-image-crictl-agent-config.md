# k3s image crictl agent config

## crictl inside a rancher/k3s pod fatals without the agent config

### Symptom

A pod using the `rancher/k3s` image to run node-level `crictl` (image prune, etc.) against the host containerd dies with:

```
time="..." level=warning msg="Config \"/var/lib/rancher/k3s/agent/etc/crictl.yaml\" does not exist, trying next: \"/bin/crictl.yaml\""
time="..." level=fatal msg="load config file: stat /bin/crictl.yaml: no such file or directory"
```

Passing `--runtime-endpoint unix:///run/k3s/containerd/containerd.sock` does NOT help.

### Root cause

In the k3s image, `crictl` is k3s's multi-call binary. It insists on loading k3s's agent config (`/var/lib/rancher/k3s/agent/etc/crictl.yaml` — which k3s writes on the HOST, pointing at the sock) and fatals when it can't find any config. The flag doesn't skip config load.

### Fix

Mount the host's k3s agent config dir into the pod at the same path (readOnly), alongside the containerd socket:

```yaml
volumeMounts:
  - name: containerd-sock
    mountPath: /run/k3s/containerd
  - name: k3s-agent-etc
    mountPath: /var/lib/rancher/k3s/agent/etc
    readOnly: true
volumes:
  - name: containerd-sock
    hostPath: { path: /run/k3s/containerd, type: Directory }
  - name: k3s-agent-etc
    hostPath: { path: /var/lib/rancher/k3s/agent/etc, type: Directory }
```

Then plain `crictl rmi --prune` works. Working examples: `infrastructure/node-prune/` CronJobs + one-shot Jobs (first failure and fix both on 2026-10-15).

### Gotchas

- Wrap a one-shot's crictl call so failure is visible: v1 "completed" green while its prune had fataled, because a trailing `|| echo` let the script exit 0. Prefer letting it fail loudly (no `||` guard) so the Job status tells the truth.
- Jobs are immutable: to re-run a fixed one-shot, use a NEW name (e.g. `-2`) and remove the old file in the same commit.
