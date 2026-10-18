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
  "kind": "app",
  "dependencies": ["tio-boot-admin"],
  "lastRunAt": "2026-01-01T10:00:00+08:00",
  "lastRunStatus": "success",
  "runCount": 12,
  "canEdit": true,
  "commands": [Command],
  "envs": [EnvVar],
  "dependencyPlan": {
    "ok": true,
    "order": [
      { "key": "tio-boot", "name": "tio-boot", "kind": "lib", "archived": false, "order": 1 },
      { "key": "tio-boot-admin", "name": "tio-boot-admin", "kind": "lib", "archived": false, "order": 2 }
    ]
  },
  "dependents": [ { "key": "myprofessor-backend", "name": "我的后端", "kind": "app", "archived": false } ],
  "createdAt": "...",
  "updatedAt": "..."
}
```
- `shell`: `auto` | `bash` | `sh` | `pwsh` | `cmd`
- `concurrency`: `queue`（同项目串行，默认）| `allow`（并行）| `cancelPrev`
- `canEdit`：服务端算好的权限标记，前端不用自己判断
- `kind`: `app`（应用项目，默认）| `lib`（构建项目），见 §4.5
- `dependencies`：直接依赖的**项目** key 列表（声明顺序）；详情接口另给 `dependencyPlan`（递归展开后的实际构建顺序，`ok=false` 时 `error` 说明原因）与 `dependents`（谁依赖了我）
- 列表接口（`GET /projects`）**不返回** `commands` / `envs` / `dependencyPlan` / `dependents`，减少体积

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
| GET | `/projects?page&pageSize&keyword&archived&mine&kind` | — | 分页；`archived` 三态：不传/`false`=未归档、`true`=已归档、`all`=全部；`kind=lib` 只取构建项目（按类型筛选，与「能不能被依赖」无关） |
| POST | `/projects` | `ProjectInput` | 创建，key 唯一 |
| GET | `/projects/{key}` | — | 详情（含 `dependencyPlan` / `dependents`） |
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
  "kind":"app", "dependencies":["tio-boot-admin"],
  "commands":[CommandInput], "envs":[EnvVarInput] }
```
> `POST /projects` 与 `PUT /projects/{key}` **都接受 `commands` 与 `envs` 内联**：前端一个表单一次提交完（有 `id` 的就是更新，没有的就是新建，缺失的删除）。这样前端不用编排多次请求。
> `kind` 省略按 `app`；`dependencies` 省略表示不改（`PUT`），传 `[]` 表示清空。

#### `GET /projects/{key}/preview` 响应：
```json
{ "workdir": "/srv/workspaces/my-app",
  "git": { "enabled": true, "url": "...", "branch": "main", "subdir": "" },
  "envs": [ { "key":"MODE", "value":"prod", "isSecret": false } ],
  "builtinEnvs": [ { "key":"BUILD_NUMBER", "value":"13" }, { "key":"PROJECT_KEY", "value":"my-app" } ],
  "script": "# 1. 拉取代码\ncd /srv/workspaces/my-app\ngit pull\n\n# 2. 构建\n..." }
```

### 4.5 项目类型与依赖构建

多模块项目里，上层项目构建前必须先构建它依赖的底层库（例如 `myprofessor-backend`
依赖 `tio-boot-admin`，而 `tio-boot-admin` 又依赖 `tio-boot`、`java-openai`）。
平台用**项目类型 + 依赖列表**表达这件事，触发时按拓扑序自动先构建依赖。

**项目类型（`kind`）**

| 取值 | 含义 | 能否被选为依赖 |
|---|---|---|
| `app` | 应用项目：常规的构建 / 部署单元（默认） | 是 |
| `lib` | 构建项目：产出被依赖的产物（`mvn install` 之类） | 是 |

类型只影响默认命令与图标：**任何未归档的项目都可以被选为依赖**。

**依赖规则**

- 依赖可以指向任意**存在**的项目（应用项目与构建项目都可以）；不能依赖自己。
- 保存时校验**不成环**：加入这条边后若从自己出发能绕回自己，返回 `40000`。
- 依赖允许指向已归档的项目（便于临时归档），但**触发时会拒绝**并说明是哪个依赖归档了，绝不静默跳过。
- 同一层内按 `dependencies` 数组顺序；整体按深度优先展开，同一个项目**只构建一次**（菱形依赖不重复构建）。

**构建顺序示例**：`myprofessor-backend` → `tio-boot-admin` → {`tio-boot`, `java-openai`}，
实际执行顺序为 `tio-boot`、`java-openai`、`tio-boot-admin`、`myprofessor-backend`。

**触发时**（`POST /projects/{key}/run`、Hook）：

| 字段 | 说明 |
|---|---|
| `withDependencies` | `true` 时先按上面的顺序构建依赖，全部成功后 D 目标项目才执行；默认 `false`（只跑本项目） |

语义细节：

- 一次带依赖的触发会产生**多条 Run**（每个依赖项目一条 + 目标项目一条），它们共享同一个 `pipelineId`：目标是 `pipelineRole=main`，依赖是 `pipelineRole=dependency`，`pipelineOrder` 从 1 开始。
- 目标项目的 Run 在依赖构建期间保持 `queued`，**不占用**全局并发闸门；每条 Run 各自进出闸门，因此不会自锁。
- 依赖用**同一个分支 / Tag**（`branch` 透传），但**不继承**触发时传入的 `env` 覆盖（那是给目标项目的参数）。
- 任何一条依赖不是 `success`：后面的依赖与目标项目都不再执行。目标 Run 直接落成终态 —— 依赖被取消时为 `canceled`，其余为 `failed`，`errorMsg` 写明 `dependency build <key> failed (run #N, status <s>)`，并写一条日志说明「未执行」。
- 取消流水线里的任意一条 Run 会**连带取消**同一条流水线里还在跑 / 排队的其它 Run。
- 单条命令触发（`commandId` / `commandIds`）与 `withDependencies` 不能同时用，返回 `40000`。
- 服务重启时，未完成的依赖流水线与普通 Run 一样被标记为中断（`error`）。

`POST /projects/{key}/run` 带依赖时的响应在原有字段外多了：
```json
{ "runId": 88, "buildNumber": 13, "status": "queued", "projectKey": "myprofessor-backend",
  "pipelineId": "pl_xxxxxxxxxxxxxxxx",
  "dependencyCount": 3,
  "dependencies": [ { "key": "tio-boot", "runId": 85, "buildNumber": 4 } ] }
```
`GET /runs/{id}` 也会返回同一条流水线的全部 Run（`pipeline` 数组，见 §7.1），运行详情页据此展示「前置构建」。

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
  "withDependencies": false,
  "async": true,
  "idempotencyKey": "optional"
}
```
> `withDependencies: true` 时先按依赖顺序构建依赖的构建项目，全部成功后才执行本项目（见 §4.5）。
> 依赖与目标共用同一个 `branch`，但触发时传入的 `env` 只作用于目标项目。

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
  "pipelineId": "pl_xxxxxxxxxxxxxxxx",
  "pipelineRole": "main",
  "pipelineOrder": 0,
  "pipeline": [
    { "runId": 85, "projectKey": "tio-boot", "projectName": "tio-boot", "buildNumber": 4,
      "status": "success", "pipelineRole": "dependency", "pipelineOrder": 1, "durationMs": 4210 },
    { "runId": 88, "projectKey": "myprofessor-backend", "projectName": "我的后端", "buildNumber": 13,
      "status": "running", "pipelineRole": "main", "pipelineOrder": 0, "durationMs": 0 }
  ],
  "steps": [RunStep]
}
```
`status`: `queued` | `running` | `success` | `failed` | `canceled` | `timeout` | `error`
`triggerType`: `web` | `api` | `cli` | `hook` | `rerun`
`prepareStatus`: `skipped` | `running` | `success` | `failed`
`pipelineId` / `pipelineRole` / `pipelineOrder`：只有依赖构建流水线里的 Run 才有（见 §4.5）；不带依赖触发时这三个字段为空。
`pipeline`：仅在 `GET /runs/{id}` 详情里返回，列出同一条流水线的全部 Run（按 `pipelineOrder` 升序，主项目排最后）。

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
| POST | `/projects/{key}/run` | `{branch?,env?,commandIds?,commandId?,withDependencies?}` | 触发，返回 `{runId,buildNumber,status,pipelineId,dependencies[],logUrl}`；`withDependencies=true` 时先构建依赖（§4.5） |
| GET | `/runs?page&pageSize&projectKey&status` | — | 分页列表（不含 steps 与 pipeline） |
| GET | `/runs/{id}` | — | 详情（含 steps 与 pipeline） |
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
{"type":"history","runId":88,"offset":0,"data":"已有日志","nextOffset":2048}
{"type":"log","runId":88,"seq":45,"offset":2048,"data":"新日志块","nextOffset":4096}
{"type":"status","runId":88,"status":"running","step":{"name":"构建","sortOrder":2}}
{"type":"truncated","runId":88,"nextOffset":4096,"status":"running","reason":"subscriber too slow"}
{"type":"exit","runId":88,"status":"success","exitCode":0,"durationMs":8123}
{"type":"error","runId":88,"message":"..."}
```
`data` 是**原始字节片段**（可能不含完整行），前端自行按 `\n` 缓冲切分。日志保留 ANSI 转义序列。

`offset` 是本帧数据在日志文件里的**起始字节偏移**（`nextOffset` 是结束偏移）。
客户端去重必须用它，不要用 `nextOffset - data.length` 去猜：JS 的 `string.length` 是 UTF-16 码元数、
日志偏移是字节数，日志里有中文/emoji 时推算出来的位置是错的（会把该显示的内容裁掉）。
服务端保证每帧都落在字符边界上（不会把多字节字符切成两半）。

**帧语义（客户端必须按这个状态机走，否则会丢日志）：**
- `history` / `log`：增量日志，按 `offset` 去重后落屏。
- `truncated`：**本连接**被服务端截断（订阅者读得太慢，服务端主动踢掉它，避免拖住执行器）。
  运行**没有**结束，客户端应带着 `nextOffset` 立即重连补齐。
- `exit`：运行已处于终态（`status` 是 `success|failed|canceled|timeout|error`），服务端在发它之前
  已经把日志文件补完，客户端收到即可收工。**`status` 不是终态的 `exit` 视为 `truncated`**
  （老版本服务端会用它通知「订阅被踢」），绝不能就此停止。
- 连接随时可能被服务端关闭（慢订阅者、写超时、代理超时）：客户端一律按 `nextOffset` 重连补齐。

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
> `checks` 是「自检」而非「安装」：平台默认不主动装工具链，只报告缺失并给安装提示；
> 需要自动装时用下面的「工具链一键安装」。
>
> **探测口径 = 执行口径**：Unix 下项目脚本用 `bash -lc` 跑，登录 shell 会读 `/etc/profile`
> 与 `/etc/profile.d/*.sh`；`checks` 与「工具链」面板在同一套环境里解析命令（`PATH` 取自
> 登录 shell）。所以「命令行里 `mvn -v` 能跑、自检却说没装」不会再出现。
> 反过来，只在 `~/.bashrc`（只有交互式 shell 读）里 `export PATH` 的工具，自检和构建都
> 找不到：把 PATH 写进 `/etc/profile.d/xxx.sh`，或把可执行文件软链到 `/usr/local/bin`。
> 项目/全局把 shell 设成 `sh` 时执行的是 `sh -c`（不读 profile），此时自检也按服务进程
> 自己的环境探测。

### `GET /api/v1/system/toolchains` （登录）
列出可一键安装的工具链组件与各自当前状态（安装动作仅 admin，见下）。
```json
{ "supported": true, "platform": "linux/amd64",
  "missing": ["go","node","pnpm"],
  "scriptPath": "/srv/deploy/data/toolchain/install.sh",
  "scriptUrl": "/api/v1/system/toolchains/script",
  "projectKey": "system-toolchain",
  "hint": "安装以普通 Run 的形式执行，可以在运行详情页看实时日志、取消或重跑",
  "installCmd": "bash /srv/deploy/data/toolchain/install.sh --only <组件>",
  "components": [
    { "key":"cc", "name":"C 工具链（gcc / g++ / make）", "note":"编译原生扩展、cargo 链接、make 构建都要它",
      "estimate":"约 200 MB", "installed":true, "version":"13", "path":"/usr/bin/gcc",
      "probes":[ {"name":"gcc","found":true,"version":"13","path":"/usr/bin/gcc"} ],
      "fields":[
        { "key":"CC_EXTRA_PACKAGES", "label":"额外软件包（可选）", "default":"",
          "placeholder":"cmake nasm libssl-dev", "help":"空格分隔，交给 apt/dnf 一起装", "value":"" }
      ] },
    { "key":"go", "name":"Go", "note":"装到 /usr/local/go 并软链到 /usr/local/bin；GOPROXY 指向 goproxy.cn",
      "estimate":"约 120 MB", "installed":false, "version":"", "path":"", "probes":[],
      "fields":[
        { "key":"GO_VERSION", "label":"Go 版本", "default":"1.24.2", "placeholder":"1.24.2", "help":"", "value":"1.24.1" },
        { "key":"GO_URL", "label":"Go 压缩包地址（可选）", "default":"",
          "placeholder":"https://.../go1.24.2.linux-amd64.tar.gz", "help":"留空自动依次试镜像", "value":"" },
        { "key":"GO_DIR", "label":"Go 安装目录", "default":"/usr/local/go", "placeholder":"/usr/local/go", "help":"", "value":"" }
      ] }
  ] }
```
`supported=false`（非 Linux 服务端）时只有 dry-run 可用。

`fields[].key` 就是安装脚本读的**环境变量名**（`JAVA_VERSION` / `JDK_URL` / `JAVA_DIR` / `GO_URL` / `NODE_VERSION` / `CERTBOT_NGINX_PLUGIN`…），
`value` 是保存过的覆盖值，空串表示用 `default`。脚本按 `${VAR:-默认值}` 取值，所以「编辑参数」不需要额外的配置格式。

### `PUT /api/v1/system/toolchains/{key}` （**仅 admin**）
保存某个组件的安装参数（版本 / 下载地址 / 安装目录…）。空串表示回默认；未知字段名或未知组件返回 `40000`。
```json
// 请求：装 JDK 8 —— 不走 apt，直接下载压缩包解压到指定目录
{ "fields": { "JAVA_VERSION": "8",
              "JDK_URL": "https://example.com/jdk-8u202-linux-x64.tar.gz",
              "JAVA_DIR": "/usr/local/jdk8" } }
```
```json
// 响应
{ "key":"java", "components":["JAVA_DIR","JAVA_VERSION","JDK_URL"],
  "fields":[ { "key":"JAVA_VERSION", "label":"JDK 大版本（apt 安装）", "value":"8",
               "default":"17", "placeholder":"17", "help":"…" } ] }
```
保存后点该组件的「安装 / 重装」即按新参数执行；这些参数会写进那次运行的 `params`
（在「安装记录」里能回看「这次按什么参数装的」）。

### `POST /api/v1/system/toolchains/install` （**仅 admin**）
触发一次安装。**安装以普通 Run 执行**：立即返回 `runId`，实时日志走
`GET /api/v1/runs/{id}/log` 与 `WS /api/v1/ws/runs/{id}`，取消用 `POST /runs/{id}/cancel`。
```json
// 请求
{ "components": ["go","pnpm"],   // 省略/空数组 = 所有缺失的组件
  "dryRun": false,               // true = 预演：只打印将执行的动作，不做任何修改
  "force": false,                // true = 已安装的也重装
  "mirror": "off" }              // 网页默认官方源；"cn" 使用国内镜像；旧调用省略时仍默认 cn

```
安装 / 重装 / 预演弹窗统一选择本次安装源。服务端校验 cn/off，并在运行参数 MIRROR 中记录选择。官方源用于 Go、Rust、Node/npm/pnpm 下载与依赖配置；国内模式配置国内镜像及 Maven 阿里云镜像。已有 Maven settings.xml 和 Cargo 配置保留并提示，可能仍使用原镜像。自定义下载地址优先；官方模式忽略已保存的 Node/Rust 镜像参数。

```json
// 响应
{ "runId": 42, "buildNumber": 1, "status": "queued",
  "projectKey": "system-toolchain",
  "components": ["go","pnpm"], "dryRun": false,
  "command": "bash '/srv/deploy/data/toolchain/install.sh' --only go,pnpm --mirror off",
  "logUrl": "/api/v1/runs/42/log" }
```
说明：

- 组件 key 走白名单（`cc java maven nvm node pnpm go rust certbot`，即内嵌脚本的 `ALL_COMPONENTS`，顺序也一致），
  未知 key 返回 `40000`（组件名会进命令行，必须挡住注入）。清单与脚本对不上会有测试拦住（`toolchain_test.go`）。
- 每个组件保存过的参数会作为**运行参数**注入本次安装（脚本按环境变量读取），并记进 `runs.params`；
  另外固定带上 `components=<本次组件列表>`，供「安装记录」展示。
- 内置项目 `system-toolchain`（名称「环境工具链安装（系统）」）会在首次使用时自动创建，
  **命令只有一条且每次触发时更新**为最新的组件选择；该项目 `hookEnabled=false`，不能被 Hook 触发。
- 全部组件都已安装时返回 `{ "runId": 0, "nothingToDo": true }`；想给已装的组件换版本，请带 `force: true` 只重装它。
- 非 Linux 服务端：真安装返回 `40000`，`dryRun` 仍可用于预览。
- 安装会调用包管理器与下载 JDK/Go/Rust/Node（几百 MB），因此限 admin；系统内并发受项目 `queue` 策略约束，不会并发装。

### `GET /api/v1/system/toolchains/script` （**仅 admin**）
以 `text/x-shellscript` 返回内嵌的安装脚本，供人工审查或拷到 Linux 机器上手动执行：
```bash
bash install-toolchains.sh                       # 装全部（已装的跳过）
bash install-toolchains.sh --only java,maven     # 只装指定组件
bash install-toolchains.sh --dry-run             # 预演
bash install-toolchains.sh --list                # 只列状态
```

### `GET /api/v1/system/middleware` （登录）

中间件（docker / postgresql / mysql / redis / elasticsearch）清单与当前状态。
前半段与 `GET /system/toolchains` 同构，区别在中间件多了**安装方式（mode）**与**运行状态**两个维度。

```json
{ "supported": true, "platform": "linux/amd64",
  "dockerAvailable": true, "dockerVersion": "Docker version 29.8.1, build 4a63305",
  "components": [
    { "key":"redis", "name":"Redis", "note":"…", "category":"cache", "estimate":"约 40 MB（镜像）",
      "installed": true, "running": true, "version":"redis:latest",
      "path":"container:deploy-mw-redis",
      "detail":"容器 deploy-mw-redis（running）  卷 deploy-mw-redis-data  端口 16379",
      "modes":["docker","native"], "defaultMode":"docker",
      "mode":"docker", "effectiveMode":"docker", "nativeSupported": true,
      "probes":[{"name":"容器 deploy-mw-redis","found":true},{"name":"redis-server","found":false}],
      "fields":[{"key":"REDIS_PORT","label":"端口","default":"6379","value":"16379","secret":false,"set":false},
                {"key":"REDIS_PASSWORD","label":"密码","default":"","value":"","secret":true,"set":true}] } ],
  "missing":["postgresql","mysql","elasticsearch"], "running":["docker","redis"],
  "actions":["install","start","stop","restart","upgrade","logs","remove"],
  "scriptPath":"/srv/deploy/data/middleware/install.sh", "scriptUrl":"/api/v1/system/middleware/script",
  "projectKey":"system-middleware", "hint":"…", "installCmd":"bash … --only <中间件> [--mode …]" }
```

字段语义（都有对应的服务端用例）：

- `modes` / `defaultMode`：该中间件可选的方式与默认方式。`docker` 只有 `native`、`elasticsearch` 只有 `docker`。
- `mode`：**保存的配置**（来自 `PUT /system/middleware/{key}`）；`effectiveMode`：**实际在跑的方式**
  （容器装了就是 `docker`，原生包装了就是 `native`）。两者可能不同 —— 例如配置成 native、但实际上有个旧容器在跑。
- `installed` / `running` 分开：**装了但没在跑**（容器 `exited`）是常见状态，要能区分出来。
- `probes[].found` 逐项独立：容器探测与原生命令探测是两条互斥的事实，
  不会「一荣俱荣」（否则会出现「容器 ✔ + psql ✔」这种自相矛盾的面板）。
- `fields[].secret=true` 的参数（`*_PASSWORD`）**永远不回明文**，`value` 恒为空串，
  改用 `set` 表示「是否已保存过」。密文以 AES-GCM 存在 `settings` 表的 `middleware.<key>` 下。
- `dockerAvailable=false` 时容器方式不可用；`supported=false` 表示服务端不是 Linux（只能 dry-run）。

### `PUT /api/v1/system/middleware/{key}` （**仅 admin**）

保存某个中间件的参数与安装方式。请求体 `{"fields":{"REDIS_PORT":"16379"},"mode":"docker"}`。

- 未知参数名或未知方式返回 `40000`（参数名会作为环境变量注入脚本、方式会进命令行，必须挡住）。
- 普通参数传空串 = 回默认（从 `values` 里删掉）。
- 密码参数三态：**传空串 = 清空**（下次安装随机生成）、**传占位符 `******` = 保持原密文**、
  **不传该键 = 不动**。这样「只改端口」不会误清密码，而「想换密码」也有明确表达方式。
- 响应回最新的 `fields`（密码仍只有 `set`）与 `mode`。

### `POST /api/v1/system/middleware/install` （**仅 admin**）

触发一次中间件操作，返回可直接跳去看日志的 Run。

```json
{ "components":["redis","postgresql"], "mode":"docker", "action":"install",
  "dryRun":false, "force":false, "mirror":"cn" }
```

| 字段 | 说明 |
|---|---|
| `components` | 中间件 key 列表（可逗号分隔）。**空数组只对 `install` 合法**，表示「所有缺失的」 |
| `mode` | `docker` / `native`，留空按各组件的默认方式 |
| `action` | `install`（默认）/ `start` / `stop` / `restart` / `remove` / `upgrade` / `logs` |
| `dryRun` | 只打印计划，不做任何修改（`install` 用） |
| `force` | 已安装的也重建（容器方式**保留数据卷**） |

响应：
```json
{ "runId": 88, "buildNumber": 7, "status": "queued", "projectKey": "system-middleware",
  "components": ["redis"], "action": "install", "dryRun": false,
  "command": "bash '/srv/deploy/data/middleware/install.sh' --only redis --action install --mode-of redis=docker",
  "logUrl": "/api/v1/runs/88/log" }
```

约束与语义：

- 未知中间件、未知动作、未知方式、`native` 但组件不支持（如 `elasticsearch`）一律 `40000`，
  **提前拦掉**而不是让使用者在日志里发现「这一项被跳过了」。
- `action != install` 时 `components` 不能为空：否则「清空勾选后点停止」会意外停掉所有中间件。
- 非 Linux 服务端：真操作返回 `40000`，`dryRun` 仍可用于预览。
- 每个组件的方式用 `--mode-of <组件>=<方式>` 逐个传给脚本（脚本只认一个全局 `--mode`，
  而各组件的方式可能不同）；这些方式同时记进 `runs.params` 的 `mode.<组件>` 便于回看。
- 参数（含**解密后的密码**）作为运行参数注入本次运行，并记进 `runs.params` —— 这是有意的：
  脚本随机生成的密码不会出现在参数里，但使用者填的必须能回看，否则「这次按什么装的」无从查起。
  **平台不对 Run 参数打码**（它们本来就是使用者自己的配置），而日志里的命令行回显会被脚本打成 `***`。
- 内置项目 `system-middleware`（名称「中间件安装（系统）」）首次使用时自动创建，
  **命令只有一条且每次触发时更新**；`hookEnabled=false`。
  与工具链的 `system-toolchain` **分开**：两者的运行历史混在一起会让「上次装数据库是什么时候」难查。
- 全部已装且无需动作时返回 `{ "runId": 0, "nothingToDo": true }`。

### `GET /api/v1/system/middleware/script` （**仅 admin**）

以 `text/x-shellscript` 返回内嵌的中间件安装脚本，供人工审查或拷到 Linux 机器上手动执行：

```bash
bash install-middleware.sh --list                        # 只看状态
bash install-middleware.sh --only docker,redis           # 装指定中间件
bash install-middleware.sh --only postgresql --mode native
bash install-middleware.sh --action restart --only redis
bash install-middleware.sh --remove --only redis         # 数据卷保留
bash install-middleware.sh --dry-run --only all
```

脚本里 `ALL_COMPONENTS` 是组件清单的**单一事实来源**（脚本按它执行、页面与 CLI 按它展示），
与 Go 侧的 `middleware.AllKeys()` 必须逐项同序一致，有测试盯着。

### `GET /api/v1/system/info` （登录）
服务器基本信息与资源占用，供顶层菜单「服务器」（`/server-info`）页面使用。

```json
{ "time":"2026-10-04T20:45:58+08:00", "hostname":"MiWiFi-R4A-srv",
  "os":"linux", "arch":"amd64", "osName":"Debian GNU/Linux 12 (bookworm)",
  "kernel":"6.1.0-13-amd64", "uptimeSec":1234567,
  "load":{"available":true,"avg1":0.42,"avg5":0.35,"avg15":0.28},
  "cpu":{"model":"Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz",
         "cores":4,"physicalCores":2,"mhz":2400,
         "available":true,"usagePercent":12.5,
         "coreUsage":[
           {"index":0,"indexLabel":"CPU0","usagePercent":3.2,"busySec":0.1,"totalSec":3.02},
           {"index":1,"indexLabel":"CPU1","usagePercent":48.7,"busySec":1.47,"totalSec":3.02}
         ],
         "sampleWindowSec":3,
         "busySec":1.5,"totalSec":12,"userSec":1.1,"systemSec":0.3,
         "idleSec":10.2,"iowaitSec":0.1},
  "memory":{"totalBytes":8363282432,"usedBytes":1642340352,
            "freeBytes":5720993792,"availableBytes":6720942080,
            "usagePercent":19.6,
            "swapTotalBytes":1073737728,"swapUsedBytes":0,"swapFreeBytes":1073737728,
            "swapKind":"swap","swapLabel":"交换分区"},
  "disks":[
    {"path":"/","device":"/dev/sda1","fstype":"ext4",
     "totalBytes":21369978880,"usedBytes":8724152320,
     "freeBytes":12645826560,"usagePercent":40.8,"status":"ok"}
  ],
  "warnings":[] }
```

数据由 `internal/metrics` 的后台采样协程每 3 秒刷新一份内存快照，这个接口只读快照，
不做任何系统调用，因此前端可以放心高频轮询（默认 5 秒）。几个**容易踩的点**，改动前先读：

| 字段 | 说明 |
|---|---|
| `cpu.usagePercent` | 是**两次采样之间的差分**，不是「开机至今的平均值」。后者在长期运行的机器上几乎恒为 30% 左右，看着像坏了。 |
| `cpu.available` | 服务刚启动、第一次差分还没形成时为 `false`，前端显示「采样中」而不是 0%。 |
| `cpu.coreUsage` | 每个逻辑核心的使用率数组，长度等于 `cores`，元素为 `{index, indexLabel, usagePercent, busySec, totalSec}`。取值方式：Linux 读 `/proc/stat` 的 `cpuN` 行（与整机行同构，所以口径天然一致），Windows 走 ntdll 的 `NtQuerySystemInformation(SystemProcessorPerformanceInformation=8)`——Win32 层没有逐核时间的公开接口。拿不到时返回空数组（前端显示「当前平台未提供逐核使用率」），不影响整机数据。注意单核是**瞬时占用**，会看到 0% 和 100% 反复跳（调度器把任务在核心间搬），属正常现象。 |
| `cpu.busySec` / `totalSec` / `userSec` / `idleSec` / `iowaitSec` | 是**所有核心累加**的时间，不是墙上时钟：16 核跑满 1 秒 → `totalSec ≈ 16`。`usagePercent = busySec / totalSec`，分子分母同口径，自动消掉核心数。 |
| `cpu.physicalCores` | Windows 上没有便宜的 API 能拿到物理核数，恒为 `0`（前端隐藏该项）。 |
| `load.available` | Windows 没有 load average，为 `false`（前端隐藏该项）。 |
| `memory.usedBytes` | Linux 上是 `MemTotal - MemAvailable`（**不是** `MemFree`）。用 `MemFree` 会把可回收的 page cache 算成占用，正常机器上直接虚报 90%+，是最常见的监控误报来源。 |
| `memory.swapKind` / `swapLabel` | `swap*` 三个字段两个平台的**口径不同**，所以标题必须照 `swapLabel` 显示，不要硬写「交换分区」：Linux 是 `swap` /「交换分区」（`/proc/meminfo`），Windows 是 `commit` /「提交内存」（`CommitTotal` vs `CommitLimit`，即「物理内存 + 页面文件」的承诺量）。Windows 的页面文件**大小**不是已用交换量，拿它当交换分区会在一台 16 GB 内存的机器上显示 42 GB 交换。 |
| `disks[].path` | Linux 是挂载点（`/`、`/home`），Windows 是卷根（`C:\`）。Linux 上只列设备名以 `/dev/` 开头的真实文件系统，`tmpfs` / `cgroup` / `overlay` 等伪文件系统、以及容器内的 overlay 根都会被过滤掉（不过滤的话页面上是一屏 0 字节的噪音）；根分区永远排第一。 |
| `disks[].freeBytes` | Linux 上是**非特权用户可用**（`statfs.Bavail`，比 `Bfree` 小——后者含 root 保留块），与 `df` 的 `Avail` 列口径一致。 |
| `disks[].status` | `ok` / `warn`；`warn` = 使用率 ≥75% 或剩余 <2GiB。阈值算在后端，是为了将来做通知/告警时口径统一，前端不必重复实现。 |
| `warnings` | 采集失败的条目说明（例如该平台不支持 CPU 采集），前端用它解释「某项为什么是空的」，而不是让用户对着 0 猜。 |
| 权限 | 任意登录用户可见（与 `/system/doctor` 一致）。**刻意不返回** `webRoot` / `dataDir` / `serverUrl` 等配置细节——那些只出现在 admin 关心的自检与系统设置里。 |

### `GET /api/v1/system/settings` / `PUT /api/v1/system/settings` （PUT 仅 admin）
```json
{ "webRoot":"/srv/deploy/workspaces", "serverUrl":"http://192.168.31.97:10055",
  "maxConcurrentRuns":5,
  "logRetentionDays":30, "logMaxBytes":104857600,
  "hookRateLimitPerMinute":60, "visibility":"allReadable",
  "allowRegistration":false, "notify":{ "enabled":false, "url":"", "on":["failed"] } }
```
`visibility`: `allReadable`（其他登录用户只读可见）| `ownerOnly`。
`serverUrl`: 服务端对外地址，注入为 `DEPLOY_SERVER_URL`；只接受绝对的 http(s) 地址，留空表示回退到启动配置（`-server-url` / `DEPLOY_SERVER_URL` / 配置文件）里的默认值。**这是运行时设置**：改完对变量预览与后续运行立即生效，不需要重启服务（它不影响本进程实际监听哪个地址）。

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
| `DEPLOY_SERVER_URL` | 服务端外部地址（系统设置里配置） |
| `GIT_BRANCH` / `GIT_COMMIT` / `GIT_COMMIT_SHORT` / `GIT_TAG` / `GIT_AUTHOR` / `GIT_MESSAGE` | 仅 `useGit=true` 时注入 |
| `DEPLOY_ENV_*` | 项目环境变量（原名注入） |

优先级（后者覆盖前者）：系统环境 → 项目 env → Secret env → 内置变量 → 触发时 `env`（仅当 `allowEnvOverride=true`）。

---

## 10. CLI 需要的最小接口集

`deploy-cli` 只依赖这些，实现顺序也照这个来：
`/auth/login`、`/auth/me`、`/projects`(GET/POST)、`/projects/{key}`(GET)、`/projects/{key}/run`、
`/runs/{id}`、`/runs/{id}/log`、`/runs/{id}/cancel`、`/runs`(GET)、`/system/doctor`、`/healthz`、WS `/ws/runs/{id}`。

中间件与任务相关（`cli` 的 `middleware` 命令组）：

`/system/middleware`(GET)、`/system/middleware/install`(POST)、`/system/middleware/{key}`(PUT)、
`/system/middleware/script`(GET)、`/tasks`(GET/POST)、`/tasks/{key}`(GET/PUT/DELETE)、`/tasks/{key}/run`(POST)。

> 服务器（键 / 名称 / 地址 / token）**不经过任何接口**：它是 CLI 的本地配置（`~/.deploy/config.yaml`），
> 服务端不知道也不需要知道。`--server` 只是在 CLIENT 侧决定用哪一条服务器配置的 `address` + `token` 去发请求。
