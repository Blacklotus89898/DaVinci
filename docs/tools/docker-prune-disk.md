# Reclaim Docker Disk Space

Removes stopped containers, dangling images, unused networks, and unused volumes. Destructive to anything not in use by a running container — confirm before running on a shared host.

```bash
docker system prune -af --volumes
```
