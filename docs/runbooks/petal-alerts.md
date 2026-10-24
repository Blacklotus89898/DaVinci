# petal alerts

## Petal alerts — PetalPipelineFailing, PetalQueueStuck, PetalNightlyValidateMissing

Defined in Homelab `services/petal/observability.yaml`. They're built on Petal's `/metrics`, which is computed from its SQLite run registry at scrape time and scraped every 60s by VMServiceScrape `petal`. All three are `severity: warning` and go to ntfy.

### PetalPipelineFailing
- **Means:** the latest finished run of `{pipeline}` from `{repo}` failed. Either it failed after its last success, or it has never succeeded. Resolves on the next successful run of that pipeline and repo.
- **Triage:** open https://petal.lab.home and click the newest failed run. The run's `error` names the failed step (`petal-checkout` means the clone or ref failed, otherwise it's a pipeline step), and each step's log is at `/api/runs/<id>/logs/<step>`.
- **Common causes:**
  - `checkout X@Y failed`: the ref is gone, GitHub is unreachable, or the repo isn't public.
  - A pipeline step failed: the actual check failed (for example yamllint found something). That's the alert working.
  - `pipeline "x" not found`: the repo has no `.petal/pipelines/x.yaml` at that ref.
  - `interrupted: the control plane restarted…`: a deploy or pod restart killed the run mid-flight. Petal uses a Recreate rollout and closes orphaned `running` runs at startup (since b88b336+). Re-run it.

### PetalQueueStuck
- **Means:** `petal_runs_queued > 0` continuously for 30m. Nothing is claiming runs.
- **Triage:**
  - `kubectl -n petal get pods`: is the pod 3/3?
  - DinD: `kubectl -n petal logs deploy/petal -c dind --tail=50`. It's known to crash 2–3× after a cold node reboot, which self-heals.
  - Petal: `kubectl -n petal logs deploy/petal -c petal --tail=50`.

### PetalNightlyValidateMissing
- **Means:** there's been no successful `validate` run on repo `homelab` for 26h. The declared schedule `decl-homelab-validate-nightly` (03:17 UTC, in `services/petal/repos-configmap.yaml`) didn't fire or didn't succeed.
- **Triage:**
  - `curl -s https://petal.lab.home/api/schedules`: `decl-homelab-validate-nightly` should be there, enabled, with `next_run_at` in the future.
  - If it's missing: the config wasn't synced. Petal only syncs at startup, so restart the pod after a ConfigMap edit (`kubectl -n petal rollout restart deploy/petal`).
  - If it's present: was Petal down at 03:17? A missed minute is skipped, not caught up. Or did the run fail? Then PetalPipelineFailing fires too.
  - Manual run: Backstage → "Run a Petal pipeline" → homelab-platform → `validate`.
