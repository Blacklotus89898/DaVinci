#!/usr/bin/env sh
# One-shot setup of knowledge-service on a new device (Linux / macOS / Git Bash).
# Idempotent: safe to re-run after `git pull` to rebuild and re-register.
#
#   git clone https://github.com/Blacklotus89898/DaVinci.git && cd DaVinci
#   sh scripts/setup.sh            # build, embeddings, smoke test, register with Claude Code
#   sh scripts/setup.sh --no-mcp   # skip the `claude mcp add` step
set -eu

REPO=$(cd "$(dirname "$0")/.." && pwd)
cd "$REPO"
REGISTER=1
[ "${1:-}" = "--no-mcp" ] && REGISTER=0

ext=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) ext=".exe" ;; esac

command -v go >/dev/null || { echo "Go 1.24+ is required (https://go.dev/dl/)"; exit 1; }
echo "==> building ($(go version | awk '{print $3}'))"
mkdir -p bin
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
go build -ldflags="-s -w -X main.version=$VERSION" -o "bin/knowledge-service$ext" ./cmd/knowledge-service
go build -ldflags="-s -w -X main.version=$VERSION" -o "bin/knowledge$ext" ./cmd/knowledge

# Semantic search is optional: without Ollama the server falls back to TF-IDF.
PROVIDER=""
if command -v ollama >/dev/null && ollama list >/dev/null 2>&1; then
  echo "==> ollama found — pulling nomic-embed-text (one-time, ~274 MB)"
  ollama pull nomic-embed-text >/dev/null 2>&1
  PROVIDER=ollama
else
  echo "==> ollama not running — using TF-IDF keyword search (install Ollama later and re-run for semantic search)"
fi

DB="$REPO/knowledge.db"
DOCS="$REPO/docs"
echo "==> indexing $(find "$DOCS" -name '*.md' | wc -l | tr -d ' ') docs"
DB_PATH="$DB" DOCS_PATH="$DOCS" EMBED_PROVIDER="$PROVIDER" "bin/knowledge$ext" ingest "$DOCS" >/dev/null
echo "==> smoke test: search 'disk pressure'"
DB_PATH="$DB" EMBED_PROVIDER="$PROVIDER" "bin/knowledge$ext" search "disk pressure" 2>/dev/null | head -5

if [ "$REGISTER" = 1 ]; then
  if command -v claude >/dev/null; then
    echo "==> registering MCP server 'knowledge' (user scope)"
    claude mcp remove knowledge -s user >/dev/null 2>&1 || true
    set -- claude mcp add knowledge "$REPO/bin/knowledge-service$ext" \
      -e "DB_PATH=$DB" -e "DOCS_PATH=$DOCS" -e LOG_LEVEL=warn --scope user
    [ -n "$PROVIDER" ] && set -- "$@" -e EMBED_PROVIDER=ollama -e EMBED_MODEL=nomic-embed-text
    "$@"
    claude mcp get knowledge | grep -E "Status|Command" || true
  else
    echo "==> Claude Code CLI not found — register later with:"
    echo "    claude mcp add knowledge \"$REPO/bin/knowledge-service$ext\" -e DB_PATH=\"$DB\" -e DOCS_PATH=\"$DOCS\" --scope user"
  fi
fi

echo "==> done. Keep devices in sync with: sh scripts/kb-sync.sh"
