# pod cleanup kubectl path

## pod-cleanup CronJob fails: exec stat /bin/sh or /kubectl not found

### Symptom
`kubectl -n kube-system get events | grep pod-cleanup` shows, per tick every 15 min:

- `exec: "/bin/sh": stat /bin/sh: no such file or directory` (old template, shell wrapper), or
- `exec: "/kubectl": stat /kubectl: no such file or directory` (bad path fix)

…ending in `BackoffLimitExceeded`. `cronjob.status.lastSuccessfulTime` frozen while `lastScheduleTime` keeps advancing. App itself can still read Synced/Healthy — ArgoCD CronJob health does not always surface it; check events/jobs, not just app status.

### Root cause
`registry.k8s.io/kubectl` (v1.34.5) is a shell-less image: no `/bin/sh` (so `sh -c '… || true'` wrappers can't run) and **kubectl lives at `/bin/kubectl`** — that path is the image's `ENTRYPOINT`. `/kubectl` does not exist (my c1a0270 assumed it did without verifying; that "fix" just swapped one exec error for another and burned 6 days of ticks).

### Fix (commit dbe0fcd)
Invoke the binary directly per phase — `command: ["/bin/kubectl"]` + plain `args` (delete pods --all-namespaces --field-selector=status.phase=<Succeeded|Failed|Unknown> --ignore-not-found). One container per phase: pod field selectors have no OR. `--ignore-not-found` replaces `|| true`. Verify RBAC separately: `kubectl -n kube-system auth can-i delete pods --as=system:serviceaccount:kube-system:pod-cleanup -A`.

### Verify any remote image's entrypoint/binary path WITHOUT pulling it
```
manifest=$(curl -sL -H "Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json" https://registry.k8s.io/v2/kubectl/manifests/v1.34.5)
cfg=$(echo "$manifest" | python -c "import json,sys; print(json.load(sys.stdin)['config']['digest'])")
curl -sL https://registry.k8s.io/v2/kubectl/blobs/$cfg | python -c "import json,sys; c=json.load(sys.stdin)['config']; print(c.get('Entrypoint'), c.get('Cmd'))"
```
registry.k8s.io 302-redirects to `us-east4-docker.pkg.dev` — `curl -L` is required (a plain GET returns empty/non-JSON). This is a metadata read, not an executable download.

### Prevention
Never guess binary paths in shell-less images — read the image config (above) or `kubectl run --rm -it --image=… -- <default-entrypoint> args` as a probe. ttlSecondsAfterFinished=60 deletes job+pod evidence fast: grab `kubectl get events` immediately, or watch the next tick live.
