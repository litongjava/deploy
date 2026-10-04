# API 契约（v1，冻结）

> 这份文档是**唯一事实来源**。后端、CLI、前端三方都按它实现；改接口先改这里。
>
> 冻结理由：前端沿用的 Ant Design Pro 模板里 `src/requestErrorConfig.ts` 已经写死了响应封装
> `{success, data, errorCode, errorMessage, showType}` 与 `Authorization: <token>`（**裸 token，不是 Bearer**）。
> 顺着模板走，前端只需要删页面、加页面，不用动请求层。

## 0. 通用约定

- Base path：`/api/v1`
- 编码：UTF-8，`Content-Type: application/json`
- 字段命名：**camelCase**

### 0.1 响应封装

成功（HTTP 200）：
```json
{ "success": true, "data": { } }
```

业务失败（**HTTP 200**，让前端模板的 `errorThrower` 统一弹 message）：
```json
{ "success": false, "errorCode": 40001, "errorMessage": "项目 key 已存在", "showType": 2 }
```

认证失败（**HTTP 401**，前端据此清 token 跳登录）：
```json
{ "success": false, "errorCode": 40100, "errorMessage": "未登录或登录已过期", "showType": 0 }
```

`showType` 沿用模板枚举：`0` 静默 / `1` warning / `2` error / `3` notification。

> ⚠️ **调用方必读**：除了 401，"资源不存在"、"参数错误"、"无权限" 这些**全部是 HTTP 200**，
> 只是 `success=false`。所以**不能用 HTTP 状态码判断成功与否**，也不能用
> `try/catch` 捕获业务错误（PowerShell 的 `Invoke-RestMethod`、Go 的 `http.Get`
> 都不会因为 `success=false` 报错）。
>
> 正确姿势：解析响应体后先看 `success`，为 `false` 就按 `errorCode` 分流。
>
> 这个坑踩过一次：一个部署脚本用 `try { GET /projects/xxx } catch { 项目不存在 }`
> 判断项目是否存在，结果因为返回的是 200，被判成"项目已存在"，
> 接着去 PUT 一个不存在的项目，一路空值。

### 0.2 错误码

| errorCode | 含义 |
|---|---|
| `40000` | 参数校验失败（`errorMessage` 给出具体字段） |
| `40100` | 未认证 / token 失效 |
| `40300` | 无权限 |
| `40400` | 资源不存在 |
| `40900` | 冲突（key 重复、名称重复、被引用） |
| `42900` | 触发过频（Hook 限流） |
| `50000` | 服务端内部错误 |

### 0.3 认证

- 登录后签发 JWT，放在 `Authorization` 头。
- 服务端**同时接受** `Authorization: <jwt>` 与 `Authorization: Bearer <jwt>`。
- CLI 与第三方可用 API Token 替代 JWT（同一请求头，前缀 `dk_`）。

### 0.4 分页

请求：`?page=1&pageSize=20`（`page` 从 1 开始，`pageSize` 最大 200，默认 20）

响应 `data`：
```json
{ "list": [], "total": 0, "page": 1, "pageSize": 20 }
```

---

## 1. 认证与初始化

### `GET /api/v1/auth/bootstrap-status` （公开）
```json
{ "success": true, "data": { "initialized": false } }
```
`initialized=false` 时前端显示初始化向导。

### `POST /api/v1/auth/bootstrap` （公开，仅未初始化时可用）
请求：`{ "username": "admin", "password": "xxx" }`
响应：`{ "success": true, "data": { "token": "<jwt>", "expiresAt": 1760000000, "user": User } }`
已初始化时返回 `40900`。

### `POST /api/v1/auth/login` （公开）
请求：`{ "username": "admin", "password": "xxx" }`
响应：`{ "token": "<jwt>", "expiresAt": 1760000000, "mustChangePassword": false, "user": User }`
密码错 → `40100`；账号禁用 → `40300`。

### `GET /api/v1/auth/me`
响应：`User`

### `POST /api/v1/auth/logout`
响应：`data: null`

### `POST /api/v1/auth/change-password`
请求：`{ "oldPassword": "x", "newPassword": "y" }`
新密码长度 ≥ 8。成功后旧 token 仍有效（MVP 不做黑名单）。

### User 对象
```json
{
  "id": 1, "username": "admin", "nickname": "管理员", "email": "",
  "role": "admin", "status": "active",
  "mustChangePassword": false,
  "lastLoginAt": "2026-01-01T10:00:00+08:00",
  "createdAt": "2026-01-01T10:00:00+08:00"
}
```
`role`: `admin` | `user`；`status`: `active` | `disabled`。

---

## 2. 用户管理（仅 admin）

| 方法 | 路径 | 请求 | 说明 |
|---|---|---|---|
| GET | `/users?page&pageSize&keyword` | — | 分页列表 |
| POST | `/users` | `{username,password,nickname,email,role}` | 新建，username 唯一 |
| PUT | `/users/{id}` | `{nickname,email,role,status}` | 修改（不含密码） |
| DELETE | `/users/{id}` | — | 删除；不能删自己、不能删最后一个 admin（`40300`） |
| POST | `/users/{id}/reset-password` | `{newPassword}` | 重置 |

---

## 3. API Token（CLI / 第三方）

| 方法 | 路径 | 请求 | 响应 |
|---|---|---|---|
| GET | `/tokens` | — | `[Token]`（不含明文） |
| POST | `/tokens` | `{name, expiresInDays?}` | `Token + { token: "<明文，仅此一次>" }` |
| DELETE | `/tokens/{id}` | — | — |

```json
{ "id": 3, "name": "ci", "prefix": "dk_ab12cd", "expiresAt": "2026-06-01T00:00:00+08:00",
  "lastUsedAt": null, "createdAt": "..." }
```
明文格式：`dk_` + 32 位随机 base62。服务端只存 `sha256`。

---

## 4. 项目

### 4.1 Project 对象（详情）
```json
{
  "id": 1,
  "key": "my-app",
  "name": "我的应用",
  "description": "",
  "workdir": "/srv/workspaces/my-app",
  "shell": "auto",
  "timeoutSec": 0,
  "concurrency": "queue",
  "hookEnabled": true,
  "ownerId": 1,
  "ownerName": "admin",
  "archived": false,
  "useGit": false,
  "gitUrl": "",
  "gitBranch": "main",
  "gitCredentialId": null,
  "gitSubdir": "",
  "gitDepth": 0,
  "gitClean": false,
  "gitSubmodule": false,
  "allowEnvOverride": true,
  "lastRunAt": "2026-01-01T10:00:00+08:00",
  "lastRunStatus": "success",
  "runCount": 12,
  "canEdit": true,
  "commands": [Command],
  "envs": [EnvVar],
  "createdAt": "...",
  "updatedAt": "..."
}
```
- `shell`: `auto` | `bash` | `sh` | `pwsh` | `cmd`
- `concurrency`: `queue`（同项目串行，默认）| `allow`（并行）| `cancelPrev`
- `canEdit`：服务端算好的权限标记，前端不用自己判断
- 列表接口（`GET /projects`）**不返回** `commands` / `envs`，减少体积

### 4.2 Command 对象
```json
{
  "id": 10, "projectId": 1, "name": "拉取代码", "script": "git pull",
  "sortOrder": 1, "enabled": true,
  "failStrategy": "abort",
  "workdirOverride": "", "timeoutSec": 0, "shellOverride": "",
  "createdAt": "...", "updatedAt": "..."
}
```
`failStrategy`: `abort` | `continue`。

### 4.3 EnvVar 对象
```json
{ "id": 5, "key": "MODE", "value": "prod", "isSecret": false }
```
`isSecret=true` 时 `value` 一律返回 `""`（前端显示占位符 `******`，提交时若仍为 `******` 则保持原值）。

### 4.4 项目接口

| 方法 | 路径 | 请求 | 说明 |
|---|---|---|---|
| GET | `/projects?page&pageSize&keyword&archived&mine` | — | 分页；`archived` 不传=未归档 |
| POST | `/projects` | `ProjectInput` | 创建，key 唯一 |
| GET | `/projects/{key}` | — | 详情 |
| PUT | `/projects/{key}` | `ProjectInput` | 更新（owner/admin） |
| DELETE | `/projects/{key}` | — | 删除（连带 runs 与日志文件，owner/admin） |
| POST | `/projects/{key}/archive` | `{archived:true}` | 归档 / 取消归档 |
| GET | `/projects/{key}/commands` | — | `[Command]` |
| POST | `/projects/{key}/commands` | `CommandInput` | 新增 |
| PUT | `/commands/{id}` | `CommandInput` | 修改 |
| DELETE | `/commands/{id}` | — | 删除 |
| POST | `/projects/{key}/commands/reorder` | `{ids:[3,1,2]}` | 按数组顺序重排 |
| PUT | `/projects/{key}/envs` | `{envs:[EnvVarInput]}` | **整体替换** |
| POST | `/projects/{key}/preview` | `{branch?, env?}` | 返回将要执行的脚本与环境变量（脱敏），不执行 |

`ProjectInput`：
```json
{ "key":"my-app", "name":"我的应用", "description":"", "workdir":"/srv/workspaces/my-app",
  "shell":"auto", "timeoutSec":0, "concurrency":"queue", "hookEnabled":true,
  "archived":false, "useGit":false, "gitUrl":"", "gitBranch":"main",
  "gitCredentialId":null, "gitSubdir":"", "gitDepth":0, "gitClean":false,
  "gitSubmodule":false, "allowEnvOverride":true,
  "commands":[CommandInput], "envs":[EnvVarInput] }
```
> `POST /projects` 与 `PUT /projects/{key}` **都接受 `commands` 与 `envs` 内联**：前端一个表单一次提交完（有 `id` 的就是更新，没有的就是新建，缺失的删除）。这样前端不用编排多次请求。

`GET /projects/{key}/preview` 响应：
```json
{ "workdir": "/srv/workspaces/my-app",
  "git": { "enabled": true, "url": "...", "branch": "main", "subdir": "" },
  "envs": [ { "key":"MODE", "value":"prod", "isSecret": false } ],
  "builtinEnvs": [ { "key":"BUILD_NUMBER", "value":"13" }, { "key":"PROJECT_KEY", "value":"my-app" } ],
  "script": "# 1. 拉取代码\ncd /srv/workspaces/my-app\ngit pull\n\n# 2. 构建\n..." }
```

---

## 5. 凭据（Git）

| 方法 | 路径 | 请求 |
|---|---|---|
| GET | `/credentials` | `[Credential]` |
| POST | `/credentials` | `{name,type,username,secret,passphrase?}` |
| PUT | `/credentials/{id}` | 同上（`secret` 为空表示不改） |
| DELETE | `/credentials/{id}` | 被项目引用时 `40900` |
| POST | `/credentials/test-git` | `{gitUrl, credentialId?, branch?}` → `{ok, message, sampleRefs?}` |

```json
{ "id": 1, "name": "gitee-deploy", "type": "https_token", "username": "ppnt",
  "createdAt": "..." }
```
`type`: `https_token` | `ssh_key`。**响应永不含 `secret`**。

---

## 6. Hook Token 与触发

### 6.1 Hook 管理（登录态，owner/admin）
| 方法 | 路径 | 请求 |
|---|---|---|
| GET | `/projects/{key}/hooks` | `[HookToken]` |
| POST | `/projects/{key}/hooks` | `{name, expiresInDays?}` → 含明文 `token` 一次 |
| DELETE | `/hooks/{id}` | — |
| POST | `/hook-tokens/{id}/reset` | 重新生成 → 含明文 `token` 一次 |

> **实现注意**：重置接口不能写成 `POST /hooks/{id}/reset`。
> 公开触发是 `POST /hooks/{token}`，而 gin 的路由器不允许同一层级出现两个
> 不同名的通配符（`:token` 与 `:id`），注册时会直接 panic。
> 因此重置放在独立的 `/hook-tokens/:id/reset` 前缀下。

```json
{ "id": 2, "projectId": 1, "name": "gitee-push", "prefix": "hk_9f8e7d",
  "enabled": true, "expiresAt": null, "lastUsedAt": null, "lastUsedIp": "",
  "callCount": 0, "createdAt": "..." }
```
明文格式：`hk_` + 32 位随机 base62。

### 6.2 触发（**免登录**）
```
# 方式一：令牌放路径
POST /api/v1/hooks/{token}

# 方式二：令牌放请求头（路径里不带令牌）
POST /api/v1/hooks
Header: X-Deploy-Token: <token>

# 两种方式都可选带 Body
Body (可省略):
{
  "branch": "main",
  "env": { "TAG": "v1.2.3" },
  "commandIds": [10, 11],
  "async": true,
  "idempotencyKey": "optional"
}
```

> **两种方式的关系**：路径优先。若路径里带了令牌，请求头会被忽略
> （所以「路径写错 + 头部正确」仍然是 401，避免两处凭据打架）。
> 头部方式的好处是**令牌不出现在 URL 里**，也就不会落进 nginx 的 `access_log`。
>
> 实现提醒：请求头方式必须有一条**不带路径参数**的路由（`POST /hooks`）。
> handler 里虽然有「路径没有 token 就读请求头」的退回逻辑，但如果只注册 `/hooks/:token`，
> 那么 `/hooks/` 会 404、而 `/hooks/xxx` 的路径参数永远非空 —— 退回逻辑就成了永远走不到的死代码。
> 这个坑实际踩过：契约写了支持请求头，实测仅请求头返回 404、路径错+头对返回 401。

异步（默认，`?wait=true` 或 `"async": false` 走同步）→ **HTTP 202**：
```json
{ "success": true, "data": {
  "runId": 88, "projectKey": "my-app", "buildNumber": 13,
  "status": "queued", "async": true,
  "statusUrl": "/api/v1/runs/88", "logUrl": "/api/v1/runs/88/log" } }
```

同步（`"async": false`）→ HTTP 200，等运行结束后返回：
```json
{ "success": true, "data": {
  "runId": 88, "projectKey": "my-app", "buildNumber": 13,
  "status": "success", "exitCode": 0, "durationMs": 8123, "async": false } }
```
Token 错 → `40100`；项目归档 → `40300`；超限 → `42900`。

---

## 7. 运行

### 7.1 Run 对象
```json
{
  "id": 88, "projectId": 1, "projectKey": "my-app", "projectName": "我的应用",
  "buildNumber": 13,
  "status": "success",
  "triggerType": "web",
  "triggerUserId": 1, "triggerUserName": "admin",
  "triggerIp": "192.168.31.5", "triggerUa": "curl/8.0",
  "requestedRef": "main",
  "params": { "TAG": "v1.2.3" },
  "prepareStatus": "success",
  "prepareDurationMs": 1200,
  "gitCommit": "1aa5a86...", "gitCommitShort": "1aa5a86", "gitBranchResolved": "main",
  "startedAt": "...", "finishedAt": "...", "durationMs": 8123,
  "exitCode": 0, "errorMsg": "",
  "logSize": 20481,
  "steps": [RunStep]
}
```
`status`: `queued` | `running` | `success` | `failed` | `canceled` | `timeout` | `error`
`triggerType`: `web` | `api` | `cli` | `hook` | `rerun`
`prepareStatus`: `skipped` | `running` | `success` | `failed`

### 7.2 RunStep 对象
```json
{ "id": 900, "runId": 88, "phase": "command", "commandId": 10,
  "name": "拉取代码", "sortOrder": 1, "status": "success",
  "startedAt": "...", "finishedAt": "...", "durationMs": 900, "exitCode": 0,
  "logOffsetStart": 0, "logOffsetEnd": 2048 }
```
`phase`: `prepare` | `command`

### 7.3 运行接口

| 方法 | 路径 | 请求 | 说明 |
|---|---|---|---|
| POST | `/projects/{key}/run` | `{branch?,env?,commandIds?,commandId?}` | 触发，返回 `{runId,buildNumber,status,logUrl}` |
| GET | `/runs?page&pageSize&projectKey&status` | — | 分页列表（不含 steps） |
| GET | `/runs/{id}` | — | 详情（含 steps） |
| GET | `/runs/{id}/log?offset=0&limit=65536` | — | `{offset,nextOffset,eof,data}`，`data` 为字符串 |
| GET | `/runs/{id}/log/download` | — | `text/plain` 附件 |
| POST | `/runs/{id}/cancel` | — | 取消（幂等） |
| POST | `/runs/{id}/rerun` | — | 用原参数再跑一次 → 同 `/run` 响应 |

### 7.4 WebSocket 实时日志

```
GET /api/v1/ws/runs/{id}?token=<jwt>&offset=<n>
```
- 浏览器 WebSocket **不能自定义 Header**，所以 token 走 query。
- 连接后：先把文件里 `offset` 之后的已有内容补发（`history` 帧），再持续推增量。

帧格式（服务端 → 客户端，JSON 文本）：
```json
{"type":"history","runId":88,"data":"已有日志","nextOffset":2048}
{"type":"log","runId":88,"seq":45,"data":"新日志块","nextOffset":4096}
{"type":"status","runId":88,"status":"running","step":{"name":"构建","sortOrder":2}}
{"type":"exit","runId":88,"status":"success","exitCode":0,"durationMs":8123}
{"type":"error","runId":88,"message":"..."}
```
`data` 是**原始字节片段**（可能不含完整行），前端自行按 `\n` 缓冲切分。日志保留 ANSI 转义序列。
运行已结束时，服务端补完历史后立即发 `exit` 帧并关闭连接。

---

## 8. 系统

### `GET /api/v1/system/doctor` （登录）
```json
{ "os":"linux","arch":"amd64","hostname":"MiWiFi-R4A-srv",
  "goVersion":"go1.24.2","serverVersion":"0.1.0","uptimeSec":123,
  "webRoot":"/srv/deploy/workspaces","webRootWritable":true,
  "dataDir":"/srv/deploy/data","dataDirWritable":true,
  "shell":{"selected":"bash","path":"/usr/bin/bash","available":["bash","sh"]},
  "checks":[
    {"name":"git","found":true,"version":"git version 2.39.5","path":"/usr/bin/git",
     "hint":"Git 准备阶段与 git 命令需要"},
    {"name":"docker","found":true,"version":"Docker version 29.8.1","path":"","hint":""},
    {"name":"nginx","found":false,"version":"","path":"","hint":"若要托管静态站点，请自行安装：apt install -y nginx"}
  ] }
```
> `checks` 是「自检」而非「安装」——对应需求 D6：平台不装工具链，只报告缺失并给安装提示。

### `GET /api/v1/system/settings` / `PUT /api/v1/system/settings` （PUT 仅 admin）
```json
{ "webRoot":"/srv/deploy/workspaces", "maxConcurrentRuns":5,
  "logRetentionDays":30, "logMaxBytes":104857600,
  "hookRateLimitPerMinute":60, "visibility":"allReadable",
  "allowRegistration":false, "notify":{ "enabled":false, "url":"", "on":["failed"] } }
```
`visibility`: `allReadable`（其他登录用户只读可见）| `ownerOnly`。

### `GET /api/v1/healthz` （公开）
```json
{ "success": true, "data": { "status": "ok", "version": "0.1.0", "time": "..." } }
```

---

## 9. 内置变量（执行时注入为环境变量）

| 变量 | 说明 |
|---|---|
| `PROJECT_KEY` / `PROJECT_NAME` | 项目 key / 名称 |
| `WORKDIR` | 实际工作目录（含 `gitSubdir` 后的路径） |
| `RUN_ID` / `BUILD_NUMBER` | 运行 ID / 项目内自增号 |
| `TRIGGER_BY` / `TRIGGER_TYPE` | 触发人用户名（hook 时为 `hook:<name>`）/ 触发方式 |
| `START_TIME` | RFC3339 |
| `DEPLOY_SERVER_URL` | 服务端外部地址 |
| `GIT_BRANCH` / `GIT_COMMIT` / `GIT_COMMIT_SHORT` / `GIT_TAG` / `GIT_AUTHOR` / `GIT_MESSAGE` | 仅 `useGit=true` 时注入 |
| `DEPLOY_ENV_*` | 项目环境变量（原名注入） |

优先级（后者覆盖前者）：系统环境 → 项目 env → Secret env → 内置变量 → 触发时 `env`（仅当 `allowEnvOverride=true`）。

---

## 10. CLI 需要的最小接口集

`deploy-cli` 只依赖这些，实现顺序也照这个来：
`/auth/login`、`/auth/me`、`/projects`(GET/POST)、`/projects/{key}`(GET)、`/projects/{key}/run`、
`/runs/{id}`、`/runs/{id}/log`、`/runs/{id}/cancel`、`/runs`(GET)、`/system/doctor`、`/healthz`、WS `/ws/runs/{id}`。
