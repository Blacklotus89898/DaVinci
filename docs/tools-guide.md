# Tools Guide

## What Are Tools?

"Tools" in this knowledge base are stored runnable commands — kubectl one-liners, bash scripts,
argocd commands — that an agent can retrieve via `get_tool` and execute immediately.

They live under `tools/` and are markdown files with `##` headings and fenced code blocks.

## Storing a Tool

Use `write_knowledge` with a path under `tools/`:

```json
{
  "path": "tools/k8s-drain-node.md",
  "heading": "Drain a Node for Maintenance",
  "content": "Evict all pods safely before node maintenance.\n\n```bash\nkubectl drain <node> --ignore-daemonsets --delete-emptydir-data\nkubectl uncordon <node>\n```"
}
```

The agent (or you) can then retrieve it:

```
get_tool(name: "drain node")
→ kubectl drain <node> --ignore-daemonsets --delete-emptydir-data
```

> **Note**: `get_tool` accepts a `name` parameter (not `query`). Pass the heading or a close match — it runs a hybrid search scoped to `tools/` paths.

## Tool vs. Runbook

| Type | Path prefix | Contains |
|---|---|---|
| **Tool** | `tools/` | Executable commands, one-liners, scripts — no prose |
| **Runbook** | `runbooks/` | Full incident response: symptom → root cause → fix → prevention |
| **Solution** | `solutions/` | One-time fixes with context and explanation |

## SRE Tool Examples

### Node Operations

```bash
# Drain
kubectl drain <node> --ignore-daemonsets --delete-emptydir-data

# Cordon without drain (stop new scheduling)
kubectl cordon <node>

# Uncordon after maintenance
kubectl uncordon <node>

# Check what's running on a node
kubectl get pods -A --field-selector spec.nodeName=<node>
```

### Pod Debugging

```bash
# Describe a crashing pod
kubectl describe pod <pod> -n <ns>

# Follow logs of previous (crashed) container
kubectl logs <pod> -n <ns> --previous

# Exec into running container
kubectl exec -it <pod> -n <ns> -- /bin/sh

# Delete stuck terminating pod
kubectl delete pod <pod> -n <ns> --grace-period=0 --force
```

### ArgoCD

```bash
# Force sync
argocd app sync <app> --force --replace

# Refresh without sync
argocd app get <app> --refresh

# Hard reset (wipes then re-applies)
argocd app sync <app> --prune --force

# Check sync status
argocd app list
```

### Disk / OOM Debugging

```bash
# Node disk usage
kubectl get nodes -o custom-columns='NAME:.metadata.name,DISK:.status.conditions[?(@.type=="DiskPressure")].status'

# Find large files on a node (via debug pod)
kubectl debug node/<node> -it --image=busybox -- df -h

# Delete completed/failed pods
kubectl delete pods -A --field-selector=status.phase=Succeeded
kubectl delete pods -A --field-selector=status.phase=Failed
```

## After-Session Workflow

At the end of every debugging session, write back what you discovered:

```
1. list_knowledge("runbooks/")    → check if runbook already exists
2. write_knowledge(...)           → create or add to existing path
3. If you used a command > once:
   write_knowledge("tools/<name>.md", ...)  → store as a tool
```

This makes every future session smarter without any manual effort.
