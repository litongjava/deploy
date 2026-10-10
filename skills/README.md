# Deploy CLI skill

The `deploy-cli` skill teaches agents to operate this repository's `deploy` client.
It covers server selection, project creation, runs, logs, middleware, and failure recovery.
The skill does not install the CLI or initialize a deploy-server.

## Install for Codex and DSH

Run these PowerShell commands from the repository root.
They install the same skill into both user-level skill directories.
If a destination already exists, compare it before replacing local changes.

```powershell
$codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME ".codex" }
$dshHome = if ($env:DSH_HOME) { $env:DSH_HOME } else { Join-Path $HOME ".dsh" }
$source = Join-Path (Get-Location) "skills/deploy-cli/SKILL.md"
foreach ($root in @($codexHome, $dshHome)) {
    $target = Join-Path $root "skills/deploy-cli"
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    Copy-Item -LiteralPath $source -Destination (Join-Path $target "SKILL.md")
    Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $target "SKILL.md")
}
Get-FileHash -Algorithm SHA256 -LiteralPath $source
```

All three hashes must match. Restart Codex or start a new session after installation.
For DSH, start a new session if its skill catalog does not list `deploy-cli`.
Do not assume that an existing session reloads its skill catalog.

Ask the agent to use `deploy-cli` to inspect a configured server before any deployment.
Check local availability with `deploy version` and `deploy --help`.
Do not trigger a remote run merely to test skill installation.

## Maintenance

Edit `deploy-cli/SKILL.md` in this repository first, then reinstall both copies.
Check examples against `deploy-cli/cmd` and the installed CLI flags.
Keep credentials, machine-specific addresses, and runtime logs out of this directory.
