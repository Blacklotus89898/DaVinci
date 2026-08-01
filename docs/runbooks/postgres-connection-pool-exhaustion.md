# Postgres "too many connections" / Pool Exhaustion

## Symptom
App errors with `FATAL: too many connections for role/database` or `remaining connection slots are reserved`. Sometimes only appears under load or after a deploy.

## Root Cause
- No connection pooler (PgBouncer/RDS Proxy) in front of Postgres, so every app pod/replica opens its own pool, multiplying `max_connections` usage by replica count
- App-side connection pool `max` size not scaled down when replica count went up (e.g., 50 pods × pool size 20 = 1000 connections against a `max_connections=200` instance)
- Connection leak: code path opens a connection/transaction and never closes it on an error branch
- Long-idle transactions holding connections open (`idle in transaction`) — often from a forgotten `COMMIT`/`ROLLBACK` or an ORM session not closed
- A migration or admin script left connections open outside the normal app pool

## Fix
```sql
-- See current connection count vs max
SHOW max_connections;
SELECT count(*) FROM pg_stat_activity;

-- Break down by state and application
SELECT state, application_name, count(*)
FROM pg_stat_activity
GROUP BY state, application_name
ORDER BY count(*) DESC;

-- Find long-running idle-in-transaction sessions (the usual leak signature)
SELECT pid, usename, application_name, state,
       now() - state_change AS idle_duration, query
FROM pg_stat_activity
WHERE state = 'idle in transaction'
ORDER BY idle_duration DESC;

-- Kill a specific runaway/leaked connection
SELECT pg_terminate_backend(<pid>);

-- Kill all idle-in-transaction sessions older than 10 minutes (emergency relief valve)
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'idle in transaction' AND now() - state_change > interval '10 minutes';
```

## Prevention
Put PgBouncer (transaction-pooling mode) in front of Postgres so app pod count can scale independently of DB connection count. Set app-side pool `max` conservatively and add a statement/idle-in-transaction timeout (`idle_in_transaction_session_timeout`) at the DB level so leaked transactions self-clean.
