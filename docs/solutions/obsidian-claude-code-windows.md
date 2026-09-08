# obsidian claude code windows

## Obsidian Claude Code Plugin — pywinpty Missing on Windows Python 3.8

### Symptom

The Claude Code integration in Obsidian on Windows starts, runs for about one
second, then dies. Visible errors, in escalating detail:

```
[Process exited: 1]
Operation aborted
pywinpty not installed for this Python interpreter:
  C:\Python38\python.exe
ACP connection closed
```

The Obsidian developer console showed nothing useful — the error surfaced only
in the plugin's own output pane.

### Root Cause

The plugin drives the agent through a Python pseudo-terminal, which on Windows
requires the `pywinpty` package installed **in the exact interpreter the plugin
invokes** — not merely some Python on `PATH`. Here that is `C:\Python38`.
Without it the PTY never opens, the ACP (Agent Client Protocol) agent process
never starts, and the plugin reports the downstream symptom
(`ACP connection closed`) rather than the root cause.

### Fix

```powershell
& "C:\Python38\python.exe" -m pip install pywinpty
& "C:\Python38\python.exe" -c "import winpty; print(winpty.__file__)"
```

Installed `pywinpty 2.0.14` (built a `cp38-cp38-win_amd64` wheel locally, no
Rust toolchain needed). Verified import resolves to
`C:\Python38\lib\site-packages\winpty\__init__.py`. Restart Obsidian fully
afterwards, not just reload.

### Status

pywinpty is confirmed installed and importable. Whether that alone clears
`ACP connection closed` is **not yet verified** — if it persists, the PTY is
fine and the agent process itself is failing. Next step is to run the plugin's
configured agent command by hand and read its stderr:

```
npx @zed-industries/claude-code-acp
```

It should sit waiting on stdin. If it exits, likely causes are Node not on the
PATH Obsidian inherits, or missing/expired Claude Code credentials.

### Note on the vault location

This vault lives on an SMB share hosted on a Linux server while Obsidian runs
on Windows. That was the first suspect and turned out **not** to be the cause
here — but it remains a real hazard for anything that spawns the CLI with the
vault as its working directory: SQLite and `flock` semantics are unreliable over
SMB. If CLI-spawning plugins misbehave later, test the same command from a local
directory before assuming a plugin bug.
