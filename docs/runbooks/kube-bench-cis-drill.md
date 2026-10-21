# kube bench cis drill

## Quarterly CIS drill — kube-bench on k3s (first run 2026-Q4: 32 PASS / 28 FAIL / 56 WARN / 14 INFO)

## Quarterly CIS drill — kube-bench (k3s)

### What runs

`infrastructure/kube-bench/cronjob.yaml` (app `apps/kube-bench/`, ns `kube-system`) — aquasec/kube-bench v0.16.0 (digest-pinned), quarterly at 04:23 UTC on the 14th of Jan/Apr/Jul/Oct. Pinned to the control-plane node `debian` via `nodeName` + an `unschedulable` toleration (bypasses the cordon — the one-shot-pod trick made permanent). `hostPID: true` + read-only hostPaths at real paths (`/var/lib/rancher/k3s`, `/etc/systemd`, `/lib/systemd`, `/usr/bin` → `/usr/local/mount-from-host/bin`).

### Trigger an off-schedule drill (diagnostic Job, throwaway precedent)

```bash
kubectl -n kube-system create job --from=cronjob/kube-bench kube-bench-drill-<quarter>
kubectl -n kube-system get job kube-bench-drill-<quarter> -w
kubectl -n kube-system logs <exact-pod-name>   # NOT -l job-name=... (see gotcha 3)
```

Findings are data, not failure: the wrapper runs with `--exit-code 0` so a completed scan WITH findings is a Successful job; only a scan that cannot run fails. The wrapper self-reports the summary line to ntfy `homelab-alerts` (busybox `wget --post-data`/`--header` — the image has no curl).

### v0.16.0 CLI facts (learned by failing 2026-10-13)

1. **`--targets` lives on the `run` subcommand** (`kube-bench run --targets master,node`), not top-level — top-level use dies with `unknown flag: --targets`. We omit it: the default runs ALL files in `cfg/<version>/` = the full control-plane scan.
2. **Release-lag trap**: GitHub master `cfg/` contains k3s-cis-1.10/1.11, but the v0.16.0 **image** ships k3s benchmarks only up to **k3s-cis-1.9** — `--benchmark k3s-cis-1.11` fails with `error validating targets: No targets configured for k3s-cis-1.11`. Verify the cfg list inside the image, not on GitHub:
   `kubectl run kb-recon --rm -i --restart=Never --image=aquasec/kube-bench:v0.16.0 --command -- sh -c 'ls /kube-bench/cfg/'`
3. **`kubectl logs -l job-name=...` truncated the 549-line report to 10 lines** (seen 2026-10-13; watcher's `--tail=25` read more). Fetch by exact pod name for the full report.
4. `kubectl run --rm -i` attach is flaky (intermittent empty output) — reliable pattern: `--command -- sh -c 'sleep 2; ...; echo ===DONE==='`.

Working invocation (in the CronJob wrapper):

```
kube-bench run --benchmark k3s-cis-1.9 --exit-code 0
```

### First run — 2026-Q4 (2026-10-13, k3s v1.34.5, benchmark k3s-cis-1.9)

**Total: 32 PASS / 28 FAIL / 56 WARN / 14 INFO.** Per section: master 16/23/11/9, etcd 0/0/7/0, controlplane 0/0/5/0, node 16/1/2/5, policies 0/4/31/0. ntfy notification verified (server JSON ACK in pod log). Baseline — future drills diff against these numbers.

### FAIL triage (28 findings, 4 classes)

**Class A — k3s embedded flags invisible to `ps` (detection gap, ~17 findings).** k3s runs apiserver/controller-manager/scheduler as threads of one `k3s server` process; kube-bench audits process args, so defaults-set-in-code read as FAIL. Includes 1.2.6/1.2.7/1.2.8 (authorization-mode — `Node,RBAC` IS the k3s default) and 4.2.4 (read-only-port — k3s disables it). Spot-check where cheap: `kubectl auth can-i get pods --as=system:anonymous` → even SelfSubjectAccessReview is Forbidden, so 1.2.1 (anonymous-auth) has zero RBAC reach regardless of the flag. Others in this class (1.2.x cert/key/CA args, 1.3.3-1.3.5, profiling 1.2.15/1.3.2/1.4.1, ciphers 1.2.29) are unverified either way — treat the FAIL as "unknown," not "vulnerable."

**Class B — node file perms, real + fixable (3 findings: 1.1.9, 1.1.10, 1.1.20).** CNI config file perms/ownership (at the k3s path `/var/lib/rancher/k3s/agent/etc/cni/net.d/...`) and PKI cert perms 600 (`/var/lib/rancher/k3s/server/tls/...`). Node-level `chmod/chown` — not in the claude-ops sudoers grant, and a live node mutation outside git → user action or a deliberate future one-shot-pod fix. Re-check at next drill.

**Class C — kubelet (1 finding: 4.2.4)** — covered in Class A (invisible-flag class).

**Class D — RBAC hygiene, real + backlog (4 findings: 5.1.1 cluster-admin bindings, 5.1.3 wildcard Roles, 5.1.5 default SA in active use, 5.1.6 auto-mounted SA tokens).** Mostly chart-origin. Actionable in git over time: `automountServiceAccountToken: false` on our own manifests; wildcard roles belong to bundled charts (cert-manager, VM operator). Candidate Phase 3/4 follow-up, not an incident.

### Expectations for the next drill (2027-01)

Numbers should match unless something changed; investigate any delta. If kube-bench >v0.16.0 ships k3s-cis-1.10+, bump benchmark + digest together and re-baseline.
