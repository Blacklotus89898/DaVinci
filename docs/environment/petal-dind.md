# petal dind

## Petal DinD Needs fuse-overlayfs — overlay2 Fails and vfs Explodes on Disk

### Symptom

Docker-in-Docker sidecar on the k3s worker either refuses to start with an
overlay2 storage-driver error, or starts on the `vfs` fallback and then fills
the disk — a 646MB engine image expanded past 8Gi, and image extraction ran
roughly 15x slower.

### Root Cause

The host's root filesystem is already overlayfs. Docker rejects
overlay2-on-overlay, so stock `docker:27-dind` silently falls back to `vfs`.
`vfs` does not use layers at all — it deep-copies every layer on every pull,
which multiplies image size and extraction time.

### Fix

Use the custom DinD image built from `services/petal/dind/Dockerfile`
(published as `ghcr.io/blacklotus89898/petal-dind:27.5.1`) and select the
driver explicitly:

```yaml
args:
  - --host=tcp://0.0.0.0:2375
  - --host=unix:///var/run/docker.sock
  - --storage-driver=fuse-overlayfs
```

Rollback if fuse-overlayfs misbehaves: `docker:27-dind` with
`--storage-driver=vfs`, and size the volume for the blowup.

### Prevention

Keep the image layer cache off the node root filesystem. It lives on the SMB
share via hostPath, not a PVC:

```yaml
- name: docker-lib
  hostPath:
    path: /mnt/smb_storage/petal-docker-lib
    type: DirectoryOrCreate
```

The kubelet evicts on root-fs pressure, and a 15Gi build cache sharing the OS
disk caused repeated disk-pressure incidents. The cache is disposable — the pod
re-pulls images if the share is unavailable.

## Petal Step Containers Resolve Bind Sources in the DinD Mount Namespace

### Symptom

A Petal pipeline step fails immediately with `bind source path does not exist`,
even though the path clearly exists inside the `petal` container.

### Root Cause

Petal creates step containers by calling the Docker API on the `dind` sidecar.
Those step containers are **dind's children**, so the daemon resolves bind-mount
*sources* in its own mount namespace — not Petal's. The step-runner passes
`/data/work/<run_id>` as the `/src` source, so that path has to exist in the
dind container too.

### Fix

Mount the same `petal-data` PVC at the same path in **both** containers:

```yaml
- name: dind
  volumeMounts:
    - name: data
      mountPath: /data     # must match the petal container exactly
- name: petal
  volumeMounts:
    - name: data
      mountPath: /data
```

The same rule bites under `deploy/compose.yaml`: with `/var/run/docker.sock`
mounted, the resolving daemon is the *host*, so `/data` must exist on the host
for step bind mounts to work. Prefer the k3s deployment for exercising the
step-runner.

### Related

Per-run workspaces live under the data PVC (`<data>/work/<run_id>`), not in the
git-sync checkout, so steps never write into the synced source tree. The
checkout is an `emptyDir` and is safe to lose — git-sync re-populates it on pod
start.
