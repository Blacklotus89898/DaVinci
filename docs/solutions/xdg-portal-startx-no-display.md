# xdg portal startx no display

## xdg-desktop-portal-gtk crash-loops with 'cannot open display:' on startx workstations

### Symptom

`systemctl --user --failed` shows `xdg-desktop-portal-gtk.service` failed with `Result: start-limit-hit`. Journal shows it crash-looping since login with:

```
xdg-desktop-portal-gtk[...]: cannot open display:
```

Seen on the archlinux workstation (i3 via startx, no display manager) 2026-11-07. Failure began right after boot — before X was even started — because the portal is D-Bus-activated at login.

### Root cause

With `startx`, the graphical session environment (DISPLAY, XAUTHORITY) exists only in the shell that ran xinit. The systemd **user manager** never learns about it, so any user service it spawns cannot open X. `xdg-desktop-portal-gtk` needs DISPLAY and dies; it gets re-activated by each portal request until the start-limit trips. Harmless-looking but it breaks flatpak file choosers, screenshot portal, etc.

Does NOT happen with a display manager: sddm/greetd import the session env for you.

### Fix

Permanent (in `.xinitrc`, before `exec i3`):

```sh
dbus-update-activation-environment --systemd DISPLAY XAUTHORITY
exec i3
```

For the already-running session (no re-login needed):

```sh
systemctl --user import-environment DISPLAY XAUTHORITY
systemctl --user restart xdg-desktop-portal-gtk
```

### Prevention

On any startx-based device, check `systemctl --user --failed` after boot as part of maintenance; a portal in start-limit-hit is this exact gap, not something malicious. Track the .xinitrc fix in dotfiles so fresh restores inherit it.
