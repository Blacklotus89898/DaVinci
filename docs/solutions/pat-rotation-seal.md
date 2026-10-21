# pat rotation seal

## Validate a GitHub PAT before kubeseal — scope errors are invisible to the seal pipeline

### Validate a GitHub PAT before kubeseal — scope errors are invisible to the seal pipeline

### Symptom
Heartbeat CronJob (arc-runners ns) fails with `remote: Invalid username or token. Password authentication is not supported for Git operations.` even though the SealedSecret sealed cleanly and the live secret is byte-identical to the token file.

### Root cause
The seal chain (file → kubectl dry-run → kubeseal → commit → ArgoCD → sealed-secrets controller) validates **nothing about the token itself**. A wrong-scope or truncated token passes through perfectly and only fails 30+ minutes later at the next cron fire — a long blind window. (2026-10-14: first re-seal d6565ab shipped a token GitHub rejected outright.)

### Fix / procedure
1. Token in a local file (e.g. `~/pat.txt`), never pasted into a transcript.
2. **Validate before sealing** — status must be 200 and classic PATs must show the needed scope in `X-OAuth-Scopes` (heartbeat push needs `repo`; fine-grained needs Contents read/write on Blacklotus89898/Homelab):
   ```bash
   tok=$(tr -d '\r\n' < ~/pat.txt)
   curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: token $tok" https://api.github.com/repos/Blacklotus89898/Homelab
   curl -sI -H "Authorization: token $tok" https://api.github.com/repos/Blacklotus89898/Homelab | grep -i x-oauth-scopes
   ```
3. Seal (controller namespace is `sealed-secrets`, NOT kube-system — kubeseal needs the flag):
   ```bash
   kubectl -n arc-runners create secret generic arc-runner-secret --from-literal=github_token="$tok" --dry-run=client -o yaml \
     | kubeseal --controller-namespace sealed-secrets --format yaml > infrastructure/arc-runners/sealed-github-pat.yaml
   ```
4. Verify: `grep -c ghp_` on the sealed file must be 0; `encryptedData:` present.
5. Commit → push → flip check (compare live secret data vs `printf %s "$tok" | base64 -w0`, echo match/mismatch only). Webhook sync lands it in ~20s; k8s cron fires in **UTC** (:07/:37Z for the heartbeat), so the proof beat is the first :07/:37Z after the flip.

### Gotchas
- kubeseal without `--controller-namespace sealed-secrets` errors `cannot get sealed secret service` and **truncates the output file** — `git checkout --` the manifest before retrying.
- Do not revoke the previous PAT until a beat lands with the new one.
