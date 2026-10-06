# 案例：用 deploy-cli 部署 tio-boot-web-hello 到 192.168.31.97

> 一份**真实执行过**的部署记录。部署对象：`tio-boot-web-hello`
> （tio-boot 示例应用）。操作方式：`deploy-cli`
> （`deploy.exe`）驱动部署平台 deploy-server
> （`http://192.168.31.97:10055`）。

---

## 0. 结论与证据

| 项 | 值 |
| --- | --- |
| 源码 | `https://gitee.com/ppnt/tio-boot-web-hello.git` @ `main` |
| 提交 | `a80247d` `update to latest version` |
| 平台项目 | `tio-boot-web-hello`（id=21，命令 6 条，环境变量 `APP_PORT=10062`） |
| 部署运行 | 运行 ID **#106**（构建号 #2） `status=success` `exitCode=0` 耗时 11.1s |
| 发布目录 | `/srv/apps/tio-boot-web-hello`（`app.jar` 4.8M、`app.properties`、`my.txt`） |
| 守护进程 | systemd `tio-boot-web-hello.service`（`active (running)`，主 PID 32881） |
| 监听端口 | `10062` |
| 外部验证 | 开发机 `curl http://192.168.31.97:10062/` → **HTTP 200** + `{"code":1,...,"ok":true,...}` |

---

## 1. 目标环境

| 项 | 值 |
| --- | --- |
| 服务器 | `192.168.31.97`（hostname `MiWiFi-R4A-srv`，Debian/Ubuntu，Linux 6.1 amd64，8 核 / 15G） |
| 部署平台 | deploy-server **0.6.5**，端口 `10055`，工作区根 `/srv/deploy/workspaces` |
| 客户端 | deploy-cli（`deploy version dev`，windows/amd64） |
| 工具链 | JDK `/usr/java/jdk1.8.0_411`、Maven `3.8.8`（`/usr/maven/apache-maven-3.8.8/bin/mvn`）、git、curl |
| 已占用端口 | 22 25 80 631 2375 5432 6379 9200 9300 10055 10060 10061 —— **80 被 nginx 占用**，所以本应用不能沿用 jar 内默认的 `server.port=80` |

---

## 2. 前置：配置 deploy-cli 的服务器

```sh
# 一条 login 就够：--name 指定的服务器不存在时，登录成功后会自动建出来。
# --name 指定的就是服务器的名字（也是配置里的键），--server 用它定位。
deploy login -a http://192.168.31.97:10055 -u admin -p '<管理员密码>' --name 971

# 或者直接把平台「个人设置 → API Token」创建的 dk_ 长效令牌写进去（推荐，不会过期）
deploy server update 971 -t dk_xxxxxxxx

# 可选：把服务器名字改成平台里的 name（--name 就是改名：键与 current 一起变成 97）
deploy server update 971 --name 97
```

```sh
deploy config list
```

```
   NAME  ADDRESS                     USER   TOKEN
-  ----  --------------------------  -----  --------------
*  97    http://192.168.31.97:10055  admin  eyJh******wgog
```

```sh
deploy --server 97 whoami            # 改名后名字就是 97，--server 用它
```


```sh
deploy --server 97 whoami              # 确认连上了、token 有效；退出码 3=鉴权、4=不可达
deploy --server 97 doctor --json       # 服务器自检：java / mvn / git / 端口
```

---

## 3. 项目定义

平台里「项目 = 一个工作目录 + 一组 shell 命令」，`deploy run` 跑的就是这些命令，
所以下面 6 条脚本就是这次部署的全部内容。

| # | 命令 | 作用 | 超时 |
| --- | --- | --- | --- |
| 1 | 环境勘察（只读） | 主机 / 工具链 / 提交 / 依赖 / 端口 | 300s |
| 2 | Maven 构建生产包 | `mvn -U clean package -Pproduction` | 1800s |
| 3 | 发布产物与外部配置 | 拷 jar 到 `/srv/apps/tio-boot-web-hello`，写 `app.properties` / `my.txt` | 300s |
| 4 | 注册 systemd 服务并重启 | 写 unit、重启、确认端口真的监听 | 300s |
| 5 | 健康检查 | 轮询 `GET /` 直到 200，否则打印日志并失败 | 600s |
| 6 | 运行状态与日志（运维） | 单独执行用，不重新构建 | 300s |

项目参数：

| 字段 | 值 |
| --- | --- |
| key / name | `tio-boot-web-hello` / `tio-boot web hello（tio-boot-web-hello）` |
| 工作目录 | `/srv/deploy/workspaces/tio-boot-web-hello` |
| Git 准备 | 启用，`https://gitee.com/ppnt/tio-boot-web-hello.git` @ `main` |
| 环境变量 | `APP_PORT=10062` |
| 并发 / shell | `queue` / `auto`（即 bash） |

### 3.1 命令 1：环境勘察（只读）

```bash
#!/usr/bin/env bash
# 1. 环境勘察（只读）
# 目的：动手之前确认主机、工具链、源码提交、依赖是否已在服务器本地 Maven 仓库、目标端口是否空闲。
set -e

WORKDIR="${WORKDIR:-/srv/deploy/workspaces/tio-boot-web-hello}"
PORT="${APP_PORT:-10062}"

echo '=== 主机 ==='
hostname
uname -srm
echo "cores: $(nproc)"
free -m | head -2

echo
echo '=== 工具链 ==='
for t in java mvn git curl; do
  printf '  %-6s %s\n' "$t" "$(command -v "$t" 2>/dev/null || echo 未安装)"
done
echo "  JAVA_HOME=${JAVA_HOME:-<未设置>}"
java -version 2>&1 | head -2 || true
mvn -v 2>&1 | head -1 || true

echo
echo '=== 源码与提交 ==='
cd "$WORKDIR"
echo "工作目录 : $WORKDIR"
echo "提交     : $(git rev-parse --short HEAD) $(git log -1 --pretty=%s)"
grep -nE '<tio\.boot\.version>|<artifactId>jfinal-aop</artifactId>' pom.xml || true

echo
echo '=== 服务器本地 Maven 仓库 ==='
for a in nexus/io/tio-boot nexus/io/jfinal-aop com/litongjava/hotswap-classloader; do
  printf '  %-34s %s\n' "$a" "$(ls -1 "/root/.m2/repository/$a" 2>/dev/null | tr '\n' ' ' || echo 无)"
done

echo
echo '=== 目标端口 ==='
if ss -ltn | grep -qE ":${PORT}\b"; then
  echo "  警告：$PORT 已被占用"
  ss -ltnp 2>/dev/null | grep -E ":${PORT}\b" || true
else
  echo "  $PORT 空闲，可用于本次部署"
fi
echo -n ' 当前所有监听端口: '
ss -ltn | tail -n +2 | awk '{print $4}' | sed 's/.*://' | sort -n -u | tr '\n' ' '
echo
```

### 3.2 命令 2：Maven 构建生产包

```bash
#!/usr/bin/env bash
# 2. Maven 构建生产包
# -Pproduction 触发 spring-boot-maven-plugin 的 repackage，产出可执行 fat jar。
set -e

export JAVA_HOME=/usr/java/jdk1.8.0_411
export PATH="$JAVA_HOME/bin:$PATH"

WORKDIR="${WORKDIR:-/srv/deploy/workspaces/tio-boot-web-hello}"
cd "$WORKDIR"

echo "工作目录 : $WORKDIR"
echo "JDK      : $(java -version 2>&1 | head -1)"
echo "Maven    : $(mvn -v 2>&1 | head -1)"
echo "提交     : $(git rev-parse --short HEAD) $(git log -1 --pretty=%s)"

# -U：强制向远程仓库核对 release 更新。
# 阿里云镜像对新发布的 nexus.io:tio-boot:2.1.7 是「先有 pom、后有 jar」的渐进缓存，
# 不加 -U 时 Maven 可能只看到本地旧状态；-U 会重新拉取，避免用到半截缓存。
mvn -U -B clean package -DskipTests -Pproduction -Dgpg.skip=true -Dmaven.javadoc.skip=true

echo '=== 构建产物 ==='
ls -lh target/tio-boot-web-hello-*.jar
```

### 3.3 命令 3：发布产物与外部配置

```bash
#!/usr/bin/env bash
# 3. 发布产物与外部配置
# 把 jar 复制到 /srv/apps/tio-boot-web-hello，并写外部端口配置。
#
# app.properties 与 my.txt 都写一份：不同版本的 tio-boot 读取的外部配置文件名不一致，
# 两个都放可以避免「配置没生效、进程偷偷监听默认的 80 端口」这种最难查的情况。
# 进程的工作目录（systemd WorkingDirectory）必须与发布目录一致，外部配置才生效。
set -e

WORKDIR="${WORKDIR:-/srv/deploy/workspaces/tio-boot-web-hello}"
APP_DIR=/srv/apps/tio-boot-web-hello
PORT="${APP_PORT:-10062}"

JAR=$(ls -t "$WORKDIR"/target/tio-boot-web-hello-*.jar 2>/dev/null | grep -vE -- '-(sources|javadoc)\.jar$' | head -1)
if [ -z "$JAR" ]; then
  echo '未找到构建产物，请先执行「Maven 构建生产包」'
  exit 1
fi

mkdir -p "$APP_DIR/logs"
cp -f "$JAR" "$APP_DIR/app.jar"
printf 'server.port=%s\n' "$PORT" > "$APP_DIR/app.properties"
printf 'server.port=%s\n' "$PORT" > "$APP_DIR/my.txt"

echo "来源 jar : $JAR"
echo "发布 jar : $APP_DIR/app.jar ($(du -h "$APP_DIR/app.jar" | cut -f1))"
echo "监听端口 : $PORT"
ls -la "$APP_DIR"
```

### 3.4 命令 4：注册 systemd 服务并重启

```bash
#!/usr/bin/env bash
# 4. 注册 systemd 服务并重启
# 端口被「别的进程」占用时直接失败，避免健康检查打到的其实是别人。
set -e

APP_DIR=/srv/apps/tio-boot-web-hello
PORT="${APP_PORT:-10062}"

if ss -ltn | grep -qE ":${PORT}\b" && ! systemctl is-active --quiet tio-boot-web-hello; then
  echo "端口 $PORT 已被其它进程占用："
  ss -ltnp 2>/dev/null | grep -E ":${PORT}\b" || true
  exit 1
fi

cat > /etc/systemd/system/tio-boot-web-hello.service <<'UNIT'
[Unit]
Description=tio-boot web hello (tio-boot-web-hello)
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/srv/apps/tio-boot-web-hello
ExecStart=/usr/java/jdk1.8.0_411/bin/java -jar /srv/apps/tio-boot-web-hello/app.jar
Restart=always
RestartSec=5
Environment=TZ=Asia/Shanghai
Environment=LANG=C.UTF-8
StandardOutput=append:/srv/apps/tio-boot-web-hello/logs/startup.log
StandardError=append:/srv/apps/tio-boot-web-hello/logs/startup-error.log

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable tio-boot-web-hello >/dev/null 2>&1 || true

# 清空上一次的日志，方便定位本次问题；systemd 的 append: 用 O_APPEND，截断是安全的。
: > "$APP_DIR/logs/startup.log"
: > "$APP_DIR/logs/startup-error.log"
rm -f "$APP_DIR"/logs/log.*.log

systemctl restart tio-boot-web-hello
sleep 4
echo "服务状态 : $(systemctl is-active tio-boot-web-hello)"

if ! ss -ltn | grep -qE ":${PORT}\b"; then
  echo "端口 $PORT 没有监听起来，当前 java 监听的端口："
  ss -ltnp 2>/dev/null | grep java || true
  echo '--- startup.log 尾部 ---'
  tail -40 "$APP_DIR/logs/startup.log" 2>/dev/null || true
  echo '--- startup-error.log 尾部 ---'
  tail -40 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
  exit 1
fi

echo '端口已监听：'
ss -ltnp 2>/dev/null | grep -E ":${PORT}\b" || true
```

### 3.5 命令 5：健康检查

```bash
#!/usr/bin/env bash
# 5. 健康检查（决定本次部署成败）
# 判据是「应用真的应答了」，不是「systemctl 说 active」。
set -e

PORT="${APP_PORT:-10062}"
APP_DIR=/srv/apps/tio-boot-web-hello
BASE="http://127.0.0.1:$PORT"

echo '=== 等待端口起来 ==='
CODE=000
UP=0
for _ in $(seq 1 30); do
  CODE=$(curl -s -o /tmp/tbwh-body -w '%{http_code}' "$BASE/" || true)
  if [ "$CODE" = "200" ]; then UP=1; break; fi
  sleep 2
done

echo "GET / -> HTTP $CODE"
echo -n '响应体: '
cat /tmp/tbwh-body 2>/dev/null || true
echo
echo '=== 响应头 ==='
curl -s -D - -o /dev/null "$BASE/" | head -8 || true

echo '=== 启动日志（前 30 行）==='
head -30 "$APP_DIR/logs/startup.log" 2>/dev/null || true

if [ "$UP" != "1" ]; then
  echo '健康检查失败'
  echo '--- startup.log 尾部 ---'; tail -60 "$APP_DIR/logs/startup.log" 2>/dev/null || true
  echo '--- startup-error.log 尾部 ---'; tail -40 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
  exit 1
fi
echo '健康检查通过'
```

### 3.6 命令 6：运行状态与日志（运维）

```bash
#!/usr/bin/env bash
# 6. 运行状态与日志（运维，用「单独执行」跑，不重新构建）
set -e

PORT="${APP_PORT:-10062}"
APP_DIR=/srv/apps/tio-boot-web-hello

echo '=== systemd 状态 ==='
systemctl status tio-boot-web-hello --no-pager -l | head -12
echo '=== 监听端口 ==='
ss -ltnp 2>/dev/null | grep -E ":${PORT}\b" || echo "$PORT 未监听"
echo '=== 发布目录 ==='
ls -lh "$APP_DIR"
echo '=== 外部配置 ==='
cat "$APP_DIR/app.properties" "$APP_DIR/my.txt"
echo '=== startup.log 尾部 ==='
tail -20 "$APP_DIR/logs/startup.log" 2>/dev/null || true
echo '=== startup-error.log 尾部 ==='
tail -10 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
```

### 3.7 在平台里创建这个项目

最省事的办法是打开 `http://192.168.31.97:10055` → 项目 → 新建项目，按上表填，
并把 6 段脚本逐条粘进「命令」。用 CLI 也能建，但**命令行传多行脚本有个 Windows 陷阱**：

- 在 Windows PowerShell 里把多行脚本作为参数传给原生 exe，PowerShell 的引号 / 换行处理会把脚本
  切碎，cobra 会把脚本里的 `mvn -v` 当成 CLI 的 `-v` 参数，报
  `unknown shorthand flag: 'v' in -v`（退出码 2）。
- 可靠做法一：先把 §3 的 6 段脚本分别存成 `01-recon.sh` … `06-ops.sh`，再在 **Git Bash / WSL** 里创建
  （bash 能正确传多行参数）：
创建project.json
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
    { "name": "1. 环境勘察（只读）",        "scriptFile": "sh/01-recon.sh" },
    { "name": "2. Maven 构建生产包",       "scriptFile": "sh/02-build.sh" },
    { "name": "3. 发布产物与外部配置",      "scriptFile": "sh/03-publish.sh" },
    { "name": "4. 注册 systemd 服务并重启", "scriptFile": "sh/04-systemd.sh" },
    { "name": "5. 健康检查",              "scriptFile": "sh/05-healthcheck.sh" },
    { "name": "6. 运行状态与日志（运维）",  "scriptFile": "sh/06-ops.sh" }
  ]
}
```
创建项目
```bash
deploy project create --file project.json
```
---

## 4. 执行部署

```sh
# 一次调用拿终态：可判定成败、带退出码
deploy --server 97 run tio-boot-web-hello --wait --json

# stdout（成功）
# {"buildNumber":2,"durationMs":11123,"exitCode":0,"runId":106,"status":"success"}
```

长部署也可以先触发再跟日志：

```sh
deploy --server 97 run tio-boot-web-hello --json      # 拿 runId
deploy --server 97 logs <runId> -f                    # 实时跟随（Ctrl+C 只断开跟随）
deploy --server 97 logs <runId>                       # 一次性拉全部历史
deploy --server 97 runs list --project tio-boot-web-hello --json
```

> 只看 `run` 不带 `--wait` / `-f` 时拿到的是 `queued`，**不能**据此判断成功。

---

## 5. 验证（都不是「平台说成功」）

```sh
# 1) 开发机直接打服务器端口 —— 最硬的一条证据
curl.exe -sS -i http://192.168.31.97:10062/
#   HTTP/1.1 200 OK
#   server:t-io
#   content-type:application/json;charset=utf-8
#   {"code":1,"msg":null,"ok":true,"error":null,"data":{}}

# 2) 服务器侧进程 / 端口 / 配置（平台上「单独执行」第 6 条命令）
deploy --server 97 run tio-boot-web-hello --command-id 82 --wait   # 82 = 第 6 条「运维」命令
```

期望看到：`systemd active (running)`、`*:10062` 监听、发布目录里 `app.jar` + 两个配置文件、
启动日志里 `Server port: 10062`。

---

## 6. 踩到的坑（含修法）

| # | 现象 | 根因 | 修法 |
| --- | --- | --- | --- |
| 1 | Maven 报 `nexus.io:tio-boot:2.1.7` 找不到 | 阿里云镜像对新发布的构件是**渐进缓存**：先有 pom、后有 jar（实测同一时刻 pom 200、jar 404，稍后 jar 也 200） | 构建命令加 **`-U`** 强制向远程核对更新；服务器本地仓库若已有该版本则可直接命中 |
| 2 | 应用起来却监听 80 而不是 10062 | jar 内 `src/main/resources/app.properties` 写死 `server.port=80`，且 80 被 nginx 占用 | 在**发布目录**写外部 `app.properties` 和 `my.txt`（tio-boot 按进程工作目录读配置），并让 systemd 的 `WorkingDirectory` 等于发布目录 |
| 3 | 健康检查打到别的进程 | 端口已被占用时 `systemctl is-active` 还是旧状态 | 命令 4 在启动前先判断「端口被占且本服务未运行」→ 直接失败 |
| 4 | 部署「成功」但业务不通 | 平台的成功只代表脚本退出码为 0 | 健康检查以 HTTP 200 为判据，并额外在开发机 `curl` 一次 |
| 5 | PowerShell 建项目报 `unknown shorthand flag: 'v' in -v` | PowerShell 向原生 exe 传多行带引号参数时把脚本切碎 | 用 Git Bash / WSL，或走平台接口 / Web UI（见 §3.7） |
| 6 | 换了 JDK 版本就编不过 | 项目 `maven.compiler.target=1.8`，服务器 PATH 里的默认 java 不一定为 8 | 构建脚本显式 `export JAVA_HOME=/usr/java/jdk1.8.0_411` 并放进 `PATH` |

---

## 7. 重跑 / 回滚 / 清理

- **重跑**：`deploy run tio-boot-web-hello --wait`。平台不做增量缓存，每次从 gitee 重新
  `fetch + checkout`，产物一定对应 `main` 的当前提交。
- **只跑某一步**：`--command-id <id>`（例如只看运维状态，不重新构建）。
- **回滚**：把代码回退到上一个可用提交再执行（`deploy run ... --branch <ref>`）。
  `/srv/apps/tio-boot-web-hello/app.jar` 每次覆盖，外部配置由命令生成，所以「回滚 = 回滚代码版本」。
- **停服务**：`systemctl stop tio-boot-web-hello`；禁用：`systemctl disable --now tio-boot-web-hello`。
- **清理工作区**：删掉 `/srv/deploy/workspaces/tio-boot-web-hello`，下次执行会重新 clone。

---

## 8. 等价接口

```bash
BASE=http://192.168.31.97:10055/api/v1
TOKEN=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<密码>"}' | jq -r .data.token)

# 触发一次运行
curl -s -X POST $BASE/projects/tio-boot-web-hello/run -H "Authorization: $TOKEN" \
  -H 'Content-Type: application/json' -d '{"branch":"main","withDependencies":false}'
# 拉日志
curl -s "$BASE/runs/<runId>/log" -H "Authorization: $TOKEN"
```
