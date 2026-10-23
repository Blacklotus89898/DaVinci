#!/usr/bin/env sh
# Sync the knowledge base (docs/) between devices through git.
# Pulls first (others' entries), then commits + pushes this device's new or
# edited entries. The server re-indexes docs/ on its next start, so restart
# your MCP client (or start a new Claude Code session) after pulling.
#
#   sh scripts/kb-sync.sh
set -eu
cd "$(dirname "$0")/.."

git pull --rebase --autostash -q
if [ -n "$(git status --porcelain -- docs)" ]; then
  git add -- docs
  n=$(git diff --cached --name-only -- docs | wc -l | tr -d ' ')
  git commit -q -m "docs(kb): sync $n file(s) from $(hostname)"
  git push -q
  echo "pushed $n knowledge file(s)"
else
  echo "knowledge base up to date (nothing local to push)"
fi
