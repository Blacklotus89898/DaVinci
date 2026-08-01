# Check Redis Memory / Eviction Status

Quick health check for Redis memory pressure: usage, policy, fragmentation, evictions, and biggest keys.

```bash
redis-cli INFO memory | grep -E 'used_memory:|used_memory_human|maxmemory|maxmemory_policy|mem_fragmentation_ratio'
redis-cli INFO stats | grep evicted_keys
redis-cli --bigkeys
```
