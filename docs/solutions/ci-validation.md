# ci validation

## Validate PR pipeline (yamllint + kubeconform + Kyverno CLI)

### What runs

`Validate PR` workflow (`.github/workflows/validate-pr.yaml`) — 8 jobs on push/PR/dispatch to main:
- **YAML lint**: `yamllint -c .yamllint .` (relaxed config; replaces old safe_load check)
- **Kubernetes manifests**: kubeconform v0.8.0 pinned, `-strict -ignore-missing-schemas` + datree CRDs-catalog
- **Kyverno policies**: `kyverno apply policies/ -r` over a render of the repo the way ArgoCD consumes it (kustomize dirs rendered, bare dirs copied flat with `tr '/' '_'`; gateway-api render excluded — upstream content)
- Helm templates, TechDocs, NodePort-conflict grep, Trivy IaC, Summary

Kyverno policies in `policies/` (CI-applied, not installed in-cluster — disk constraint): `disallow-latest-tag`, `require-container-resources`, `require-probes`, all Enforce-mode rules with explicit per-kind container paths (no autogen). petal excluded from latest/probes rules (deliberate dev float + unprobed git-sync sidecar).

### Failure lessons from the first red run (run 36654791899, 2026-09-30)

- **yamllint EOF hygiene**: default `empty-lines: max-end: 0` + `new-line-at-end-of-file` flagged 17 files (16 trailing blank line at EOF, 1 missing final newline). Fixed in commit b4a8eb7 with a python pass that collapses trailing blanks and ensures the final newline.
- **kyverno CLI tarball member name**: the v1.19.1 tarball contains `kyverno`, NOT `kubectl-kyverno`. GNU tar exit 2 = "member not in archive". Definitive plain-text source for release-tarball contents without downloading anything: the krew-index manifest `https://raw.githubusercontent.com/kubernetes-sigs/krew-index/master/plugins/kyverno.yaml`.
- **NodePort grep double-count**: extracting ports must grab ONLY the `nodePort: <value>` fragment — `grep -rhoE 'nodePort: *[0-9]+' ... | grep -oE '[0-9]+'` — because the line's comment often repeats the port (and node IP) in a URL, and whole-line digit extraction counts those too.
- **Test the exact pipeline locally before pushing**, not an approximation of it: the round-2 "fix" was still wrong because I'd only tested the OLD pipeline locally.

### Anonymous GitHub failure diagnostics (no auth available)

- Job-logs API (`.../jobs/{id}/logs`) returns 403 "Must have admin rights" anonymously.
- **Check-run annotations work anonymously**: `GET /repos/{owner}/{repo}/commits/{sha}/check-runs` → for each failed check run, `GET /repos/{owner}/{repo}/check-runs/{id}/annotations`. The check-runs LIST response has NO `annotations_url` key — build the URL from `cr["id"]`.
- Annotation lists are capped/deduped (showed 9 file errors when a repo-wide local scan found 17) — treat them as hints, not ground truth.
- When polling runs by head SHA, **filter on workflow `name == 'Validate PR'`**: Release Please also triggers on push to main and will masquerade as the verdict.

### Green state

Run 36655446842 (commit d150752, 2026-09-30): all 8 jobs success. Workflow treats GitHub runners fetching pinned tools inside job steps as the sanctioned path (local binary downloads are denied on this host).
