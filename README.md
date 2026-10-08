# project-deploy

一个**自托管的轻量命令式 CI/CD 平台**：项目 = 一个工作目录 + 一组 sh 命令；部署 = 点一下跑起来、实时看日志、拿退出码。
不写 Jenkinsfile / 流水线 DSL / YAML 编排，直接写 shell。

- [组成与架构](#组成与架构) · [构建与快速开始](#构建与快速开始)
- [部署到服务器](#部署到服务器)
- [使用指南](#使用指南) · [命令行客户端](#命令行客户端)
- [服务器与 `--server`](#服务器与---server多台服务器) · [AI 对接](#ai-对接让-ai-通过-deploy-cli-部署与运维)
- [中间件一键安装](#中间件一键安装docker--postgresql--mysql--redis--elasticsearch)
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
| ↳ 一键安装 | 两套内嵌脚本 + 独立接口：**构建工具链**（`/system/toolchains`）与**中间件**（`/system/middleware`） |
| [`deploy-cli/`](deploy-cli/) | CLI：**服务器管理**、登录、触发部署、跟随日志、中间件安装；Go 1.24 / cobra |
| [`deploy-server-ui/`](deploy-server-ui/) | Web 界面：React 18 / Umi Max 4 / Ant Design 5，裁剪自 Ant Design Pro |
| [`skills/deploy-cli/`](skills/deploy-cli/SKILL.md) | 给 AI 用的技能说明：怎么选服务器、部署、装中间件、读日志与退出码 |
| [`tools/`](tools/) | 运维与冒烟脚本：`sshpoke` 做密码认证 SSH 执行/传文件；`remote-scripts` 初始化和校验服务器；`smoke` 验证进程树终止及 API 部署 |

> **仓库**：<https://gitee.com/ppnt/deploy> —— 前端、后端、CLI、工具与文档都在这一个仓库里：
>
> ```
> deploy/                 ← 仓库根
>   deploy-server/        ← 服务端（Go）
>   deploy-server-ui/     ← Web 界面（React）
>   deploy-cli/           ← 命令行客户端（Go）
>   skills/deploy-cli/    ← 给 AI 用的技能说明
>   tools/                ← 服务器初始化与冒烟脚本
>   docs/                 ← 需求规格说明、API 契约、AI 对接
> ```

命令在**服务端所在机器**执行，模型预留 Runner 字段；Git 准备是项目级开关；`admin` / `user` 按项目归属判权。
交付采用 SQLite + 单二进制，也支持 Docker，无需 Redis 或消息队列；工具链由用户安装，平台只自检并提示。
完整决策 D1–D6、需求与验收见[需求规格说明](docs/需求规格说明.md)。

文档：[API 契约](docs/API设计.md)（接口唯一事实来源，改接口先改这里）、[服务端说明](deploy-server/README.md)、[CLI 说明](deploy-cli/README.md)。

核心能力：

- **Secret 落盘前打码**：环境变量与 Git 凭据明文在写日志前替换为 `***`，下载日志也已脱敏。
- **Git 准备可控**：`fetch + checkout -B <ref> origin/<ref>` 强制对齐；凭据经 `GIT_ASKPASS` / `GIT_SSH_COMMAND` 注入，不拼进 URL。
- **执行可控**：同项目默认串行排队，避免同时 checkout；取消时终止整棵进程树。
- **日志可续传**：WebSocket 实时查看、断线补发与去重，机制见[运行与日志](#运行与日志)。

前端 `dist/` 经构建入口拷入 `deploy-server/internal/web/dist/`，再由 `go:embed` 编进二进制，**同一进程、同一端口**提供界面与 API。
实测内嵌 **33 个文件 / 3.19 MB**，二进制约 **17 MB（17.1 MB）**；运行服务器不需要前端目录或 Node。
验证时调用下面的 `doctor`，应看到 `web.built=true`；若为 `false`，查看 `note`，按下一节先构建前端再编译后端，避免嵌入占位页。

```bash
curl -s http://127.0.0.1:10055/api/v1/system/doctor -H 'Authorization: <你的token>'
```

全文 `<尖括号>` 表示需替换的地址、令牌或路径；替换后再执行。示例项目 `my-app`、运行 ID `88` 和示例域名也需换成自己的值。

## 构建与快速开始

推荐在开发机构建，只把二进制传到服务器；服务器无需安装 Go/Node，部署脚本用到的工具另行安装。
**项目构建脚本用到的工具装在服务端机器上**（命令在那里执行）——包括作者自己常用的 `build`，安装方法见[建议安装 build 命令](#建议安装-build-命令多模块构建)。
构建机需要 **Go 1.24+、Node 22、pnpm**，Linux/macOS 还需 make、git；顺序必须是**构建前端 → 拷贝前端 → 编译后端**。
交叉编译必须设置 `CGO_ENABLED=0`；SQLite 使用纯 Go 驱动，因此可以关闭 CGO、免依赖交付。
构建入口拷贝时排除 `*.map`（曾占 dist 一半体积，24 个文件 / 20.8 MB）和 `*hot-update*`（`max dev` 开发态产物）。

### Windows 开发机

**`build.ps1` 只能在 Windows 上跑**：虽然产出 Linux 二进制，但路径处理按 Windows 编写，脚本开头会拒绝非 Windows 环境。
本节使用 `pnpm.cmd`，避开 PowerShell 执行策略对 `pnpm.ps1` 的限制。

```powershell
# 1) 构建前端
cd D:\code\project\project-deploy\deploy\deploy-server-ui
pnpm.cmd install
pnpm.cmd build

# 2) 构建后端（脚本会自动探测前端目录位置）
cd ..\deploy-server
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version 0.1.0 -Local

# 3) 启动本机版本
.\bin\deploy-server.exe `
    -listen 127.0.0.1:10055 `
    -data-dir .\data `
    -web-root .\workspaces
```

打开 <http://127.0.0.1:10055>，按[首次初始化](#首次初始化)创建管理员。
脚本内部会切换到服务端目录，因此用正确脚本路径可从任意目录调用；`-NoProfile` 避免个人配置干扰，`-ExecutionPolicy Bypass` 用于本次脚本执行。

| Windows 构建参数 | 默认 | 作用 |
|---|---|---|
| `-Version <版本号>` | `0.1.0` | 写入二进制，healthz 与 doctor 可查看 |
| `-SkipUI` | 关闭 | 跳过前端拷贝；**刚构建的前端不会被嵌入**，仅复用旧产物、只改后端时使用 |
| `-Local` | 关闭 | 除 Linux/amd64 外，额外构建 Windows 版 |

产物在 `deploy-server\bin\`：`deploy-server-linux-amd64` 用于服务器，`deploy-server.exe` 用于 Windows 本机（需 `-Local`）。
Windows 安装 PowerShell 7 时可将 `powershell` 换成 `pwsh`；含中文的 `.ps1` 文件需 **UTF-8 BOM**，否则 Windows PowerShell 5.1 按 ANSI 解码会报语法错误。

### Linux/macOS 开发机

一律使用 `make ui && make linux`，在仓库根目录（本 README 所在目录）这样执行：

```bash
cd deploy-server-ui && pnpm install && pnpm build   # 构建前端
cd ../deploy-server && make ui && make linux        # 拷前端 + 交叉编译
```

`make ui` 会自动探测前端源码目录（`../deploy-server-ui` 等候选路径）；也可以用 `make ui UI_DIR=<路径>` 指定。
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
# 克隆仓库（换成你自己的地址，当前为 https://gitee.com/ppnt/deploy）
cd /srv && git clone https://gitee.com/ppnt/deploy.git deploy && cd deploy
cd deploy-server-ui && pnpm install && pnpm build
cd ../deploy-server && make ui && make linux
cp -r bin/deploy-server-linux-amd64 /usr/local/bin/deploy-server
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

**默认监听 `127.0.0.1:10055`**。仓库 unit 用 `0.0.0.0:10055` 是有意让局域网浏览器与 CLI 访问；有公网入口时，**必须改回 `127.0.0.1:10055`，经 nginx 反代 + HTTPS**。
先调整待上传的 [unit](deploy-server/deploy/deploy-server.service)；监听地址与目录按实际环境改（仓库 unit 用的是 `0.0.0.0:10055` 与 `/srv/deploy`）。
服务端对外地址**不写进 unit**：装好后在「系统设置 → 服务端外部地址」里填，保存即生效。它会被注入为 `DEPLOY_SERVER_URL`，所以地址变了在页面上改一下就行，不用改 unit、也不用重启。

```sh
cp deploy/deploy-server.service /etc/systemd/system
```

```ini
ExecStart=/usr/local/bin/deploy-server -listen 0.0.0.0:10055 -data-dir /srv/deploy/data -web-root /srv/deploy/workspaces
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
  curl -fsS http://127.0.0.1:10055/api/v1/healthz
'
```

现成的 [20-install-deploy-server.sh](tools/remote-scripts/20-install-deploy-server.sh) 已包含安装、建目录、装 unit、重启、健康检查和最近日志。
Windows 需脚本化密码认证时可配合 [sshpoke](tools/sshpoke/)；Windows 自带 `ssh.exe` 不接受脚本喂密码。普通 `scp`/`ssh` 可交互输入密码。

### 不用 systemd

安装好二进制、建立上述目录后，可选前台运行或 `nohup` 后台运行：

```bash
# 前台调试，看到「HTTP 服务已就绪」后可访问；Ctrl+C 会优雅退出
/usr/local/bin/deploy-server \
    -listen 127.0.0.1:10055 -data-dir /srv/deploy/data \
    -web-root /srv/deploy/workspaces

# 或后台运行
nohup /usr/local/bin/deploy-server \
    -listen 127.0.0.1:10055 -data-dir /srv/deploy/data \
    -web-root /srv/deploy/workspaces \
    > /var/log/deploy-server.log 2>&1 &
curl -fsS http://127.0.0.1:10055/api/v1/healthz
```

仍建议 systemd：开机自启、崩溃自动重启，并在停止时先发 SIGTERM，让运行中的部署收尾。

### Docker 部署

**构建上下文必须是仓库根目录**（Dockerfile 要同时看到 `deploy-server/` 与 `deploy-server-ui/`）：

```bash
cd deploy            # 仓库根目录
docker build -t deploy-server:1.0.0 -f deploy-server/Dockerfile .
docker run -d --name deploy-server -p 10055:10055 \
  -v /srv/deploy/data:/data -v /srv/deploy/workspaces:/workspaces \
  --restart unless-stopped deploy-server:1.0.0
```

也可以直接用 make（它会自己切到仓库根目录）：`cd deploy-server && make docker`。

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
        proxy_pass http://127.0.0.1:10055;
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
curl -X POST http://192.168.31.97:10055/api/v1/auth/change-password \
  -H 'Authorization: <你的token>' -H 'Content-Type: application/json' \
  -d '{"oldPassword":"admin12345","newPassword":"<新密码，至少8位>"}'
```

验收项目 `demo-log-stream` 可在项目列表删除。

### 第一步：创建项目

「项目 → 新建项目」，必填 Key 和名称：

| 配置 | 含义 / 默认 |
|---|---|
| Key | 唯一标识，只允许字母、数字、点、下划线、短横线，例如 `my-app`；CLI 与接口用它引用项目。**`new` 是前端「新建项目」路由（`/projects/new`）的保留字**，别用作 Key，否则详情页地址会被新建页占用 |
| 名称 | 显示名，可用中文 |
| **项目类型** | `应用项目`（默认，常规的构建/部署单元）或 `构建项目`（产出被依赖的产物，例如底层库）。类型只影响默认命令与图标：**任何项目都可以被选为依赖** |
| **依赖的项目** | 多选，列出全部项目。点「执行」时按依赖顺序先构建它们，见[依赖构建](#依赖构建多模块项目) |
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
| `START_TIME` | 开始时间（RFC3339） |
| `DEPLOY_SERVER_URL` | 本服务对外地址，在「系统设置 → 服务端外部地址」里配置 |
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

### 依赖构建（多模块项目）

上层项目的构建常常要先装好底层库（例如 `myprofessor-backend` 依赖 `tio-boot-admin`，
而 `tio-boot-admin` 又依赖 `tio-boot`、`java-openai`）。平台用**项目类型 + 依赖列表**表达：

1. 把底层库建成**构建项目**：类型选「构建项目」，命令写产生产物的那几条（如 `mvn -DskipTests install`）。构建项目自己也可以依赖更底层的构建项目（例如 `tio-boot-admin` 依赖 `tio-boot`、`java-openai`），形成多层依赖链。
2. 在上层项目里把**依赖的项目**多选上（列出全部项目；保存时会校验不是自己、不成环）。
3. 触发时用「**含依赖执行**」（网页上「执行」按钮的下拉，或接口 `withDependencies: true`）。

执行顺序按依赖深度优先展开、依赖在前、同一个项目只构建一次：

```
myprofessor-backend
└── tio-boot-admin
    ├── tio-boot
    └── java-openai
→ 实际顺序：tio-boot → java-openai → tio-boot-admin → myprofessor-backend
```

规则：

- 每次触发都会**重新构建全部依赖**（不做增量缓存），结果确定、不会拿到旧包。
- 依赖用**同一个分支 / Tag**（触发时传的 `branch` 会透传），但**不继承**触发时传入的 `env` 覆盖——那是给上层项目的参数。依赖自己的项目变量、Secret 与内置变量照常注入。
- 每条 Run 有**自己的构建号与日志文件**；一次带依赖的触发用 `pipelineId` 串起来，运行详情页会列出「前置构建」并可跳转。
- **任何一条依赖失败就中止**：后面的依赖与上层项目都不执行，上层 Run 记 `failed`，错误信息与日志直接写明是哪个依赖、哪条 Run 失败。依赖被取消时上层记 `canceled`。
- 取消流水线里的任意一条 Run，会连带取消同一条流水线里还在跑或排队的其它 Run。
- 依赖被归档 / 删掉 / 改成应用项目后，触发时会明确报错并指出是哪个依赖，不会静默跳过。
- 依赖项目之间**同一项目串行**（沿用项目的并发策略），协调器本身不占并发额度，因此不会自锁。

> **完整案例**：[docs/cases/mosskb-web-部署案例.md](docs/cases/mosskb-web-部署案例.md) 记录了一次真实的多模块 Java 部署：
> `mosskb-web`（tio-boot，JDK 21）依赖 5 个必须**源码构建**的库（`tio-boot`、`tio-boot-admin`、`api-table`、
> `java-openai` 含补丁、`mosskb-business`），数据库留在开发机、应用跑在服务器上。里面写清了每条命令为什么这么写，
> 以及工作目录必填、SPA 表单残留、依赖未发布到中央仓库、JDK 版本缺失、跨机连库等 8 个坑的修法。

### 建议安装 build 命令（多模块构建）

平台的每条命令都是一段 shell 脚本，工程一多（不同操作系统、不同 `JAVA_HOME`、`mvn -Pproduction` 之类的参数）把构建命令散落在每个项目里就很难维护。
作者自己的 **go-build** 就是为这件事写的：环境变量与构建命令写进一份配置文件，按当前操作系统挑一组来执行，仓库地址 <https://github.com/litongjava/build>。

**安装（需要 Go 1.21+）**：

```bash
go install github.com/litongjava/build@latest

# 让 go install 出来的可执行文件进入 PATH（重开终端后生效）
export PATH="$PATH:$(go env GOPATH)/bin"     # Linux / macOS
$env:Path += ";$(go env GOPATH)\bin"         # Windows PowerShell（写进 $PROFILE 可持久化）

command -v build                              # 能打印出路径就装好了
```

> **装在哪台机器**：项目命令是在**服务端所在机器**上执行的，所以 `build` 必须装在服务端机器上（自检见[环境自检与系统设置](#环境自检与系统设置)）。
> 服务端不方便装 Go 时，在自己机器上交叉编译再传上去即可：
>
> ```bash
> git clone https://github.com/litongjava/build && cd build
> GOOS=linux GOARCH=amd64 go build -o build .        # 产出 Linux 二进制
> scp build 'root@<服务器地址>:/usr/local/bin/build' && ssh 'root@<服务器地址>' 'chmod +x /usr/local/bin/build'
> # 本仓库自带 tools/sshpoke 也能传（密码走 SSHPOKE_PASS 环境变量）：
> #   sshpoke -host <服务器地址> put .\build /usr/local/bin/build
> ```

**工程里放一份 `.build.txt`**（不带参数运行时会在当前目录依次找 `.build.txt`、`build.txt`）：

```ini
[linux.env]
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64

[linux.build]
mvn clean package -DskipTests -Pproduction

[win.env]
set JAVA_HOME=D:\java\jdk-17

[win.build]
mvn clean package -DskipTests -Pproduction

[mac.env]
export JAVA_HOME=~/java/jdk-17

[mac.build]
mvn clean package -DskipTests -Pproduction
```

**在平台里怎么用**：把项目的某条命令直接写成 `build`（工作目录就是项目工作目录，会自动找到 `.build.txt`），要指定文件就写 `build .build.txt`。
行为要点：

- 按当前操作系统选 `[win.*]` / `[mac.*]` / `[linux.*]` 段：`set ` / `export ` 开头的行作为环境变量注入，该段其余行按顺序执行（Unix 走 `sh -c`，Windows 走 `cmd /C`）。
- **任一条命令失败即以退出码 1 结束**，平台的「失败策略」据此把这次运行标记为失败——所以末尾放健康检查仍然有效。
- 平台注入的内置变量（`WORKDIR`、`PROJECT_KEY`、`BUILD_NUMBER`、`TRIGGER_BY`…）会被子进程继承，可以直接在 `.build.txt` 里用 `${WORKDIR}`。
- 与[依赖构建](#依赖构建多模块项目)天然配合：底层库的**构建项目**命令写 `build`，上层项目选「含依赖执行」，一次触发就是完整的多模块构建链。

> Java 工程还需要**服务端机器**上有 `java` 与 `mvn`。本次交付的 `192.168.31.97` 上这两项（以及 `go`）都还没装，先按[环境自检与系统设置](#环境自检与系统设置)里的提示补齐，否则 `build` 起来了也会在 `mvn` 这一步失败。

### 第三步：执行

| 入口 | 操作 |
|---|---|
| 网页 | 项目列表或详情点「执行」（有依赖时可选「仅执行本项目」）；单条命令用「单独执行」 |
| CLI | `deploy run my-app -f`，实时跟随日志 |
| HTTP Hook | 下节的 `POST /api/v1/hooks/{token}`，Body 带 `"withDependencies": true` 即含依赖构建 |

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
curl -X POST 'http://<服务器地址>:10055/api/v1/hooks/<Hook令牌>' \
  -H 'Content-Type: application/json' \
  -d '{"branch":"main","env":{"TAG":"v1.2.3"}}'
# 同步等待
curl -X POST 'http://<服务器地址>:10055/api/v1/hooks/<Hook令牌>?wait=true' \
  -H 'Content-Type: application/json' -d '{"async":false}'
# 请求体也可以省略
curl -X POST 'http://<服务器地址>:10055/api/v1/hooks/<Hook令牌>'
# 令牌改放请求头（不出现在 URL 里，也就不会落进 nginx 的 access_log）
curl -X POST 'http://<服务器地址>:10055/api/v1/hooks' \
  -H 'X-Deploy-Token: <Hook令牌>' \
  -H 'Content-Type: application/json' -d '{"async":true}'
```

两种方式**路径优先**：路径里带了令牌时请求头会被忽略，所以「路径写错 + 头部正确」仍是 401，避免两处凭据打架。

令牌放路径或请求头 `X-Deploy-Token` 均可（后者对应不带路径参数的 `POST /api/v1/hooks`）。

字段：`branch` 覆盖分支、`env` 覆盖变量（需项目开启「允许参数覆盖」）、`commandIds` 选命令、`async` 选择异步。
默认按令牌限流 **60 次/分钟**，超限业务码 `42900`；记录来源 IP 和 User-Agent，供运行历史追溯。

## 命令行客户端

Linux/macOS 在仓库根目录构建：

```bash
cd deploy-cli && go build -o deploy ./cmd/deploy
```

Windows 在仓库根目录构建：

```powershell
cd deploy-cli
go build -o deploy.exe ./cmd/deploy
# 或者：go install ./cmd/deploy   （装进 $GOPATH/bin，命令名就是 deploy）
```

将产物放入 PATH 后使用下面的 `deploy` 命令；Windows 也可在产物目录用 `.\deploy.exe`。
配置文件为 `~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`），**里面装的是「服务器」清单**：
`contexts` 下的键就是服务器名字，部署时用 `--server <名字>` 指定要操作哪一台，`server update <旧名> --name <新名>` 改名：

```yaml
current: test
contexts:
  test:
    address: http://192.168.31.97:10055
    token: <你的token>
    user: admin
    comment: 测试机
  prod:
    address: http://10.0.0.5:10055
    token: <另一个token>
    user: admin
    comment: 生产
```

优先级：**flag（`-a/--address` / `--token` / `--server`）> 环境变量（`DEPLOY_ADDRESS` / `DEPLOY_TOKEN`；`DEPLOY_SERVER` 为地址的历史名，仍兼容但优先级更低）> 配置文件 > 默认值**。
CI 可用环境变量避免配置落盘。用服务器名字管理多台服务器见[服务器与 `--server`](#服务器与---server多台服务器)。
常用命令（替换服务器名字、项目 Key 与运行 ID）：

```bash
deploy server add 97 -a 'http://192.168.31.97:10055' -t '<API令牌>' -u admin
deploy server list                        # 列出全部服务器（NAME=名字，即配置里的键；token 打码）
deploy whoami                             # 当前登录用户与服务器
deploy project list
deploy project get my-app
deploy run my-app -f                      # 跟随日志
deploy run my-app --wait --json           # 只等结果，机器可读
deploy run my-app -e TAG=v1.2.3 --branch release -f
deploy logs 88 -f
deploy runs list --project my-app --status failed
deploy stop 88
deploy doctor
deploy middleware list                    # 中间件状态
deploy middleware install redis --dry-run --no-follow
deploy --server prod run my-app --wait    # 指定另一台服务器
deploy version
```

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 部署失败（failed / timeout / error / canceled） |
| `2` | 用法错误（参数不对、服务器名字不存在） |
| `3` | 鉴权失败 |
| `4` | 网络或服务不可用 |

**业务失败也返回 HTTP 200**（信封里 `success:false`），CLI 已把它映射成退出码，判断成败请看退出码与 `--json` 字段。
`Ctrl+C` 默认**只断开日志跟随，不取消运行**；加 `--cancel-on-interrupt` 才取消运行。
`--json` 输出机器可读结果，提示走 stderr；登录密码交互输入不回显，非 TTY 或 `--no-color` 会剥离 ANSI。

## 服务器与 `--server`（多台服务器）

需求把「服务端部署与初始化」交给使用者手工完成，CLI 只接受**名字、地址、token**。
它们都存在本地配置文件里，用 `--server <名字>` 选择。**名字就是配置里的键**：
`server update <旧名> --name <新名>` 改的就是它，`current` 会跟着改名走：

```bash
# 1) 使用者手工部署并初始化服务端（浏览器创建管理员）
# 2) 在服务端「个人设置 → API Token」创建一个 dk_ 前缀的长期令牌
# 3) 交给 CLI（非交互，一条命令）
deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxxxxxxx -u admin
```

| 命令 | 作用 |
|---|---|
| `deploy server list` | 列出全部服务器（当前服务器带 `*`，`NAME` 是名字，即配置里的键；token 打码） |
| `deploy server get <名字>` | 看单个服务器的名字/地址/用户/是否当前 |
| `deploy server add <名字> -a <地址> -t <token> [-u <用户>]` | 新建或覆盖，并设为默认（`--no-set-current` 可不让它成为默认） |
| `deploy server use <名字>` | 切换默认服务器 |
| `deploy server update <名字> [-a] [-t] [-u] [--name 新名] [--comment]` | 只改传了的字段；`--name` 是改名（键与 `current` 一起改） |
| `deploy server remove <名字> [--yes]` | 删除一个服务器 |
| `deploy server current` | 打印当前服务器名字 |
| `deploy --server <名字> <任何命令>` | 临时指定服务器，优先于配置里的 `current` |

要点：

- **token 建议用 API Token（`dk_` 前缀）而不是登录得到的 JWT**：JWT 会过期，无人值守的 AI/CI 用它会突然 401。
- **名字**允许中文（如 `测试机`）与点/短横线，但不能含空格、冒号或斜杠（它参与命令行定位）。
- `update --name <新名>` 就是**改名（改键）**：地址/token/用户名整条搬过去，`current` 跟着走；
  新名已存在、为空或非法时报用法错误（退出码 2），不会静默覆盖。
- 名字不存在时命令会返回**退出码 2** 并提示 `deploy server list`，不会静默连到默认地址。
  `login --name <名字>` 不再要求先 `server add`：该服务器不存在时，会在**登录成功后自动创建**并写入。
- 配置文件权限收紧到 0600；`deploy server list` 等命令输出里的 token 一律打码。
- `deploy login` 仍然可用（用用户名密码换 JWT）；写入目标是 `--name` 指定的服务器（缺省 `default`），不存在则在登录成功后自动创建。全局 `--server` **不决定写入目标**，只决定这次请求用哪台已存服务器的地址/token（只传 `--server X` 而不传 `--name` 时会打一条 stderr 警告）。
- 地址覆盖只认 `-a/--address`：旧的 `--server` 地址别名已删除，`-s` 简写也早已移除；`service` 命令别名同样已删除，服务器清单一律用 `deploy server ...` 管理。

## AI 对接（让 AI 通过 deploy-cli 部署与运维）

AI 不需要专有协议：**deploy-cli + `--json` 就是接口**。会执行 shell 的 Agent（Claude Code、Codex、Cursor、自研脚本）都能直接驱动。

```bash
deploy --help                 # 全部子命令
deploy <子命令> --help         # 某个子命令的参数
deploy server list --json     # 我配了哪几台服务器（{current, path, servers:[{key,name,address,user,comment,token,active}]}）
deploy whoami                 # 当前连的服务器与用户（退出码 3=鉴权失败、4=不可达）
deploy project list --json    # 服务器上有什么项目
deploy middleware list --json # 中间件状态
```

这些命令给出的都是**运行时事实**而不是写死的文档：项目列表、中间件状态、服务端版本都在里面，
所以不会过期。这里**没有给 AI 专用的命令** —— AI 用的就是普通人用的 `deploy`。

给 AI 的三条硬约定：

1. **先看命令树与当前连接**（`deploy --help`、`deploy server list --json`、`deploy whoami`），
   不要猜项目 key 与服务器名字。
2. **长任务用 `--wait --json` 或 `--no-follow --json` 拿 `runId`**，再 `deploy logs <runId> -f`；
   只看触发结果里的 `queued` 不能判定成败。
3. **改服务器状态前先 `--dry-run`**（中间件 `install` 支持），并把计划说给使用者确认。

完整说明见 [docs/ai/AI对接.md](docs/ai/AI对接.md)（含「为什么不做 MCP」的分析）；
给 Agent 的技能说明在 [skills/deploy-cli/SKILL.md](skills/deploy-cli/SKILL.md)，
可以整个目录拷到 Agent 的 skills 目录直接使用。

> **token 等于服务器权限**。平台本质是远程命令执行，请给 AI 配**测试服务器**的 token，
> 生产服务器单独一个键，不要交给无人值守的 Agent。

## 环境自检与系统设置

「系统设置 → 环境自检」、`deploy doctor` 或前述 doctor 接口报告：

- OS、架构、主机名、版本、运行时长。
- 工作目录与数据目录是否可写：**真写临时文件验证**，并非只看权限位。
- 当前选中 shell 和可用 shell 列表。
- 14 项工具的存在性、版本、路径：`git` `bash` `sh` `rsync` `curl` `docker` `nginx` `node` `npm` `pnpm` `go` `java` `mvn` `certbot`；缺失项给安装提示。
  `java`/`maven`/`node`/`pnpm`/`go`/`certbot` 在下面[一键安装工具链](#一键安装工具链侧边栏环境自检页)里有对应组件，点一下就装（工具链面板另有 `cc`/`nvm`/`rust` 三项，不在这份自检清单里）；其余（`docker`、`nginx` 等）仍按提示在服务器上装。

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

### 一键安装工具链（侧边栏「环境自检」页）

侧边栏有一级菜单**「环境自检」**：上半部分是自检清单，下半部分就是**「工具链一键安装」**——把常用构建工具链（外加 `certbot` 这个证书客户端）装到服务端机器上，不用登服务器敲命令。

| 组件 | 装什么（默认） | 可改的参数 | 大致体积 |
|---|---|---|---|
| `cc` | gcc / g++ / make / pkg-config / libssl-dev | `CC_EXTRA_PACKAGES` 额外包（如 `cmake nasm`） | 约 200 MB |
| `java` | 发行版 OpenJDK 17，写 `JAVA_HOME` 到 `/etc/profile.d/java.sh` | `JAVA_VERSION`（8/11/17/21）、`JDK_URL`（填了就改走压缩包安装）、`JAVA_DIR`（默认 `/usr/local/jdk`） | 约 180 MB |
| `maven` | 发行版 mvn + 阿里云镜像 `settings.xml`（已存在则不覆盖） | `MAVEN_URL`、`MAVEN_DIR` | 约 30 MB |
| `nvm` | nvm 本体 → `/usr/local/nvm`，写 `/etc/profile.d/nvm.sh` | `NVM_VERSION`、`NVM_GIT_URL`、`NVM_DIR` | 约 5 MB |
| `node` | 经 nvm 装 LTS 并设为默认版本 | `NODE_VERSION`（`--lts`/`20`/`v20.11.1`）、`NVM_NODEJS_ORG_MIRROR` | 约 60 MB |
| `pnpm` | 经 corepack 装到 `/usr/local/bin`，registry 指向 npmmirror | `PNPM_VERSION` | 约 10 MB |
| `go` | 装到 `/usr/local/go`，软链到 `/usr/local/bin`，`GOPROXY=goproxy.cn` | `GO_VERSION`、`GO_URL`、`GO_DIR` | 约 120 MB |
| `rust` | 系统级 rustup（`/usr/local/cargo`），crates 走 rsproxy | `RUST_TOOLCHAIN`、`RUSTUP_HOME`、`CARGO_HOME`、`RUSTUP_DIST_SERVER` | 约 250 MB |
| `certbot` | 发行版 certbot + nginx 插件（`python3-certbot-nginx`） | `CERTBOT_NGINX_PLUGIN`（填 `0` 只装本体）、`CERTBOT_EXTRA_PACKAGES` | 约 30 MB |

> `certbot` 是唯一一个非构建工具：自检里的 14 项工具中，只有它以前「报了缺失却没法一键装」。它只装 ACME 客户端，**不签发证书**——签发要求域名解析到本机、80/443 可达（HTTP-01），内网机或 DNS 指向别处（例如本仓库 `tools/remote-scripts/10-provision-nginx.sh` 里那台）装了也用不上，装完按提示手动签：`certbot --nginx -d <域名>`。

**怎么用**

1. 勾选要装的组件 →「安装所选」，或直接点「一键安装缺失（n）」。
2. 确认框会列出「要装什么 + 当前参数」，确定后**跳到运行详情页看实时日志**（安装就是一次普通运行：可取消、可重跑、可下载日志）。
3. 想换版本（例如装 **JDK 8**）：点该行「编辑」→ 填 `JDK_URL=https://.../jdk-8uXXX-linux-x64.tar.gz`、`JAVA_DIR=/usr/local/jdk8`（`JAVA_VERSION` 填 8）→ 保存 → 点该行「重装」。

**安装记录**：面板下方列出最近的安装运行（时间 / 装的组件 / 用了哪些参数 / 状态 / 耗时 / 日志入口）；
「装到哪」在**运行日志末尾的汇总表**（`组件 | 结果 | 版本 | 位置`）和列表的「版本 / 位置」列都能看到。

要点与边界：

- **仅管理员**可安装或改参数；普通账号能看状态、版本、位置与历史记录。
- **仅 Linux 服务端**：脚本基于 bash + apt/dnf；其他平台可以点「预演」查看计划，或「下载脚本」拿到 Linux 机器上手动跑。
- **预演（dry-run）**：只打印将要执行的动作，不做任何修改——第一次用、或改完参数心里没底时先点它。
- **幂等**：已装好的组件默认跳过；要换版本就用该行的「重装」（对应 `force`）。
- **保守修改**：只写 `/usr/local`、`/etc/profile.d` 和工具自己的配置；**不动 apt 源**，**不覆盖**已有的 `~/.m2/settings.xml` 与 `~/.cargo/config.toml`。
- **默认国内镜像**（脚本层 `--mirror off` 可关）：Go 走阿里云/golang.google.cn，Node 走 npmmirror，Rust 走 rsproxy，npm/pnpm registry 指向 npmmirror，Maven 走阿里云。
- **nvm 的 node 只在 login shell 可见**：平台默认的 `bash -lc` 没问题；项目 Shell 若选了 `sh`，请改成 `bash` 或写绝对路径。`pnpm`/`go`/`rustc` 有 `/usr/local/bin` 软链，任何 shell 都能用。
- 想手动跑：页面上「下载脚本」，或直接取仓库里的 `deploy-server/internal/toolchain/install.sh`：

```bash
bash install.sh --list                        # 只看状态（含版本与位置）
bash install.sh --only java,maven,go          # 装指定组件
bash install.sh --dry-run                     # 预演
bash install.sh --skip rust                   # 除 rust 全装
NODE_VERSION=20 bash install.sh --only node,pnpm                   # 指定 Node
JDK_URL=https://.../jdk-8u202-linux-x64.tar.gz JAVA_DIR=/usr/local/jdk8 \
  bash install.sh --only java                                      # 指定 JDK 8
CC_EXTRA_PACKAGES='cmake nasm' bash install.sh --only cc           # 额外软件包
bash install.sh --only certbot                                     # 只装 certbot（含 nginx 插件）
CERTBOT_EXTRA_PACKAGES=python3-certbot-dns-cloudflare \
  bash install.sh --only certbot                                   # 额外装 DNS 挑战插件
```

- 装完回「环境自检」点**重新检测**即可看到新的版本与路径。

## 中间件一键安装（docker / postgresql / mysql / redis / elasticsearch）

上一条装的是**构建工具链**（无状态、装完即用）；这里装的是**有状态服务**：
有端口、有密码、有数据卷，装完还要能启停查，重复点「安装」也**不能**把数据冲掉。
两者刻意分成两套组件表、两个内嵌脚本、两个接口（`/system/toolchains` 与 `/system/middleware`），
因为它们对「重装」的容忍度完全不同。

| 中间件 | 默认方式 | 说明 | 大致体积 |
|---|---|---|---|
| `docker` | **native**（只能原生） | 其余四项默认都跑在它的容器里，所以它是前置 | 约 400 MB |
| `postgresql` | docker | 数据卷持久化，密码留空则随机生成 | 约 150 MB（镜像） |
| `mysql` | docker | 数据卷持久化，root 密码留空则随机生成 | 约 600 MB（镜像） |
| `redis` | docker | 开启 AOF 持久化 | 约 40 MB（镜像） |
| `elasticsearch` | docker（只能容器） | 单节点 + 关闭安全（内网自用） | 约 1.4 GB |

安装位置与命名（容器方式）：容器名 `deploy-mw-<key>`，数据卷 `deploy-mw-<key>-data`，
统一加 `deploy-mw-` 前缀避免和服务器上已有的容器撞名。

### 网页

侧边栏「环境自检」页 → 下半部分**中间件面板**：勾选 →「安装所选」/「一键安装缺失（n）」，
确认后跳到运行详情页看实时日志（安装就是一次普通运行，可取消、可重跑、可下载日志）。
另有「启动 / 停止 / 重启 / 移除」按钮（都要二次确认），以及「预演（dry-run）」与「下载脚本」。
每一行都能「编辑」参数（端口、密码、版本、安装方式）。

### CLI

```bash
deploy middleware list                        # 状态表：方式 / 已装 / 在跑 / 版本 / 位置
deploy middleware get redis                   # 单个中间件的状态与可编辑参数
deploy middleware install redis --dry-run --no-follow   # 先预演（只打印计划）
deploy middleware install redis --no-follow   # 真装，返回 runId
deploy logs <runId> -f                        # 跟安装日志
deploy middleware install --missing --no-follow         # 只装缺失的
deploy middleware install redis -m native     # 指定方式（docker / native）
deploy middleware start|stop|restart redis --no-follow  # 启停
deploy middleware logs redis --no-follow      # 最近 100 行日志
deploy middleware set redis -p REDIS_PORT=16380 -p REDIS_PASSWORD=<密码>
deploy middleware remove redis --yes --no-follow        # 只删容器，**数据卷保留**
deploy middleware fields redis                # 列出可编辑参数
deploy middleware script > install.sh         # 导出内嵌脚本，可拿去手动跑
```

`--no-follow` 是给脚本/AI 用的（触发后立刻返回 `runId`）；不加则默认跟随日志。
`remove` 在非交互环境**必须**加 `--yes`。

### 参数与密码

可编辑参数直接就是脚本读的**环境变量名**（`PG_PORT`、`REDIS_PASSWORD`、`ES_VERSION`…），
改完保存，下次安装/重建时生效。

- **密码类参数（`*_PASSWORD`）在服务端加密存储**（AES-GCM，与项目 Secret 同一套密钥），
  接口只回「已设置 / 未设置」，**绝不出明文**；命令行回显里也会被打成 `***`。
- 密码留空 = 安装时随机生成 32 位，并在日志末尾「连接信息」里**明文出现一次**（请立即保存）。
  之后可用 `docker inspect deploy-mw-<key> --format '{{.Config.Env}}'` 回看。
- `deploy middleware set <key> -p XXX_PASSWORD=` （传空串）= **清空已保存的密码**，回到随机生成；
  不传该键 = 保持原密码不变。所以「只改端口」不会误清密码。
- `elasticsearch` 只支持容器方式、`docker` 只支持原生方式，传错会被明确拒绝而不是静默换路。

### 边界与已知坑

- **只在 Linux 服务端可用**（脚本基于 bash + apt/dnf + docker）。其他平台可以点「预演」看计划。
- **幂等**：已装且在跑的默认跳过，**不会重建容器**（重建才有丢数据的风险）。
  要换镜像/版本用 `--force`，它会删容器重建、**数据卷保留**。
- **移除不删数据**：`remove` 只删容器（原生方式则不卸载系统包，只提示手动命令），
  数据卷与数据目录一律保留。要彻底删数据得手动 `docker volume rm deploy-mw-<key>-data` —— 这是破坏性操作。
- **镜像拉不动是常态**：Docker Hub 在国内网络常常不通，或服务器给 docker 守护进程配的代理已失效
  （`systemctl show docker --property=Environment` 能看到 `HTTP(S)_PROXY`，配置在
  `/etc/systemd/system/docker.service.d/*.conf`）。脚本会先 pull，**失败但本地已有该 tag 时自动回退用本地镜像**；
  本地也没有才失败，并打印排查命令。注意 `docker pull` 每次都要先问 registry 要 manifest，
  所以**即使镜像就在本地，pull 也会失败**——这就是回退分支存在的理由。拉取默认会等约 60 秒超时才回退，跟日志时别以为卡死了。
- **端口冲突很常见**：服务器上往往已经跑着 redis/postgres/es。脚本在端口被占用时只**警告并继续**
  （`IGNORE_PORT_CONFLICT=1`），容器可能因此起不来 —— 先用 `deploy middleware get <key>` 看端口，
  必要时 `set` 换端口。
- **默认参数对公网不安全**：Redis 可能无密码、ES 关闭了安全（`xpack.security.enabled=false`）、
  数据库端口直接暴露。只在内网使用；公网暴露前必须加固。
- 安装记录按内置项目 `system-middleware` 归类（工具链是 `system-toolchain`），
  在「运行」页按项目筛选就能回看每一次装/停/删。

### 手动执行（不经过平台）

```bash
bash install-middleware.sh --list                        # 只看状态
bash install-middleware.sh --only docker,redis           # 装指定中间件
bash install-middleware.sh --only postgresql --mode native
bash install-middleware.sh --action restart --only redis
bash install-middleware.sh --remove --only redis         # 数据卷保留
bash install-middleware.sh --dry-run --only all          # 预演
PG_PORT=15432 REDIS_PASSWORD=secret bash install-middleware.sh --only postgresql,redis
```

脚本源文件是 [deploy-server/internal/middleware/install.sh](deploy-server/internal/middleware/install.sh)，
也可以用 `deploy middleware script` 或页面上「下载脚本」导出。

## 排查与测试

**先看响应体 `success`：业务失败也可能返回 HTTP 200，不能仅凭状态码判断成功。** 业务码与格式见 [API 契约](docs/API设计.md)。

| 现象 | 排查方法 |
|---|---|
| 页面正常但实时日志不刷新 | 反代未转发 WebSocket 或开启缓冲，按[反向代理与 HTTPS](#反向代理与-https)补齐配置 |
| 运行结束后日志"停在一半"、尾巴缺失 | 服务端补日志是分块（128KB）循环读的，必须读到 EOF 才发 `exit`；客户端只认**终态** `exit` 帧，`truncated` 帧或非终态 `exit` 都要按 `nextOffset` 重连补齐。排查见 `deploy-server/internal/api/handlers_ws.go` 的 `drainFile` / `finishStream` 与 `LogTerminal` 的重连状态机 |
| 日志里中文/emoji 处缺字、重连后错位 | 日志偏移是**字节**、`string.length` 是 UTF-16 码元数，去重必须用帧里的 `offset`（服务端保证落在字符边界），不能用 `nextOffset - data.length` 猜 |
| 日志每行越来越往右缩进、长行被拦腰截断（"阶梯"错位） | 前端 xterm 必须设 `convertEol: true`（见 `deploy-server-ui/src/components/LogTerminal`）。日志文件是按 `\n` 写的，而 xterm 默认的 `convertEol:false` 遇到 LF 只下移一行、**不回列首**，于是每行都接在上一行末尾继续画 |
| 「重新执行」跳到新运行后日志一片空白 | 已修：`RunDetail` 里 `<LogTerminal key={id}>` 强制按运行 ID 重挂。**同一条路由换参数时 React 不会重挂组件**，终端里记着旧运行的「已落屏字节数」与旧 WebSocket，新日志会被按 offset 去重整段丢掉。若仍遇到空白，用终端工具条上的「重载日志」（清空并从 0 重拉）或「重连」 |
| Git 准备失败 | 用项目「测试连接」验证地址、凭据；私有仓库先在凭据管理创建凭据 |
| 「工作目录已存在且不是 Git 仓库」 | 有意拒绝覆盖以防误删；确认内容后清空或换目录 |
| `command not found` / 自检说某个工具没装 | 脚本用 `bash -lc` 跑，会读 `/etc/profile` 与 `/etc/profile.d/*.sh`；**环境自检与「工具链」面板按同一套环境探测**，所以命令行里能跑的工具，自检也会认。反过来，只在 `~/.bashrc`（只有交互式 shell 读）里 `export PATH` 的工具，自检和构建都找不到：把 PATH 写进 `/etc/profile.d/xxx.sh`，或把可执行文件软链到 `/usr/local/bin`（后者对 systemd 最省事） |
| 部署成功但页面还是旧内容 | 检查缓存；HTML 用 `no-cache`，静态资源用长缓存并给文件名加版本号 |
| 取消后仍有子进程 | 正常应终止整棵进程树；Linux 确认残留后附 `ps -ef` 输出反馈 |
| `database is locked` | 已使用 WAL、`busy_timeout`、连接池上限 1；检查其他进程是否直接打开 `deploy.db` |
| 重启后仍显示运行中 | 正常应标成中断；检查 `/srv/deploy/data` 是否可写 |
| Hook 鉴权/权限/限流失败 | 检查令牌错误、删除、过期或项目归档；确认启用 Hook；超限等一分钟或调整限流 |
| `endpoint not found: /api/v1/system/middleware` | 服务端是旧版本（中间件接口自 0.6.0 起提供）；升级二进制后重启 |
| 中间件安装失败但日志说「本机已有镜像」 | 那只是**警告**：拉取失败后回退到了本地镜像，真正的失败在下一行；继续往下读日志 |
| 中间件安装一直 `running`、日志停在「拉取镜像」 | 正常，`docker pull` 默认要等约 60 秒才超时回退。国内网络可给 docker 配镜像加速或代理 |
| 中间件装完容器立刻退出 | 多数是端口被占用或数据目录权限。日志末尾有 `docker logs <容器名>` 的尾部输出；也可 `deploy middleware logs <key>` |
| `nothingToDo: true` | 要装的都已装好；想重建（换镜像/版本）加 `--force`，数据卷会保留 |
| `--server` 报 `server "x" does not exist` | 键打错了（不是 `name` 标签）；`deploy server list` 看已配的服务器，或 `deploy server add` 新建 |
| `deploy` 命令返回 3 但密码没错 | 用的可能是过期的 JWT。改用 API Token（`dk_` 前缀）：`deploy server update <键> -t <新token>` |

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

服务端集成测试覆盖登录、建项目、执行、日志、Hook、取消、权限隔离、Secret 打码与 WebSocket；
中间件接口另有一组用例（清单与顺序、未知组件/动作/方式的拒绝、参数加密不回明文、
逐组件安装方式、脚本 `ALL_COMPONENTS` 与 Go 侧组件表逐项一致）；
CLI 原验收为 81 个用例，另加服务器管理、`--server` 选择与中间件动作的用例。
Windows 冒烟脚本（在仓库根目录执行，第二条替换服务器地址）：

```powershell
powershell -File tools\smoke\test-cancel-tree.ps1
powershell -File tools\smoke\deploy-mosskb-via-api.ps1 -Base 'http://<服务器地址>:10055/api/v1'
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
