# project-deploy

English | [简体中文](README.zh-CN.md)

A lightweight, self-hosted CI/CD platform. Define a working directory and a sequence of shell commands, then deploy through the web UI, CLI, or HTTP hooks with live logs and exit codes.

[Repository](https://gitee.com/ppnt/deploy) · [API reference](docs/API设计.md) · [CLI reference](deploy-cli/README.md)

- [Overview](#overview)
- [Quick start](#quick-start)
- [Server deployment](#deployment)
- [Project workflow](#workflow)
- [CLI and automation](#cli)
- [Toolchains and middleware](#installers)
- [Operations](#operations)
- [Documentation](#documentation)

<a id="overview"></a>

## Overview

Deployment steps are ordinary shell scripts; no pipeline DSL is required. Commands run on the server hosting the platform, or inside its container when deployed with Docker. The server ships as a single binary with an embedded UI and SQLite, with no external database or message queue required.

| Component | Purpose |
|---|---|
| [deploy-server](deploy-server/) | Go 1.24, Gin, SQLite, API, and execution engine |
| [deploy-server-ui](deploy-server-ui/) | React 18, Umi Max 4, and Ant Design 5 |
| [deploy-cli](deploy-cli/) | Server management, deployments, and logs |
| [skills](skills/README.md) | Agent instructions and installation guide |
| [tools](tools/) | SSH utilities, provisioning scripts, and smoke checks |

Core features:

- **Controlled execution:** queues, timeouts, process-tree cancellation, and dependency builds.
- **Live logs:** WebSocket streaming, reconnect recovery, history, and downloads.
- **Git and secrets:** optional Git preparation, encrypted credentials, and masking of configured secrets in deployment logs.
- **Automation:** CLI JSON output and HTTP hooks for scripts and AI agents.

<a id="quick-start"></a>

## Quick start

Build prerequisites: **Go 1.24+, Node.js 22, and pnpm**; Linux/macOS also need make and Git. Build the frontend, copy its output into the server, then compile Go. The target server needs only the resulting binary and the tools used by project scripts.

Start each build example from the repository root. Replace placeholders and sample names with your own values.

### Windows

```powershell
cd deploy-server-ui
pnpm.cmd install
pnpm.cmd build
cd ..\deploy-server
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Local
.\bin\deploy-server.exe -listen 127.0.0.1:10055 -data-dir .\data -web-root .\workspaces
```

The Windows-only script produces `bin/deploy-server-linux-amd64`; `-Local` also produces `bin/deploy-server.exe`. Use `-Version <version>` to set version metadata. Use `-SkipUI` only when intentionally reusing the previously embedded frontend.

### Linux / macOS

```bash
cd deploy-server-ui
pnpm install
pnpm build
cd ../deploy-server
make ui
make run
```

For a Linux server, run `make linux` or `make linux-arm64` after `make ui`. These targets set `CGO_ENABLED=0` and write binaries to `deploy-server/bin/`.

### Initial setup

Open <http://127.0.0.1:10055> and create the first administrator account. There are **no preconfigured accounts or default passwords**. Usernames require at least 3 characters and passwords at least 8. Complete setup before exposing the instance to other users.

Use **Environment Check** to inspect server tools. Set the external URL under **System Settings → Server External URL**; scripts receive it as `DEPLOY_SERVER_URL` without a restart.

<a id="deployment"></a>

## Server deployment

### Linux with systemd

Upload the binary and the supplied [service unit](deploy-server/deploy/deploy-server.service) from the repository root:

```bash
scp deploy-server/bin/deploy-server-linux-amd64 'root@<server>:/tmp/deploy-server'
scp deploy-server/deploy/deploy-server.service 'root@<server>:/tmp/deploy-server.service'
```

Then run the following as root on the server. This example binds to loopback; access the UI through a reverse proxy or SSH tunnel.

```bash
install -m 0755 /tmp/deploy-server /usr/local/bin/deploy-server
mkdir -p /srv/deploy/data /srv/deploy/workspaces
chmod 750 /srv/deploy/data /srv/deploy/workspaces
install -m 0644 /tmp/deploy-server.service /etc/systemd/system/deploy-server.service
sed -i 's/0.0.0.0:10055/127.0.0.1:10055/' /etc/systemd/system/deploy-server.service
systemctl daemon-reload
systemctl enable --now deploy-server
systemctl is-active deploy-server
curl -fsS http://127.0.0.1:10055/api/v1/healthz
```

The supplied unit runs as root and originally listens on `0.0.0.0:10055` for LAN access. Review its paths and permissions. The service uses the host system's timezone. Keep the graceful shutdown settings; `NoNewPrivileges=true` can prevent scripts from using sudo to elevate privileges.

### Docker

Build from the repository root so the Dockerfile can access both the frontend and backend:

```bash
docker build -t deploy-server:local -f deploy-server/Dockerfile .
docker run -d --name deploy-server \
  -p 127.0.0.1:10055:10055 \
  -v /srv/deploy/data:/data \
  -v /srv/deploy/workspaces:/workspaces \
  --restart unless-stopped deploy-server:local
```

The image includes Git, Bash, curl, rsync, and an SSH client. Project commands run inside the container: host directories require mounts, and container-local systemctl cannot manage host services. Prefer systemd when deploying directly to the host.

### Reverse proxy and HTTPS

For public access, use HTTPS and keep port 10055 private. Nginx must forward WebSocket upgrades and disable buffering for live logs. Add this location to your HTTPS server block:

```nginx
location / {
    proxy_pass http://127.0.0.1:10055;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 3600s;
    proxy_buffering off;
}
```

Configure your domain and certificates before enabling HTTPS. Certbot HTTP-01 validation requires DNS to point to the public entry server and the challenge endpoint to be reachable.

<a id="workflow"></a>

## Project workflow

1. **Create a project:** choose a unique key, name, and working directory. The default directory is `${WORKSPACE_ROOT}/${PROJECT_KEY}`; avoid the reserved key `new`.
2. **Configure Git if needed:** Git preparation is disabled by default. For private repositories, create a credential and test the connection.
3. **Add commands:** steps run in order, with optional working-directory, shell, and timeout overrides. The default failure policy stops execution; “continue” ignores that step's failure.
4. **Run and inspect:** trigger from the UI, CLI, or a hook, then check the final status, exit code, and logs.

Projects queue runs by default. Other policies allow parallel runs or cancel the previous run. A timeout of 0 means no limit. For Bash scripts, use `set -e` and finish with a meaningful health check; each script's exit code is evaluated using its failure policy.

### Environment variables

| Variable | Meaning |
|---|---|
| `WORKDIR` | Resolved working directory |
| `PROJECT_KEY`, `PROJECT_NAME` | Project identity |
| `RUN_ID`, `BUILD_NUMBER` | Run ID and per-project build number |
| `TRIGGER_BY`, `TRIGGER_TYPE`, `START_TIME` | Trigger metadata and RFC3339 start time |
| `DEPLOY_SERVER_URL` | External URL from system settings |
| `GIT_BRANCH`, `GIT_COMMIT`, `GIT_COMMIT_SHORT`, `GIT_TAG`, `GIT_AUTHOR`, `GIT_MESSAGE`, `GIT_DIRTY` | Available when Git preparation is enabled |

Environment precedence, lowest to highest: system → project variables → project secrets → built-in variables → trigger parameters. Trigger overrides require the project's permission to override parameters.

### Dependency builds

Configure dependencies and choose **Run with dependencies**, or send `withDependencies: true` through the API. Dependencies run before the target, and each project is built once per pipeline. All dependencies are rebuilt on each trigger; failures stop downstream execution.

The selected branch is passed to dependencies, but trigger-time environment overrides apply only to the target. Each run has its own logs and build number; canceling a run also cancels active or queued runs in the same pipeline.

### Platform differences

| Behavior | Linux | Windows |
|---|---|---|
| Default shell | `bash -lc` | PowerShell with `-NoProfile -Command`; prefers `pwsh` |
| Environment variables | `${BUILD_NUMBER}` | `$env:BUILD_NUMBER` |
| Process-tree cancellation | Process group signals | `taskkill /T /F` |

<a id="cli"></a>

## CLI and automation

Install from the repository root, then ensure GOBIN (or GOPATH/bin) is on your PATH:

```bash
cd deploy-cli
go install ./cmd/deploy
```

Create an API token under **Personal Settings → API Token**, then register the server. Prefer a revocable `dk_` API token for automation because login JWTs expire.

```bash
deploy server add prod -a 'https://deploy.example.com' -t '<api-token>' -u admin
deploy server list
deploy whoami
deploy project list
deploy --server prod run my-app -f
deploy --server prod run my-app --wait --json
deploy logs 88 -f
deploy stop 88
deploy doctor
```

Server profiles are stored in `~/.deploy/config.yaml`. Use `deploy server use <name>` to change the default or `--server <name>` for one command. Explicit flags take precedence over environment variables (`DEPLOY_ADDRESS`, `DEPLOY_TOKEN`), then saved configuration. See the [CLI reference](deploy-cli/README.md) for details.

| Exit code | Meaning |
|---|---|
| 0 | Success |
| 1 | Run failed, timed out, errored, or was canceled |
| 2 | Invalid usage or unknown server name |
| 3 | Authentication failure |
| 4 | Network or service unavailable |

Ctrl+C disconnects log following without canceling the run; add `--cancel-on-interrupt` to cancel it. A queued run is not a successful deployment: automation must wait for the final result.

### HTTP hooks

Create a hook token in the project details. Prefer the request header form to keep tokens out of URL access logs:

```bash
curl -X POST 'https://deploy.example.com/api/v1/hooks' \
  -H 'X-Deploy-Token: <hook-token>' \
  -H 'Content-Type: application/json' \
  -d '{"branch":"main","withDependencies":true,"async":true}'
```

Hooks return a run ID asynchronously by default. Use `?wait=true` or `"async": false` to wait for completion. Check the response body's `success` field as well as the run status: business errors can use HTTP 200.

AI agents can use the CLI's `--help` and `--json` output to discover commands and inspect live state. See the [AI integration guide](docs/ai/AI对接.md) and [skill installation guide](skills/README.md).

<a id="installers"></a>

## Toolchains and middleware

Administrators can install tools from **Environment Check** and follow installation logs as regular runs. Installation scripts target Linux; use dry-run to review the plan before applying changes.

| Category | Components |
|---|---|
| Toolchains and utilities | C/C++, Java, Maven, nvm, Node.js, pnpm, Go, Rust, Certbot |
| Middleware | Docker, PostgreSQL, MySQL, Redis, Elasticsearch |

Toolchains and middleware have separate settings and scripts. Docker is installed natively; other middleware services use containers by default. Elasticsearch supports container installation only. Installing Certbot does not issue a certificate.

```bash
deploy middleware list
deploy middleware get redis
deploy middleware install redis --dry-run --no-follow
deploy middleware install redis --no-follow
deploy logs <run-id> -f
deploy middleware restart redis --no-follow
deploy middleware logs redis --no-follow
```

`--no-follow` returns immediately with a run ID. Existing installations are skipped by default; `--force` rebuilds containers while retaining data volumes. Removal also retains data volumes. Saved `*_PASSWORD` parameters are encrypted; generated passwords may appear once in installation logs, so restrict access to those logs.

Review ports, passwords, and access controls before exposing middleware: defaults are intended for private networks, and Elasticsearch security is disabled by default. See [toolchain/install.sh](deploy-server/internal/toolchain/install.sh) and [middleware/install.sh](deploy-server/internal/middleware/install.sh) for scripts and supported options.

<a id="operations"></a>

## Operations

### Updates and backups

Replace the server binary and restart the service to upgrade. Back up the entire data directory while the service is stopped; back up project workspaces separately if needed.

```bash
systemctl stop deploy-server
tar czf "deploy-backup-$(date +%F).tar.gz" -C /srv/deploy data
systemctl start deploy-server
```

**Keep `data/secret.key` with the database.** Losing it makes encrypted secrets and Git credentials unrecoverable. Protect CLI configuration and tokens as server credentials. The platform executes commands with the service account's permissions; the supplied systemd unit uses root.

### Common issues

| Symptom | Check |
|---|---|
| UI missing or showing a placeholder | Rebuild the frontend, copy it, then rebuild the server; verify `web.built` with `deploy doctor`. |
| Live logs stop updating | Check WebSocket proxy headers and buffering. |
| Git preparation fails | Test the connection and credentials; use an empty directory or an existing Git checkout. |
| Tool not found | Install it on the server and expose it to the execution shell; Bash login shells read `/etc/profile`, not just `~/.bashrc`. |
| Middleware container exits | Check ports, directory permissions, and `deploy middleware logs <key>`. |
| CLI authentication fails | Check the selected server and token; replace an expired JWT with an API token. |
| HTTP 200 but operation failed | Inspect `success`, the final run status, and the exit code. |

```bash
journalctl -u deploy-server -n 100 --no-pager
curl -fsS http://127.0.0.1:10055/api/v1/healthz
```

### Development checks

Run from the repository root:

```bash
cd deploy-server
go test ./... -count=1
cd ../deploy-cli
go test ./... -count=1
```

Additional process-tree and deployment checks are available in [tools/smoke](tools/smoke/).

<a id="documentation"></a>

## Documentation

Some detailed documents are currently available in Chinese only.

| Document | Contents |
|---|---|
| [Server guide](deploy-server/README.md) | Configuration, deployment, and code layout |
| [CLI reference](deploy-cli/README.md) | Commands, server profiles, authentication, and output formats |
| [Frontend guide](deploy-server-ui/README.md) | UI development |
| [API contract](docs/API设计.md) | Request and response definitions |
| [Requirements](docs/需求规格说明.md) | Scope, design decisions, and acceptance criteria |
| [AI integration](docs/ai/AI对接.md) | CLI-based automation |
| [Skills](skills/README.md) | Agent skill setup |
| [Java deployment example](docs/cases/mosskb-web-部署案例.md) | A multi-module application with dependency builds |
