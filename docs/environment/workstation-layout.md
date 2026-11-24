# Workstation layout (Arch laptop, reorganized 2026-11-24)

## Directory layout
- `~/workspace/projects/` — own repos: DaVinci (this KB + MCP), InHouseROS, Oscar, Darling, DevOps-Platform, MyROS2, whitelotus, Kali (CP/ML practice), Sandbox
- `~/workspace/mcgill/` — coursework by course code (comp4xx, ecse4xx), robotics team (rover-2025, MyROS2 webrtc branch), hackathons
- `~/workspace/forks/` — third-party clones (googletest, libdatachannel, ohmypi, zeal PKGBUILD)
- `~/workspace/playground/` — scratch (myCPP, mySandbox incl. librealsense, ghidra, eclipse)
- `~/workspace/security/` — scan scripts + `reports/` (nikto, slowloris)
- `~/workspace/archive/` — old keyitdev dotfiles backup
- `~/dotfiles` (stow; stays in $HOME), `~/myVault` (Obsidian; registered by path), `~/notes`, `~/Documents/personal`
- Move log: `~/workspace/.moves.log`

## After the move
- knowledge MCP path: `~/workspace/projects/DaVinci/bin/knowledge-service` (Claude Code user scope + `~/.config/opencode/opencode.jsonc`)
- Python venvs were relocated in place (paths rewritten). CMake build dirs must be regenerated: `rm -rf build && cmake --preset <name>`.
- Root-only setup lives in `~/dotfiles/system/` (`perf-fix.sh`, `dev-tools.sh`); user isn't in sudoers → use `run0`.
