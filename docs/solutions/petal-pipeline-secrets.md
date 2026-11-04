# petal pipeline secrets

## Petal pipeline secrets (ADR 0006) — how they flow and the dind mount gotcha

**What:** Petal steps request `secrets: [name]` and, separately, `docker: true` (the DinD daemon). The grants live in `services/petal/repos-configmap.yaml`: per repo `secrets:` + `docker:` at `trusted_refs` (default = `default_ref`), and `self.secrets` / `self.docker` for the git-synced Petal repo. The key is `trusted_refs`. The old `secret_refs` existed for one day and now FAILS config load (KnownFields), and Petal exits at startup on a config error. Values are keys of the `petal-secrets` SealedSecret (`services/petal/sealed-secrets.yaml`), mounted at `/etc/petal-secrets`, passed with `-secrets /etc/petal-secrets`. A step sees `/run/petal/secrets/<name>` (read-only) and `PETAL_SECRET_<NAME>_FILE`. Only `docker: true` steps get `DOCKER_HOST` and host networking; others run on dind's bridge.

**Gotcha — mount in BOTH containers:** step containers are children of the dind sidecar, which resolves bind-mount sources in its own mount namespace. A Secret mounted only into the petal container fails with "bind source path does not exist". The same volume is mounted at the same path in dind. No `subPath` (it freezes rotations); `optional: true` so the pod starts before unsealing.

**Refusals (by design):**
- `step "x" requests secret "y" / requests docker, which repo "r" at ref "<ref>" is not granted` → the ref isn't in `trusted_refs` (a branch, tag, or even main's SHA), or the grant is missing. Fix the grant in git, then restart Petal (the config is read at startup).
- `secret "y": not readable` → the key is missing from `petal-secrets`; re-seal with `kubeseal --controller-namespace sealed-secrets --format yaml --merge-into services/petal/sealed-secrets.yaml` (the controller ns is `sealed-secrets`, not kube-system; without `--format yaml` the file is rewritten as JSON).

**Verify:** run `secrets-e2e` on homelab@main. The log must show `value in the log: ***` and a hash prefix; `without-secret` must pass.

**Boundary (see the correction chunk for the bypass history):** a docker-granted trusted ref reaches every secret and the node through the privileged daemon. Grant docker only where you'd grant everything (today: self + petal, for docker-image). Trusted refs mean something only while branch protection on them is enforced; as of 2026-10-25 admin pushes bypass it on Homelab and Petal. Masking is best-effort.

## Correction 2026-10-25: docker access is a grant — and the dind 0.0.0.0 listener trap

**The first version was bypassable.** Every step had `DOCKER_HOST` + host networking, and dind can see `/etc/petal-secrets`, so a pipeline on an ungranted branch read a secret with `docker run -v /etc/petal-secrets:/s alpine cat /s/<key>` — unmasked (that run had no secrets loaded). Ref grants on secret *requests* alone mean nothing while any step can drive the daemon.

**Fix (Petal 0a11440):** `docker: true` on a step requests the daemon; repos-configmap grants `docker: true` per repo (and `self.docker`) at `trusted_refs` (renamed from `secret_refs` — an old `secret_refs` key now fails config load). Non-docker steps run on dind's default bridge without `DOCKER_HOST`; dind listens on `127.0.0.1:2375` only.

**Trap — dind crashloop "listen tcp 127.0.0.1:2375: bind: address already in use":** the docker:dind entrypoint prepends `--host=unix:///var/run/docker.sock --host=tcp://0.0.0.0:2375` whenever the container args start with a flag. Passing `dockerd` as the FIRST arg skips those defaults (`args: [dockerd, --host=tcp://127.0.0.1:2375, --host=unix:///var/run/docker.sock, --storage-driver=fuse-overlayfs]`). Verify with `kubectl -n petal logs deploy/petal -c dind | grep "API listen"` → only 127.0.0.1 and the socket.

**Verify the boundary:** on a throwaway branch, a step without `docker: true` must see `DOCKER_HOST` unset and fail to reach tcp://127.0.0.1:2375, tcp://172.17.0.1:2375 and the socket; a `docker: true` step on that branch must be refused before running. Bridge egress (git clone, pip) works.

**Sealing:** `kubeseal --controller-namespace sealed-secrets --format yaml --merge-into services/petal/sealed-secrets.yaml` — without `--format yaml` the file is rewritten as JSON; comments are always dropped (so the file has none).
