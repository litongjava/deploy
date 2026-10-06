---
name: deploy-cli
description: Use when deploying, running tasks, or installing/operating middleware (docker/postgresql/mysql/redis/elasticsearch) on a self-hosted deploy-server through the deploy CLI, and when a request mentions deploy-cli, deploy-server, 部署平台, 中间件安装, 一键安装, --server, or running a project's deployment from an agent. Handles choosing a server (name + address + token stored locally, the name IS the config key), triggering runs, following logs, reading exit codes, and the middleware install/start/stop/remove flow with dry-run first.
---

# 用 deploy-cli 部署与运维

## 这是什么

`deploy` 是一个自托管「命令式 CI/CD 平台」的 CLI。平台本身（deploy-server）由使用者
**手工部署并初始化**；AI 通过 CLI 操作它：触发部署、执行任务、安装与启停中间件。

- 命令在**服务器上**执行，不是本机 —— `deploy run` 的 shell 跑在 deploy-server 所在机器。
- CLI 只是客户端：`deploy logs` 读的是服务端日志文件的字节流。
- **token 等于服务器权限**（平台本质是远程命令执行）。不要打印、外传或提交它。

## 先做的三件事（顺序不能反）

### 1. 问清「操作哪台服务器」

服务器存在 `~/.deploy/config.yaml`（Windows：`%USERPROFILE%\.deploy\config.yaml`）。
**不要猜服务器**，先列出来：

```bash
deploy server list --json           # 已配的服务器；{current, path, servers:[{key,name,address,user,comment,token,active}]}
deploy whoami                       # 当前服务器 + 是否可达 + 登录态（退出码 3/4）
```

**名字就是配置里的键，只有一个，别再造一个「显示名」**：

- `name` —— `contexts` 下的键，也是服务器的名字。`--server <名字>` 用它定位，
  `server update <旧名> --name <新名>` 改的就是它（改键，`current` 跟着走）。

配置里地址在 `address` 字段，没有独立的显示名字段。**永远用这个名字去引用服务器**。

指定服务器用全局参数 `--server <key>`，它优先于配置里的 `current`：

```bash
deploy --server test run my-app --wait --json
```

若目标服务器不存在，让**使用者**提供地址与 token（也可 `deploy login -a <地址> --name <键>`：登录成功后会自动创建），然后：

```bash
deploy server add 97 -a http://<地址>:10055 -t <token> -u admin
```

### 2. 一次问清环境，不要试探

这里**没有给 AI 专用的命令**：用的就是普通 `deploy`。命令清单看 `--help`，当前连的是哪台看 `whoami`：

```bash
deploy --help                 # 全部子命令
deploy <子命令> --help         # 某个子命令的参数
deploy server list --json     # 已配的服务器（名字 + 地址；key 与 name 同值）
deploy whoami                 # 当前服务器与用户（退出码 3=鉴权失败、4=不可达）
deploy doctor --json          # 服务端环境自检（工具链、端口）
deploy middleware list --json # 中间件状态
```

`--help` 与这些命令给的都是**实时事实**，不是写死的文档；连不上是退出码 4、token 失效是退出码 3，
据此决定下一步，不要反复试探。

### 3. 看项目 key，不要编

```bash
deploy project list --json
deploy project get <key> --json     # 含命令列表与变量，能看出这个项目会做什么
```

## 输出与退出码（解析结果的唯一正确方式）

| 项 | 约定 |
|---|---|
| stdout | 只有数据。`--json` 是 JSON；跟随日志时是 JSONL，每行 `{"type":"log","data":"..."}` |
| stderr | 人类提示、警告、调试信息。**不会**污染 stdout |
| 颜色 | 非 TTY 自动关闭；可加 `--no-color` |
| `0` | 成功 |
| `1` | 部署失败（`failed`/`timeout`/`error`/`canceled`） |
| `2` | 用法错误（参数不对、服务器名字不存在、缺必填项） |
| `3` | 鉴权失败（token 缺失/无效/过期） |
| `4` | 网络或服务不可用 |

**业务失败也返回 HTTP 200**（信封里 `success:false`），CLI 已把它映射成退出码。
所以：**看退出码 + 看 `--json` 的字段**，不要只看 HTTP 状态。

## 部署：最常用的两条路径

```bash
# A. 一次调用拿终态（推荐给 AI：可判定成败，有退出码）
deploy --server <名称> run <projectKey> --wait --json
# → {runId, status, exitCode, durationMs, buildNumber}

# B. 长部署：先触发，再按需跟日志
deploy --server <名称> run <projectKey> --json        # 记下 runId
deploy --server <名称> logs <runId> -f                # 实时跟随（Ctrl+C 只断开跟随，不取消运行）
deploy --server <名称> logs <runId>                   # 一次性拉全部历史
```

带参数与分支：

```bash
deploy run my-app -e TAG=v1.2.3 --branch release --wait --json
```

取消、查历史：

```bash
deploy stop <runId>
deploy runs list --project <key> --status failed --json
```

**注意**：`run` 不带 `-f`/`--wait` 只触发并立刻返回，拿到的是 `queued`，
**不能据此判断部署成功**。要结论就必须 `--wait` 或跟日志到终态。

## 中间件：docker / postgresql / mysql / redis / elasticsearch

### 铁律：先 `--dry-run`

```bash
deploy --server <名称> middleware list --json     # 现状：installed / running / mode / missing
deploy --server <名称> middleware install redis --dry-run --no-follow --json
```

dry-run 只打印计划、不改任何东西。**第一次碰一台服务器时必须先预演**，
并把计划里要暴露的端口、要挂的数据卷念给使用者确认。

### 安装 / 启停 / 移除

```bash
# 安装（默认容器方式；--mode native 走发行版包管理器）
deploy middleware install redis --no-follow --json            # → runId
deploy logs <runId> -f                                        # 跟安装日志

# 一次装多个（顺序即执行顺序，docker 应在最前）
deploy middleware install docker redis postgresql --no-follow

# 只装缺失的（服务端按现状判断）
deploy middleware install --missing --no-follow

# 启停查
deploy middleware start|stop|restart redis --no-follow
deploy middleware logs redis --no-follow

# 移除：**只删容器/包，数据卷与数据目录保留**
deploy middleware remove redis --yes --no-follow
```

- `--no-follow` 是给 AI/脚本用的：触发后立刻返回 `runId`，不阻塞。
  不加则默认跟随日志（适合人）。
- `remove` 非交互环境**必须** `--yes`。
- `elasticsearch` 只支持容器方式；`docker` 只支持原生方式。传错方式会被服务端明确拒绝。
- 中间件安装只在 **Linux 服务端**可用；其它平台只能 `--dry-run`。

### 改参数（端口 / 密码 / 版本）

```bash
deploy middleware fields redis                      # 列出可编辑参数与当前值
deploy middleware get redis                         # 状态 + 参数 + 版本 + 位置
deploy middleware set redis -p REDIS_PORT=16380 -p REDIS_PASSWORD=<密码>
deploy middleware set postgresql -p PG_VERSION=17 --mode native
deploy middleware set redis -p REDIS_PASSWORD=      # 传空串 = 清空（回到随机生成）
```

密码类参数（`*_PASSWORD`）在服务端**加密存储**，接口只回「已设置」，无法读回。
`set` 只改你传了的键 —— 不传的键保持原值。

### 端口冲突与镜像拉取（实机踩过的坑）

1. **端口冲突是常态**：服务器上往往已经跑着 redis/postgres/es。安装前用
   `deploy middleware get <key>` 看端口，必要时先 `set` 换端口，
   或确认那是不是本平台以前装的容器（容器名 `deploy-mw-<key>`）。
   端口被占用时脚本只**警告**并继续（`IGNORE_PORT_CONFLICT=1`），容器可能因此起不来。
2. **镜像拉取失败很常见**：Docker Hub 在国内网络常常不通（或守护进程配的代理已失效）。
   脚本会先尝试 pull，失败但本地已有该 tag 时**自动回退用本地镜像**并给出警告。
   若本地也没有，安装会失败并打印排查命令（`systemctl show docker --property=Environment`
   看代理、`docker images` 看本地 tag）。此时**不要反复重试**，把结论交给使用者。
3. **拉取会阻塞约 60 秒**才开始回退 —— 跟日志时耐心等，不要以为卡死了。

## 安全边界（必须主动告知使用者）

- token 只存本地配置文件（0600）。**不要**把它写进命令输出、日志、提交或聊天记录。
- 平台能执行任意 shell：能登录 = 能在服务器上为所欲为。测试与生产**用不同的服务器名字**分开，
  默认只碰测试服务器。
- 中间件默认参数对公网**不安全**：Redis 可能无密码、ES 关闭了安全
  （`xpack.security.enabled=false`）、数据库端口直接暴露。只在内网用；
  公网暴露前必须加固，并把这件事明确说出来。
- `remove` 不是「清空环境」：数据卷会保留。要彻底删数据得手动
  `docker volume rm deploy-mw-<key>-data` —— 这是破坏性操作，**必须先问使用者**。
- 任何会改服务器状态的命令，先 `--dry-run`；没有 dry-run 的（启停/移除）先说明会做什么。

## 常见错误与对策

| 现象 | 含义 / 对策 |
|---|---|
| 退出码 3 | token 缺失/失效 → 让使用者提供新 token，`deploy server update <名字> -t <新token>` |
| 退出码 4 | 服务端不可达 → 用 `deploy whoami`（退出码 3 是鉴权、4 是不可达）确认，再查网络与 systemd |
| `server "x" does not exist` | 名字打错 → `deploy server list`（报的就是服务器的名字，也就是配置里的键） |
| `endpoint not found: /api/v1/system/middleware` | 服务端版本旧，没有中间件接口 → 需要升级服务端 |
| `不支持 native 方式` | 该中间件只支持容器（如 elasticsearch）→ 去掉 `-m native` |
| `only available on Linux servers` | 服务端不是 Linux → 只能 dry-run |
| `nothingToDo: true` | 要装的都已装好。想重建要加 `--force`（会保留数据卷） |
| 安装失败但日志说「本机已有镜像」 | 拉取失败后回退成功只是警告，真正的失败在下一行；继续往下读日志 |
| 日志里密码是 `***` | 正常。明文只在日志末尾「连接信息」里出现一次 |

## 最小决策流程

1. `deploy whoami` / `deploy server list --json` —— 我在操作哪台？可达吗？登录了吗？
2. `deploy project list --json` / `deploy middleware list --json` —— 有什么项目和中间件？
3. 要改服务器状态 → 先 `--dry-run`，把计划说给使用者听。
4. 执行 → `--no-follow --json` 拿 `runId` → `deploy logs <runId> -f` 看过程 → 报告终态与退出码。
5. 失败时**读日志**再下结论：区分「镜像拉不动」「端口被占」「方式不支持」「平台是旧版本」——
   它们的对策完全不同，不要笼统地说「装失败了」。
