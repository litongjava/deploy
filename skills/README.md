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
$source = Join-Path (Get-Location) "skills/deploy-cli"
foreach ($root in @($codexHome, $dshHome)) {
    $target = Join-Path $root "skills/deploy-cli"
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    Get-ChildItem -LiteralPath $source | Copy-Item -Destination $target -Recurse -Force
    foreach ($file in (Get-ChildItem -LiteralPath $source -File -Recurse)) {
        $relative = $file.FullName.Substring($source.Length + 1)
        $installed = Join-Path $target $relative
        if ((Get-FileHash -LiteralPath $file.FullName).Hash -ne (Get-FileHash -LiteralPath $installed).Hash) {
            throw "Installed file mismatch: $installed"
        }
    }
    Write-Output "Installed and verified: $target"
}
```

The commands copy and verify the entire skill, including bundled assets. Restart Codex or start a new session after installation.
For DSH, start a new session if its skill catalog does not list `deploy-cli`.
Do not assume that an existing session reloads its skill catalog.

Ask the agent to use `deploy-cli` to inspect a configured server before any deployment.
Check local availability with `deploy version` and `deploy --help`.
Do not trigger a remote run merely to test skill installation.

## Maintenance

Edit `deploy-cli/SKILL.md` in this repository first, then reinstall both copies.
The bundled `deploy-cli/assets/tio-boot-web-hello` files mirror `demo/tio-boot-web-hello`.
When updating that demo, refresh the bundled copy and reinstall the entire skill directory.
Check examples against `deploy-cli/cmd` and the installed CLI flags.
Keep credentials, machine-specific addresses, and runtime logs out of this directory.
