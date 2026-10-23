# Sync the knowledge base (docs/) between devices through git.
# Pulls first (others' entries), then commits + pushes this device's new or
# edited entries. The server re-indexes docs/ on its next start, so restart
# your MCP client (or start a new Claude Code session) after pulling.
#
#   pwsh scripts/kb-sync.ps1
$ErrorActionPreference = 'Stop'
Set-Location (Resolve-Path "$PSScriptRoot\..")

git pull --rebase --autostash -q
if ($LASTEXITCODE -ne 0) { throw 'git pull failed — resolve, then re-run' }
if (git status --porcelain -- docs) {
    git add -- docs
    $n = @(git diff --cached --name-only -- docs).Count
    git commit -q -m "docs(kb): sync $n file(s) from $env:COMPUTERNAME"
    git push -q
    Write-Host "pushed $n knowledge file(s)"
} else {
    Write-Host 'knowledge base up to date (nothing local to push)'
}
