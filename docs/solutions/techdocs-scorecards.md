# techdocs scorecards

## TechDocs per service + catalog scorecards (annotation scheme + facts matrix)

### Design

Phase 4 item 2 (commit a131165): every catalog Component (11 of them) has a TechDocs set and a scorecard.

**Annotations = truth, docs = human view.** Each service component carries:

- `homelab.io/image-pinned`: `"true"` | `"false"` | `"chart"` (chart = Helm chart version is the pin)
- `homelab.io/has-slo`: `"true"` | `"false"`
- `homelab.io/recovery: git"` (fleet-wide: repo is the source of truth; PV contents = accepted loss, 2026-09-29 decision)

Chose plain string annotations over the Backstage scorecards plugin: annotations render in search/Inspect with zero extra backend memory (disk-first cluster), and values come from repo facts, not runtime guesses. Each `services/<name>/docs/index.md` opens with a Scorecard table (Check/Status/Notes) mirroring the annotations; `services/homelab-platform/docs/index.md` holds the fleet-wide rollup (umbrella component deliberately has NO scorecard annotations).

**Docs placement rule**: `services/<name>/` is the TechDocs home even when the workload's manifests live elsewhere. Docs-only dirs for argocd, homelab-platform, jenkins, openobserve (manifests at `infrastructure/openobserve/`), paperless (deprecated, no app). Safe because nothing points an Application at those paths, and coexistence of `mkdocs.yml`+`docs/` inside a Directory-type Application dir is proven by audiobookshelf (synced/healthy for months). CI's TechDocs job sweeps `find services/ -name mkdocs.yml` and strict-builds everything found.

### Facts matrix (2026-10-14, from repo, not runtime)

| Service | pinned | SLO | notes |
|---|---|---|---|
| argocd | chart 10.2.1 | yes | UI http://192.168.0.108:31991, https 30443 |
| audiobookshelf | true (digest-only) | no | app Suspended by design; :30030 |
| authentik | true (2025.2.4 digests) | no | postgres parked, WAL reset pending; ns `default`; :30080 |
| backstage | true (CI sha tag ddf415be) | yes | :30900; restart via `rollout/backstage` (chart Deployment is a 0-replica shell) |
| jenkins | false | no | chart `targetRevision: "*"` + `lts-jdk21` — the fleet's only real pin gap; :30808 |
| kavita | true (0.9.1@sha256:31181a32…) | yes | :30050 |
| linkding | true (1.46.2@sha256:0c0a9a04…) | yes | :30090 |
| openobserve | true (v0.14.6) | no | observability probing itself is circular; :30500 |
| paperless | false | no | deprecated 2026-09-28, scaled to zero, PVs kept |
| petal | false (DELIBERATE :latest) | yes | user choice; sidecars pinned petal-dind 27.5.1, git-sync v4.1.0; :30990 |

NodePort map (verified from repo values): 30030 audiobookshelf, 30080 authentik, 30090 linkding, 30050 kavita, 30300 grafana, 30500 openobserve, 30808 jenkins, 30900 backstage, 30990 petal, 31991 argocd http, 30443 argocd https.

### Gotchas

- Existing docs had stale `:latest` image lines (kavita/audiobookshelf/linkding) — corrected to the digest-pinned refs from the deployment manifests; keep docs image fields synced with manifests when bumping.
- `homelab.io/image-pinned: "chart"` needs quotes in YAML ("true"/"false" strings would otherwise parse as booleans and Backstage rejects non-string annotation values).
- Verify ingestion: `kubectl -n backstage exec backstage-postgresql-0 -- env PGPASSWORD=backstage psql -U backstage -d backstage_plugin_catalog -t -A -c "SELECT entity_ref FROM refresh_state WHERE entity_ref LIKE 'component:%'"` — expect 11 after the wildcard location refreshes.
