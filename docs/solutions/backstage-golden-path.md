# backstage golden path

## Backstage golden path — catalog ingestion via wildcard location (github provider rejected)

**Design (live 2026-10-14, commit 8ec8478):** the scaffolder template `templates/homelab-service/template.yaml` publishes `apps/<name>/`, `services/<name>/`, and `catalog/components/<name>.yaml` as a PR; ArgoCD converges the Application; the catalog ingests the new component via ONE static wildcard location in `apps/backstage/values.yaml`:

```yaml
catalog:
  locations:
    - type: url
      target: https://github.com/Blacklotus89898/Homelab/blob/main/catalog/components/*.yaml
```

The last path segment may be `*` (GitHub tree read) — a scaffolded service drops a file in `catalog/components/` and the next refresh ingests it with **no hand-editing of any registry file**. The old `catalog/all.yaml` static registry is now unreferenced (kept in git as an inert file).

### Rejected: the github catalog provider (failed live, with DB evidence)

`providers.github.homelab` with `organization: 'Blacklotus89898'` + `catalogPath: '/catalog/components/*.yaml'`:

- **`organization` on a USER does an org-style member sweep** — Blacklotus89898 is a user, not an org; the provider scanned 62 collaborator repos (tsechris, lectern, taran9999, …) creating 63 `location:default/generated-<sha>` Location wrappers as junk catalog entities.
- **`catalogPath` globs are NOT expanded** — targets came out with literal `*` (`.../blob/main/catalog/components/*.yaml`) and never resolved: **zero Components ingested via the provider**; the catalog was fed entirely by the static all.yaml fallback.
- Ingestion-domain globs belong to `catalog.locations` (tree read), not to provider `catalogPath`.

### Verification recipe (Backstage 1.53 per-plugin DBs)

Backstage 1.53 gives each plugin its OWN postgres database (`backstage_plugin_catalog`, `backstage_plugin_auth`, …) — there is no single `backstage` DB of tables, no `entities` table, and `final_entities` has no `spec` column. Query the real schema:

```bash
PGPASSWORD=$(kubectl -n backstage get secret backstage-postgresql -o jsonpath='{.data.password}' | base64 -d)
kubectl -n backstage exec backstage-postgresql-0 -- env PGPASSWORD="$PGPASSWORD" psql -U backstage -d backstage_plugin_catalog -t -A -c \
  "SELECT r.entity_ref FROM refresh_state r"
# spec fields: r.unprocessed_entity::json->'spec'->>'target';  managed-by-location: final_entities.final_entity::json->'metadata'->'annotations'
```

Orphaned provider Locations are swept by the `catalog_orphan_cleanup` task (~30s cadence) after the location disappears from config.

### Gotchas that cost debugging time

- **ArgoCD multi-source apps: `.status.sync.revision` (singular) is EMPTY** — revisions live in `.status.sync.revisions[]` (array, chart-ref source first). Rollout watchers must key on rollout phase/RS, not app revision.
- **`Validate PR` triggers ONLY on PR→main, push→main, workflow_dispatch** (`.github/workflows/validate-pr.yaml`) — a bare push to a feature branch creates ZERO check-runs; to CI-proof a branch, run the checks locally (kubeconform v0.8.0 replica command in solutions/ci-validation.md; Jenkins file: python yaml.safe_load_all + nodePort dup grep) or rely on the PR itself. A scaffolder PR to main DOES trigger CI.
- **Dry-run E2E of a scaffolder output**: template locally on a throwaway branch, validate with local kubeconform (Application CR shows as skipped — ignore-missing-schemas, same as CI), then delete the branch. gh CLI is unauthenticated here so PRs must come from Backstage (it has the PAT) — the first real scaffold via the UI is the true CI-on-PR proof.
- **`$text:` placeholders in catalog API entities** (apis.yaml fetching swagger from in-cluster NodePorts) need the NodePort host in `backend.reading.allow` (added `192.168.0.108`) or every refresh warns NotAllowedError.
- **Helm 4 + `--repo` flag on this box fails** (`no cached repo found: .../vm-index.yaml`); render from the tgz downloaded from the GitHub release URL found in https://backstage.github.io/charts/index.yaml instead.
- **values.yaml app-config changes propagate**: commit → ArgoCD sync rewrites the `backstage-appconfig` Secret → checksum annotation change on the adopted Deployment template → Rollout canary dance (setWeight 50 → SLO AnalysisRun gate → 5-min pause → promote). Portal serves throughout.

## Post-deploy verification (wildcard location live)

### Post-deploy verification (2026-10-14, live)

- Rollout promoted 8ec8478 (stableRS 79d497b774 → 76ccc8d887, phase Healthy, SLO gate green, portal served throughout).
- **Wildcard glob works**: all 9 Components re-ingested, each with `location_key = url:https://github.com/Blacklotus89898/Homelab/tree/main/catalog/components/<file>.yaml` — note the resolver rewrites `blob/*` to per-file **tree** URLs; the multi-doc component (`homelab-platform` inside jenkins.yaml) comes through correctly.
- **Provider junk swept by orphan cleanup** within minutes of the new backend boot: refresh_state went from 63 provider Location rows to 0 (`location_key LIKE 'github-provider%'`), total locations 5 (4 statics + the wildcard root).
- **`reading.allow` fix verified on a clean-boot pod**: a canary pod started seconds after the ConfigMap apply still warned NotAllowed for a few minutes (consistent with kubelet configmap-volume propagation lag — treat first-canary warnings as stale); the pod that booted after the CM settled has NEVER warned; 0 warnings across all pods post-promotion over multiple refresh cycles.

### Gotchas that cost debugging time
