# petal multi repo runs

## Petal multi-repo runs (ADR 0004) — how it works and the traps found shipping it

### How it works (live 2026-10-24, Petal 456e289)
- A run can name `repo` (an alias in the allowlist) and `ref` (branch, tag or SHA; defaults to the repo's `default_ref`). Without `repo`, the run uses the git-synced self repo exactly as before.
- **Allowlist = trust boundary:** Homelab `services/petal/repos-configmap.yaml`, mounted at `/etc/petal/repos.yaml` (`-repos` flag). Only public https repos. Petal reads the file at startup, so **restart the pod after editing it**.
- **Checkout** is a synthetic first step `petal-checkout` (a reserved step name) that runs pinned `alpine/git:2.54.0` through the normal step path:
  - blob-less clone, then resolve `origin/<ref>` first and a tag or SHA after that;
  - `^{commit}` peels annotated tags;
  - detached checkout.
  The resolved SHA goes into `runs.git_sha` and a `source.resolved` event. URL and ref are passed as env vars, never interpolated into the script.
- The pipeline spec loads from the run's own tree (`.petal/pipelines/<name>.yaml` in that repo).
- **Backstage:** a component opts in with the annotation `petal.io/repo: <alias>`. `petal:run` looks it up through the catalog service and refuses a component without one. The template's EntityPicker filters on `metadata.annotations.petal.io/repo: {exists: true}`.
- Smoke test without Backstage: `POST /api/runs {"pipeline":"validate","repo":"homelab"}` with the bearer token (secret `petal-api-token` in ns petal).

### Trap 1 — busybox `grep --include` silently passes the check
In an `alpine` step, `grep -r ... --include="*.yaml"` is unsupported, so grep prints its usage text and outputs nothing. A "find duplicates" check then saw an empty list and reported **"no conflicts"** (run_9ada32dccd6ef469).
- Fix: select files with `find ... -exec grep -h ... {} +`.
- Make "zero matches" a hard failure, so a broken check can't pass.

### Trap 2 — whole-line number grep double-counts ports in comments
`grep -oE '[0-9]+'` over `nodePort:` lines also caught ports repeated in trailing comments (`# http://192.168.0.108:30310`) and reported false duplicates (run_53eb0ed73729cdf7).
- Fix: `grep -ohE 'nodePort: *[0-9]+'`, then extract the number. Same pattern as CI's validate-pr job.

### Prevention
Before trusting a check that reports "nothing found", always read the step log of its first real run.

## Petal modular steps, needs DAG, timeouts, cancel (ADR 0005) — and the cancel-during-create container leak

### What exists (Petal 62daabb, live 2026-10-24)
- **`uses:`** reusable steps live in `.petal/steps/<name>.yaml` in the same repo. `inputs` (with `default`/`required`) reach the container as `INPUT_*` env. There's no nesting. The pipeline step's `name` keys its logs, so one library step can be used several times. Example: Homelab `.petal/steps/yamllint.yaml`.
- **`needs:`** either no step declares it (file order, one at a time, the original behaviour), or **every** step does (`needs: []` for roots). A mix is a load error. DAG mode runs ready steps in parallel up to `max_parallel` (default 2, cap 4). Fail-fast cancels running siblings, and never-started steps get `step.skipped`.
- **Timeouts:** step (30m default), run (60m default) and checkout (5m). The pool keeps a 2h safety ceiling. Errors name the limit.
- **Cancel:** `POST /api/runs/<id>/cancel` with the bearer token:
  - queued → 200 (atomic UPDATE);
  - running → 202 (cause `cancel requested`; containers killed, unstarted steps skipped, run ends `canceled`);
  - finished → 409.
- Verified live: Homelab `validate`'s two roots started 1ms apart, and three cancels (at +0s, +2s and +5s into the step) all ended `canceled` with 0 leftover containers.

### Gotcha 1 — cancelled contexts eat your audit events
After cancel, any store write on the run's ctx fails, so `step.finished {canceled}` and `step.skipped` silently vanish. Every executor registry write now uses `context.WithoutCancel(ctx)`. The unit test asserts the events exist after a cancel.

### Gotcha 2 — cancel during ContainerCreate leaks a container
In the first live cancel test (run_b5779f5cd2c96aa4), the daemon had created `petal-run_<id>-yamllint` (state `Created`), but the client only saw ctx's error, so no remove was ever registered.
- Fix: a failed create force-removes the container by name.
- Every run also ends with a sweep of containers labelled `petal.run=<id>`.
- Check: `kubectl -n petal exec deploy/petal -c dind -- docker ps -aq --filter label=petal.run | wc -l` should be 0 when nothing is running.

### Known cost
Every Petal deploy (Recreate rollout, single SQLite writer) causes ~2 minutes of downtime and pages SloFrontDoorDown for petal (both the in-cluster and the gateway probe).
