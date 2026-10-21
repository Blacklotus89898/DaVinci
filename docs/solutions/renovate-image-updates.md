# renovate image updates

## Renovate on Homelab (hosted app) + image-pin policy

### Setup
- **Flavor**: Mend-hosted Renovate GitHub App — repo is public, so free, with ZERO cluster/CI footprint (no CronJob, no secrets). Config: `.github/renovate.json5` in the Homelab repo (pushed 2026-09-29, commit 37d9a76).
- **Activation (one-time, user UI step)**: https://github.com/apps/renovate → install on `Blacklotus89898/Homelab`. First run opens an `onboarding` PR; merging/renaming `renovate.json5` config starts real updates.
- **Policy**: weekly schedule (Mon ~05:00 UTC), PR-only — NO automerge (git-only rule: PR merge → ArgoCD applies). prHourlyLimit 2 / prConcurrentLimit 5 to trickle the backlog. Majors get a `major` label.

### Exclusions (deliberate, in packageRules)
- `services/petal/**` — petal image floats `:latest` BY USER DECISION while under active development (comment in `services/petal/deployment.yaml`). Never PR it.
- `apps/arc-controller/**`, `apps/arc-runners*/**` — app Suspended by user choice; bumps are noise until revived.

### Managers that matter here
- `docker` manager: plain-manifest `image:` fields in `infrastructure/` + `services/` (now all pinned tag@digest or digest-only).
- `argocd` manager: helm-chart Application sources — `victoria-metrics-k8s-stack 0.95.0` (apps/victoria-metrics/application.yaml), `gha-runner-scale-set-controller 0.10.1` (excluded while suspended).

### Image-pin sweep done same day (all = digests already running, so rollouts were no-download restarts)
- kavita `0.9.1@sha256:31181a…`, linkding `1.46.2@sha256:0c0a9a…` (tag names recovered by matching live pod digests against Docker Hub tag API)
- audiobookshelf digest-only `@sha256:180aca…` — running image PREDATES all current version tags (stale float); Renovate's first PR brings a real version
- pod-cleanup: `bitnami/kubectl:latest` → `registry.k8s.io/kubectl:v1.34.5` — official image, pinned to cluster k8s version (bitnami stopped publishing version tags, only `latest`+sha remain)

### Known gaps (flagged, not fixed here)
- **authentik runs unmanaged**: pod `ghcr.io/goauthentik/server:latest` in ns `default`, no ArgoCD app — only referenced by `catalog/components/authentik.yaml`. Adopting it into git is a separate roadmap decision.
- Juice-shop also floats `:latest` (demo app in default ns — low priority).
