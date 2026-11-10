# 518pan download throttling

## 518pan.org presigned links: per-connection throttle, per-IP 'down-busy' block, and why HEAD 200 lies

### Symptom

Downloads from `down.518pan.org` (Chinese file host behind Cloudflare) stall or die mid-transfer in browsers; resume never works. curl range tests crawl at ~9.6 KB/s per connection.

Measured 2026-11-10 on a 386 MB presigned zip (`sig=` + `e=` expiry params, `response-content-disposition`).

### The throttle model (measured)

| Probe | Result |
|---|---|
| Single range flow | ~9.6 KB/s per connection, hard per-connection cap |
| 4 parallel flows | 37 KB/s aggregate — scales linearly |
| 16 parallel flows | 38 KB/s aggregate — **hard per-IP cap ~38 KB/s** |
| Baseline sanity (OVH 10 MB) | 6.45 MB/s — user's link was NOT the bottleneck |

Optimum: ~4 connections. More connections does NOT help beyond 38 KB/s.

### The trap: aggressive parallelism triggers a block

Opening many connections (16-flow test + 32-chunk downloader) tripped their load protection:

```
HTTP/2 302
location: https://518pan.net/down-busy.html
```

After the flag: **every GET body is redirected** — ranged AND full-file. HEAD still returns 200 (misleading: cheap metadata passes, bytes don't). The block is **per-IP**, not per-signature: a freshly regenerated presigned link got the same 302.

### What works

- Be polite: ≤4 connections, 15s stagger, back off minutes (not seconds) on any 302
- Retry loop with `curl -C -` (NO `-L`! `-L` would append the busy HTML page into the file), treat "size unchanged" as busy → sleep and retry
- Chunked downloader with per-chunk byte-offset resume survives stalls with zero progress loss; verify `206` + Content-Type before writing (never trust a redirect-followed response)
- Block duration: unclear (minutes to hours; China peak hours are worst — free host, ~10:00 CST). Link `e=` expiry gave ~18h margin, which absorbed it

### Diagnosis recipe

1. `curl -sI` on the link → metadata (size, `accept-ranges`)
2. Timed 4 MB range pull → per-connection rate
3. 4 × 2 MB parallel → per-IP aggregate (compare scaling)
4. Big file from a neutral host (e.g. `proof.ovh.net/files/10Mb.dat`) → rules out local link
5. 302 with `location: …down-busy.html` → backed off, wait; do NOT hammer
