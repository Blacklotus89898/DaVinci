# image digest rot and pvc volumename

## Pinned image digest rots while prod runs from cache; PVC volumeName blocks rebuild

Found by the 2026-11-05 k3d bootstrap rehearsal on ubuntu.home (Homelab PRs #20, #21; `docs/bootstrap.md` → Rehearsal).

### Symptom
- Fresh cluster: authentik pods `ImagePullBackOff`, `NotFound` for `ghcr.io/goauthentik/server@sha256:3623…`. Prod looked fine because the worker had the image cached.
- Fresh cluster: `linkding-pvc` Pending forever ("unbound immediate PersistentVolumeClaims"), because the manifest pinned `volumeName: pvc-26195814…`, a PV that only exists on the current cluster.

### Root cause
- ghcr dropped the authentik 2025.2.4 multi-arch index; the `2025.2.4` tag now resolves to an **arm64-only** manifest. Re-pinning to the tag's digest crashlooped on amd64 with `exec format error`. The version label matched, so checking the label alone was not enough.
- linkding: once `volumeName` was removed from git, ArgoCD server-side apply (argocd-controller is the sole owner of the field) tried to clear the immutable field on the bound PVC: `spec is immutable after creation`. `ignoreDifferences` + `RespectIgnoreDifferences=true` did NOT prevent it.

### Fix
- authentik: pin the same index digest from Docker Hub, `docker.io/beryju/authentik@sha256:36233579415aa2e2e52a6b0c45736cb871fe71460bfe0cf95d83f67528fb1182` (has linux/amd64; byte-identical to what prod ran).
- linkding: drop `volumeName`, keep `/spec/volumeName` in ignoreDifferences, and opt the PVC out of SSA with the annotation `argocd.argoproj.io/sync-options: ServerSideApply=false`. Client-side apply leaves fields it never applied alone (no last-applied annotation exists).

### Prevention
- Before trusting a rebuild, check every pinned digest still resolves AND contains linux/amd64. Read the manifest's `mediaType`: a single `manifest.v2` (not index) needs its config's `architecture` checked.
- Anonymous ghcr check: token from `https://ghcr.io/token?scope=repository:<repo>:pull`, then `curl -sI -H "Authorization: Bearer $t" -H "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json" https://ghcr.io/v2/<repo>/manifests/<digest>`.
- Never pin `volumeName` on dynamically provisioned PVCs in git.
- In PowerShell, don't name a function `man` (it's an alias for help).
