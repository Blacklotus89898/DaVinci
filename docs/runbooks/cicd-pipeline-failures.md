# CI Pipeline Failing on Build/Cache/Registry Issues

## Symptom
CI (GitHub Actions, GitLab CI, etc.) fails intermittently or after an unrelated change, with errors like `no space left on device` on the runner, `denied: requested access to the resource is denied` on docker push, or a build that passes locally but fails in CI.

## Root Cause
- Runner disk fills up from accumulated Docker layers/build cache across jobs (self-hosted runners especially, since they aren't wiped between runs like ephemeral GitHub-hosted ones)
- Registry auth token/credential expired or lacks push scope for the target repo/tag
- Build cache poisoned by a previous failed/partial layer, causing non-reproducible builds ("works on my machine" but not on a clean runner)
- Flaky network pull of a base image or dependency from a registry/package index under rate limiting (e.g., Docker Hub anonymous pull limits)
- Parallel jobs racing on the same cache key/tag, causing one job to overwrite or corrupt what another is reading

## Fix
```bash
# On a self-hosted runner: check disk pressure and reclaim space
df -h
docker system df
docker system prune -af --volumes   # aggressive — confirm nothing in-flight needs it first

# Verify registry credentials/scope from the runner's context
docker login <registry> -u <user> --password-stdin <<< "$TOKEN"
docker pull <registry>/<image>:<tag>   # read-scope sanity check
docker push <registry>/<image>:<tag>   # write-scope sanity check

# Reproduce a "works locally, fails in CI" build with no cache to rule out poisoned layers
docker build --no-cache -t debug-build .

# Check for registry rate limiting (Docker Hub example — look for 429s)
docker pull <image> 2>&1 | grep -i "toomanyrequests\|rate limit"
```

## Prevention
Give self-hosted runners a scheduled `docker system prune` (or use ephemeral/autoscaling runners that start clean). Pin base images to a digest, not just a mutable tag, so a cache hit is guaranteed reproducible. Use an authenticated pull-through registry mirror to avoid anonymous rate limits on public registries.
