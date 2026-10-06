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
go build -o deploy.exe ./cmd/deploy   # Windows
go build -o deploy ./cmd/deploy       # Linux / macOS
```

也可以直接装进 `GOBIN`（未设置时是 `$GOPATH/bin`）：

```powershell
go install ./cmd/deploy
```

> main 包放在 `cmd/deploy/`，所以 `go install ./cmd/deploy` 产出的命令名就是 `deploy`
> —— `go install` 按**导入路径最后一段**命名（之前 main 在模块根，产出的是 `deploy-cli`），
> 这也正是 `go build` 必须写 `./cmd/deploy` 的原因。

带版本信息构建（`deploy version` 会显示）：

```powershell
go build -ldflags "-s -w `
  -X deploy-cli/internal/version.Version=0.1.0 `
  -X deploy-cli/internal/version.Commit=$(git rev-parse --short HEAD) `
  -X deploy-cli/internal/version.BuildTime=$(Get-Date -Format o)" -o deploy.exe ./cmd/deploy
```

### 交叉编译

```powershell
$env:GOOS='linux';  $env:GOARCH='amd64'; go build -o deploy-linux-amd64 ./cmd/deploy
$env:GOOS='linux';  $env:GOARCH='arm64'; go build -o deploy-linux-arm64 ./cmd/deploy
$env:GOOS='darwin'; $env:GOARCH='arm64'; go build -o deploy-darwin-arm64 ./cmd/deploy
$env:GOOS='windows'; $env:GOARCH='amd64'   # 记得还原
```

### 把二进制放到 PATH

```powershell
Move-Item deploy.exe "$env:USERPROFILE\bin\deploy.exe"   # 确保该目录已在 PATH 中

# 或者直接装到 GOBIN（未设置时是 $GOPATH/bin）
go install ./cmd/deploy
```

---

## 2. 配置：服务器（名字 + 地址 + token）

配置文件：`~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`），
目录 `0700`、文件 `0600`（Windows 上权限位是尽力而为）。可用 `DEPLOY_CONFIG` 指定其它路径。

`contexts` 下的**键就是服务器的名字**：部署/运维时用 `--server <名字>` 指定操作哪一台，
`server update <旧名> --name <新名>` 改的也是它。没有独立的显示名标签：

```yaml
current: "97"
contexts:
  "97":
    address: http://192.168.31.97:10055
    token: dk_9f8e7d...
    user: admin
  prod:
    address: https://deploy.example.com
    token: dk_1a2b3c...
    user: admin
```

改名就是改键：`deploy server update 97 --name prod` 之后，配置里的键变成 `prod`，
`current` 与 `--server prod` 一起生效。新名已存在、为空或含空格/冒号/斜杠时会被拒绝。

推荐的建立方式（非交互，一条命令）：

```bash
deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxxxxxxx -u admin
```

> **建议用 API Token（`dk_` 前缀）而不是用户名密码换来的 JWT**：JWT 会过期，
> 无人值守的 CI / AI 用它会在某个时刻突然 401（退出码 3）。API Token 在
> 服务端「个人设置 → API Token」创建，长期有效、可随时撤销。

### 优先级（高 → 低）

| 顺序 | 来源 |
|---|---|
| 1 | 命令行 flag：`-a/--address` / `--token` / `--server`（`--server` 选配置文件里的某个服务器） |
| 2 | 环境变量：`DEPLOY_ADDRESS` / `DEPLOY_TOKEN`（CI 里可以完全不用配置文件；`DEPLOY_SERVER` 是地址的历史名，仍兼容但优先级更低） |
| 3 | 配置文件：`--server` 指定的服务器，否则 `current` 指向的服务器 |
| 4 | 默认值：`http://127.0.0.1:10055`（token 无默认值，为空即视为未登录） |

`--server` 点名的服务器不存在时**不静默回退**，而是返回退出码 2 并提示 `deploy server list`。

### 全局 flag

| flag | 说明 |
|---|---|
| `--server <名字>` | 使用配置文件中指定名字的服务器（优先于 `current`） |
| `-a, --address URL` | 覆盖服务端地址。地址覆盖**只认** `-a/--address`；旧的 `--server` 地址别名已删除 |
| `--token TOKEN` | 覆盖 token（JWT 或 `dk_` 开头的 API Token） |
| `--json` | stdout 只输出机器可读 JSON / JSONL，人类提示一律走 stderr |
| `--no-color` | 禁用彩色；非 TTY 或 `NO_COLOR=1` 时自动禁用 |
| `--verbose` | 调试点（HTTP 请求、重连、配置来源）输出到 stderr |

---

## 3. 命令速查

```text
deploy server list | get <名字> | add <名字> -a URL -t TOKEN [-u USER] [--no-set-current]
deploy server use <名字> | update <名字> [-a|-t|-u|--name 新名|--comment] | remove <名字> [--yes] | current
deploy --server <名字> <任何命令>            # 临时指定服务器

deploy login [-a URL] [-u USER] [-p PASS] [--token TOKEN] [--name NAME]
deploy logout
deploy whoami
deploy project list [--keyword K] [--page N] [--page-size N] [--archived]
deploy project get <key>
deploy project create --key K --name N --workdir DIR --command "脚本" [--command "名称::脚本"] ...
deploy project create --file project.json     # 从 JSON 读项目定义（命令可用 scriptFile）
deploy run <key> [-f] [-e K=V ...] [--branch REF] [--wait] [--cancel-on-interrupt] [--command-id ID]
deploy logs <runId> [-f] [--offset N]
deploy runs list [--project KEY] [--status S] [--page N] [--page-size N]
deploy stop <runId>
deploy doctor

deploy middleware list | get <key> | fields [key] | script
deploy middleware install [key...] [--missing] [-m docker|native] [--dry-run] [--force] [--no-follow]
deploy middleware start|stop|restart|logs <key...> [--no-follow]
deploy middleware remove <key...> --yes [--no-follow]
deploy middleware set <key> [-p KEY=VALUE ...] [--mode docker|native]

deploy config get [key] | set <key> <value> | list | path
deploy version [--short]        # 也支持 deploy --version
```

---

## 4. 命令示例

### 4.1 登录 / 登出 / 身份

```powershell
# 交互式：-p 省略时输入密码不回显（golang.org/x/term.ReadPassword）
deploy login -a http://127.0.0.1:10055 -u admin

# 非交互（CI）
deploy login -a http://127.0.0.1:10055 -u admin -p "$env:DEPLOY_PASSWORD"

# 直接用 API Token 跳过登录（会先调 /auth/me 校验；401 则不落盘）
deploy login -a https://deploy.example.com --token dk_xxxxxxxx

# 登录并写入指定服务器（--name 指定写进哪个服务器；不存在则登录成功后自动创建）
deploy login -a https://prod.example.com -u admin --name prod

deploy whoami            # 显示用户/角色/服务器名字/地址（token 失效 → 退出码 3）
deploy whoami --json     # { server:<服务器名字>, address:<URL>, name, user }
deploy logout            # 清空当前服务器的 token（同时尽力调 /auth/logout）
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

Windows 上没有 bash 的 `$(cat sh\01.sh)`：把项目定义写成 JSON，用 `--file` 读入，
每条命令用 `scriptFile` 指向脚本（相对 JSON 文件所在目录，`sh\01.sh` 这种写法也行）。
显式 flag 会覆盖文件里的同名字段，`--command` 一旦传入则完全取代文件里的 `commands`：

```json
{
  "key": "tio-boot-web-hello",
  "name": "tio-boot web hello",
  "workdir": "/srv/deploy/workspaces/tio-boot-web-hello",
  "useGit": true,
  "gitUrl": "https://gitee.com/ppnt/tio-boot-web-hello.git",
  "gitBranch": "main",
  "env": { "APP_PORT": "10062" },
  "commands": [
    { "name": "1. 环境勘察（只读）", "scriptFile": "sh/01-recon.sh" },
    { "name": "2. Maven 构建生产包", "scriptFile": "sh/02-build.sh" }
  ]
}
```

```powershell
deploy project create --file project.json
```

字段名与 API 一致，所以 `deploy project get <key> --json` 导出的内容改一改就能再建。
`commands` 里每条命令可以内联 `script`，或给 `scriptFile`；两者都写会报错。

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

### 4.4 服务器管理

```powershell
# 新增（并设为默认服务器）；同名会被更新，不会重复添加
deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxxxxxxx -u admin
deploy server add prod -a https://deploy.example.com -t dk_yyyyyyyy --no-set-current

deploy server list                 # 表格：* 标记当前，NAME=名字（即配置里的键），token 打码
deploy server list --json          # { current, path, servers:[{key,name,address,user,comment,token,active}] }（key 与 name 同值）
deploy server get 97 --json        # { key, name, address, user, comment, token, active }（add/update 同形，URL 字段是 address）
deploy server current --json       # { current, path, name, address, user, token, comment }
deploy server get 97               # 单个服务器的名字/地址/用户/备注/是否当前
deploy server use 97               # 切换默认服务器
deploy server update 97 -t dk_newtoken          # 只改 token，其余字段保持
deploy server update 97 --name prod             # 改名：键与 current 一起变成 prod
deploy server update 97 -a http://10.0.0.5:10055
deploy server remove prod --yes    # 非交互必须 --yes
deploy server current              # 打印当前服务器名字（便于脚本取值）

# 不改默认服务器，临时指定（推荐脚本/CI 用这种，无状态）
deploy --server prod run my-app --wait --json
```

约定与边界：

- **名字**（`add` 的位置参数，也是配置里的键）允许中文与点/短横线，但不能含**空格、冒号、斜杠**（会让 `--server` 传参要靠引号、并与 URL scheme 混淆）。
- `update --name <新名>` 就是**改名（改键）**：地址/token/用户名/备注整条搬过去，`current` 跟着走；
  新名已存在、为空或非法时报用法错误（退出码 2），不会静默覆盖。
- `update` **只改显式传了的字段**：不传 `-t` 就不会动 token，避免「只想换地址」却把 token 清掉。
- `add` 一个**只填地址不填 token** 的服务器是允许的（之后可 `deploy login -a <地址> --name <名字>` 补，或 `deploy server update <名字> -t <token>`），
  但命令会明确提示还没有 token。
- `server list`、`whoami` 等命令的输出里 token 一律打码；只有 `deploy config get token` 会输出明文（给脚本用）。

### 4.5 中间件（docker / postgresql / mysql / redis / elasticsearch）

```powershell
deploy middleware list                                  # 方式 / 已装 / 在跑 / 版本 / 位置
deploy middleware list --json
deploy middleware get redis                             # 状态 + 详情 + 可编辑参数
deploy middleware fields redis                          # 只要参数表

# 先预演（只打印计划，不做任何修改）
deploy middleware install redis --dry-run --no-follow

# 真装：--no-follow 立刻返回 runId；不加则默认跟随日志
deploy middleware install redis --no-follow --json
deploy logs 88 -f

deploy middleware install docker redis postgresql --no-follow   # 一次多个；docker 应在最前
deploy middleware install --missing --no-follow                 # 只装缺失的
deploy middleware install redis -m native --no-follow           # 指定方式
deploy middleware install redis --force --no-follow             # 已装的也重建（数据卷保留）

deploy middleware start redis --no-follow
deploy middleware stop redis --no-follow
deploy middleware restart redis --no-follow
deploy middleware logs redis --no-follow                # 最近 100 行日志
deploy middleware remove redis --yes --no-follow        # 只删容器，数据卷保留

# 参数（就是脚本读的环境变量名）；密码类在服务端加密存储、不回显
deploy middleware set redis -p REDIS_PORT=16380 -p REDIS_PASSWORD=<密码>
deploy middleware set postgresql -p PG_VERSION=17 --mode native
deploy middleware set redis -p REDIS_PASSWORD=          # 空串 = 清空（回到随机生成）

deploy middleware script > install.sh                   # 导出内嵌脚本，可拿去手动跑
```

要点：

- `--no-follow` 与 `--follow`（默认 true）成对；AI/脚本用前者，人用后者。
- `remove` 非交互环境**必须** `--yes`；`--force` 让「容器不存在」也算成功。
- `elasticsearch` 只支持容器方式、`docker` 只支持原生方式，传错服务端会明确拒绝（退出码 2）。
- 中间件操作只在 **Linux 服务端**可用；其他平台只能 `--dry-run`。
- 退出码沿用全局约定：安装/启停失败会让跟随的日志以退出码 1 收尾。

### 4.6 给 AI / 脚本用

这里**没有给 AI 专用的子命令**：AI 用的就是普通 `deploy`。命令清单看 `--help`，“现在连的是哪台”看 `whoami`：

```powershell
deploy --help              # 全部子命令
deploy run --help          # 某个子命令的参数
deploy server list --json # 已配的服务器
deploy whoami              # 当前服务器与用户（退出码 3=鉴权失败、4=不可达）
deploy doctor --json       # 服务端环境自检
```

配合 `--json`（stdout 只有数据、提示走 stderr）与退出码 0/1/2/3/4，脚本即可判断成败。
详细说明见 [docs/ai/AI对接.md](../docs/ai/AI对接.md)，Agent 技能说明见
[skills/deploy-cli/SKILL.md](../skills/deploy-cli/SKILL.md)。

### 4.7 自检与配置

```powershell
deploy doctor            # 调 GET /system/doctor，彩色 ✔/✘ + 安装提示
deploy doctor --json     # 原样输出契约里的 doctor data

deploy config get                       # 全部配置（token 打码）
deploy config get address               # 单项（token 会输出明文，供脚本使用）
deploy config set address http://127.0.0.1:10055
deploy config get --json                 # { current, address, token, user, path }
deploy config get address --json         # { key, value, server:<当前服务器名字> }
deploy config list --json                # { current, servers:[{key,name,address,user,token,active}] }（key 与 name 同值）
deploy config set token dk_xxx
deploy config set current prod          # 切换默认服务器（低层写法，优先用 server use）
deploy config list                      # 所有服务器（NAME=名字，即配置里的键）
deploy config path                      # 配置文件路径

deploy version
deploy --version
```

> `config` 是低层接口（键值读写），面向脚本；日常与多服务器场景请用 `server` 命令组，
> 它带校验、能整段管理一个服务器，并且不会因为只改一个字段而把别的字段清掉。

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
  3) deploy login -a "$DEPLOY_ADDRESS" -u "$USER" -p "$PASS"; deploy run my-app -f ;;
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
  cmd/deploy/main.go          # 入口：错误 → 退出码映射，错误与提示写 stderr
                              #   放在 cmd/deploy/ 下，go install ./cmd/deploy 才产出 deploy
  cmd/                        # 各 cobra 子命令
    root.go                   #   全局 flag（--server/--address/--token/--json/--no-color/--verbose）
    server.go                 #   服务器管理（list/get/add/use/update/remove/current）
    middleware.go             #   中间件（list/get/install/start/stop/restart/remove/logs/set/fields/script）
    login/logout/whoami/project*/run/logs/runs/stop/doctor/config/version
  internal/api/               # 契约类型 + HTTP 客户端 + 错误分类（错误自带 ExitCode()）
  internal/ws/                # WebSocket 帧、Dialer、Follower（历史补齐 + 增量 + 断线按 offset 续拉）
  internal/config/            # ~/.deploy/config.yaml 读写：服务器（名称/地址/token/备注）、优先级解析
  internal/output/            # 彩色输出、ANSI 剥离、等宽表格（CJK 宽度对齐）
  internal/exitcode/          # 退出码常量与 err → 退出码分类
  internal/version/           # -ldflags -X 注入的版本变量
```

## 8. 测试

```powershell
go build -o deploy.exe ./cmd/deploy
go vet ./...
go test ./...
go test ./... -cover
```

覆盖的关键逻辑：

- 退出码映射（`internal/exitcode`、`internal/api`：401→3、5xx→4、40001→2、传输错误→4、未分类→2）
- ANSI 剥离（含跨块切分的转义序列）、非 TTY 不产生颜色、`--json` 时提示不污染 stdout
- 配置读写（0600 权限、多服务器、`--server` 优先级与「服务器名字不存在 → 退出码 2」、`update` 只改传了的字段）
- 日志 offset 续拉（分块拉到 `eof`、重连去重、鉴权失败不重试、Ctrl+C 中断语义）
- 命令层：进程内跑完整 cobra 命令树，对照真实 HTTP + WebSocket 假服务端校验退出码与输出
- 中间件动作：逐个校验 `action` / `components` / `mode` / `dryRun` 请求体，密码参数不回显、空串表示清空
