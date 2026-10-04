# deploy-cli

轻量命令式 CI/CD 平台（`deploy-server`）的命令行客户端。

- Go 1.24+，单二进制，无运行时依赖
- 命令行框架：`spf13/cobra`；实时日志：`gorilla/websocket`
- 严格按 `docs/API设计.md`（v1 冻结契约）实现：Base path `/api/v1`、响应封装 `{success,data,errorCode,errorMessage,showType}`、`Authorization: <裸 token>`

---

## 1. 安装

### 从源码构建

```powershell
cd deploy-cli
go build -o deploy.exe .            # Windows
go build -o deploy .                # Linux / macOS
```

带版本信息构建（`deploy version` 会显示）：

```powershell
go build -ldflags "-s -w `
  -X deploy-cli/internal/version.Version=0.1.0 `
  -X deploy-cli/internal/version.Commit=$(git rev-parse --short HEAD) `
  -X deploy-cli/internal/version.BuildTime=$(Get-Date -Format o)" -o deploy.exe .
```

### 交叉编译

```powershell
$env:GOOS='linux';  $env:GOARCH='amd64'; go build -o deploy-linux-amd64 .
$env:GOOS='linux';  $env:GOARCH='arm64'; go build -o deploy-linux-arm64 .
$env:GOOS='darwin'; $env:GOARCH='arm64'; go build -o deploy-darwin-arm64 .
$env:GOOS='windows'; $env:GOARCH='amd64'   # 记得还原
```

### 把二进制放到 PATH

```powershell
Move-Item deploy.exe "$env:USERPROFILE\bin\deploy.exe"   # 确保该目录已在 PATH 中
```

---

## 2. 配置

配置文件：`~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`），
目录 `0700`、文件 `0600`（Windows 上权限位是尽力而为）。可用 `DEPLOY_CONFIG` 指定其它路径。

```yaml
current: default
contexts:
  default:
    server: http://127.0.0.1:8080
    token: eyJhbGciOi...
    user: admin
  prod:
    server: https://deploy.example.com
    token: dk_9f8e7d...
    user: admin
```

### 优先级（高 → 低）

| 顺序 | 来源 |
|---|---|
| 1 | 命令行 flag：`--server` / `--token` |
| 2 | 环境变量：`DEPLOY_SERVER` / `DEPLOY_TOKEN`（CI 里可以完全不用配置文件） |
| 3 | 配置文件当前 context 的 `server` / `token` |
| 4 | 默认值：`http://127.0.0.1:8080`（token 无默认值，为空即视为未登录） |

### 全局 flag

| flag | 说明 |
|---|---|
| `--server URL` | 覆盖服务端地址 |
| `--token TOKEN` | 覆盖 token（JWT 或 `dk_` 开头的 API Token） |
| `--json` | stdout 只输出机器可读 JSON / JSONL，人类提示一律走 stderr |
| `--no-color` | 禁用彩色；非 TTY 或 `NO_COLOR=1` 时自动禁用 |
| `--verbose` | 调试点（HTTP 请求、重连、配置来源）输出到 stderr |

---

## 3. 命令速查

```text
deploy login [-s URL] [-u USER] [-p PASS] [--token TOKEN] [--context NAME]
deploy logout
deploy whoami
deploy project list [--keyword K] [--page N] [--page-size N] [--archived]
deploy project get <key>
deploy project create --key K --name N --workdir DIR --command "脚本" [--command "名称::脚本"] ...
deploy run <key> [-f] [-e K=V ...] [--branch REF] [--wait] [--cancel-on-interrupt] [--command-id ID]
deploy logs <runId> [-f] [--offset N]
deploy runs list [--project KEY] [--status S] [--page N] [--page-size N]
deploy stop <runId>
deploy doctor
deploy config get [key] | set <key> <value> | list | path
deploy version [--short]        # 也支持 deploy --version
```

---

## 4. 命令示例

### 4.1 登录 / 登出 / 身份

```powershell
# 交互式：-p 省略时输入密码不回显（golang.org/x/term.ReadPassword）
deploy login -s http://127.0.0.1:8080 -u admin

# 非交互（CI）
deploy login -s http://127.0.0.1:8080 -u admin -p "$env:DEPLOY_PASSWORD"

# 直接用 API Token 跳过登录（会先调 /auth/me 校验；401 则不落盘）
deploy login -s https://deploy.example.com --token dk_xxxxxxxx

# 登录到指定 context（写进哪个 context）
deploy login -s https://prod.example.com -u admin --context prod

deploy whoami            # 显示用户/角色/服务器/context（token 失效 → 退出码 3）
deploy logout            # 清空当前 context 的 token（同时尽力调 /auth/logout）
```

### 4.2 项目

```powershell
deploy project list
deploy project list --keyword blog --page-size 100

deploy project get my-app          # 基本信息 + 命令列表 + 环境变量

# 非交互创建（'名称::脚本' 可以指定命令名，省略则自动命名）
deploy project create `
  --key my-app --name "我的应用" --workdir /srv/workspaces/my-app `
  --command "git pull" `
  --command "依赖::npm ci" `
  --command "构建::npm run build" `
  --env MODE=prod --env DEBUG=0 `
  --shell auto --concurrency queue --timeout 1800 --fail-strategy abort

# 交互式向导（不带必需参数时自动进入）
deploy project create
```

创建项目还支持 Git 准备阶段：`--use-git --git-url git@host:org/repo.git --git-branch main
--git-subdir sub --git-depth 1 --git-clean --git-submodule`。

### 4.3 触发运行与日志

```powershell
deploy run my-app                          # 触发并立刻返回 runId
deploy run my-app -f                       # 先补历史日志，再实时跟随到结束
deploy run my-app --wait                   # 不打印日志，只等结果
deploy run my-app -f -e TAG=v1.2.3 --branch main
deploy run my-app --command-id 10 --command-id 11
deploy run my-app -f --cancel-on-interrupt # Ctrl+C 时取消运行

deploy logs 88                             # 一次性拉全部历史日志
deploy logs 88 -f                          # 实时跟随（断线自动重连 + 按 offset 续拉）
deploy logs 88 --offset 4096               # 从指定字节偏移开始

deploy runs list --project my-app --status failed --page 2
deploy stop 88                             # 幂等取消
```

`deploy run -f` 的收尾摘要与退出码：

```text
== my-app 构建开始 ==
npm ci
npm run build
构建成功
运行 ID #88（构建号 #13）  success  退出码 0  耗时 8.1s   → 退出码 0
运行 ID #87（构建号 #7）   failed   退出码 1  耗时 2.3s   → 退出码 1
```

> **两个数字的区别**：`运行 ID #88` 是全局运行 ID，`deploy logs` / `deploy stop` 都用它；
> `构建号 #13` 是项目内第几次部署（服务端日志头部 `=== Deploy Run #13 ===` 里的那个数字）。
> 摘要会同时标出两者，避免混淆；拿不到构建号时退化为只显示 `运行 ID #88`。

`--json` 跟随时的输出（stdout 每行一个 JSON，可逐行解析）：

```json
{"data":"构建开始\n","type":"log"}
{"data":"构建成功\n","type":"log"}
{"buildNumber":13,"durationMs":8123,"exitCode":0,"runId":88,"status":"success"}
```

### 4.4 自检与配置

```powershell
deploy doctor            # 调 GET /system/doctor，彩色 ✔/✘ + 安装提示
deploy doctor --json     # 原样输出契约里的 doctor data

deploy config get                       # 全部配置（token 打码）
deploy config get server                # 单项（token 会输出明文，供脚本使用）
deploy config set server http://127.0.0.1:8080
deploy config set token dk_xxx
deploy config set current prod          # 切换 context（不存在则以当前 server 为模板新建）
deploy config list                      # 所有 context
deploy config path                      # 配置文件路径

deploy version
deploy --version
```

---

## 5. 退出码

| 退出码 | 含义 | 触发场景 |
|---|---|---|
| `0` | 成功 | 命令正常完成；`run`/`logs -f` 的运行状态为 `success`；跟随被 Ctrl+C 打断但**没有**取消运行 |
| `1` | 部署失败 | 运行终态为 `failed` / `timeout` / `error` / `canceled` |
| `2` | 用法错误 | 参数不合法、命令/子命令不存在、非法 key、`K=V` 格式错误、本地配置文件损坏、`errorCode` 为 `40000`/`40400`/`40900` |
| `3` | 鉴权失败 | 未登录、HTTP 401、`errorCode 40100`、403 / `40300` |
| `4` | 网络或服务不可用 | 连接被拒、DNS 失败、超时、HTTP 5xx / `errorCode 50000`、`42900` 限流、响应不符合契约 |

脚本示例：

```powershell
deploy run my-app -f
switch ($LASTEXITCODE) {
  0 { "部署成功" }
  1 { "部署失败" }
  2 { "命令用法写错了" }
  3 { "token 过期，需要重新登录" }
  4 { "服务端不可达" }
}
```

```bash
deploy run my-app -f || case $? in
  1) echo "部署失败" ;;
  3) deploy login -s "$DEPLOY_SERVER" -u "$USER" -p "$PASS"; deploy run my-app -f ;;
  4) echo "服务端不可达，稍后重试" ;;
esac
```

---

## 6. 行为约定

- **Ctrl+C**：`run -f` / `logs -f` 默认只断开日志跟随，**不取消运行**（退出码 0）；
  只有 `deploy run --cancel-on-interrupt` 才会先调 `POST /runs/{id}/cancel`，等终态后按状态返回。
- **断线重连**：跟随日志时若 HTTP/WebSocket 断开，会退避重连（0.5s→5s 封顶，不设次数上限，直到运行结束或 Ctrl+C），
  每次重连前先用 `GET /runs/{id}/log?offset=<nextOffset>` 补齐历史，服务端重发更早内容时按 offset 裁掉重复前缀 —— **不丢日志、不重复刷屏**。
- **ANSI**：TTY 下原样保留日志颜色；非 TTY / `--no-color` / `NO_COLOR=1` 时剥离（跨数据块被切分的转义序列也能正确剥离）。
- **`--json`**：stdout 只出现 JSON；`run -f --json` 是 JSONL 日志事件 + 一行结果对象；所有提示/警告/错误都走 stderr。
- **交互与非交互等价**：`login`（`-p`/`--token`）、`project create`（全部 `--flag`）都能在无 TTY 环境完成。
- **`logs`**：不带 `-f` 只回放历史日志并返回 0；带 `-f` 会给出终态摘要，运行失败时返回 1。

---

## 7. 代码结构

```text
deploy-cli/
  main.go                     # 入口：错误 → 退出码映射，错误与提示写 stderr
  cmd/                        # 各 cobra 子命令（root/login/logout/whoami/project*/run/logs/runs/stop/doctor/config/version）
  internal/api/               # 契约类型 + HTTP 客户端 + 错误分类（错误自带 ExitCode()）
  internal/ws/                # WebSocket 帧、Dialer、Follower（历史补齐 + 增量 + 断线按 offset 续拉）
  internal/config/            # ~/.deploy/config.yaml 读写、多 context、优先级解析
  internal/output/            # 彩色输出、ANSI 剥离、等宽表格（CJK 宽度对齐）
  internal/exitcode/          # 退出码常量与 err → 退出码分类
  internal/version/           # -ldflags -X 注入的版本变量
```

## 8. 测试

```powershell
go build -o deploy.exe .
go vet ./...
go test ./...
go test ./... -cover
```

覆盖的关键逻辑：

- 退出码映射（`internal/exitcode`、`internal/api`：401→3、5xx→4、40001→2、传输错误→4、未分类→2）
- ANSI 剥离（含跨块切分的转义序列）、非 TTY 不产生颜色、`--json` 时提示不污染 stdout
- 配置读写（0600 权限、多 context、flag > env > 配置 > 默认值）
- 日志 offset 续拉（分块拉到 `eof`、重连去重、鉴权失败不重试、Ctrl+C 中断语义）
- 命令层：进程内跑完整 cobra 命令树，对照真实 HTTP + WebSocket 假服务端校验退出码与输出
