# One-shot setup of knowledge-service on a new Windows device (PowerShell 7+).
# Idempotent: safe to re-run after `git pull` to rebuild and re-register.
#
#   git clone https://github.com/Blacklotus89898/DaVinci.git; cd DaVinci
#   pwsh scripts/setup.ps1            # build, embeddings, smoke test, register with Claude Code
#   pwsh scripts/setup.ps1 -NoMcp     # skip the `claude mcp add` step
param([switch]$NoMcp)
$ErrorActionPreference = 'Stop'

$Repo = (Resolve-Path "$PSScriptRoot\..").Path
Set-Location $Repo

if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go 1.24+ is required (https://go.dev/dl/)' }
Write-Host "==> building ($((go version) -split ' ' | Select-Object -Index 2))"
New-Item -ItemType Directory -Force bin | Out-Null
$Version = (git describe --tags --always --dirty 2>$null); if (-not $Version) { $Version = 'dev' }
go build -ldflags="-s -w -X main.version=$Version" -o bin\knowledge-service.exe .\cmd\knowledge-service
go build -ldflags="-s -w -X main.version=$Version" -o bin\knowledge.exe .\cmd\knowledge

# Semantic search is optional: without Ollama the server falls back to TF-IDF.
$Provider = ''
if ((Get-Command ollama -ErrorAction SilentlyContinue) -and ($null -ne (ollama list 2>$null))) {
    Write-Host '==> ollama found — pulling nomic-embed-text (one-time, ~274 MB)'
    ollama pull nomic-embed-text | Out-Null
    $Provider = 'ollama'
} else {
    Write-Host '==> ollama not running — using TF-IDF keyword search (install Ollama later and re-run for semantic search)'
}

$Db = Join-Path $Repo 'knowledge.db'
$Docs = Join-Path $Repo 'docs'
$env:DB_PATH = $Db; $env:DOCS_PATH = $Docs; $env:EMBED_PROVIDER = $Provider
Write-Host "==> indexing $((Get-ChildItem $Docs -Recurse -Filter *.md).Count) docs"
.\bin\knowledge.exe ingest $Docs | Out-Null
Write-Host "==> smoke test: search 'disk pressure'"
.\bin\knowledge.exe search 'disk pressure' 2>$null | Select-Object -First 5

if (-not $NoMcp) {
    if (Get-Command claude -ErrorAction SilentlyContinue) {
        Write-Host "==> registering MCP server 'knowledge' (user scope)"
        claude mcp remove knowledge -s user 2>$null | Out-Null
        $mcpArgs = @('mcp', 'add', 'knowledge', (Join-Path $Repo 'bin\knowledge-service.exe'),
            '-e', "DB_PATH=$Db", '-e', "DOCS_PATH=$Docs", '-e', 'LOG_LEVEL=warn', '--scope', 'user')
        if ($Provider) { $mcpArgs += @('-e', 'EMBED_PROVIDER=ollama', '-e', 'EMBED_MODEL=nomic-embed-text') }
        & claude @mcpArgs
        claude mcp get knowledge | Select-String 'Status|Command'
    } else {
        Write-Host '==> Claude Code CLI not found — register later with:'
        Write-Host "    claude mcp add knowledge `"$Repo\bin\knowledge-service.exe`" -e DB_PATH=`"$Db`" -e DOCS_PATH=`"$Docs`" --scope user"
    }
}

Write-Host '==> done. Keep devices in sync with: pwsh scripts/kb-sync.ps1'
