# Redis Hitting maxmemory / Evicting Keys Unexpectedly

## Symptom
`INFO stats` shows `evicted_keys` climbing, cache hit rate drops, or writes fail with `OOM command not allowed when used memory > 'maxmemory'` (when eviction policy is `noeviction`).

## Root Cause
- `maxmemory` set too low for actual working-set size as traffic/data grew
- Eviction policy mismatched to use case — e.g., `noeviction` on a cache workload causes write errors instead of evicting; `allkeys-lru` on a workload that mixes cache + durable data evicts data that should never be evicted
- Key TTLs not set (or set too long) so cold/unused keys accumulate and are never naturally expired
- Big keys (large hashes/sets/sorted sets, or unbounded lists) consuming disproportionate memory — one bad access pattern can dominate usage
- Memory fragmentation (`mem_fragmentation_ratio` >> 1) — Redis reports high used memory to the OS but actual data is much smaller

## Fix
```bash
# Check current memory usage, policy, and fragmentation
redis-cli INFO memory | grep -E 'used_memory:|used_memory_human|maxmemory|maxmemory_policy|mem_fragmentation_ratio'

# Check eviction stats
redis-cli INFO stats | grep evicted_keys

# Find the biggest keys (sampling scan, safe on production)
redis-cli --bigkeys

# Inspect a specific suspiciously large key
redis-cli MEMORY USAGE <key>

# Check keys with no TTL set that probably should have one
redis-cli --scan | while read key; do
  ttl=$(redis-cli TTL "$key")
  [ "$ttl" = "-1" ] && echo "$key"
done | head -20

# Raise maxmemory if it's genuinely undersized (requires config persistence too)
redis-cli CONFIG SET maxmemory 4gb
redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

## Prevention
Set TTLs on all cache keys at write time rather than relying on eviction. Pick `maxmemory-policy` deliberately: `allkeys-lru`/`allkeys-lfu` for pure caches, `volatile-lru` when the instance mixes cacheable and must-not-evict data (only keys with a TTL are eviction-eligible). Alert on `mem_fragmentation_ratio` and `evicted_keys` rate, not just raw memory usage.
