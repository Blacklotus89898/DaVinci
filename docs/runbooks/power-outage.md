# power outage

## Power outage (no UPS): prepare, recover, verify

Also known as: electricity goes out, blackout, power cut, power failure, brownout, everything went dark, host lost power.

The whole lab is one Proxmox host (node `my`) on one SMR disk (`hard_drive`, ~94%) with no UPS. A power cut takes everything down at once: Proxmox, both k3s VMs (100 `debian` server, 104 `k3s-worker-01`), the Pi-hole DNS at 192.168.0.53, Glance LXC 116 and ubuntu.home.

### What has to be true for an unattended recovery
1. **BIOS: Restore on AC Power Loss = Power On.** Only the user can check this, at the console. If it's off, the host stays off.
2. **Proxmox `onboot=1` with an order.** DNS first, then the k3s server, then the worker:
   ```
   qm config 100 | grep -E 'onboot|startup|scsi0'; qm config 104 | grep -E 'onboot|startup|scsi0'
   pct list; for c in $(pct list | awk 'NR>1{print $1}'); do echo "$c $(pct config $c | grep -E 'hostname|onboot' | paste -sd' ')"; done
   qm list
   qm set 100 --onboot 1 --startup order=2,up=60,down=180
   qm set 104 --onboot 1 --startup order=3,up=30,down=180
   pct set <pihole-ct> --onboot 1 --startup order=1   # if Pi-hole is a CT on this host
   pct set 116 --onboot 1                             # glance
   ```
   Both k3s VMs were `onboot=0` as of 2026-10-22, so a power cut left the cluster down until someone started them by hand.
3. **Disk cache:** check `cache=` on `scsi0`. Default or `none` is crash-safe enough. `writeback` or `unsafe` can corrupt the qcow2 files on a hard cut.
4. **Clean shutdown beats crash recovery.** If the outage time is known, shut the host down from the Proxmox UI beforehand. The guest agents shut the VMs down cleanly. Power-on and autostart then do the rest.

### Pre-outage backup (k3s datastore)
k3s runs on **kine/SQLite** (`/var/lib/rancher/k3s/server/db/state.db`), not embedded etcd, so there are **no automatic snapshots**. The empty `db/etcd/` directory and the `etcd=true` node label are misleading. Take a consistent copy with a one-shot pod (nodeName debian, hostPath `/var/lib/rancher/k3s/server`, alpine plus `apk add sqlite`):
`sqlite3 db/state.db ".backup db-backup/state.db.pre-outage-<date>"`, then run `PRAGMA integrity_check`.
Keep it on the node in the root-only `db-backup/` directory. It contains every Secret in plaintext, so never copy it to a workstation without the operator's say-so.
To restore: stop k3s, move the bad `state.db*` aside, copy the backup to `db/state.db`, start k3s. The restore also needs the existing `server/token`.
Done 2026-10-28: `state.db.pre-outage-20261028`, integrity ok.
The sealed-secrets keyring backup must also be current (re-export after each ~30-day key rotation). It's the only way to decrypt git's SealedSecrets on a rebuild.

### Expected noise during the outage
- The GitHub watchdog pages "Cluster dark?" (solutions/heartbeat-dead-man-switch.md). This is expected.
- On return: a burst of alerts while pods start, and TulipNoCycle if a 10:05 or 15:00 slot was missed. Run `cycle_run` via MCP if needed.

### Post-boot checklist (baseline 2026-10-28: 35 apps, 34 Synced/Healthy + audiobookshelf Suspended; 60 running pods; 17 PVCs)
```
kubectl get nodes                                    # both Ready (debian SchedulingDisabled = normal)
kubectl -n argocd get applications | grep -v 'Synced.*Healthy'
kubectl get pods -A | grep -v -E 'Running|Completed'
ssh k3s-worker-01 'findmnt /mnt/smb_storage'         # virtiofs mounted (kavita, audiobookshelf, petal dind)
kubectl -n tulip get pods; kubectl -n warden get pods
```
Known cold-boot behaviour:
- **petal dind** crash-restarts 2–3 times, then self-heals.
- **istio-cni / ztunnel** restart a few times on node boot.
- **Worker came up with no network (2026-11-04):** the VM had a second, unused NIC (enp6s19 on vmbr3), and its only NetworkManager profile ("Wired connection 1") wasn't bound to an interface, so NM brought up the wrong card. With the worker offline, **every app is down**: `debian` is cordoned (since 2026-03), so nothing can reschedule there. Fixed by removing the second NIC in Proxmox (VM 104 → Hardware). If it recurs: open the VM console, run `nmcli con show`, and bind the profile with `nmcli con mod "Wired connection 1" connection.interface-name <nic>`.
- **Pi-hole answers slowly right after boot**, so `.home` names fail for a few minutes. Use IPs instead: Proxmox 192.168.0.115:8006, worker 192.168.0.108, debian 192.168.0.112.
- **Root app health wedge:** see runbooks/argocd-app-of-apps-wedge.md.
- **Glance:** container restart policy is unless-stopped, and it resolves through Pi-hole 192.168.0.53.
- **ubuntu.home:** minikube container is restart=no, so it stays down until someone runs `minikube start`.
