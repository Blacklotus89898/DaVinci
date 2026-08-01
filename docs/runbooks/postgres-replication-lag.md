# Postgres Replica Falling Behind (Replication Lag)

## Symptom
Read replicas return stale data, or monitoring alerts on `pg_stat_replication` lag / `replay_lag` growing instead of staying near zero.

## Root Cause
- Replica is under-provisioned (disk IOPS or CPU) and can't apply WAL as fast as the primary generates it
- A long-running query on the replica is blocking WAL replay (`hot_standby` conflict — replica pauses replay to avoid canceling the query, up to `max_standby_streaming_delay`)
- Network bottleneck/latency between primary and replica (cross-region replicas especially)
- Primary is under a heavy write burst (bulk import, large batch job) that outpaces normal replication capacity
- WAL sender/receiver process crashed or was restarted and is catching up from a large backlog

## Fix
```sql
-- On the primary: check lag per replica
SELECT client_addr, state, sent_lsn, replay_lsn,
       pg_wal_lsn_diff(sent_lsn, replay_lsn) AS lag_bytes,
       replay_lag
FROM pg_stat_replication;

-- On the replica: check what's currently blocking replay (if any)
SELECT now() - pg_last_xact_replay_timestamp() AS replication_delay;

-- Find long-running queries on the replica that might be conflicting with replay
SELECT pid, now() - query_start AS duration, query
FROM pg_stat_activity
WHERE state = 'active'
ORDER BY duration DESC;
```
```bash
# Check disk/IO pressure on the replica host
iostat -x 1 5
```

## Prevention
Size replicas to match (or exceed) the primary's write IOPS capacity, not just storage size. Set `max_standby_streaming_delay` deliberately (don't leave it at `-1`/infinite) so long replica queries can't stall replication indefinitely, and route long analytical queries to a dedicated replica instead of the one serving live reads.
