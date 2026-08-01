# Find Large Files on a Node via Debug Pod

Spins up a debug container with the host filesystem mounted at /host — use when a node reports DiskPressure and you need to find what's eating space.

```bash
kubectl debug node/<node> -it --image=busybox -- chroot /host du -ahx / | sort -rh | head -20
```
