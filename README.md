# project-deploy

一个**自托管的轻量命令式 CI/CD 平台**：项目 = 一个工作目录 + 一组 sh 命令；部署 = 点一下跑起来、实时看日志、拿退出码。
不写 Jenkinsfile / 流水线 DSL / YAML 编排，直接写 shell。

- [组成与架构](#组成与架构) · [构建与快速开始](#构建与快速开始)
- [部署到服务器](#部署到服务器)
- [使用指南](#使用指南) · [命令行客户端](#命令行客户端)
- [排查与测试](#排查与测试) · [平台差异](#平台差异) · [安全提醒](#安全提醒)

## 组成与架构

| 目录 / 服务端关注点 | 职责与技术选型 |
|---|---|
| [`deploy-server/`](deploy-server/) | 服务端：REST API、WebSocket 日志、执行引擎、内嵌前端；Go 1.24 |
| ↳ Web 框架 / 实时日志 | **Gin v1.10.0** / **gorilla/websocket v1.5.3** |
| ↳ 持久化 | **GORM v1.25.12** + **SQLite**（`glebarez/sqlite` 纯 Go 驱动） |
| ↳ 认证 | `golang-jwt/jwt/v5`（HS256），支持 `dk_` 前缀的长效 API Token |
| ↳ 密码 / 加密 | `golang.org/x/crypto/bcrypt` + 标准库 **AES-256-GCM** |
| ↳ 配置 | `gopkg.in/yaml.v3`；命令行 > 环境变量 > 配置文件 > 默认值 |
| ↳ 前端托管 | 标准库 **`embed`**，支持 SPA 路由回退；`/api/` 下的 404 仍返回 JSON |
| [`deploy-cli/`](deploy-cli/) | CLI：登录、触发部署、跟随日志；Go 1.24 / cobra |
| [`deploy-server-ui/`](deploy-server-ui/) | Web 界面：React 18 / Umi Max 4 / Ant Design 5，裁剪自 Ant Design Pro |
| [`tools/`](tools/) | 运维与冒烟脚本：`sshpoke` 做密码认证 SSH 执行/传文件；`remote-scripts` 初始化和校验服务器；`smoke` 验证进程树终止及 API 部署 |

命令在**服务端所在机器**执行，模型预留 Runner 字段；Git 准备是项目级开关；`admin` / `user` 按项目归属判权。
交付采用 SQLite + 单二进制，也支持 Docker，无需 Redis 或消息队列；工具链由用户安装，平台只自检并提示。
完整决策 D1–D6、需求与验收见[需求规格说明](docs/需求规格说明.md)。

文档：[API 契约](docs/API设计.md)（接口唯一事实来源，改接口先改这里）、[部署记录](docs/部署记录.md)（真实部署与验收证据）、[服务端说明](deploy-server/README.md)、[CLI 说明](deploy-cli/README.md)。

核心能力：

- **Secret 落盘前打码**：环境变量与 Git 凭据明文在写日志前替换为 `***`，下载日志也已脱敏。
- **Git 准备可控**：`fetch + checkout -B <ref> origin/<ref>` 强制对齐；凭据经 `GIT_ASKPASS` / `GIT_SSH_COMMAND` 注入，不拼进 URL。
- **执行可控**：同项目默认串行排队，避免同时 checkout；取消时终止整棵进程树。
- **日志可续传**：WebSocket 实时查看、断线补发与去重，机制见[运行与日志](#运行与日志)。

前端 `dist/` 经构建入口拷入 `deploy-server/internal/web/dist/`，再由 `go:embed` 编进二进制，**同一进程、同一端口**提供界面与 API。
实测内嵌 **33 个文件 / 3.19 MB**，二进制约 **17 MB（17.1 MB）**；运行服务器不需要前端目录或 Node。
验证时调用下面的 `doctor`，应看到 `web.built=true`；若为 `false`，查看 `note`，按下一节先构建前端再编译后端，避免嵌入占位页。

```bash
curl -s http://127.0.0.1:8080/api/v1/system/doctor -H 'Authorization: <你的token>'
```

全文 `<尖括号>` 表示需替换的地址、令牌或路径；替换后再执行。示例项目 `my-app`、运行 ID `88` 和示例域名也需换成自己的值。

## 构建与快速开始

推荐在开发机构建，只把二进制传到服务器；服务器无需安装 Go/Node，部署脚本用到的工具另行安装。
构建机需要 **Go 1.24+、Node 22、pnpm**，Linux/macOS 还需 make、git；顺序必须是**构建前端 → 拷贝前端 → 编译后端**。
交叉编译必须设置 `CGO_ENABLED=0`；SQLite 使用纯 Go 驱动，因此可以关闭 CGO、免依赖交付。
构建入口拷贝时排除 `*.map`（曾占 dist 一半体积，24 个文件 / 20.8 MB）和 `*hot-update*`（`max dev` 开发态产物）。

### Windows 开发机

**`build.ps1` 只能在 Windows 上跑**：虽然产出 Linux 二进制，但路径处理按 Windows 编写，脚本开头会拒绝非 Windows 环境。
本节使用 `pnpm.cmd`，避开 PowerShell 执行策略对 `pnpm.ps1` 的限制。

```powershell
cd D:\code\project\project-deploy\deploy-server-ui
pnpm.cmd install
pnpm.cmd build
cd ..\deploy-server
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version 0.1.0 -Local
.\bin\deploy-server.exe `
    -listen 127.0.0.1:8080 `
    -data-dir .\data `
    -web-root .\workspaces `
    -server-url http://127.0.0.1:8080
```

打开 <http://127.0.0.1:8080>，按[首次初始化](#首次初始化)创建管理员。
脚本内部会切换到服务端目录，因此用正确脚本路径可从任意目录调用；`-NoProfile` 避免个人配置干扰，`-ExecutionPolicy Bypass` 用于本次脚本执行。

| Windows 构建参数 | 默认 | 作用 |
|---|---|---|
| `-Version <版本号>` | `0.1.0` | 写入二进制，healthz 与 doctor 可查看 |
| `-SkipUI` | 关闭 | 跳过前端拷贝；**刚构建的前端不会被嵌入**，仅复用旧产物、只改后端时使用 |
| `-Local` | 关闭 | 除 Linux/amd64 外，额外构建 Windows 版 |

产物在 `deploy-server\bin\`：`deploy-server-linux-amd64` 用于服务器，`deploy-server.exe` 用于 Windows 本机（需 `-Local`）。
Windows 安装 PowerShell 7 时可将 `powershell` 换成 `pwsh`；含中文的 `.ps1` 文件需 **UTF-8 BOM**，否则 Windows PowerShell 5.1 按 ANSI 解码会报语法错误。

### Linux/macOS 开发机

一律使用 `make ui && make linux`；在仓库根目录执行：

```bash
cd deploy-server-ui && pnpm install && pnpm build
cd ../deploy-server && make ui && make linux
```

产物为 `deploy-server/bin/deploy-server-linux-amd64`。ARM 服务器使用 `make linux-arm64`（对应 `GOARCH=arm64`）。
本机试跑可在 `deploy-server` 下执行 `make run`；要部署服务器则继续下一节。

### 只有 Linux 服务器时就地构建

这条路线会在服务器保留 Go 与 Node 工具链；下面是 Debian/Ubuntu、amd64 的安装命令，ARM 需替换 Go 下载包架构：

```bash
apt-get update
apt-get install -y git curl make ca-certificates
curl -fsSL https://go.dev/dl/go1.24.2.linux-amd64.tar.gz | tar -C /usr/local -xz
export PATH=$PATH:/usr/local/go/bin
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs
corepack enable && corepack prepare pnpm@latest --activate
cd /srv && git clone '<你的仓库地址>' project-deploy && cd project-deploy
cd deploy-server-ui && pnpm install && pnpm build
cd ../deploy-server && make ui && make linux
```

后续会话需确保 `/usr/local/go/bin` 在 PATH 中。也可上传已有源码，省去 clone。

## 部署到服务器

以下服务器操作以 Linux/root 为例。工具链按项目安装：Git 拉取需要 `git`，静态站需要 `nginx`/`rsync`，构建需要相应语言工具链，容器任务需要 `docker`。
Debian/Ubuntu 最小静态站环境：

```bash
apt-get update
apt-get install -y git rsync curl nginx
```

### 安装 systemd 服务

**默认监听 `127.0.0.1:8080`**。仓库 unit 用 `0.0.0.0:8080` 是有意让局域网浏览器与 CLI 访问；有公网入口时，**必须改回 `127.0.0.1:8080`，经 nginx 反代 + HTTPS**。
先调整待上传的 [unit](deploy-server/deploy/deploy-server.service)；**`-server-url` 必须改为自己的地址**，它会注入 `DEPLOY_SERVER_URL`。

```ini
ExecStart=/usr/local/bin/deploy-server \
    -listen 0.0.0.0:8080 \
    -data-dir /srv/deploy/data \
    -web-root /srv/deploy/workspaces \
    -server-url http://<本机IP>:8080
```

unit 中这三项配置别删：

- `Environment=TZ=Asia/Shanghai`：固定时区，避免日志时间偏移。
- `KillSignal=SIGTERM` + `TimeoutStopSec=90`：为停止时的优雅收尾留出时间。
- `NoNewPrivileges=true`：拦截 setuid 提权，**脚本里 `sudo` 可能失败**；服务已用 root 运行，通常不需要再提权。

在开发机仓库根目录传二进制和 unit，再远程安装；将 `<服务器地址>` 换成实际 SSH 地址：

```bash
scp deploy-server/bin/deploy-server-linux-amd64 'root@<服务器地址>:/tmp/deploy-server-linux-amd64'
scp deploy-server/deploy/deploy-server.service 'root@<服务器地址>:/tmp/deploy-server.service'
ssh 'root@<服务器地址>' '
  install -m 0755 /tmp/deploy-server-linux-amd64 /usr/local/bin/deploy-server
  mkdir -p /srv/deploy/data /srv/deploy/workspaces
  chmod 750 /srv/deploy/data /srv/deploy/workspaces
  install -m 0644 /tmp/deploy-server.service /etc/systemd/system/deploy-server.service
  systemctl daemon-reload
  systemctl enable --now deploy-server
  sleep 2
  systemctl is-active deploy-server
  curl -fsS http://127.0.0.1:8080/api/v1/healthz
'
```

现成的 [20-install-deploy-server.sh](tools/remote-scripts/20-install-deploy-server.sh) 已包含安装、建目录、装 unit、重启、健康检查和最近日志。
Windows 需脚本化密码认证时可配合 [sshpoke](tools/sshpoke/)；Windows 自带 `ssh.exe` 不接受脚本喂密码。普通 `scp`/`ssh` 可交互输入密码。

### 不用 systemd

安装好二进制、建立上述目录后，可选前台运行或 `nohup` 后台运行；将 URL 中的占位符换成本机 IP：

```bash
# 前台调试，看到「HTTP 服务已就绪」后可访问；Ctrl+C 会优雅退出
/usr/local/bin/deploy-server \
    -listen 127.0.0.1:8080 -data-dir /srv/deploy/data \
    -web-root /srv/deploy/workspaces -server-url 'http://<本机IP>:8080'

# 或后台运行
nohup /usr/local/bin/deploy-server \
    -listen 127.0.0.1:8080 -data-dir /srv/deploy/data \
    -web-root /srv/deploy/workspaces -server-url 'http://<本机IP>:8080' \
    > /var/log/deploy-server.log 2>&1 &
curl -fsS http://127.0.0.1:8080/api/v1/healthz
```

仍建议 systemd：开机自启、崩溃自动重启，并在停止时先发 SIGTERM，让运行中的部署收尾。

### Docker 部署

**构建上下文必须是仓库根目录**，Dockerfile 要同时看到 `deploy-server` 和 `deploy-server-ui`：

```bash
docker build -t deploy-server:1.0.0 -f deploy-server/Dockerfile .
docker run -d --name deploy-server -p 8080:8080 \
  -v /srv/deploy/data:/data -v /srv/deploy/workspaces:/workspaces \
  --restart unless-stopped deploy-server:1.0.0
```

镜像预装 `git bash curl rsync openssh-client`。容器里执行的是**容器内的命令**，部署到宿主机需挂载目录或 `docker.sock`；容器内无法 `systemctl reload nginx`，因为没有宿主机 systemd。
部署本机优先 systemd；Docker 更适合构建并把产物输出到挂载卷。公网部署同样遵守上面的监听与反代要求。

### 反向代理与 HTTPS

反代**必须转发 WebSocket**并关闭缓冲，否则出现「实时日志不刷新」。下面使用示例域名 `deploy.example.com`，域名和证书路径需成套替换：

```nginx
server {
    listen 443 ssl;
    server_name deploy.example.com;
    ssl_certificate     /etc/letsencrypt/live/deploy.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/deploy.example.com/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
        proxy_buffering off;
    }
}
```

签证书使用 `certbot --nginx -d deploy.example.com`。**HTTP-01 验证要求域名解析到本机**，DNS 指向别处会失败；证书尚不存在时，硬写上述路径会让 `nginx -t` 失败。
若公网入口在另一台机器，应在入口机器部署相应服务，或仅用 HTTP + 内网访问。

### 升级、备份与卸载

升级覆盖二进制并重启，**数据不受影响**；以下在开发机仓库根目录运行：

```bash
scp deploy-server/bin/deploy-server-linux-amd64 'root@<服务器地址>:/tmp/deploy-server-linux-amd64'
ssh 'root@<服务器地址>' 'install -m 0755 /tmp/deploy-server-linux-amd64 /usr/local/bin/deploy-server && systemctl restart deploy-server'
ssh 'root@<服务器地址>' 'systemctl is-active deploy-server; journalctl -u deploy-server -n 20 --no-pager'
```

重启先停止接收新请求，给在跑的 Run 最多 **60 秒**收尾；下次启动将遗留的 `running/queued` 标成「服务重启导致本次执行中断」。
**`data` 目录就是全部平台状态**，在服务器停服备份：

```bash
systemctl stop deploy-server
tar czf deploy-backup-$(date +%F).tar.gz -C /srv/deploy data
systemctl start deploy-server
```

**`/srv/deploy/data/secret.key` 丢失 = 已加密的 Secret 环境变量与 Git 凭据无法恢复**，AES-GCM 无后门，必须随数据库一起备份；`jwt.key` 丢失只需重新登录。
卸载前确认备份和数据保留需求，最后的目录删除会清除数据与工作区：

```bash
systemctl disable --now deploy-server
rm -f /etc/systemd/system/deploy-server.service /usr/local/bin/deploy-server
systemctl daemon-reload
# 仅在确认不再需要数据时执行
rm -rf /srv/deploy
```

## 使用指南

### 首次初始化

**没有默认密码，也不预置账号**：`users` 表初始为空。前端通过 `GET /api/v1/auth/bootstrap-status` 得到 `initialized:false` 后，引导第一个访问者进入 `/user/setup`。
自设管理员（用户名 ≥ 3 位、密码 ≥ 8 位），向导同时展示环境自检；提交后立即登录，初始化接口此后返回 `40900`，拒绝再次创建。
管理员在「用户管理」新建的账号默认要求首次登录改密；公开注册默认关闭。部署后请尽快初始化，别让未初始化实例暴露公网。

**本次交付的 `192.168.31.97` 是例外**：验收时已设管理员 **`admin` / `admin12345`，请立刻修改**。
可用「个人设置 → 修改密码」，也可在取得登录 token 后调用：

```bash
curl -X POST http://192.168.31.97:8080/api/v1/auth/change-password \
  -H 'Authorization: <你的token>' -H 'Content-Type: application/json' \
  -d '{"oldPassword":"admin12345","newPassword":"<新密码，至少8位>"}'
```

验收项目 `demo-log-stream` 可在项目列表删除。

### 第一步：创建项目

「项目 → 新建项目」，必填 Key 和名称：

| 配置 | 含义 / 默认 |
|---|---|
| Key | 唯一标识，只允许字母、数字、点、下划线、短横线，例如 `my-app`；CLI 与接口用它引用项目 |
| 名称 | 显示名，可用中文 |
| 工作目录 | 默认 `${WORKSPACE_ROOT}/${PROJECT_KEY}`，如 `/srv/deploy/workspaces/my-app` |
| Shell | `auto`，解释器选择见[平台差异](#平台差异) |
| 并发策略 | 默认 `queue` 串行排队；`allow` 并行；`cancelPrev` 新任务取消旧任务 |
| 默认超时 | 0（不限），单位秒，限制整个 Run 总时长 |
| Hook 触发 | 默认启用；关闭后项目 Hook 令牌不能触发 |
| Git 准备阶段 | 默认关闭；`useGit=true` 时先准备 Git，再执行命令 |

Git 支持 HTTPS/SSH 仓库，默认分支 `main`；私有仓库先建凭据再引用。「测试连接」会实际执行 `git ls-remote`，用于验证地址与凭据。

### 第二步：写命令

命令从上到下执行，每条配置如下：

| 字段 | 说明 |
|---|---|
| 名称 / 脚本 | 名称写入日志；脚本就是多行 shell |
| 启用 | 关闭则跳过 |
| 失败策略 | 默认「中止」：失败即停；「继续」：忽略错误往下执行 |
| 覆盖项 | 可单独覆盖工作目录、超时、shell |

执行前会注入这些内置环境变量：

| 变量 | 含义 |
|---|---|
| `WORKDIR` | 实际工作目录，已展开占位符并拼上 Git 子目录 |
| `PROJECT_KEY` / `PROJECT_NAME` | 项目 Key / 名称 |
| `RUN_ID` / `BUILD_NUMBER` | 运行 ID / 项目内自增构建号 |
| `TRIGGER_BY` / `TRIGGER_TYPE` | 触发人 / 触发方式（`web`、`cli`、`hook`） |
| `START_TIME` / `DEPLOY_SERVER_URL` | 开始时间 / 本服务地址 |
| `GIT_BRANCH` / `GIT_COMMIT` / `GIT_COMMIT_SHORT` / `GIT_TAG` / `GIT_AUTHOR` / `GIT_MESSAGE` / `GIT_DIRTY` | 仅 `useGit=true` 时注入 |

环境变量优先级（后者覆盖前者）：**系统环境 → 项目普通变量 → 项目 Secret 变量 → 内置变量 → 触发时传入的参数**。

静态站示例：以下三段分别保存为三条项目命令；先配置好 nginx 站点，将 `mysite.example.com` 换成自己的域名，站点目录默认 `/var/www/mysite`，可用项目变量 `SITE_ROOT` 覆盖。

```bash
# 1. 同步站点文件
set -e
SITE_ROOT="${SITE_ROOT:-/var/www/mysite}"
mkdir -p "$SITE_ROOT"
rsync -a --delete --exclude '.git/' "${WORKDIR}/" "$SITE_ROOT"/
chown -R www-data:www-data "$SITE_ROOT"
echo "已同步 $(find "$SITE_ROOT" -type f | wc -l) 个文件"
```

```bash
# 2. 校验配置后重载；配置坏了就不 reload
set -e
nginx -t
systemctl reload nginx
```

```bash
# 3. 健康检查，非 200 明确失败
set -e
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: mysite.example.com' http://127.0.0.1/)
echo "HTTP $code"
[ "$code" = "200" ] || exit 1
```

写脚本的四点：

- 每条加 `set -e`，避免中间失败却继续并最终报成功。
- 用 `${WORKDIR}`，不要硬编码工作目录。
- **最后一条命令的退出码决定整次部署成败**，因此把健康检查放在末尾并使用「中止」策略。准确说，每条脚本的最终退出码交给平台判断；「中止」会在前面失败时立即停止，「继续」会忽略该条失败，即使它是最后一条。
- 「项目详情 → 预览」可先查看实际会执行的内容，不真的执行。

### 第三步：执行

| 入口 | 操作 |
|---|---|
| 网页 | 项目列表或详情点「执行」，也可对某条命令「单独执行」 |
| CLI | `deploy run my-app -f`，实时跟随日志 |
| HTTP Hook | 下节的 `POST /api/v1/hooks/{token}`，适合 CI 或外部系统 |

### 运行与日志

状态共七种：`queued` / `running` / `success` / `failed` / `canceled` / `timeout` / `error`；「运行」页可按项目和状态筛选历史。
详情页展示运行 ID、构建号、触发方式/人、来源 IP、Git commit、准备阶段与总耗时、退出码、日志大小；步骤表展示命令阶段、耗时、退出码和日志字节区间。
xterm 日志终端保留 ANSI 颜色，支持暂停自动滚动、滚到底部、清屏、搜索与下载；运行可取消或重新执行。

实时日志不丢不重的机制只在这里说明：

- WebSocket `/api/v1/ws/runs/{id}` 每帧带 `nextOffset`，表示日志文件的**字节偏移**。
- 断线后带 `?offset=<nextOffset>` 重连，服务端先补发历史，再推增量。
- 客户端用 `nextOffset - len(data)` 推算帧起点去重；这里 `len(data)` 按字节计。
- 服务端保证帧内 `data` 字节数与文件增长量恒等；历史回看接口为 `GET /api/v1/runs/{id}/log?offset=N`。

### HTTP Hook

项目详情 → Hook Token → 新建；明文仅创建时显示一次，之后只显示前缀，可重置、删除、设过期时间并查看 IP 记录。
免登录凭 token 触发，请求体可省略；默认异步返回 **202 + runId**，`?wait=true` 或 `async:false` 同步返回 **200 + 最终 status / exitCode / durationMs**。

```bash
# 默认异步
curl -X POST 'http://<服务器地址>:8080/api/v1/hooks/<Hook令牌>' \
  -H 'Content-Type: application/json' \
  -d '{"branch":"main","env":{"TAG":"v1.2.3"}}'
# 同步等待
curl -X POST 'http://<服务器地址>:8080/api/v1/hooks/<Hook令牌>?wait=true' \
  -H 'Content-Type: application/json' -d '{"async":false}'
# 请求体也可以省略
curl -X POST 'http://<服务器地址>:8080/api/v1/hooks/<Hook令牌>'
```

契约允许 token 放入 `X-Deploy-Token` 头；当前处理器有读取逻辑，但路由只注册 `/hooks/:token`，因此目前请使用路径 token，不能直接照抄仅请求头的调用。

字段：`branch` 覆盖分支、`env` 覆盖变量（需项目开启「允许参数覆盖」）、`commandIds` 选命令、`async` 选择异步。
默认按令牌限流 **60 次/分钟**，超限业务码 `42900`；记录来源 IP 和 User-Agent，供运行历史追溯。

## 命令行客户端

Linux/macOS 在仓库根目录构建：

```bash
cd deploy-cli && go build -o deploy .
```

Windows 在仓库根目录构建：

```powershell
cd deploy-cli
go build -o deploy.exe .
```

将产物放入 PATH 后使用下面的 `deploy` 命令；Windows 也可在产物目录用 `.\deploy.exe`。
配置文件为 `~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`），结构如下；token 替换为登录或 API 令牌：

```yaml
current: default
contexts:
  default:
    server: http://192.168.31.97:8080
    token: <你的token>
    user: admin
```

优先级：**flag > 环境变量（`DEPLOY_SERVER` / `DEPLOY_TOKEN`）> 配置文件 > 默认值**。CI 可用环境变量避免配置落盘。
常用命令（替换服务器地址、API 令牌、项目 Key 与运行 ID）：

```bash
deploy login -s 'http://<服务器地址>:8080' -u admin
deploy login -s 'http://<服务器地址>:8080' --token '<API令牌>'
deploy whoami
deploy project list
deploy project get my-app
deploy run my-app -f
deploy run my-app -e TAG=v1.2.3 --branch release -f
deploy logs 88 -f
deploy runs list --project my-app --status failed
deploy stop 88
deploy doctor
deploy config set server 'http://<另一服务器地址>:8080'
deploy version
```

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 部署失败（failed / timeout / error） |
| `2` | 用法错误 |
| `3` | 鉴权失败 |
| `4` | 网络或服务不可用 |

`Ctrl+C` 默认**只断开日志跟随，不取消运行**；加 `--cancel-on-interrupt` 才取消运行。
`--json` 输出机器可读结果，提示走 stderr；登录密码交互输入不回显，非 TTY 或 `--no-color` 会剥离 ANSI。

## 环境自检与系统设置

「系统设置 → 环境自检」、`deploy doctor` 或前述 doctor 接口报告：

- OS、架构、主机名、版本、运行时长。
- 工作目录与数据目录是否可写：**真写临时文件验证**，并非只看权限位。
- 当前选中 shell 和可用 shell 列表。
- 14 项工具的存在性、版本、路径：`git` `bash` `sh` `rsync` `curl` `docker` `nginx` `node` `npm` `pnpm` `go` `java` `mvn` `certbot`；缺失项给安装提示。

| 系统设置 | 默认值 | 行为 |
|---|---|---|
| 最大并发数 | 5 | 修改**立刻生效**，不用重启 |
| 日志保留 | 30 天 | 启动时及每 6 小时清理超期运行记录和日志 |
| 单次运行日志上限 | 100 MB | 达上限后停止写入并提示 |
| Hook 限流 | 60 次/分钟 | 按令牌计 |
| 项目可见性 | `allReadable` | 其他登录用户只读可见；`ownerOnly` 则不可见 |
| 通知 | 关闭 | 预留自定义 Webhook / 飞书 / 钉钉 |

监听地址、工作目录等影响进程本身的配置需改配置文件并重启，不在界面热改。
**凭据管理**支持 `https_token`（用户名 + Token/密码）与 `ssh_key`（私钥），AES-GCM 加密，接口永不返回密文；编辑留空表示不改，被项目引用时禁止删除。

## 排查与测试

**先看响应体 `success`：业务失败也可能返回 HTTP 200，不能仅凭状态码判断成功。** 业务码与格式见 [API 契约](docs/API设计.md)。

| 现象 | 排查方法 |
|---|---|
| 页面正常但实时日志不刷新 | 反代未转发 WebSocket 或开启缓冲，按[反向代理与 HTTPS](#反向代理与-https)补齐配置 |
| Git 准备失败 | 用项目「测试连接」验证地址、凭据；私有仓库先在凭据管理创建凭据 |
| 「工作目录已存在且不是 Git 仓库」 | 有意拒绝覆盖以防误删；确认内容后清空或换目录 |
| `command not found` | systemd 的 PATH 很干净；`bash -lc` 加载 `/etc/profile`，工具在其他位置时写全路径 |
| 部署成功但页面还是旧内容 | 检查缓存；HTML 用 `no-cache`，静态资源用长缓存并给文件名加版本号 |
| 取消后仍有子进程 | 正常应终止整棵进程树；Linux 确认残留后附 `ps -ef` 输出反馈 |
| `database is locked` | 已使用 WAL、`busy_timeout`、连接池上限 1；检查其他进程是否直接打开 `deploy.db` |
| 重启后仍显示运行中 | 正常应标成中断；检查 `/srv/deploy/data` 是否可写 |
| Hook 鉴权/权限/限流失败 | 检查令牌错误、删除、过期或项目归档；确认启用 Hook；超限等一分钟或调整限流 |

忘记管理员密码有三条路，按影响从小到大选择：

1. 由另一个 admin 在「用户管理」重置。
2. 停服后清空用户表，再启动并访问 `/user/setup`；项目、运行历史、Hook 令牌仍在，新管理员有全部项目权限：

   ```bash
   systemctl stop deploy-server
   sqlite3 /srv/deploy/data/deploy.db 'DELETE FROM users;'
   systemctl start deploy-server
   ```

3. 备份后删库重建，会丢失全部数据库数据。

服务器日志与运行日志（替换项目 Key、运行 ID）：

```bash
journalctl -u deploy-server -n 100 --no-pager -f
ls -la '/srv/deploy/data/logs/<project-key>/'
cat '/srv/deploy/data/logs/<project-key>/<runID>.log'
```

在仓库根目录运行测试：

```bash
cd deploy-server && go test ./... -count=1
cd ../deploy-cli && go test ./... -count=1
```

服务端集成测试覆盖登录、建项目、执行、日志、Hook、取消、权限隔离、Secret 打码与 WebSocket；CLI 原验收为 81 个用例。
Windows 冒烟脚本（在仓库根目录执行，第二条替换服务器地址）：

```powershell
powershell -File tools\smoke\test-cancel-tree.ps1
powershell -File tools\smoke\deploy-mosskb-via-api.ps1 -Base 'http://<服务器地址>:8080/api/v1'
```

前者按 Windows 进程语义验证孙子进程一并终止；后者用 REST + 显式 UTF-8 请求体完成建项目、部署静态站与健康检查。

## 平台差异

| 项目 | Linux | Windows |
|---|---|---|
| 默认解释器 | `bash -lc` | `powershell -NoProfile -Command`，有 `pwsh` 优先 |
| 环境变量引用 | `${BUILD_NUMBER}` | `$env:BUILD_NUMBER` |
| 进程树终止 | `Setpgid` + `kill(-pgid)` | `taskkill /T /F` |

项目脚本不要假定跨平台通用；生产部署以 Linux 为主。

## 安全提醒

平台本质是远程命令执行，安全边界靠**谁能登录平台**控制。服务有意以 root 运行，以便 rsync 到站点目录、reload nginx。
默认仅监听 `127.0.0.1`；公网入口必须反代 + HTTPS，建议再限制来源 IP（防火墙或反代白名单）。
无预置账号和默认密码，部署后务必尽快初始化；交付服务器密码按[首次初始化](#首次初始化)立即修改。
`secret.key` 必须一起备份，丢失后果见[升级、备份与卸载](#升级备份与卸载)。登录失败 **5 次锁定该用户名 10 分钟**。
