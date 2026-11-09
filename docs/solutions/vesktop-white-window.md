# vesktop white window

## Vesktop opens a blank white window (renderer never paints) on Arch — stale AUR version vs current Discord web bundle

### Symptom

Vesktop "doesn't start": window maps at the saved position but stays **completely white**, sometimes for minutes. Discord gateway actually connects in ~2s (so network is fine), Vencord plugin logs scroll, but nothing ever renders. User-perceived as "app won't start" plus general sluggishness.

Confirmed 2026-11-09 on archlinux workstation (vesktop 1.5.3-1 from AUR, i3/X11, Intel UHD 620).

### Diagnosis (the isolation that worked)

1. Launch from CLI with logs: `vesktop --enable-logging` — gateway connects, then:
   - `Uncaught TypeError: Cannot read properties of undefined (reading 'push')` from `VCDRenderer`
   - `Uncaught (in promise) Error: Script failed to execute` from `VCDPreload` — **the preload can't inject into the new Discord bundle** (chunks named `fast-connect.*.js`)
2. Window maps (check with `xdotool search --class vesktop`) but screenshot (`scrot`) shows pure white
3. Retry with `vesktop --disable-gpu` — **still white** → rules out GPU/compositing; the failure is app-version vs Discord's evolving web bundle

### Fix

Update the AUR package (installed 1.5.3; upstream Vencord/Vesktop latest was v1.6.9 at diagnosis):

```sh
yay -S vesktop
```

If still white after update, reset the per-user session cache (546MB at diagnosis; settings.json survives, only login state is lost):

```sh
pkill -x vesktop
rm -rf ~/.config/vesktop/sessionData
vesktop
```

### Prevention

- AUR packages don't update via `pacman -Syu` warnings unless the AUR helper checks them — this device's `update-notify` timer already counts AUR updates via `yay -Qua`; act when it reports one.
- Electron "white window + working network" = renderer-side failure: check preload/console errors first, then try `--disable-gpu` to split GPU vs version causes.
