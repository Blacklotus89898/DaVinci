# Reclaim CI Runner Disk Space

Clear accumulated Docker build cache/layers on a self-hosted CI runner that's hitting "no space left on device".

```bash
docker system df
docker system prune -af --volumes
```
