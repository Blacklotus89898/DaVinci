# staging rehearsal

## Staging rehearsal lane (ApplicationSet + kustomize overlay)

### What the staging lane is

`apps/staging/applicationset.yaml` (list generator) stamps the kustomize overlays under `environments/staging/<app>/` into their own namespaces, so upgrades get rehearsed before promotion. Root app-of-apps applies the ApplicationSet CR via its `apps/` directory recursion; the ApplicationSet controller owns the generated `staging-<app>` Applications (they are invisible to root's directory scan — the two mechanisms don't fight; verified no root wedge 2026-10-14).

Pilot: linkding. Adding a rehearsal target = one element in the list + one overlay dir; nothing else.

### How to rehearse an upgrade

1. Find the candidate's digest (registry tags API gives `digest` per tag).
2. Edit `environments/staging/<app>/kustomization.yaml` `images:` block — set BOTH `newTag` and `digest` (image is digest-pinned in the base; a tag-only override keeps pulling the old digest).
3. Commit + push. Watch `kubectl -n argocd get app staging-<app>` reach Synced/Healthy and `probe_success{job="staging-front-doors"}` return 1 in vmsingle.
4. Promote = same reference in the base `services/<app>/deployment.yaml` (+ any doc image lines); the overlay stays in lockstep so staging mirrors prod between rehearsals.

Pass criteria for a candidate: staging app Synced/Healthy + staging probe green with the candidate digest running. `StagingFrontDoorDown` (warning) fires if the candidate fails — that means REJECTED, do not promote.

### Design decisions (disk-first)

- Own namespace per overlay → no name fights with prod.
- PVC is deleted in the overlay (`$patch: delete`); an emptyDir stands in. The prod PVC is PV-bound (`spec.volumeName`), so a naive copy would fight over the PV; staging is deliberately stateless — it validates "new version serves", NOT data migrations.
- Service becomes ClusterIP, nodePort removed: nodePorts are cluster-global and prod owns 30090. Staging front doors are probed in-cluster only.
- Staging probes/alerts are a SEPARATE `VMProbe staging-front-doors` + `VmRule staging-front-door` (hard-down, warning severity, no SLO math): rehearsal targets must be able to fail loudly without burning the prod front-door error budget.
- Cost: one ~128Mi pod per rehearsed service + the candidate image layer (pulled once to the worker, reused at promotion). Zero PVC, zero nodePort.

### Gotchas (2026-10-14, all hit live)

- `$patch: delete` SMP must carry the BASE's namespace (`namespace: linkding`), not the overlay target ns — the delete matches before the namespace transform rewrites survivors.
- VMProbe `jobName:` IS the `job` label on the resulting `probe_*` series (`job="staging-front-doors"`), and the VMProbe shows ~17s delay before vmagent's first scrape — wait one interval before declaring the series missing.
- `services/<app>` gains a passthrough `kustomization.yaml` (same pattern as blackbox-exporter): render is semantically identical (kustomize normalizes field order + strips comments; ArgoCD diffs structurally → prod stays Synced). TechDocs `docs/` stays out of `resources:` — kustomize only builds what's listed.
- CI: kubeconform scans `environments/` too and renders+validates every `environments/staging/*/` overlay (`kubectl kustomize | kubeconform`); nodeport dup-grep covers `environments/`.

### First rehearsal (validated E2E)

linkding 1.46.2 → 1.47.0 (digest e35cb50e…): staging Synced/Healthy, staging probe = 1, prod probes 6/6 = 1 throughout, promotion rolled prod with the digest already cached on the worker (no second pull). Commits 9125a4f (lane), c63af50 (rehearsal), 2a03bb5 (promotion) — all CI 9/9 green.

## Staging rehearsal for hostPath-backed apps: kavita lane extension

### Extending the lane to a hostPath-backed service (kavita, 2026-10-19)

The staging-rehearsal pattern (see this file's main section) was designed around PVC-backed linkding. Kavita's prod storage is two **hostPaths** — a WRITABLE config dir (`/var/lib/kavita/config`) and a readOnly data mount (`/mnt/smb_storage`). Rules learned extending the lane:

- **The writable hostPath is the dangerous one**: upgrade candidates commonly run schema/config migrations on boot. If staging shares prod's config dir, the candidate migrates prod's config and you may not be able to roll back. Both volumes → `emptyDir` in the overlay (JSON-patch `replace` on `/spec/template/spec/volumes/N`, keep the volume names so mounts keep resolving).
- **The readOnly data hostPath is merely a scheduling pin** (node-local, SMB-pinned): dropping it to emptyDir makes staging schedulable anywhere. An empty library still proves "new version boots and serves HTTP" — which is the rehearsal's whole claim; data migrations are explicitly out of scope.
- **Downsize prod's incident-fat resources**: prod kavita carries 2 CPU/2Gi requests (from an OOM fix); the rehearsal overlay replaces resources with 100m/256Mi req, 1/1536Mi lim. Actual staging cost: 324Mi, 6m CPU. A candidate that OOMs in the small profile is a finding, not a false alarm — but size against the app's idle footprint, not prod's headroom.
- **No-PVC services skip the PVC-delete patch** — the overlay's patch list is per-service; only the Service ClusterIP/nodePort-strip and images-digest knob are universal.
- **Base needs a passthrough `kustomization.yaml`** if it was a plain Argo directory app (linkding/kavita pattern: list exactly deployment/service/limitrange, exclude `docs/`). Argo auto-detects kustomize; rendering is identical; verify prod app stays Synced/Healthy after the flip.
- Digest discipline: fetch the digest from the registry API into a shell var, assert it equals the base's pin, and heredoc it into the overlay — never hand-transcribe.
- Busybox `wget http://localhost:8428` inside the vmsingle pod → connection refused: busybox resolves localhost to ::1, vmsingle binds IPv4. Use `http://127.0.0.1:8428/api/v1/query`.
