# authentik postgres torn wal

## ## Execution log — 2026-10-14 run

### Executed 2026-10-14 — result

**Outcome: full recovery, zero data loss.**

- User approved the staged Job; committed as c5d9dd2 (postgres already at `replicas: 0` in git since c46e963 — the required precondition).
- Job went **Complete**: `Write-ahead log reset`, fresh checkpoint `0/39000028`, `Database cluster state: shut down` (clean), crashloop core removed from the PVC.
- Follow-up commit 2c9f3d2 in the same window: Job manifest deleted from git + postgres `replicas: 1` restored. **The Job must leave git before its 1h TTL deletion + next ArgoCD sync could recreate it** — re-running pg_resetwal on a healthy DB is destructive.
- Postgres pod started clean (no PANIC loop); authentik-server recovered readiness on its own once the DB returned (no restart needed); worker/redis untouched; ArgoCD app `authentik` → **Healthy** after 6 days `Progressing`. CI 9/9 on both commits.

### Verification one-liners
```bash
kubectl -n default logs job/authentik-postgres-resetwal   # control-data echo (before TTL cleanup)
kubectl -n default get deploy authentik-postgres -o jsonpath='{.status.readyReplicas}'
kubectl -n argocd get app authentik -o jsonpath='{.status.health.status}'
```
