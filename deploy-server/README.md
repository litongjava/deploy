# deploy-server

轻量命令式 CI/CD 平台的**服务端**。定位见仓库根目录的
`docs/需求规格说明.md`，接口契约见 `docs/API设计.md`。

一句话：**项目 = 一个工作目录 + 一组 sh 命令；部署 = 点一下把它跑起来，实时看日志，拿到退出码。**

## 特性

- 单二进制交付：前端用 `go:embed` 打进可执行文件，解压即用
- SQLite（纯 Go 驱动，`CGO_ENABLED=0` 可交叉编译），零外部依赖
- 执行引擎：进程树完整终止、超时、取消、同项目串行排队、全局并发上限（可动态调整）
- 实时日志：WebSocket 推送，**每帧带 `nextOffset`**，刷新/断网后按偏移补齐，不丢不重
- Secret 环境变量 AES-GCM 加密，**日志按行打码后才落盘**（下载下来的日志文件也是干净的）
- Git 准备阶段：项目级开关，clone/fetch + 强制对齐到目标 ref，凭据走 `GIT_ASKPASS`，绝不拼进 URL
- HTTP Hook：`POST /api/v1/hooks/{token}` 免登录触发，支持 `?wait=true` 同步等结果
- 环境自检：报告 git/docker/nginx/node 等是否存在及版本（**不代替你安装**）

## 快速开始

```bash
# 本机开发
go build -o bin/deploy-server ./cmd/server
./bin/deploy-server -data-dir ./data -web-root ./workspaces -listen 127.0.0.1:10055
```

首次访问 `http://127.0.0.1:10055` 会引导创建管理员账号。

带前端一起构建 —— **Windows**：

```powershell
cd ..\deploy-server-ui; pnpm.cmd build                 # 先构建前端
cd ..\deploy-server
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1 -Local
```

> 注意**不要**加 `-SkipUI`：加了它脚本就跳过「把 dist 拷进内嵌目录」这一步，
> 于是你刚构建的前端不会被编进去，二进制里还是上一次的旧 UI。
> `-SkipUI` 只在「前端没改动、只想重编后端」时用。

带前端一起构建 —— **Linux/macOS**：

```bash
cd ../deploy-server-ui && pnpm install && pnpm build   # 先构建前端
cd ../deploy-server && make ui && make linux           # make ui 就是"拷贝前端"
```

> `build.ps1` **只用于 Windows**（路径处理按 Windows 写，脚本开头会拒绝在 Linux 上运行）。
> Linux/macOS 请用 Makefile —— 即使装了 PowerShell Core（`pwsh`）也别用那个脚本。

## 配置

优先级：**命令行参数 > 环境变量 > 配置文件 > 默认值**。

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-listen` | `DEPLOY_LISTEN` | `127.0.0.1:10055` | 监听地址 |
| `-data-dir` | `DEPLOY_DATA_DIR` | `./data` | 数据库、日志、密钥 |
| `-web-root` | `DEPLOY_WEB_ROOT` | `./workspaces` | 项目工作目录根（项目里用 `${WORKSPACE_ROOT}` 引用） |
| `-server-url` | `DEPLOY_SERVER_URL` | 由 listen 推导 | 对外地址的**启动默认值**，注入给命令的 `DEPLOY_SERVER_URL`；运行时可在「系统设置」里改 |
| `-config` | — | 自动查找 | 配置文件路径 |

> 默认只监听 `127.0.0.1`。这是有意的：本服务本质是「远程命令执行」，
> 要对外提供就必须经过反代并加上 HTTPS，不要直接 `0.0.0.0` 裸奔。

`data-dir` 下会自动生成 `jwt.key` 与 `secret.key`（0600）。
**`secret.key` 丢了，已加密的 Secret 环境变量与 Git 凭据就解不回来了**，请一并备份。

## 部署到服务器

```bash
# 1) 拷二进制与 systemd 单元
scp bin/deploy-server-linux-amd64 root@HOST:/usr/local/bin/deploy-server
scp deploy/deploy-server.service root@HOST:/etc/systemd/system/

# 2) 建目录并启动
ssh root@HOST 'mkdir -p /srv/deploy/{data,workspaces} && chmod +x /usr/local/bin/deploy-server'
ssh root@HOST 'systemctl daemon-reload && systemctl enable --now deploy-server'

# 3) 看状态
ssh root@HOST 'systemctl status deploy-server --no-pager; journalctl -u deploy-server -n 50 --no-pager'
```

升级：把新二进制覆盖 `/usr/local/bin/deploy-server` 后 `systemctl restart deploy-server`。
数据与日志都在 `/srv/deploy` 下，不会被覆盖。

## 目录结构

```
deploy-server/
  cmd/server/            入口（配置、优雅关闭、日志清理定时任务）
  internal/config/       四层配置合并
  internal/store/        GORM 模型与 SQLite 初始化
  internal/secret/       AES-GCM 加解密
  internal/ids/          随机令牌生成与哈希
  internal/runlog/       日志落盘 + 打码 + 发布订阅
  internal/runner/       执行引擎（进程树、超时、取消、Git 准备）
  internal/api/          HTTP 路由、中间件、各 handler、WebSocket
  internal/web/          前端静态资源 embed
  deploy/                systemd 单元
  scripts/build.ps1      Windows 构建脚本
```

## 测试

```bash
go test ./... -count=1
```

`internal/api/integration_test.go` 会把「登录 → 建项目 → 执行 → 看日志 → Hook 触发 → 取消 → 权限隔离」
整条主链路真的跑一遍，可以当作需求文档里 MVP 验收清单的自动化版本。

## 跨平台注意

服务端在 Linux 与 Windows 上都能跑，但**用户脚本的语法不同**：

| | Linux | Windows |
|---|---|---|
| 解释器 | `bash -lc` | `pwsh -NoProfile -Command` |
| 引用环境变量 | `${BUILD_NUMBER}` | `$env:BUILD_NUMBER` |
| 进程树终止 | `Setpgid` + `kill(-pgid)` | `taskkill /T /F` |

同一个项目脚本不要指望在两个平台上通用。
