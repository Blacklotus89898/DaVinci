# heartbeat sh filename too long

## cluster-heartbeat pods Error: "can't open ...: Filename too long" (sh invoked without -c)

### Symptom

- `cluster-heartbeat` CronJob fires on schedule (jobs appear) but every pod ends `Error` in a backoff loop; job never completes.
- Pod log is one line: `/bin/sh: can't open 'git config --global ... (entire script)': Filename too long`
- `heartbeat` branch on GitHub frozen at the baseline commit — no beats; the GitHub watchdog will eventually page "Cluster dark" (false).
- Notably the in-cluster ntfy fallback ALSO stays silent — the script never executes at all, so not even the failure path runs.

### Root cause

`command: ["/bin/sh", "-eu"]` with the script supplied as a k8s `args:` string-block. Without `-c`, sh interprets the first positional argument as the PATH OF A SCRIPT FILE to open — the whole multi-line script text as one filename → `ENAMETOOLONG`. Nothing inside the script runs.

### Fix

Commit 078875e: `command: ["/bin/sh", "-eu", "-c"]` — flags may be combined before `-c`; the args entry then becomes the script string.

```yaml
command: ["/bin/sh", "-eu", "-c"]   # -c is mandatory when the script rides in args
args:
  - |
    git config ...
```

### Prevention

- Any container whose script lives in `args:` MUST have `-c` as the last `command` entry. Review with: `kubectl -n <ns> get cronjob <name> -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].command}'` → expect `[..., "-c"]`.
- A pod log showing the whole script quoted after `can't open` is this exact signature — don't debug the script's content, debug its invocation.
- Sibling lesson: runbooks/pod-cleanup-kubectl-path.md (opposite failure — shell-less image, invoke the binary directly). Between the two: shell images + `-c` for scripts; shell-less images + direct binary args.
