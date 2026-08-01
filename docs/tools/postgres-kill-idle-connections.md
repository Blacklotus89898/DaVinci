# Kill Idle-in-Transaction Postgres Connections

Emergency relief for connection pool exhaustion: terminate leaked idle-in-transaction sessions older than 10 minutes.

```sql
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'idle in transaction' AND now() - state_change > interval '10 minutes';
```
