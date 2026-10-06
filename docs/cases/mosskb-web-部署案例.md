# 案例：用 deploy-server 部署 java-mosskb（mosskb-web）到 192.168.31.97

> 这是一份**真实执行过**的部署案例：目标服务器 `192.168.31.97`，数据库留在开发机 `192.168.31.225:5432/moss_kb`，
> 全程通过 deploy-server（<http://192.168.31.97:10055>）的网页完成：建项目 → 写命令 → 点「执行」。
> 本文记录每一步做了什么、命令为什么这么写、以及踩到的 8 个坑。
>
> 涉及仓库：`java-mosskb`（应用）、`tio-boot`、`tio-boot-admin`、`api-table`、`java-openai`（依赖）。
> 全部仓库地址见 <https://gitee.com/ppnt>。

---

## 0. 结论与证据

最终一次执行（网页「执行」= **含依赖**）的流水线 `pl_ZxMn2DY8HBPajW3T`：

| 顺序 | Run | 项目 | 类型 | 结果 | 耗时 |
| --- | --- | --- | --- | --- | --- |
| 1 | 34 | tio-boot | 构建项目 | success | 12.9s |
| 2 | 35 | tio-boot-admin | 构建项目 | success | 5.4s |
| 3 | 36 | api-table | 构建项目 | success | 3.1s |
| 4 | 37 | java-openai | 构建项目 | success | 8.0s |
| 5 | 38 | java-mosskb-business | 构建项目 | success | 5.4s |
| 6 | 39 | **mosskb-web** | 应用项目 | **success** | 16.4s |

执行顺序由依赖深度决定，网页项目详情页会直接写出：`tio-boot → tio-boot-admin → api-table → java-openai → java-mosskb-business → mosskb-web`。
首次执行（依赖要下载，tio-boot-admin 用了 136.5s）约 4 分钟，缓存热了之后整条链 **约 51 秒**。

**部署后的四项验证（都不是"平台说成功"）：**

| 验证 | 命令（在开发机执行） | 结果 |
| --- | --- | --- |
| 服务可达 | `curl --compressed http://192.168.31.97:10060/api/display/info` | `code:200`，返回 MossKB 外观 JSON |
| 走库的业务链路 | `curl -X POST http://192.168.31.97:10060/api/user/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"Kimi@2024"}'` | `code:200` + 有效 JWT |
| 真在用开发机的库 | `psql -h 127.0.0.1 -U postgres -d moss_kb -c "select client_addr,count(*) from pg_stat_activity where datname='moss_kb' group by 1"` | `192.168.31.97 \| 10`（10 条 JDBC 空闲连接） |
| 进程与文件 | 项目里第 6 条命令「查看运行状态与日志（运维）」单独执行（run 40） | `systemd active`、`*:10060` 监听、`/srv/apps/mosskb-web` 下 53M jar + `my.txt`(0600) + `secrets.txt`(0600) |

---

## 1. 场景与目标

| 项 | 值 |
| --- | --- |
| 部署对象 | `D:\code\java\project-mosskb\java-mosskb\mosskb-web`（tio-boot 后端，JDK 21，Maven 多模块） |
| 目标服务器 | `192.168.31.97`（Ubuntu，deploy-server 0.4.1，Docker/Nginx/Git 已装，**只装了 JDK 8**） |
| 数据库 | **开发机** `192.168.31.225:5432/moss_kb`（PostgreSQL 18 + pgvector + pg_trgm，31 张业务表，已有数据） |
| 部署方式 | deploy-server 项目管理 + 网页执行；产物落到 `/srv/apps/mosskb-web`，由 systemd 托管 |
| 服务端口 | `10060` |
| 不在本次范围 | 前端 `java-mosskb-ui`（独立仓库，平台上已有项目，需要 Node） |

> 关键前提：`mosskb-web` 运行目录里的 `my.txt` / `secrets.txt` 是**按进程工作目录**加载的
> （`nexus.io.tio.utils.environment.EnvUtils.load()`：`new File("my.txt")`、`new File("secrets.txt")`）。
> 所以「发布目录 + 配置 + 启动进程的工作目录」必须是同一个 `/srv/apps/mosskb-web`，否则配置不生效。

---

## 2. 开工前的三项准备（都不是平台问题，但不做就会失败）

### 2.1 确认服务器真的开机、真的在局域网

第一次探测时 `192.168.31.97` 的 ARP 是 `Incomplete`，TCP「连上」但没有任何响应（默认网关和另一台 `192.168.31.150` 都正常）。
结论：服务器当时没开机。**开机后 ARP 立刻有 MAC、`/api/v1/auth/bootstrap-status` 返回 200**。

```powershell
# 开发机上判断"服务器是否真的在线"
Get-NetNeighbor -InterfaceAlias 'Ethernet' -AddressFamily IPv4 |
  Where-Object IPAddress -eq '192.168.31.97' | Select-Object IPAddress,LinkLayerAddress,State
curl.exe -sS -m 8 http://192.168.31.97:10055/api/v1/auth/bootstrap-status
```

> 顺带一个干扰项：本机 v2rayN 开着 TUN（`EnableTun=true` + `StrictRoute=true`），对不可达地址会「接得上、没数据」，
> 看起来像服务坏了。**先看 ARP 有没有 MAC，再怀疑中间件。**

### 2.2 服务器上要有 JDK 21（现成的只有 JDK 8）

`doctor` 里 `java`/`mvn` 都显示 not found，但 `tio-boot` 那次构建日志里出现过 `JAVA_HOME=/usr/java/jdk1.8.0_411`，
说明 JDK 8 在 `/usr/java` 下、只是不在服务进程的 PATH 里。本项目 `maven.compiler.target=21`，必须补 JDK 21。
做法：**把"装 JDK 21"写成构建项目的第一条命令（幂等）**，并统一用绝对路径 `/usr/java/jdk-21.0.2/bin/java`，
不依赖 PATH。镜像用华为云的 OpenJDK 21（实测可下载）：

```bash
set -e
JDK=/usr/java/jdk-21.0.2
if [ ! -x "$JDK/bin/java" ]; then
  echo "未找到 $JDK，从华为云镜像下载 OpenJDK 21"
  mkdir -p /usr/java
  curl -fL --retry 3 --connect-timeout 20 -o /tmp/jdk21.tar.gz \
    https://repo.huaweicloud.com/openjdk/21.0.2/openjdk-21.0.2_linux-x64_bin.tar.gz
  tar -xzf /tmp/jdk21.tar.gz -C /usr/java
  rm -f /tmp/jdk21.tar.gz
fi
"$JDK/bin/java" -version
echo "mvn: $(command -v mvn || echo 未找到)"
mvn -v 2>/dev/null | head -2 || true
```

### 2.3 让服务器能连开发机的 PostgreSQL

本机 `listen_addresses` 已经是 `*`，但 `pg_hba.conf` 只放行了 `172.16.0.0/12`，而且 Windows 防火墙那条 5432 规则只允许 `172.16.0.0/12`：

```powershell
# 1) pg_hba.conf 追加（改完 reload 生效）
"host    all             all             192.168.31.0/24         scram-sha-256" |
  Add-Content 'D:\Program Files\PostgreSQL\18\data\pg_hba.conf'
& 'D:\Program Files\PostgreSQL\18\bin\psql.exe' -h 127.0.0.1 -U postgres -c "select pg_reload_conf();"

# 2) 防火墙放行局域网（原有规则只允许 172.16/12）
New-NetFirewallRule -DisplayName 'PostgreSQL LAN (mosskb deploy)' -Direction Inbound `
  -Protocol TCP -LocalPort 5432 -RemoteAddress 192.168.31.0/24 -Action Allow -Profile Any
```

服务器侧最终自查（见第 6 条运维命令）：`192.168.31.225:5432 可达`。

---

## 3. 依赖分析：哪些必须"手动构建"

`mosskb-web` 不是单模块：它依赖 `mosskb-business`，而 `mosskb-business` 又依赖一批 `nexus.io:*` 库。
**这些库有一部分没有发布到中央仓库**，服务器上直接 `mvn package` 必然失败。判定方法与结果：

```powershell
# 判定 1：这个版本在中央仓库（阿里云镜像）里到底有没有
curl.exe -sS -o NUL -w "%{http_code}`n" https://maven.aliyun.com/repository/public/nexus/io/api-table/1.6.1/api-table-1.6.1.pom
# 判定 2：本机仓库里它是"下载来的"还是"本地 install 的"
Get-Content D:\dev_maven_repository\nexus\io\api-table\1.6.1\_remote.repositories
#   ...jar>=   ← 空仓库号 = 本地 mvn install 装进去的，中央仓库没有
```

| 依赖 | 版本 | 中央仓库 | 结论 |
| --- | --- | --- | --- |
| `nexus.io:tio-boot`（t-io 反应堆） | 2.1.7 | **404** | 必须源码构建（平台上已有 `tio-boot` 构建项目） |
| `nexus.io:tio-boot-admin-web` | 2.1.6 | **404** | 必须源码构建 |
| `nexus.io:api-table` | 1.6.1 | **404** | 必须源码构建 |
| `nexus.io:java-openai` | 1.3.7 | 中央仓库有，**但缺 MossKB 需要的方法** | 必须用打过补丁的源码构建 |
| `nexus.io:java-db` | 1.6.0 | 200 | 直接下载即可 |
| `nexus.io:jfinal-aop` | 1.4.0 | 200 | 直接下载即可 |

`java-openai` 的两个方法只有补丁里才有，第一次构建就是被它们卡住的：

```
[ERROR] KnowledgeModelService.java:[63,34] cannot find symbol
[ERROR]   symbol:   method setThinking(java.util.Map<java.lang.String,java.lang.String>)
[ERROR]   location: class nexus.io.chat.UniChatRequest
[ERROR] MossKbApplicationChatMessageService.java:[222,30] cannot find symbol
[ERROR]   symbol:   method streamOpenAi(nexus.io.chat.UniChatRequest,okhttp3.Callback)
[ERROR]   location: class nexus.io.chat.UniChatClient
```

补丁在 `java-mosskb` 仓库里随代码走：`docs/customizations/2026-10-03/java-openai.patch`（10 KB，8 个 hunk），
`java-openai-manifest.json` 记录了基准 commit 与逐文件 sha256。构建时**下载补丁 → git apply → install**，
这样"补丁"和"应用"永远同一个版本，不用手工同步两份代码。

---

## 4. 项目设计：一个应用 + 五个构建项目

| 项目 Key | 类型 | 依赖 | Git（public，可匿名 clone） | 产物 |
| --- | --- | --- | --- | --- |
| `tio-boot` | 构建 | — | `https://gitee.com/ppnt/tio-boot.git` | t-io 全家桶 2.1.7 → `/root/.m2` |
| `tio-boot-admin` | 构建 | `tio-boot` | `https://gitee.com/ppnt/tio-boot-admin.git` | tio-boot-admin 2.1.6 |
| `api-table` | 构建 | — | `https://gitee.com/ppnt/api-table.git` | api-table 1.6.1 |
| `java-openai` | 构建 | — | `https://gitee.com/ppnt/java-openai.git` | java-openai 1.3.7（含补丁） |
| `java-mosskb-business` | 构建 | 上面四个 | `https://gitee.com/ppnt/java-mosskb.git` | mosskb-business 1.0.4 |
| `mosskb-web` | **应用** | `java-mosskb-business` | `https://gitee.com/ppnt/java-mosskb.git` | 发布到 `/srv/apps/mosskb-web` 并用 systemd 跑起来 |

设计要点：

- **同一个仓库可以出现在两个项目里**（`java-mosskb` 既做 business 又做 web）。两个项目各有自己的
  `/srv/deploy/workspaces/<key>`，互不干扰；这正是本平台"项目 = 工作目录 + 命令"的用法。
- **`mvn -pl <module>` 而不是 `-am`**：business 由依赖项目先 `install` 进本地仓库，web 侧只编自己，
  这样 web 一定嵌的是**本次**构建出来的 business jar（父 pom 里那段注释说的就是这个坑）。
- **依赖项目每次都会重建**（平台不做增量缓存），换来"不会拿到旧包"的确定性。

---

## 5. 每个项目的命令（可直接复制）

### 5.1 `tio-boot`（已有，保持不动）

Git：`https://gitee.com/ppnt/tio-boot.git` @ `main`，工作目录 `/srv/worksapce/tio-boot`，一条命令 `build`
（仓库自带 `build.txt`，内容是 `mvn clean install -DskipTests -Dgpg.skip`，`JAVA_HOME=/usr/java/jdk1.8.0_411`）。
**注意目录名 `worksapce` 是历史拼写，不要"顺手修正"**，否则会重新 clone 一份。

### 5.2 `tio-boot-admin`（构建项目）

- 工作目录：`/srv/deploy/workspaces/tio-boot-admin`
- Git：`https://gitee.com/ppnt/tio-boot-admin.git` @ `main`
- 依赖的构建项目：`tio-boot`

```bash
set -e
export JAVA_HOME=/usr/java/jdk-21.0.2
export PATH="$JAVA_HOME/bin:$PATH"
cd "${WORKDIR:-/srv/deploy/workspaces/tio-boot-admin}"
# 仓库 main 的 revision=2.1.6，它引用的 nexus.io:tio-boot 2.1.6 未发布到中央仓库；
# 服务器上的「tio-boot」构建项目已装好 2.1.7，这里用属性覆盖对齐，避免 http/core 版本混搭。
mvn -B clean install -DskipTests -Dgpg.skip=true -Dmaven.javadoc.skip=true -Dtio-boot.version=2.1.7
```

> 为什么用 `-Dtio-boot.version=2.1.7`：`tio-boot-admin` 的 pom 里 `tio-boot.version` 是属性，
> 覆盖它比"再建一个 t-io 2.1.6 的构建项目"简单；而且 `java-mosskb` 的父 pom 本来就把
> `tio-core/tio-http-server/...` 统一压到 2.1.7，两边结果一致（flatten 插件会把 2.1.7 写进产物 pom）。

### 5.3 `api-table`（构建项目）

- 工作目录：`/srv/deploy/workspaces/api-table`
- Git：`https://gitee.com/ppnt/api-table.git` @ `main`
- 依赖：无（它的 `java-db 1.6.0`、`tio-boot 2.1.0` 在中央仓库有）

```bash
set -e
# api-table 自己的 build.txt 以 1.8 为编译级别，服务器上现成有 JDK 8，直接沿用。
export JAVA_HOME=/usr/java/jdk1.8.0_411
export PATH="$JAVA_HOME/bin:$PATH"
cd "${WORKDIR:-/srv/deploy/workspaces/api-table}"
mvn -B clean install -DskipTests -Dgpg.skip=true -Dmaven.javadoc.skip=true
```

### 5.4 `java-openai`（构建项目，含补丁）

- 工作目录：`/srv/deploy/workspaces/java-openai`
- Git：`https://gitee.com/ppnt/java-openai.git` @ `main`
- 依赖：无

命令 1「应用 MossKB 定制补丁」：

```bash
set -e
cd "${WORKDIR:-/srv/deploy/workspaces/java-openai}"
# mosskb-business 需要补丁里的 UniChatRequest.setThinking 与 UniChatClient.streamOpenAi；
# 这两个改动只在 java-mosskb 仓库保存为补丁，补丁随仓库版本走，避免手工维护差异。
curl -fsSL --retry 3 -o /tmp/java-openai.patch \
  https://gitee.com/ppnt/java-mosskb/raw/main/docs/customizations/2026-10-03/java-openai.patch
git apply /tmp/java-openai.patch
echo '已应用 java-openai.patch'
git status --short
```

命令 2「构建并安装 java-openai 1.3.7」：

```bash
set -e
# 该库自身 build.txt 的编译级别是 1.8，服务器上有现成 JDK 8。
export JAVA_HOME=/usr/java/jdk1.8.0_411
export PATH="$JAVA_HOME/bin:$PATH"
cd "${WORKDIR:-/srv/deploy/workspaces/java-openai}"
mvn -B clean install -DskipTests -Dgpg.skip=true -Dmaven.javadoc.skip=true
```

> 平台的 Git 准备阶段每次都会把工作区 reset 回 `origin/main`，所以**补丁必须每次重新 apply**，
> 不能指望上一次的改动还在。反过来这也保证了"提交号 + 补丁"就是可复现的输入。

### 5.5 `java-mosskb-business`（构建项目）

- 工作目录：`/srv/deploy/workspaces/java-mosskb-business`
- Git：`https://gitee.com/ppnt/java-mosskb.git` @ `main`
- 依赖的构建项目：`tio-boot`、`tio-boot-admin`、`api-table`、`java-openai`

命令 1：装 JDK 21（内容见 §2.2，两个 Java 项目共用同一段）。

命令 2「构建并安装 mosskb-business」：

```bash
set -e
export JAVA_HOME=/usr/java/jdk-21.0.2
export PATH="$JAVA_HOME/bin:$PATH"
cd "${WORKDIR:-/srv/deploy/workspaces/java-mosskb-business}"
mvn -B -pl mosskb-business -am install -DskipTests -Dgpg.skip=true -Dmaven.javadoc.skip=true
```

### 5.6 `mosskb-web`（应用项目）

- Key `mosskb-web`，类型**应用项目**，工作目录 `/srv/deploy/workspaces/mosskb-web`
- Git：`https://gitee.com/ppnt/java-mosskb.git` @ `main`
- 依赖的构建项目：`java-mosskb-business`（其余五个由它传递）
- 环境变量（**Secret 会在日志里被平台自动打码**，实测命令回显里是 `jdbc.pswd=***`）：

| 变量 | 值 | Secret |
| --- | --- | --- |
| `DB_HOST` | `192.168.31.225` | 否 |
| `DB_PSWD` | 开发机 PostgreSQL 密码 | **是** |
| `GITEE_API_KEY` | 模型平台 Key | **是** |

命令 1「确认 JDK 21」：

```bash
set -e
JDK=/usr/java/jdk-21.0.2
[ -x "$JDK/bin/java" ] || { echo "缺少 JDK 21：请先执行依赖项目 java-mosskb-business（它的第一条命令会安装）"; exit 1; }
"$JDK/bin/java" -version
echo "mvn: $(command -v mvn || echo 未找到)"
```

命令 2「构建 mosskb-web 生产包」：

```bash
set -e
export JAVA_HOME=/usr/java/jdk-21.0.2
export PATH="$JAVA_HOME/bin:$PATH"
cd "${WORKDIR:-/srv/deploy/workspaces/mosskb-web}"
# 不带 -am：mosskb-business 由依赖项目先 install 到本地仓库，
# 这样 web 里嵌的必定是本次构建出来的 business jar。
mvn -B -pl mosskb-web package -Pproduction -DskipTests -Dgpg.skip=true -Dmaven.javadoc.skip=true
ls -lh mosskb-web/target/mosskb-web-*.jar
```

命令 3「发布产物与运行配置」（写 `my.txt` / `secrets.txt`，数据库指向开发机）：

```bash
set -e
APP_DIR=/srv/apps/mosskb-web
JAR=$(ls -t "${WORKDIR}"/mosskb-web/target/mosskb-web-*.jar | grep -vE -- '-(sources|javadoc)\.jar$' | head -1)
[ -n "$JAR" ] || { echo '找不到 mosskb-web 的 jar'; exit 1; }
mkdir -p "$APP_DIR/logs"
cp -f "$JAR" "$APP_DIR/mosskb-web.jar"
cat > "$APP_DIR/my.txt" <<EOF
jdbc.url=jdbc:postgresql://${DB_HOST}:5432/moss_kb
jdbc.user=postgres
jdbc.pswd=${DB_PSWD}
app.admin.secret.key=ryNy01Fe8Atcl8kCX6dxnC/0IGVn0EOx7VMBnJva0J2zchBc485p2mTj9eeD+1dy
server.port=10060
app.env=prod
jdbc.showSql=false
kb.playwright.enable=false
kb.python.runner=docker
kb.python.image=python:3.12-slim
http.multipart.max-request-size=110100480
http.multipart.max-file-size=104857600
EOF
printf 'GITEE_API_KEY=%s\n' "$GITEE_API_KEY" > "$APP_DIR/secrets.txt"
chmod 600 "$APP_DIR/my.txt" "$APP_DIR/secrets.txt"
echo "jar: $(du -h "$APP_DIR/mosskb-web.jar" | cut -f1)"
sed -e 's/^\(jdbc.pswd=\).*/\1***/' "$APP_DIR/my.txt"
```

> `my.txt` 必须和 `java -jar` 的工作目录一致（§1 的 EnvUtils 说明）；
> `http.multipart.*` 这两项也只能写在本地配置里，写进 jar 内的 `app.properties` 不生效。

命令 4「注册 systemd 服务并重启」：

```bash
set -e
cat > /etc/systemd/system/mosskb-web.service <<'UNIT'
[Unit]
Description=Java MossKB (mosskb-web)
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/srv/apps/mosskb-web
ExecStart=/usr/java/jdk-21.0.2/bin/java -jar /srv/apps/mosskb-web/mosskb-web.jar
Restart=always
RestartSec=5
Environment=TZ=Asia/Shanghai
Environment=LANG=C.UTF-8
StandardOutput=append:/srv/apps/mosskb-web/logs/startup.log
StandardError=append:/srv/apps/mosskb-web/logs/startup-error.log

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable mosskb-web >/dev/null 2>&1 || true
: > /srv/apps/mosskb-web/logs/startup.log
: > /srv/apps/mosskb-web/logs/startup-error.log
systemctl restart mosskb-web
sleep 3
systemctl is-active mosskb-web
```

命令 5「健康检查（决定本次成败）」：

```bash
set -e
CODE=000
for i in $(seq 1 30); do
  CODE=$(curl -s -o /tmp/mosskb-info.json -w '%{http_code}' http://127.0.0.1:10060/api/display/info || true)
  [ "$CODE" = "200" ] && break
  sleep 2
done
echo "HTTP $CODE"
cat /tmp/mosskb-info.json 2>/dev/null; echo
if [ "$CODE" != "200" ]; then
  echo '--- systemctl status ---'; systemctl status mosskb-web --no-pager -l | tail -20
  echo '--- startup.log ---'; tail -60 /srv/apps/mosskb-web/logs/startup.log 2>/dev/null
  echo '--- startup-error.log ---'; tail -40 /srv/apps/mosskb-web/logs/startup-error.log 2>/dev/null
  exit 1
fi
echo 'MossKB 后端已就绪: http://192.168.31.97:10060/api/display/info'
```

命令 6「查看运行状态与日志（运维）」（**用「单独执行」跑，不重新构建**）：

```bash
set -e
echo '=== systemd 状态 ==='
systemctl status mosskb-web --no-pager -l | head -12
echo '=== 监听端口 ==='
ss -ltnp 2>/dev/null | grep 10060 || echo '10060 未监听'
echo '=== 发布目录 ==='
ls -lh /srv/apps/mosskb-web | head -10
echo '=== startup.log 尾部 ==='
tail -25 /srv/apps/mosskb-web/logs/startup.log
echo '=== startup-error.log 尾部 ==='
tail -15 /srv/apps/mosskb-web/logs/startup-error.log 2>/dev/null || true
echo '=== 到开发机数据库的连通性 ==='
timeout 3 bash -c 'cat < /dev/null > /dev/tcp/192.168.31.225/5432' && echo '192.168.31.225:5432 可达' || { echo '192.168.31.225:5432 不可达'; exit 1; }
```

这次的实际输出（run 40，success）：

```
Oct 05 11:21:43 MiWiFi-R4A-srv systemd[1]: Started mosskb-web.service - Java MossKB (mosskb-web).
=== 监听端口 ===
LISTEN 0 1000 *:10060 *:*  users:(("java",pid=8560,fd=51))
=== 发布目录 ===
-rw-r--r-- 1 root root  53M mosskb-web.jar
-rw------- 1 root root  410 my.txt
-rw------- 1 root root   55 secrets.txt
=== 到开发机数据库的连通性 ===
192.168.31.225:5432 可达
```

---

## 6. 操作步骤（网页）

1. 打开 <http://192.168.31.97:10055>，用 `admin` 登录（交付时给的是 `admin12345`，**请立刻改**）。
2. 「项目 → 新建项目」，依次建上表 6 个项目。填表时注意：
   - **工作目录是必填项**，平台不会按 Key 自动生成，建议统一 `/srv/deploy/workspaces/<key>`；
   - 底层库把「项目类型」选成**构建项目**，否则上层项目的「依赖的构建项目」下拉里看不到它；
   - 有 `build.txt` 的仓库（如 `tio-boot`）命令可以直接写 `build`，其余项目写完整 `mvn`；
   - 环境变量：勾上 Secret 的会打码，普通变量明文可见。
3. 「项目 → mosskb-web → 执行」（有依赖时默认**含依赖**；只想跑本项目用下拉里的「仅执行本项目」）。
4. 「运行」页看实时日志：支持暂停滚动/搜索/下载，能直接看到每条命令、退出码与耗时。
5. 需要单独跑某条命令（例如看日志）用该行的「单独执行」，**不会**触发依赖或整条流水线。

---

## 7. 踩到的 8 个坑（含修法）

| # | 现象 | 根因 | 修法 |
| --- | --- | --- | --- |
| 1 | 保存项目报「请填写工作目录」 | 工作目录是必填，且不会按 Key 推导 | 显式填 `/srv/deploy/workspaces/<key>` |
| 2 | 新建项目页里还留着上一个项目的输入 | 同一路由（`/projects/new`）SPA 不重新挂载，表单状态被复用 | 换路由或整页刷新后再填；**自动化脚本必须每次刷新并 `wait_for_element('#key')`**，否则会建出"半新半旧"的项目 |
| 3 | `nexus.io:api-table:jar:1.6.1` 的 POM missing，business 构建失败 | api-table 未发布到中央仓库 | 建 `api-table` 构建项目，`mvn install` 到服务器本地仓库 |
| 4 | `UniChatRequest.setThinking` / `UniChatClient.streamOpenAi` 找不到符号 | java-openai 的这两个改动只在 java-mosskb 仓库里以补丁形式存在 | 建 `java-openai` 构建项目：下载补丁 → `git apply` → `install` |
| 5 | tio-boot-admin 2.1.6 依赖的 `tio-boot 2.1.6` 也不在中央仓库 | 仓库 revision 与依赖属性都锁在 2.1.6 | 构建时 `-Dtio-boot.version=2.1.7`，与 `java-mosskb` 父 pom 的版本压平一致 |
| 6 | `doctor` 说没有 java/mvn，可历史上又有 Java 构建成功过 | JDK 8 在 `/usr/java` 下，不在服务进程 PATH 里；项目需要的是 JDK 21 | 构建项目第一条命令装 JDK 21，并统一用绝对路径调用 |
| 7 | 服务器连不上开发机 5432 | `pg_hba.conf` 只有 `172.16.0.0/12`；Windows 防火墙规则也只放行该网段 | 追加 `192.168.31.0/24` 的 hba 行 + 新建防火墙规则 |
| 8 | 服务起来了，但 `my.txt` 里的配置像没生效 | `my.txt`/`secrets.txt` 按**进程工作目录**查找 | 让 systemd 的 `WorkingDirectory` = 发布目录 = jar 所在目录 |

另外两条经验：

- **别只看平台的成功/失败**：`mosskb-web` 第一次跑失败时，平台报的是 `dependency build java-mosskb-business failed (run #32 ...); the main project was not executed`，
  真正的原因在依赖那条 run 的日志里（编译错误）。看日志要往上游找。
- **首次构建慢是正常的**：`tio-boot-admin` 第一次 136.5s（下依赖），第二次 5.4s。首跑别急着判失败。

---

## 8. 验证清单（复制即用）

```bash
# 1) 服务与外观接口
curl -sS --compressed http://192.168.31.97:10060/api/display/info

# 2) 走数据库的登录（返回 JWT 说明库连接、表结构、种子数据都对）
curl -sS -X POST http://192.168.31.97:10060/api/user/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<管理员密码>"}'

# 3) 确认连的是"本机数据库"：在开发机上看有没有来自服务器的连接
#    psql -h 127.0.0.1 -U postgres -d moss_kb
select client_addr, application_name, state, count(*)
from pg_stat_activity where datname = 'moss_kb' group by 1,2,3;

# 4) 服务器侧进程/端口/配置（在平台上用「单独执行」跑第 6 条命令）
```

> `/api/user/login` 是**方法名映射**出来的路径（`ApiUserController.login`）。写成 `/api/auth/login` 会返回 401
> ——那 401 是拦截器给的，别误判成"数据库不通"。

---

## 9. 重跑、回滚与清理

- **重跑**：项目详情点「执行」（含依赖）。平台不做增量缓存，每次都是全新构建，产物一定是最新代码。
- **只重跑上层**：下拉选「仅执行本项目」（依赖的 jar 还在服务器本地仓库里）。
- **回滚**：把项目 Git 分支/Tag 指到上一个可用提交再执行；`/srv/apps/mosskb-web` 每次都会覆盖 jar，
  配置由命令生成，因此回滚 = 回滚代码版本。需要保留历史产物时，可在发布命令里加一步
  `cp mosskb-web.jar mosskb-web.jar.$(date +%F_%H%M%S)`。
- **停服务**：`systemctl stop mosskb-web`（或禁用：`systemctl disable --now mosskb-web`）。
- **清工作区**：删掉项目里的 Git 工作区目录即可，下回执行会重新 clone。

---

## 10. 等价 API / CLI 操作

网页上做的每一步都有等价接口（`docs/API设计.md`、仓库里的 `tools/smoke/deploy-mosskb-via-api.ps1`）：

```bash
BASE=http://192.168.31.97:10055/api/v1
TOKEN=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<密码>"}' | jq -r .data.token)

# 触发一次含依赖的执行
curl -s -X POST $BASE/projects/mosskb-web/run -H "Authorization: $TOKEN" \
  -H 'Content-Type: application/json' -d '{"branch":"main","withDependencies":true}'
# 跟随日志
curl -s "$BASE/runs/<runId>/log" -H "Authorization: $TOKEN"
```

CLI：`deploy run mosskb-web -f`（`deploy login -a http://192.168.31.97:10055 -u admin` 之后）。

---

## 11. 遗留与下一步

1. **前端 UI 未部署**：`java-mosskb-ui`（Vue 3 + Vite）是独立仓库，平台上已有同名项目但历史上因缺 Node 失败。
   要一起上线的话：在服务器上装 Node（或用「环境自检 → 一键安装工具链」），构建后由 Nginx 托管 `dist/`，
   反向代理 `/api` 到 `127.0.0.1:10060`。
2. **隔离 Python 执行器**：`my.txt` 里是 `kb.python.runner=docker` + `python:3.12-slim`，
   服务器上需先 `docker pull python:3.12-slim`（执行固定 `--pull=never`），否则"函数库"相关功能会拒绝执行。
3. **健康检查端点**：这里用公开的 `/api/display/info`，它不碰数据库。想让部署失败更早暴露，
   可以再加一条 `POST /api/user/login` 的断言（本页第 8 节第 2 条）。
4. **Secret 管理**：`api-table` 等构建项目目前都靠公开仓库匿名克隆；私有化时应改用平台的 Git 凭据，
   把 token 从命令里挪出去。
