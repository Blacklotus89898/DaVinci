# backstage stuck plugin initialization

## Backstage pod never ready: "still initializing: 'techdocs'" (orphaned knex migration transaction)

Seen 2026-11-25 during the Backstage move to https://backstage.lab.home + ambient enrollment (Homelab #80). Applies to any Backstage plugin.

### Symptom
- New pod Running 0/1 for 10+ min; readiness `/.backstage/health/v1/readiness` → 503.
- Logs repeat `Plugin initialization in progress, still initializing: '<plugin>'`; the plugin itself logs nothing.
- The container had restarted once (startup probe killed it during the first boot).
- Argo Rollouts canary stays Progressing at 50%; the old pods keep serving.

### Root cause
The killed first container was inside a knex migration transaction on the plugin's database (`backstage_backend_public_keys__knex_migrations_lock`). Its postgres session survived as `idle in transaction` (in ambient mode the connection runs through ztunnel and wasn't torn down when the process died), holding a row lock. The restarted process blocks on `update ..._knex_migrations_lock set is_locked` forever.

### Diagnose
```sql
-- in backstage-postgresql-0: PGPASSWORD="$POSTGRES_POSTGRES_PASSWORD" psql -U postgres
select datname, client_addr, state, wait_event_type, date_trunc('second', now()-query_start) age, left(query,110)
from pg_stat_activity where datname like 'backstage_plugin_%' and state <> 'idle' order by age desc;
```
Look for an old `idle in transaction` plus an `active` query waiting on `Lock | transactionid`, both from the new pod's IP. (`knex_migrations_lock.is_locked = 1` alone is a different, rarer case.)

### Fix
```sql
select pid, pg_terminate_backend(pid) from pg_stat_activity
where datname = 'backstage_plugin_<plugin>' and state = 'idle in transaction' and now() - state_change > interval '10 minutes';
```
The plugin finishes initializing within seconds; the canary continues on its own.

### Prevention
- Give the first boot enough startup-probe budget that it isn't killed mid-migration (`startupProbe.failureThreshold` in apps/backstage/values.yaml).
- After a Backstage pod restart during startup, check pg_stat_activity before waiting it out.
