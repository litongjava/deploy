# AI 对接（让 AI 通过 deploy-cli 部署与运维）

> 定位：**把 deploy-cli 做成 AI 可以安全驱动的接口**，而不是给 AI 一个新的专有协议。
> 理由见「为什么不做 MCP」一节 —— 结论是 CLI + `--json` 已经够用，且更好调试。

## 1. 三条链路

```
使用者（人）                           AI Agent
   │                                     │
   │ 1. 手工部署 deploy-server            │ 3. 读 skill / 读 deploy --help
   │    并完成首次初始化                   │    拿到「当前服务器 + 项目 + 命令清单」
   ▼                                     ▼
┌──────────────────────┐        ┌──────────────────────────┐
│ deploy-server        │◄───────┤ deploy-cli               │
│ (192.168.31.97:10055)│  HTTP  │ · --server <名字> 选服务器  │
└──────────┬───────────┘        │ · --json 机器可读输出      │
           │                    └──────────────────────────┘
           │ 2. 名称+地址+token 存在本地 ~/.deploy/config.yaml
           ▼
   在服务器上执行命令（项目部署 / 中间件安装）
```

需求的三步对应关系：

| 需求 | 落点 |
|---|---|
| ① 手工完成 deploy-server 部署和初始化 | `deploy-server/README.md`、根 `README.md` 的「部署到服务器」 |
| ② 名字、地址、token 存本地文件，用 `--server <名字>` 指定 | `deploy server add`、`deploy --server <名字> <命令>` |
| ③ AI 能操作 CLI 部署、执行任务、装中间件 | `deploy --help`（命令清单）、`deploy middleware`（中间件）、本目录的 skill |

## 2. 使用者要做的两步

### 2.1 手工部署并初始化服务端（不需要 AI 参与）

按[根 README 的「部署到服务器」](../../README.md#部署到服务器)把二进制 + systemd unit 装好，
浏览器打开服务端地址，按向导创建管理员。**记下地址。**

### 2.2 把「名字 / 地址 / token」交给 CLI

token 在服务端「个人设置 → API Token」创建（`dk_` 前缀，长期有效，适合 AI 使用；
用管理员密码登录拿到的 JWT 会过期，不适合无人值守）。

```bash
deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxxxxxxx -u admin
```

命令里的 `97` 就是**服务器的名字，也是配置里的键**：`--server 97` 用它，
`deploy server update 97 --name <新名>` 可以改名（键与 `current` 一起改）。

也可以直接 `deploy login -a <地址> -u admin --name test`：该服务器不存在时会在**登录成功后自动创建**，所以 `server add` 不是前置步骤。

写进 `~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`，权限 0600）：

```yaml
current: "97"
contexts:
  "97":
    address: http://192.168.31.97:10055
    token: dk_xxxxxxxx
    user: admin
  prod:
    address: http://10.0.0.5:10055
    token: dk_yyyyyyyy
    user: admin
```

**注意：配置文件里地址在 `address`，没有单独的「显示名」字段** —— `contexts` 下的键就是服务器的名字。
引用服务器一律用这个名字（也就是 `--server` 的参数）。

之后**每条命令都可以点服务器**，`--server` 优先于配置里的 `current`：

```bash
deploy --server 97 run my-app --wait --json
deploy --server prod runs list --status failed
```

## 3. AI 怎么用

### 3.1 第一步：用 `--help` 与 `whoami` 看清现状

这里**没有给 AI 专用的命令** —— AI 用的就是普通 `deploy`：

```bash
deploy --help                 # 全部子命令
deploy run --help             # 某个子命令的参数
deploy server list --json     # 已配的服务器（{current, path, servers:[{key,name,address,user,comment,token,active}]}）
deploy whoami                 # 当前连的服务器 / 用户（退出码 3=鉴权失败、4=不可达）
deploy project list --json    # 服务器上有什么项目
deploy middleware list --json # 中间件状态
```

这些是**命令的实时输出**，不是写死的文档 —— 服务器列表、项目、中间件状态都会变，
写死在提示词里必然过期。连不上（退出码 4）或 token 失效（退出码 3）也能从退出码直接判断。

### 3.2 典型动作序列

```bash
# 看有什么项目
deploy project list --json

# 触发部署并等结果（推荐：一次调用拿到终态与退出码）
deploy run my-app --wait --json

# 长部署：先触发，再按需跟日志
deploy run my-app --json                       # → runId
deploy logs 88 -f                              # 实时日志

# 看中间件状态
deploy middleware list --json

# 装中间件（先预演，再真装，再跟日志）
deploy middleware install redis --dry-run --no-follow --json
deploy middleware install redis --no-follow --json    # → runId
deploy logs 801 -f

# 启停 / 移除（移除只删容器，数据卷保留）
deploy middleware start redis --no-follow
deploy middleware remove redis --yes --no-follow
```

### 3.3 输出与退出码约定

| 约定 | 内容 |
|---|---|
| stdout | 只有数据。`--json` 时是 JSON 对象；跟随日志时是 JSONL，每行 `{"type":"log","data":"..."}` |
| stderr | 所有人类提示、警告、调试信息，**不会污染 stdout** |
| 颜色 | 非 TTY 自动关闭，也可用 `--no-color` / `NO_COLOR=1` |
| 退出码 | `0` 成功 / `1` 部署失败 / `2` 用法错误 / `3` 鉴权失败 / `4` 网络或服务不可用 |

**AI 必须看 `--json` 里的字段，不能只看退出码**：业务的 400 也返回 HTTP 200
（信封里的 `success:false`），而 `deploy` 已经把这类错误映射成退出码 2。

### 3.4 长任务的两个开关

中间件的安装/启停默认**跟随日志**（`--follow`），这符合人在终端里用的直觉；
AI/脚本要「触发后就返回」就加 `--no-follow`：

```bash
deploy middleware install redis --no-follow --json   # 立刻返回 runId
```

`--dry-run`（仅 `install`）只打印计划，不做任何修改 —— **AI 第一次碰一台新服务器时应该先用它**。

## 4. 为什么不做 MCP

评估过 MCP（Model Context Protocol），结论是当前形态用 CLI 更合适：

| 维度 | CLI + `--json` | MCP Server |
|---|---|---|
| 接入成本 | 会 shell 的 Agent 直接可用（Claude Code、Codex、Cursor、自研脚本） | 每个客户端都要配一份连接；服务端要维护会话、鉴权透传、SSE |
| 调试 | 人直接敲同一条命令，所见即 AI 所见 | 要在客户端与服务端两头看日志 |
| 鉴权 | 复用 `~/.deploy/config.yaml` 的 token，不新增凭据面 | 要么把 token 交给客户端，要么服务端再做一套 |
| 稳定性 | 无长连接、无协议版本协商 | 连接生命周期、心跳、重连都要处理 |
| 能力面 | 部署、日志、任务、中间件已经全是命令 | 需要为每个能力写 tool schema 并保持同步 |

保留的后路：cobra 命令树本身就是单一事实来源 —— `deploy --help` / `deploy <命令> --help`
覆盖了全部能力面，**可以据此生成 function-calling 的 tools 或 MCP tools**。
真要做协议层时，从命令树派生即可，不需要另写一套定义。

## 5. 安全边界（必须让 AI 知道）

- 平台本质是**远程命令执行**：能登录 + 能执行 = 能在服务器上跑任意 shell。token 等于服务器权限。
- token 只存在本地配置文件（0600），**不要**打印到日志、发到聊天窗口或提交进仓库。
- 生产服务器与测试服务器用不同的服务器名字分开配，AI 默认只该碰测试服务器。
- `deploy middleware remove` 只删容器/包，**数据卷会保留**：这不是「清空环境」。
- 中间件默认参数对公网是不安全的：Redis 无密码、ES 关闭安全（`xpack.security.enabled=false`）。
  只在内网使用；公网暴露前必须按 `deploy middleware get <key>` 里的说明加固。

## 6. 排错

| 现象 | 原因 / 处理 |
|---|---|
| `server "xxx" does not exist` | `--server` 的**名字**打错了（名字就是配置里的键）；`deploy server list` 看已配的服务器 |
| 退出码 3 | token 缺失/失效：重新 `deploy server update <名字> -t <新token>` |
| 退出码 4 | 服务端不可达：`deploy whoami` 的退出码 3 是鉴权、4 是不可达；再查网络与 systemd |
| `middleware` 命令报 404 | 服务端是旧版本；升级到带 `/system/middleware` 的版本（本仓库 0.6.0 起） |
| 中间件安装一直 `running` | 多数是在拉镜像。`deploy logs <runId>` 看进度；国内网络慢可配 Docker 镜像加速 |
| `不支持 native 方式` | 该中间件只支持容器（`elasticsearch`）；去掉 `-m native` 或改用 docker |
| 装中间件被拒（非 Linux） | 脚本基于 bash + apt/dnf + docker，只在 Linux 服务端可用；其他平台只能 `--dry-run` |

## 7. 相关文件

- [`skills/deploy-cli/SKILL.md`](../../skills/deploy-cli/SKILL.md) —— 给 AI 的技能说明（可直接放进 skill 目录）
- [`docs/API设计.md`](../API设计.md) —— 接口契约（中间件接口见 §9）
- [`deploy-cli/README.md`](../../deploy-cli/README.md) —— CLI 全部命令
- [`deploy-server/internal/middleware/install.sh`](../../deploy-server/internal/middleware/install.sh) —— 中间件安装脚本（可脱离平台手动跑）
