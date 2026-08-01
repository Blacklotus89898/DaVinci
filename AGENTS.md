# Agent Instructions

## Knowledge Base — Use These Tools

| Tool | When to use |
|---|---|
| `list_knowledge` | Start of any SRE/DevOps task — discover what runbooks already exist |
| `search_knowledge` | Before answering any non-trivial question |
| `write_knowledge` | After solving a problem, resolving an incident, or finding something non-obvious |
| `delete_knowledge` | When a runbook is outdated, wrong, or superseded |
| `get_tool` | Retrieve a stored script/one-liner by name for immediate execution |

### Search Tips

- Use **concrete SRE terms**: service names, error messages, exit codes, kubectl commands
- Try the exact error string first (`OOMKilled`, `CrashLoopBackOff`, `no space left on device`)
- If nothing returns, broaden: `search_knowledge("oom memory")` instead of `"OOMKilled"`
- The search uses FTS5 BM25 keyword matching + TF-IDF vectors merged via RRF — exact terms
  score highest; related-vocabulary terms score slightly lower via the vector channel
- Prefix matching is on: `kube` hits `kubernetes`, `argo` hits `argocd`

## Workflow

### Before answering

1. Call `list_knowledge` (no filter) to orient yourself — see what categories exist.
2. Call `search_knowledge` with the user's question. Use concrete terms: service names, error messages, kubectl commands, pod names.
3. Use the returned context. Cite the source path.

### After solving

1. Call `write_knowledge` immediately after closing an incident or solving a non-trivial issue.
2. Write content a future on-call could act on in the dark at 3am:
   - What the symptom was
   - Root cause
   - Exact commands / manifests to apply
   - How to prevent recurrence

### Pruning

Call `delete_knowledge` when:
- A runbook references old image tags, deprecated flags, or removed resources
- A fix was superseded by a permanent infra change
- You wrote something incorrect and want to replace it

## Path Conventions

| Category | Path prefix | Retrieved via |
|---|---|---|
| Incident runbooks | `runbooks/<service>-<symptom>.md` | `search_knowledge` |
| One-time fixes | `solutions/<topic>.md` | `search_knowledge` |
| Executable scripts / one-liners | `tools/<name>.md` | `get_tool("<name>")` |
| How-to guides | `guides/<topic>.md` | `search_knowledge` |
| Architecture notes | `architecture/<component>.md` | `search_knowledge` |

A `tools/` entry must contain a fenced code block — `get_tool` extracts the first one and returns raw code for execution.

## Write-back Template

```
## Symptom
[What the user/alert saw]

## Root Cause
[Why it happened]

## Fix
```bash
# Exact commands
```

## Prevention
[How to stop it from happening again]
```

## Examples

```json
{
  "path": "runbooks/argocd-oom.md",
  "heading": "ArgoCD OOM — Pod Accumulation",
  "content": "## Symptom\nArgoCD OOMKilled. Disk pressure on nodes.\n\n## Root Cause\nCompleted pods not GC'd, fill /var/lib/kubelet.\n\n## Fix\nkubectl apply -f infrastructure/pod-cleanup/\n\n## Prevention\nPod-cleanup CronJob runs every 15 min."
}
```
