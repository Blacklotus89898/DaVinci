# trivy action setup exit 6

## trivy-action fails in setup-trivy with exit code 6 (GitHub Actions)

### Symptom
The image CI job (Petal `build-petal.yaml`, 2026-11-03) goes red in "Scan image for vulnerabilities". The step `aquasecurity/setup-trivy` logs `found version: 0.76.1 for v0.76.1/Linux/64bit`, then `##[error]Process completed with exit code 6`. **Build and push already succeeded**, so the image exists; only the scan step and the overall check fail.

### Root cause
The binary download in trivy-action's bundled installer (`install.sh`, curl) fails. Curl exit 6 means "couldn't resolve host". **Pinning does not help**: `@master` (trivy v0.76.1) and the `v0.36.0` tag SHA (trivy v0.70.0) fail the same way. The problem is the installer path, not a specific release.

### Fix
Bypass the action and run Trivy from its pinned container image (Docker Hub pulls work on the runner):
```yaml
- name: Scan image for vulnerabilities
  id: trivy
  if: steps.build.outcome == 'success'
  run: |
    docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v "$PWD:/out" \
      aquasec/trivy:0.70.0 image --format sarif --output /out/trivy-image.sarif \
      --severity CRITICAL,HIGH --exit-code 0 \
      ghcr.io/<owner>/<image>:${{ github.sha }}
```
The socket mount lets PR builds scan the locally loaded image. The SARIF upload step works unchanged. Applied in Petal 3rd commit after 4a77199 (run 41760668110 green).

### Still exposed (as of 2026-11-03)
Homelab `build-backstage.yaml` and `validate-pr.yaml` ("Trivy IaC scan", a **required** check) still use trivy-action (pinned to v0.36.0 SHA `ed142fd`). They probably fail the same way. For the IaC scan, the equivalent is `aquasec/trivy:0.70.0 config ...` with the repo mounted.

### Prevention
Don't float third-party actions on `@master`. For scanners, prefer a pinned container image over an action that downloads binaries at runtime.

## Where the container fix is applied

As of 2026-11-03, no repo uses trivy-action:
- Petal `build-petal.yaml`: image scan via `aquasec/trivy:0.70.0 image`.
- Homelab `build-backstage.yaml`: image scan via `aquasec/trivy:0.70.0 image`, with the docker socket mounted.
- Homelab `validate-pr.yaml` "Trivy IaC scan" (a required check): `docker run --rm -v "$PWD:/repo" -w /repo aquasec/trivy:0.70.0 config --format sarif --output /repo/trivy-iac.sarif --severity CRITICAL,HIGH --exit-code 0 .`

Homelab PR #10 (merge f12afb4) shipped the last two; every check passed on the PR. To bump Trivy, change the image tag. There is no installer to break.
