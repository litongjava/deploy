# project-deploy

[English](README.md) | 简体中文

一个轻量、自托管的 CI/CD 平台。为项目配置工作目录和一组 Shell 命令，即可通过网页、CLI 或 HTTP Hook 执行部署，实时查看日志与退出码。

[代码仓库](https://gitee.com/ppnt/deploy) · [API 文档](docs/API设计.md) · [CLI 文档](deploy-cli/README.md)

- [项目概览](#overview)
- [快速开始](#quick-start)
- [服务器部署](#deployment)
- [项目使用](#workflow)
- [命令行与自动化](#cli)
- [工具链与中间件](#installers)
- [运维与排查](#operations)
- [更多文档](#documentation)

<a id="overview"></a>

## 项目概览

部署步骤就是普通的 Shell 脚本，无需编写流水线 DSL。命令在平台服务端所在机器上执行；使用 Docker 部署时，命令在容器内执行。服务端以单个二进制交付，内嵌网页并使用 SQLite，无需外部数据库或消息队列。

| 组件 | 用途 |
|---|---|
| [deploy-server](deploy-server/) | Go 1.24、Gin、SQLite、API 与执行引擎 |
| [deploy-server-ui](deploy-server-ui/) | 基于 React 18、Umi Max 4 和 Ant Design 5 的 Web 管理界面 |
| [deploy-cli](deploy-cli/) | 服务器管理、部署与日志命令行客户端 |
| [skills](skills/README.md) | AI Agent 技能与安装说明 |
| [tools](tools/) | SSH 工具、服务器初始化与冒烟检查脚本 |

核心功能：

- **执行可控：** 队列、超时、进程树取消与依赖构建。
- **实时日志：** WebSocket 推送、断线续传、历史查看与下载。
- **Git 与密钥：** 按需启用 Git 准备，加密保存凭据，并在部署日志中遮蔽已配置的 Secret。
- **自动化：** 通过 CLI JSON 输出与 HTTP Hook 对接脚本和 AI Agent。

<a id="quick-start"></a>

## 快速开始

构建环境需要 **Go 1.24+、Node.js 22 和 pnpm**；Linux/macOS 还需 make 与 Git。构建顺序为：前端构建 → 拷贝前端产物 → 编译 Go。运行服务器只需二进制文件，以及项目脚本所需的工具。

以下构建示例均从仓库根目录开始执行。请替换占位值及示例名称。

### Windows

```powershell
cd deploy-server-ui
pnpm.cmd install
pnpm.cmd build
cd ..\deploy-server
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Local
.\bin\deploy-server.exe -listen 127.0.0.1:10055 -data-dir .\data -web-root .\workspaces
```

该脚本仅支持 Windows，默认生成 `bin/deploy-server-linux-amd64`；加上 `-Local` 还会生成 `bin/deploy-server.exe`。可用 `-Version <version>` 设置版本信息；仅在需要复用已嵌入的前端时使用 `-SkipUI`。

### Linux / macOS

```bash
cd deploy-server-ui
pnpm install
pnpm build
cd ../deploy-server
make ui
make run
```

部署到 Linux 服务器时，在 `make ui` 后执行 `make linux` 或 `make linux-arm64`。这些目标会设置 `CGO_ENABLED=0`，产物位于 `deploy-server/bin/`。

### 首次初始化

打开 <http://127.0.0.1:10055>，创建首个管理员账号。系统**没有预置账号或默认密码**，用户名至少 3 位，密码至少 8 位。对外开放实例前，请先完成初始化。

在「环境自检」中检查服务端工具；在「系统设置 → 服务端外部地址」中填写访问地址。该设置无需重启，会以 `DEPLOY_SERVER_URL` 注入项目脚本。

<a id="deployment"></a>

## 服务器部署

### 使用 systemd

在仓库根目录上传二进制文件与提供的 [service unit](deploy-server/deploy/deploy-server.service)：

```bash
scp deploy-server/bin/deploy-server-linux-amd64 'root@<server>:/tmp/deploy-server'
scp deploy-server/deploy/deploy-server.service 'root@<server>:/tmp/deploy-server.service'
```

然后以 root 在服务器上执行以下命令。示例绑定本机回环地址，请通过反向代理或 SSH 隧道访问网页。

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

仓库中的 unit 以 root 运行，原始监听地址为 `0.0.0.0:10055`，便于局域网访问。服务默认使用宿主机系统时区。请检查目录与权限，并保留优雅停止配置；`NoNewPrivileges=true` 可能阻止脚本通过 sudo 提权。

### Docker

从仓库根目录构建，确保 Dockerfile 能同时访问前端与后端：

```bash
docker build -t deploy-server:local -f deploy-server/Dockerfile .
docker run -d --name deploy-server \
  -p 127.0.0.1:10055:10055 \
  -v /srv/deploy/data:/data \
  -v /srv/deploy/workspaces:/workspaces \
  --restart unless-stopped deploy-server:local
```

镜像包含 Git、Bash、curl、rsync 和 SSH 客户端。项目命令在容器内执行：访问宿主机目录需要挂载，容器内的 systemctl 不能管理宿主机服务。直接部署宿主机应用时，建议使用 systemd。

### 反向代理与 HTTPS

公网访问应使用 HTTPS，并避免直接暴露 10055 端口。Nginx 必须转发 WebSocket 升级请求并关闭缓冲，实时日志才能正常工作。在 HTTPS server 配置中加入：

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

启用 HTTPS 前，请配置好域名与证书。Certbot 的 HTTP-01 验证要求 DNS 指向公网入口服务器，且验证端点可以访问。

<a id="workflow"></a>

## 项目使用

1. **创建项目：** 设置唯一 Key、名称与工作目录，默认目录为 `${WORKSPACE_ROOT}/${PROJECT_KEY}`；不要使用保留 Key `new`。
2. **按需配置 Git：** Git 准备默认关闭。私有仓库需先创建凭据，并通过「测试连接」验证。
3. **添加命令：** 步骤按顺序执行，可分别覆盖工作目录、Shell 与超时。默认失败即中止；「继续」策略会忽略该步骤的失败。
4. **执行并检查：** 通过网页、CLI 或 Hook 触发，然后检查最终状态、退出码与日志。

项目默认串行排队，也可设置为并行执行或取消上一次运行；超时设为 0 表示不限时。Bash 脚本建议使用 `set -e`，并在最后执行有效的健康检查；平台根据每条脚本的退出码及失败策略判断结果。

### 环境变量

| 变量 | 含义 |
|---|---|
| `WORKDIR` | 实际工作目录 |
| `PROJECT_KEY`, `PROJECT_NAME` | 项目标识与名称 |
| `RUN_ID`, `BUILD_NUMBER` | 运行 ID 与项目构建号 |
| `TRIGGER_BY`, `TRIGGER_TYPE`, `START_TIME` | 触发信息与 RFC3339 开始时间 |
| `DEPLOY_SERVER_URL` | 系统设置中的服务端外部地址 |
| `GIT_BRANCH`, `GIT_COMMIT`, `GIT_COMMIT_SHORT`, `GIT_TAG`, `GIT_AUTHOR`, `GIT_MESSAGE`, `GIT_DIRTY` | 启用 Git 准备时注入 |

环境变量优先级由低到高：系统环境 → 项目普通变量 → 项目 Secret → 内置变量 → 触发参数。通过触发参数覆盖变量时，需开启项目的「允许参数覆盖」。

### 依赖构建

配置依赖项目后，选择「含依赖执行」，或在 API 中传入 `withDependencies: true`。依赖先于目标项目执行，同一项目在一次流水线中只构建一次。每次触发都会重新构建全部依赖；依赖失败会中止后续执行。

触发时指定的分支会传递给依赖，但临时环境变量覆盖仅作用于目标项目。各次运行有独立日志与构建号；取消其中一次运行，会同时取消该流水线中正在执行或排队的其他运行。

### 平台差异

| 行为 | Linux | Windows |
|---|---|---|
| 默认解释器 | `bash -lc` | PowerShell，使用 `-NoProfile -Command` 参数，优先选择 `pwsh` |
| 环境变量引用 | `${BUILD_NUMBER}` | `$env:BUILD_NUMBER` |
| 进程树取消 | 进程组信号 | `taskkill /T /F` |

<a id="cli"></a>

## 命令行与自动化

在仓库根目录安装，并将 GOBIN（未设置时为 GOPATH/bin）加入 PATH：

```bash
cd deploy-cli
go install ./cmd/deploy
```

在「个人设置 → API Token」中创建令牌，再添加服务器。自动化建议使用可撤销的 `dk_` API Token，避免登录 JWT 过期导致任务中断。

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

服务器配置保存在 `~/.deploy/config.yaml`。使用 `deploy server use <name>` 切换默认服务器，或通过 `--server <name>` 为单次命令选择服务器。显式参数优先于环境变量（`DEPLOY_ADDRESS`、`DEPLOY_TOKEN`），其次为已保存配置。详细规则见 [CLI 文档](deploy-cli/README.md)。

| 退出码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 运行失败、超时、错误或取消 |
| 2 | 参数错误或服务器名称不存在 |
| 3 | 鉴权失败 |
| 4 | 网络或服务不可用 |

Ctrl+C 默认只断开日志跟随，不取消运行；加上 `--cancel-on-interrupt` 才会取消。进入队列不代表部署成功，自动化流程应等待最终结果。

### HTTP Hook

在项目详情中创建 Hook Token。建议通过请求头传递，避免令牌出现在 URL 访问日志中：

```bash
curl -X POST 'https://deploy.example.com/api/v1/hooks' \
  -H 'X-Deploy-Token: <hook-token>' \
  -H 'Content-Type: application/json' \
  -d '{"branch":"main","withDependencies":true,"async":true}'
```

Hook 默认异步返回运行 ID；使用 `?wait=true` 或 `"async": false` 可等待完成。除运行状态外，还应检查响应体的 `success` 字段：业务错误也可能返回 HTTP 200。

AI Agent 可通过 CLI 的 `--help` 与 `--json` 了解命令并查询实时状态。参见 [AI 对接指南](docs/ai/AI对接.md)与[技能安装说明](skills/README.md)。

<a id="installers"></a>

## 工具链与中间件

管理员可在「环境自检」中安装工具，安装过程会作为普通运行记录并提供日志。安装脚本面向 Linux；建议先通过预演查看执行计划。

| 类别 | 组件 |
|---|---|
| 工具链与辅助工具 | C/C++, Java, Maven, nvm, Node.js, pnpm, Go, Rust, Certbot |
| 中间件 | Docker, PostgreSQL, MySQL, Redis, Elasticsearch |

工具链与中间件使用独立配置和脚本。Docker 采用原生安装，其余中间件默认使用容器；Elasticsearch 仅支持容器安装。安装 Certbot 不会自动签发证书。

```bash
deploy middleware list
deploy middleware get redis
deploy middleware install redis --dry-run --no-follow
deploy middleware install redis --no-follow
deploy logs <run-id> -f
deploy middleware restart redis --no-follow
deploy middleware logs redis --no-follow
```

`--no-follow` 会立即返回运行 ID。默认跳过已安装组件；`--force` 会重建容器，但保留数据卷，移除操作也保留数据卷。已保存的 `*_PASSWORD` 参数会加密存储；安装时生成的密码可能在日志中显示一次，请限制日志访问权限。

中间件默认配置面向内网，Elasticsearch 默认关闭安全认证。对外开放前，请检查端口、密码与访问控制。完整脚本及选项见 [toolchain/install.sh](deploy-server/internal/toolchain/install.sh) 和 [middleware/install.sh](deploy-server/internal/middleware/install.sh)。

<a id="operations"></a>

## 运维与排查

### 升级与备份

升级时替换服务端二进制并重启服务。请停服后备份整个数据目录；项目工作区如需保留，应单独备份。

```bash
systemctl stop deploy-server
tar czf "deploy-backup-$(date +%F).tar.gz" -C /srv/deploy data
systemctl start deploy-server
```

**务必将 `data/secret.key` 与数据库一起备份。** 丢失该文件将无法恢复已加密的 Secret 与 Git 凭据。CLI 配置和令牌也应按服务器凭据保护。平台以服务账号的权限执行命令，仓库提供的 systemd unit 使用 root。

### 常见问题

| 现象 | 排查方法 |
|---|---|
| 网页缺失或显示占位页 | 按前端构建、拷贝、后端编译的顺序重建，并用 `deploy doctor` 检查 `web.built`。 |
| 实时日志不刷新 | 检查反代的 WebSocket 请求头与缓冲配置。 |
| Git 准备失败 | 测试仓库连接与凭据，并使用空目录或已有 Git 工作区。 |
| 找不到构建工具 | 在服务端安装工具并配置执行环境；Bash 登录 Shell 读取 `/etc/profile`，不能只配置 `~/.bashrc`。 |
| 中间件容器退出 | 检查端口冲突、目录权限及中间件日志。 |
| CLI 鉴权失败 | 检查所选服务器与令牌，建议将过期 JWT 换成 API Token。 |
| HTTP 200 但操作失败 | 检查 `success`、最终运行状态与退出码。 |

```bash
journalctl -u deploy-server -n 100 --no-pager
curl -fsS http://127.0.0.1:10055/api/v1/healthz
```

### 开发检查

在仓库根目录执行：

```bash
cd deploy-server
go test ./... -count=1
cd ../deploy-cli
go test ./... -count=1
```

更多进程树取消与部署检查脚本见 [tools/smoke](tools/smoke/)。

<a id="documentation"></a>

## 更多文档

部分详细文档目前仅提供中文版本。

| 文档 | 内容 |
|---|---|
| [服务端说明](deploy-server/README.md) | 配置、部署与代码结构 |
| [命令行说明](deploy-cli/README.md) | 命令、服务器配置、鉴权与输出格式 |
| [前端说明](deploy-server-ui/README.md) | 前端开发 |
| [API 契约](docs/API设计.md) | 请求与响应定义 |
| [需求规格](docs/需求规格说明.md) | 项目范围、设计决策与验收标准 |
| [AI 对接](docs/ai/AI对接.md) | 基于 CLI 的自动化 |
| [技能安装](skills/README.md) | Agent 技能配置 |
| [Java 部署案例](docs/cases/mosskb-web-部署案例.md) | 含依赖构建的多模块应用部署 |
