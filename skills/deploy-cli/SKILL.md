---
name: deploy-cli
description: Deploy projects, run tasks, and manage middleware on a self-hosted deploy-server using the deploy CLI. Use for deploy-cli or deploy-server operations.
---

# Deploy CLI

`deploy` executes commands on a remote deploy-server. Installing this skill does not install the CLI or initialize the server.

## Select and verify the server

- Check `deploy version`, `deploy --help`, and the relevant subcommand's `--help`. Prefer current flag definitions over older help examples.
- If missing, locate the binary (`Get-Command deploy` on Windows). To build from source, run `go install ./cmd/deploy` in the repository's `deploy-cli` directory with Go 1.24+.
- Read configured servers with `deploy server list --json`; avoid displaying token fields. Configuration defaults to `~/.deploy/config.yaml`, overridden by `DEPLOY_CONFIG`.
- The server name is its configuration key, not a separate display name. Use the user's intended target; ask if ambiguous. Verify it with `deploy --server <server> whoami` and carry the same `--server` through every remote command, including logs and cancellation.
- Address/token precedence is flags > `DEPLOY_ADDRESS` / `DEPLOY_TOKEN` > selected server configuration. Legacy `DEPLOY_SERVER` also means an address. Check overrides without exposing credentials and resolve any target mismatch before continuing.
- For a new connection, use `deploy login -a <address> --name <server>` with user-provided connection details.

Tokens grant remote command execution. Keep them in local configuration with restricted permissions; redact credentials from output and logs. Inspect the target and effects before changes, and act within the user's authorization. Never trigger a deployment merely to test skill installation.

## Inspect or create a project

```bash
deploy --server <server> doctor --json
deploy --server <server> project list --json
deploy --server <server> project get <project> --json
```

Read actual project keys, commands, and variables before running. Create projects only when requested, after checking for an existing one:

```bash
deploy --server <server> project create --file project.json --json
deploy --server <server> project get <project> --json
```

Example definition (adapt paths and commands to the server):

```json
{
  "key": "my-app",
  "name": "my-app",
  "workdir": "/srv/deploy/workspaces/my-app",
  "shell": "sh",
  "concurrency": "queue",
  "hookEnabled": false,
  "commands": [{"name": "check", "script": "pwd"}]
}
```

`workdir` is an absolute server path. Each command uses either `script` or `scriptFile`; the latter is a local file resolved relative to the JSON file. Explicit flags override JSON, and `--command` replaces its command list. Git preparation requires `useGit: true` and `gitUrl`; inspect branch and cleanup settings before execution. Creating a project does not run it.

### Bundled Java deployment example

Use [assets/tio-boot-web-hello/project.json](assets/tio-boot-web-hello/project.json) as a complete Maven/systemd example. Copy the entire `assets/tio-boot-web-hello` directory to a working directory before adapting it; retain the adjacent `sh/` directory so relative `scriptFile` paths resolve. These files mirror the repository's `demo/tio-boot-web-hello` example, including its original comments and messages.

| Script | Purpose |
|---|---|
| [01-recon.sh](assets/tio-boot-web-hello/sh/01-recon.sh) | Inspect host, tools, source, Maven cache, and ports |
| [02-build.sh](assets/tio-boot-web-hello/sh/02-build.sh) | Build the executable JAR with Maven's production profile |
| [03-publish.sh](assets/tio-boot-web-hello/sh/03-publish.sh) | Copy the JAR and write external port configuration |
| [04-systemd.sh](assets/tio-boot-web-hello/sh/04-systemd.sh) | Write the service unit, enable it, clear old logs, and restart |
| [05-healthcheck.sh](assets/tio-boot-web-hello/sh/05-healthcheck.sh) | Wait for HTTP 200 and show diagnostic logs |
| [06-ops.sh](assets/tio-boot-web-hello/sh/06-ops.sh) | Inspect service status, ports, configuration, and logs |

Before creating a project from this example, adapt the Git URL/branch, project key, workspace, JAR pattern, application directory, service name, and port (default `10062`). The build and service scripts hard-code JDK `/usr/java/jdk1.8.0_411`; update both consistently. The inspection script assumes a Maven cache under `/root/.m2`. The example requires Linux, Bash, Maven, Java, curl, and systemd, with permission to write `/srv` and `/etc/systemd/system` and manage services. Keep the service working directory aligned with the published external configuration.

The full project runs all six steps. For an operations-only check, select the actual command ID for `06-ops.sh` using `--command-id`; do not rerun the whole project. Review the service replacement and log deletion in step 4 before execution. Import with `project create --file <copied-directory>/project.json --json`, then inspect the created project before an authorized run.

## Run and track completion

```bash
# Wait for a terminal result.
deploy --server <server> run <project> --wait --json

# Or start, retain runId, and follow separately.
deploy --server <server> run <project> --json
deploy --server <server> logs <runId> -f --json

# History and cancellation.
deploy --server <server> runs list --project <project> --json
deploy --server <server> stop <runId>
```

- Pass variables with `-e TAG=v1.2.3` and a branch with `--branch release`. For selected steps, obtain IDs from `project get` and use `--command-id <id>`.
- `run` has no `--dry-run` or `--no-follow`. Without waiting/following, its response only acknowledges submission.
- Interrupted waiting may exit with code 0 while the server continues. Report success only after terminal `status=success`; disconnecting log follow does not cancel a run.
- If submission times out, inspect `runs list` before retrying to avoid duplicate execution. There is no `runs get`; resume tracking with the original run ID and `logs -f --json`.

## Manage middleware

Supported components: `docker`, `postgresql`, `mysql`, `redis`, and `elasticsearch`. Installation requires a Linux server; other platforms support dry-run only. Docker uses native mode, Elasticsearch uses container mode, and other components default to containers.

Inspect configuration and preview installation first:

```bash
deploy --server <server> middleware list --json
deploy --server <server> middleware get redis --json
deploy --server <server> middleware fields redis
deploy --server <server> middleware install redis --dry-run --no-follow --json
deploy --server <server> logs <runId> -f --json
```

Dry-run creates a run record and logs without installing. Read its full plan and terminal result, then confirm exposed ports and mounted volumes are within the user's authorization before installation. Defaults may expose unauthenticated services; harden credentials, access, and ports before public exposure.

```bash
deploy --server <server> middleware set redis -p REDIS_PORT=16380
deploy --server <server> middleware install redis --no-follow --json
deploy --server <server> logs <runId> -f --json
deploy --server <server> middleware get redis --json
```

- After completion, verify `installed`, `running`, ports, and mode with `middleware get`.
- `--no-follow` returns a run ID; otherwise middleware operations follow by default. Use `--follow`, not `-f`, for middleware commands. The separate `logs` command supports `-f`.
- Install multiple components in dependency order, e.g. `middleware install docker redis postgresql`, or use `install --missing`. Rebuilding requires `--force` / `-F`; native installation uses `--mode native`.
- Use `middleware start`, `stop`, `restart`, or `logs` with the component key and `--no-follow`. Track returned run IDs. Only installation supports dry-run.
- `middleware set <component> -p KEY=value` changes only supplied keys. Password values are encrypted and cannot be read back; an empty password value resets it to generated-password behavior.
- `middleware remove <component> --yes --no-follow` requires authorization for removal. Container removal preserves data volumes; native removal prints manual instructions. Deleting a volume is a separate destructive operation requiring explicit authorization.

## Interpret results and failures

`--json` writes data to stdout; diagnostics go to stderr. Log follow emits JSONL, with `{"type":"log","data":"..."}` entries and a terminal summary containing `runId`, `status`, and possibly `exitCode`. Parse it line by line.

| Exit code | Meaning |
|---|---|
| 0 | CLI success; still verify the run's terminal status |
| 1 | Run failed, timed out, errored, or was canceled |
| 2 | Invalid arguments or unknown server |
| 3 | Authentication failure |
| 4 | Network or service unavailable |

HTTP 200 can contain `success:false`; use CLI exit codes and result fields. Report the server, project/component, run ID, terminal status, exit code when available, and verification outcome. State explicitly when completion remains unconfirmed.

For failures, inspect logs before retrying:

- Port conflicts may only produce a warning before container startup fails. Check existing services and configured ports.
- Image pulls may pause for about 60 seconds, then fall back to a matching local image. A fallback warning is not itself a failure; if no image exists, inspect Docker connectivity and proxy settings before retrying.
- A missing middleware API endpoint indicates an older server requiring an upgrade.
- `nothingToDo: true` means the requested components are already installed; use `--force` only when rebuilding is intended.
