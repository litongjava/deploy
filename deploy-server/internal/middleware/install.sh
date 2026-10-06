#!/usr/bin/env bash
# 部署平台「中间件一键安装」脚本。
#
# 与 internal/toolchain/install.sh 并列的第二套脚本：那套装的是**构建工具链**
# （gcc/java/maven/node/go/rust/certbot，无状态、随装随用）；这套装的是**有状态服务**
# （docker/postgresql/mysql/redis/elasticsearch），它们的安装参数里有端口、密码、
# 数据目录，装完还要能启停查，所以单独成一套，不跟构建工具链混在一个脚本里。
#
# 这个文件被 go:embed 进 deploy-server 二进制（见 internal/middleware/middleware.go），
# 页面上点「安装」时由平台落盘到 <dataDir>/middleware/install.sh，再以普通 Run 执行。
# 也可以脱离平台手动跑：
#
#   bash install.sh --list                          # 只看状态
#   bash install.sh --only docker,redis             # 装指定中间件
#   bash install.sh --only postgresql --mode native
#   bash install.sh --action restart --only redis   # 启停查
#   bash install.sh --remove --only redis           # 移除（保留数据卷）
#   bash install.sh --dry-run --only all            # 预演
#
# 设计原则：
#   1. 幂等：已装且在跑的默认跳过；重复点「安装」不会重建容器、不会丢数据。
#   2. 一个组件失败不影响其它组件：逐个执行、最后汇总，退出码反映是否有失败。
#   3. **绝不删除数据**：--remove 只删容器/包，数据卷与数据目录一律保留并打印位置。
#   4. 只在 Linux + bash 下有意义；其它平台用 --dry-run 看计划。
set -uo pipefail

SCRIPT_VERSION="1.1.0"
ALL_COMPONENTS="docker postgresql mysql redis elasticsearch"
MIRROR="${MIRROR:-cn}"

# ---------- 默认参数（同名环境变量可覆盖；平台侧「编辑参数」注入的就是这些） ----------
PG_VERSION="${PG_VERSION:-16}"
PG_PORT="${PG_PORT:-5432}"
PG_USER="${PG_USER:-postgres}"
PG_PASSWORD="${PG_PASSWORD:-}"
PG_DATA_DIR="${PG_DATA_DIR:-/srv/deploy/middleware/postgresql}"

MYSQL_VERSION="${MYSQL_VERSION:-8.0}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-}"
MYSQL_DATA_DIR="${MYSQL_DATA_DIR:-/srv/deploy/middleware/mysql}"

# 官方文档的取镜像命令就是 `docker run --name some-redis -d redis`，也就是 latest。
# 用 latest 而不是 7：本机常常早就有一份 redis:latest，而「拉取失败」在
# 网络不通的环境里是常态（见 container_apply 里的兜底），用 latest 才有机会直接复用。
REDIS_VERSION="${REDIS_VERSION:-latest}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"
REDIS_DATA_DIR="${REDIS_DATA_DIR:-/srv/deploy/middleware/redis}"

ES_VERSION="${ES_VERSION:-8.15.0}"
ES_PORT="${ES_PORT:-9200}"
ES_TRANSPORT_PORT="${ES_TRANSPORT_PORT:-9300}"
ES_JAVA_OPTS="${ES_JAVA_OPTS:--Xms1g -Xmx1g}"
ES_DATA_DIR="${ES_DATA_DIR:-/srv/deploy/middleware/elasticsearch}"

# 容器/卷统一加前缀，避免和服务器上已有的容器、卷撞名
CONTAINER_PREFIX="${CONTAINER_PREFIX:-deploy-mw-}"
VOLUME_PREFIX="${VOLUME_PREFIX:-deploy-mw-}"
# 端口被占用时是否继续（默认继续并给出警告，避免一次安装全线失败）
IGNORE_PORT_CONFLICT="${IGNORE_PORT_CONFLICT:-1}"

ONLY=""; MODE_OVERRIDE=""; MODE_OF=""; ACTION="install"; DRY_RUN=0; FORCE=0; LIST_ONLY=0

if [ -t 1 ]; then C_HEAD=$'\033[1;36m'; C_OK=$'\033[1;32m'; C_WARN=$'\033[1;33m'; C_ERR=$'\033[1;31m'; C_OFF=$'\033[0m'
else C_HEAD=""; C_OK=""; C_WARN=""; C_ERR=""; C_OFF=""; fi

section() { printf '\n%s==> %s%s\n' "$C_HEAD" "$*" "$C_OFF"; }
info() { printf '    %s\n' "$*"; }
ok()   { printf '%s    ✓ %s%s\n' "$C_OK" "$*" "$C_OFF"; }
warn() { printf '%s    ! %s%s\n' "$C_WARN" "$*" "$C_OFF"; }
err()  { printf '%s    ✗ %s%s\n' "$C_ERR" "$*" "$C_OFF"; }
have() { command -v "$1" >/dev/null 2>&1; }

# run 是唯一执行「有副作用」动作的入口：dry-run 时只打印。
run() {
  if [ "$DRY_RUN" = "1" ]; then printf '    [dry-run] %s\n' "$*"; return 0; fi
  "$@"
}

# mask_literal <值> —— 把一段输出里的某个字面量换成 ***。
#
# 为什么必须做：安装日志写在平台的运行日志里，**任何登录用户都能看**（平台只对项目
# Secret 环境变量打码，而这里是我们自己打印的文本，不走平台那套）。而运行日志会打印
# docker run 的完整命令行，里面带 --requirepass / POSTGRES_PASSWORD= 明文；
# 密码仍然会在脚本末尾的「连接信息」里出现一次（那是给人抄走的），
# 但没必要在每一行命令回显里再铺一遍。
mask_literal() {
  local v="$1"
  if [ -z "$v" ]; then cat; return 0; fi
  # 用 | 作分隔符，密码里出现 / 或 : 也不会破坏表达式
  sed "s|$v|***|g"
}

# 把 docker 的输出里的密码统一打码；pull 的输出顺带缩进。
mask_all_secrets() {
  mask_literal "${REDIS_PASSWORD:-}" | mask_literal "${PG_PASSWORD:-}" | mask_literal "${MYSQL_ROOT_PASSWORD:-}"
}

usage() {
  cat <<'EOF'
部署平台「中间件一键安装」

用法：bash install.sh [选项]

  --only a,b           只处理这些中间件（docker postgresql mysql redis elasticsearch，或 all）
  --mode docker|native 安装方式；默认 docker，native 用发行版包管理器直接装
  --mode-of c=mode     逐组件指定方式，可重复（例：--mode-of redis=native），优先于 --mode
  --action <动作>      install（默认）/ start / stop / restart / remove / upgrade / logs
  --mirror cn|off      是否使用国内镜像（默认 cn）
  --dry-run            预演：只打印将要做的事，不做任何修改
  --force              已安装的也重建（容器方式会保留数据卷）
  --remove             等价于 --action remove
  --list               只列出中间件与检测结果
  -h, --help           显示本帮助

参数用环境变量覆盖，例如：
  PG_PORT=15432 REDIS_PASSWORD=secret bash install.sh --only postgresql,redis
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --only)   ONLY="${2:-}"; shift 2 ;;
    --mode)   MODE_OVERRIDE="${2:-}"; shift 2 ;;
    --mode-of)
      _mo="${2:-}"; shift 2
      case "$_mo" in
        *=*) MODE_OF="${MODE_OF} ${_mo}" ;;
        *) err "--mode-of 需要 组件=方式 的形式（收到：$_mo）"; exit 2 ;;
      esac ;;
    --action) ACTION="${2:-install}"; shift 2 ;;
    --mirror) MIRROR="${2:-cn}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --force)  FORCE=1; shift ;;
    --remove) ACTION="remove"; shift ;;
    --list)   LIST_ONLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) err "未知参数：$1"; usage; exit 2 ;;
  esac
done

case "$ACTION" in
  install|start|stop|restart|remove|upgrade|logs) ;;
  *) err "未知动作：$ACTION（可用 install/start/stop/restart/remove/upgrade/logs）"; exit 2 ;;
esac

# ---------- 平台识别 ----------
# 注意：**只有 --dry-run 允许非 root**，因为预演只打印、不做任何修改；
# 这样在开发机上也能先看一眼计划。真正动手必须 root。
if [ "$(id -u)" != "0" ]; then
  if [ "$DRY_RUN" = "1" ]; then
    warn "非 root 运行：仅允许 dry-run"
  elif have sudo; then
    exec sudo -E bash "$0" "$@"
  else
    err "需要 root 权限（要装包、写 /etc、调用 docker）"; exit 1
  fi
fi

OS_ID="unknown"; OS_VER=""
if [ -r /etc/os-release ]; then . /etc/os-release; OS_ID="${ID:-unknown}"; OS_VER="${VERSION_ID:-}"; fi
case "$OS_ID" in
  debian|ubuntu|raspbian|linuxmint|pop) PKG=apt ;;
  rhel|centos|rocky|almalinux|fedora|openeuler|anolis|kylin) PKG=dnf ;;
  *) PKG=unknown ;;
esac
[ "$PKG" = "dnf" ] && ! have dnf && PKG=yum

ARCH="$(uname -m)"

APT_UPDATED=0
apt_update() {
  [ "$APT_UPDATED" = "1" ] && return 0
  APT_UPDATED=1
  run env DEBIAN_FRONTEND=noninteractive apt-get update -qq || true
}
pkg_install() {
  case "$PKG" in
    apt) apt_update; run env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ;;
    dnf) run dnf install -y "$@" ;;
    yum) run yum install -y "$@" ;;
    *)   err "不支持的发行版（$OS_ID）：请手动安装 $*"; return 1 ;;
  esac
}

ensure_prereqs() {
  local need=()
  for t in curl; do have "$t" || need+=("$t"); done
  if [ ${#need[@]} -gt 0 ]; then
    section "安装基础依赖：${need[*]}"
    pkg_install curl ca-certificates || return 1
  fi
  return 0
}

profile_write() { # profile_write <文件名> <内容>
  local f="/etc/profile.d/$1"
  if [ "$DRY_RUN" = "1" ]; then info "[dry-run] 写 $f"; return 0; fi
  printf '%s\n' "$2" > "$f"
  chmod 0644 "$f"
}

# ---------- 通用小工具 ----------
# 生成随机密码：优先 openssl，退回 /dev/urandom。
rand_password() {
  if have openssl; then openssl rand -hex 16; return 0; fi
  head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n'
}

container_name() { echo "${CONTAINER_PREFIX}$1"; }
volume_name()    { echo "${VOLUME_PREFIX}$1-data"; }

docker_ready() { have docker && docker info >/dev/null 2>&1; }

container_exists() { have docker && docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$(container_name "$1")"; }
container_running() { have docker && docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$(container_name "$1")"; }

# 端口是否被占用（ss 优先，退回 netstat）。只用于给出可读的警告，不阻断安装。
port_in_use() {
  local p="$1"
  if have ss; then ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$p\$" && return 0; return 1; fi
  if have netstat; then netstat -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$p\$" && return 0; return 1; fi
  return 1
}

# mode_of <组件>：逐组件方式（--mode-of redis=native）优先，
# 其次全局 --mode，最后按组件默认（与 middleware.go 的 DefaultMode 一致）。
mode_of() {
  local c="$1" pair
  for pair in $MODE_OF; do
    case "$pair" in
      "$c"=*) echo "${pair#*=}"; return 0 ;;
    esac
  done
  if [ -n "$MODE_OVERRIDE" ]; then echo "$MODE_OVERRIDE"; return 0; fi
  case "$1" in
    docker) echo native ;;   # docker 自己当然用原生方式装
    *)      echo docker ;;
  esac
}

# 原生方式不支持时明确报错，而不是静默改走另一条路 —— 使用者要能预期装到哪。
require_native_supported() {
  local c="$1"
  case "$c" in
    docker)
      err "docker 只能用原生方式安装（--mode native），它本身就是容器的运行时"
      return 1 ;;
    elasticsearch)
      err "elasticsearch 不支持原生安装：官方推荐容器，且原生包要额外配 JDK 与 vm.max_map_count"
      info "改用容器方式：bash $0 --only elasticsearch --mode docker"
      return 1 ;;
  esac
  return 0
}

# ---------- systemd 辅助（原生方式） ----------
systemd_available() { have systemctl && [ -d /run/systemd/system ]; }

unit_start() { # unit_start <单元名> <组件名>
  local unit="$1" name="$2"
  if ! systemd_available; then
    warn "没有 systemd，请手动启动 $name（例如 $unit）"
    return 0
  fi
  run systemctl enable "$unit" >/dev/null 2>&1 || true
  run systemctl restart "$unit" || { err "$unit 启动失败"; return 1; }
  ok "$name 已启动（systemctl status $unit）"
}
unit_stop() {
  local unit="$1" name="$2"
  if ! systemd_available; then warn "没有 systemd，请手动停止 $name"; return 0; fi
  run systemctl stop "$unit" || { err "$unit 停止失败"; return 1; }
  ok "$name 已停止"
}

# require_installed / dry_run_note：dry-run 下什么都没装，不能拿机器上**已有**的
# 版本当成安装结果（否则预演会显示一堆「成功」，其实是旧东西）。
require_installed() {
  if [ "$DRY_RUN" = "1" ]; then return 0; fi
  if ! "$1"; then err "$2 安装后仍不可用"; return 1; fi
  return 0
}
dry_run_note() {
  if [ "$DRY_RUN" = "1" ]; then ok "预演：将安装到 $1"; return 0; fi
  return 1
}

# ---------- docker 本体 ----------
detect_docker() { have docker && docker --version >/dev/null 2>&1; }

install_docker_native() {
  if detect_docker && [ "$FORCE" != "1" ]; then
    ok "已安装：$(docker --version 2>/dev/null)（路径 $(command -v docker)）"
    docker_ready || warn "docker 已装但守护进程没在跑：systemctl enable --now docker"
    return 0
  fi
  ensure_prereqs || return 1
  case "$PKG" in
    apt)
      # 走发行版仓库：版本比 Docker 官方源旧，但不需要改 apt 源（符合「保守修改」原则）。
      # 需要更新版本时使用者可自行按官方文档换源。
      pkg_install docker.io || return 1 ;;
    dnf|yum)
      pkg_install docker || {
        warn "仓库里没有 docker，尝试 docker-ce"
        pkg_install docker-ce || return 1
      } ;;
    *) return 1 ;;
  esac
  dry_run_note "$(command -v docker 2>/dev/null || echo /usr/bin/docker)" && return 0
  unit_start docker "docker" || return 1
  docker_ready || warn "docker 装了但 docker info 失败，检查守护进程与权限"
  ok "$(docker --version 2>/dev/null) → $(command -v docker)"
}

docker_action() { # docker_action <动作>  —— docker 组件自身
  case "$1" in
    start|restart) unit_start docker "docker" ;;
    stop)          unit_stop docker "docker" ;;
    remove)
      err "不支持移除 docker 本体：平台上其它中间件都依赖它"
      info "确需卸载请手动执行：apt remove --purge docker.io（或 dnf remove docker）"
      return 1 ;;
    upgrade) err "docker 本体升级请按官方文档操作（本脚本不替使用者换 apt 源）"; return 1 ;;
    logs)    if have journalctl; then journalctl -u docker -n 50 --no-pager; else err "没有 journalctl"; return 1; fi ;;
    *)       return 1 ;;
  esac
}

# ---------- 容器方式的通用动作 ----------
# container_apply <组件名> <端口> <镜像> [docker run 选项...] [-- 容器命令...]
#
# 「选项」与「容器命令」用 `--` 明确分开，不能靠位置猜：
# docker run 的语法是 `docker run [选项...] 镜像 [命令...]`，而选项的个数不固定，
# 只有调用方知道从哪开始是「命令」。最初按固定位置 shift 取值，结果调用方把容器命令
# 写在选项后面时（redis 就是），`redis-server` 会被当成镜像名，
# 于是去拉 `redis-server:latest`，报一个和真实问题毫不相干的错。
#
# 已经存在且不是 --force 时只做「确保在跑」，绝不重建 —— 重建会换掉数据卷指向。
container_apply() {
  local c="$1" port="$2" image="$3"; shift 3
  local name; name="$(container_name "$c")"

  # 收集选项直到 `--`，其后是容器命令
  local opts=() cmd=() seen_sep=0 a
  for a in "$@"; do
    if [ "$seen_sep" = "0" ] && [ "$a" = "--" ]; then seen_sep=1; continue; fi
    if [ "$seen_sep" = "0" ]; then opts+=("$a"); else cmd+=("$a"); fi
  done

  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] docker pull $image"
    # 按实际执行顺序打印：选项 → 镜像 → 命令。命令可能为空，`${cmd[*]}` 展开成空串。
    info "[dry-run] docker run -d --name $name --restart unless-stopped ${opts[*]} $image ${cmd[*]}" \
      | mask_all_secrets
    return 0
  fi

  if ! docker_ready; then
    err "docker 不可用（未安装或守护进程没跑）：先装 docker 组件，或改用 --mode native"
    return 1
  fi

  if container_exists "$c" && [ "$FORCE" != "1" ]; then
    if container_running "$c"; then
      ok "容器已在运行：$name"
    else
      run docker start "$name" >/dev/null || { err "容器 $name 启动失败"; return 1; }
      ok "容器已启动：$name"
    fi
    return 0
  fi

  if [ -n "${port:-}" ] && port_in_use "$port"; then
    warn "端口 $port 已被占用；若是上一次装的同名容器属正常，其它进程占用会让容器起不来"
    [ "$IGNORE_PORT_CONFLICT" = "1" ] || { err "端口 $port 被占用，已按 IGNORE_PORT_CONFLICT=0 中止"; return 1; }
  fi

  info "拉取镜像 $image"
  local pull_out pull_rc
  pull_out="$(docker pull "$image" 2>&1)"; pull_rc=$?
  if [ "$pull_rc" != "0" ]; then
    printf '%s\n' "$pull_out" | mask_all_secrets | sed 's/^/    /'
    # 本机已经有这个 tag 时不该直接失败：docker 每次 pull 都要先问 registry 要 manifest，
    # 所以「离线/代理不通 + 镜像其实在本地」一定会走进这个分支。先看看本地有没有。
    if docker image inspect "$image" >/dev/null 2>&1; then
      warn "拉取失败，但本机已有 $image，直接用本地镜像继续"
    else
      err "镜像拉取失败：$image，本机也没有这个 tag"
      info "常见原因：Docker Hub 不可达（或给守护进程配了代理但代理已不通）。检查——"
      info "  systemctl show docker --property=Environment     # 有没有 HTTP(S)_PROXY"
      info "  cat /etc/systemd/system/docker.service.d/*.conf  # 代理通常写在这里"
      info "  docker images                                    # 本地是否已有可用的 tag"
      info "内网无外网时，也可以从别处 docker save 再 docker load 过来"
      return 1
    fi
  fi

  if container_exists "$c"; then
    warn "容器 $name 已存在，按 --force 重建（数据卷 $(volume_name "$c") 保留）"
    run docker rm -f "$name" >/dev/null || { err "删除旧容器失败"; return 1; }
  fi

  if ! run docker run -d --name "$name" --restart unless-stopped "${opts[@]}" "$image" ${cmd[@]+"${cmd[@]}"}; then
    err "容器 $name 启动失败，用 docker logs $name 看原因"
    return 1
  fi
  # 容器起来不等于服务可用（数据库还要初始化），给一个短的等待窗口。
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    [ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" = "true" ] && break
    sleep 1
  done
  if [ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != "true" ]; then
    err "容器 $name 起来后又退出了，日志尾部："
    docker logs --tail 20 "$name" 2>&1 | sed 's/^/        /'
    return 1
  fi
  ok "容器已启动：$name（docker logs -f $name 看日志）"
}

container_action() { # container_action <动作> <组件名>
  local act="$1" c="$2" name; name="$(container_name "$c")"
  if [ "$DRY_RUN" = "1" ]; then info "[dry-run] docker $act $name"; return 0; fi
  if ! have docker; then err "没有 docker，无法对容器执行 $act"; return 1; fi
  case "$act" in
    start)
      container_exists "$c" || { err "容器 $name 不存在，先执行安装"; return 1; }
      run docker start "$name" >/dev/null && ok "已启动 $name" ;;
    stop)
      container_exists "$c" || { warn "容器 $name 不存在，无需停止"; return 0; }
      run docker stop "$name" >/dev/null && ok "已停止 $name" ;;
    restart)
      container_exists "$c" || { err "容器 $name 不存在，先执行安装"; return 1; }
      run docker restart "$name" >/dev/null && ok "已重启 $name" ;;
    upgrade)
      container_exists "$c" || { err "容器 $name 不存在，先执行安装"; return 1; }
      warn "升级不删容器：请用 --force 重新执行安装以换新镜像（数据卷保留）"
      return 1 ;;
    logs)
      container_exists "$c" || { err "容器 $name 不存在"; return 1; }
      docker logs --tail 100 "$name" 2>&1 | sed 's/^/        /' ;;
    remove)
      if ! container_exists "$c"; then warn "容器 $name 不存在，无需移除"; return 0; fi
      run docker rm -f "$name" >/dev/null || { err "移除容器失败"; return 1; }
      ok "已移除容器 $name"
      info "数据卷 $(volume_name "$c") 已保留（要彻底删除：docker volume rm $(volume_name "$c")）"
      ;;
    *) return 1 ;;
  esac
}

# ---------- PostgreSQL ----------
detect_postgresql() {
  if container_exists postgresql || container_running postgresql; then return 0; fi
  have psql || have pg_ctl || have postgres
}

# pg_docker_args 只负责「随机密码 + 拼选项」，结果放进全局 DOCKER_ARGS。
pg_docker_args() {
  local vol; vol="$(volume_name postgresql)"
  if [ -n "${PG_PASSWORD:-}" ]; then
    info "使用自定义 PG_PASSWORD"
  else
    PG_PASSWORD="$(rand_password)"
    info "未提供 PG_PASSWORD，已随机生成（见末尾汇总）"
  fi
  DOCKER_ARGS=(-p "${PG_PORT}:5432" -e "POSTGRES_PASSWORD=${PG_PASSWORD}" -e "POSTGRES_USER=${PG_USER}"
               -v "${vol}:/var/lib/postgresql/data" --health-cmd "pg_isready -U ${PG_USER}"
               --health-interval 10s --health-timeout 5s --health-retries 10)
}

install_postgresql() {
  local mode; mode="$(mode_of postgresql)"
  if [ "$mode" = "native" ]; then
    require_native_supported postgresql || return 1
    if detect_postgresql && [ "$FORCE" != "1" ]; then
      ok "已安装：$( (have psql && psql --version) || echo 'postgresql')"
      return 0
    fi
    ensure_prereqs || return 1
    case "$PKG" in
      apt)     pkg_install "postgresql-${PG_VERSION}" || pkg_install postgresql || return 1 ;;
      dnf|yum) pkg_install "postgresql-server" || return 1 ;;
      *) return 1 ;;
    esac
    if [ "$PKG" != "apt" ] && [ "$DRY_RUN" != "1" ]; then
      run su - postgres -c "/usr/bin/initdb -D /var/lib/pgsql/data" 2>/dev/null || true
    fi
    dry_run_note "/etc/postgresql 与 /var/lib/postgresql" && return 0
    unit_start postgresql "PostgreSQL" || return 1
    ok "PostgreSQL $(psql --version 2>/dev/null | awk '{print $3}')（数据目录 /var/lib/postgresql）"
    warn "原生方式不会自动改 pg_hba.conf，远程连接请自行按需配置"
    return 0
  fi

  if container_running postgresql && [ "$FORCE" != "1" ]; then
    ok "已安装：容器 $(container_name postgresql) 在运行"
    return 0
  fi
  pg_docker_args
  container_apply postgresql "${PG_PORT}" "postgres:${PG_VERSION}" "${DOCKER_ARGS[@]}"
}

# ---------- MySQL ----------
detect_mysql() {
  if container_exists mysql || container_running mysql; then return 0; fi
  have mysql || have mysqld
}

install_mysql() {
  local mode; mode="$(mode_of mysql)"
  if [ "$mode" = "native" ]; then
    require_native_supported mysql || return 1
    if detect_mysql && [ "$FORCE" != "1" ]; then
      ok "已安装：$( (have mysql && mysql --version) || echo mysql)"
      return 0
    fi
    ensure_prereqs || return 1
    case "$PKG" in
      apt)     pkg_install "mysql-server" || pkg_install "mariadb-server" || return 1 ;;
      dnf|yum) pkg_install "mysql-server" || pkg_install "mariadb-server" || return 1 ;;
      *) return 1 ;;
    esac
    dry_run_note "/var/lib/mysql" && return 0
    unit_start mysql "MySQL" || unit_start mariadb "MariaDB" || return 1
    ok "MySQL 已装（数据目录 /var/lib/mysql）"
    warn "原生方式 root 密码由发行版初始化决定，需要时执行 mysql_secure_installation"
    return 0
  fi

  if container_running mysql && [ "$FORCE" != "1" ]; then
    ok "已安装：容器 $(container_name mysql) 在运行"
    return 0
  fi
  local vol; vol="$(volume_name mysql)"
  if [ -n "${MYSQL_ROOT_PASSWORD:-}" ]; then
    info "使用自定义 MYSQL_ROOT_PASSWORD"
  else
    MYSQL_ROOT_PASSWORD="$(rand_password)"
    info "未提供 MYSQL_ROOT_PASSWORD，已随机生成（见末尾汇总）"
  fi
  container_apply mysql "${MYSQL_PORT}" "mysql:${MYSQL_VERSION}" \
    -p "${MYSQL_PORT}:3306" -e "MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD}" \
    -v "${vol}:/var/lib/mysql" --health-cmd "mysqladmin ping -h 127.0.0.1" \
    --health-interval 10s --health-timeout 5s --health-retries 10 \
    -- "mysqld" "--character-set-server=utf8mb4" "--collation-server=utf8mb4_unicode_ci"
}

# ---------- Redis ----------
detect_redis() {
  if container_exists redis || container_running redis; then return 0; fi
  have redis-server || have redis-cli
}

install_redis() {
  local mode; mode="$(mode_of redis)"
  if [ "$mode" = "native" ]; then
    require_native_supported redis || return 1
    if detect_redis && [ "$FORCE" != "1" ]; then
      ok "已安装：$(redis-server --version 2>/dev/null | head -1)"
      return 0
    fi
    ensure_prereqs || return 1
    case "$PKG" in
      apt)     pkg_install redis-server || return 1 ;;
      dnf|yum) pkg_install redis || return 1 ;;
      *) return 1 ;;
    esac
    if [ -n "${REDIS_PASSWORD:-}" ] && [ "$DRY_RUN" != "1" ]; then
      local conf=/etc/redis/redis.conf
      [ -f "$conf" ] || conf=/etc/redis.conf
      if grep -q '^# *requirepass' "$conf" 2>/dev/null; then
        sed -i "s|^# *requirepass.*|requirepass ${REDIS_PASSWORD}|" "$conf"
        ok "已写入 requirepass 到 $conf"
      else
        warn "$conf 里没有可替换的 requirepass 行，请手动设置密码"
      fi
    fi
    dry_run_note "/var/lib/redis" && return 0
    unit_start redis-server "Redis" || unit_start redis "Redis" || return 1
    ok "Redis 已装（数据目录 /var/lib/redis）"
    return 0
  fi

  if container_running redis && [ "$FORCE" != "1" ]; then
    ok "已安装：容器 $(container_name redis) 在运行"
    return 0
  fi
  local vol; vol="$(volume_name redis)"
  local args=()
  if [ -n "${REDIS_PASSWORD:-}" ]; then
    info "启用 requirepass"
    args=(redis-server --appendonly yes --requirepass "${REDIS_PASSWORD}")
  else
    info "未设置 REDIS_PASSWORD：Redis 将无密码启动（内网自用可接受，公网必须设密码）"
    args=(redis-server --appendonly yes)
  fi
  container_apply redis "${REDIS_PORT}" "redis:${REDIS_VERSION}" \
    -p "${REDIS_PORT}:6379" -v "${vol}:/data" \
    --health-cmd "redis-cli ping" --health-interval 10s --health-timeout 5s --health-retries 10 \
    -- "${args[@]}"
}

# ---------- Elasticsearch ----------
detect_elasticsearch() {
  if container_exists elasticsearch || container_running elasticsearch; then return 0; fi
  have elasticsearch
}

install_elasticsearch() {
  local mode; mode="$(mode_of elasticsearch)"
  if [ "$mode" = "native" ]; then
    require_native_supported elasticsearch || return 1
  fi

  if container_running elasticsearch && [ "$FORCE" != "1" ]; then
    ok "已安装：容器 $(container_name elasticsearch) 在运行"
    return 0
  fi
  if ! have docker && [ "$DRY_RUN" != "1" ]; then
    err "elasticsearch 需要 docker（native 方式不支持）：先装 docker 组件"
    return 1
  fi
  local vol; vol="$(volume_name elasticsearch)"

  # vm.max_map_count 是 ES 的硬性要求，默认值 65530 会让它起不来。
  # 这里**临时**提高（不写 /etc/sysctl.d，遵循「保守修改」）；重启后失效，
  # 所以把持久化命令打印出来让使用者自己决定。
  local current_mmap=0
  [ -r /proc/sys/vm/max_map_count ] && current_mmap="$(cat /proc/sys/vm/max_map_count)"
  if [ "$current_mmap" -lt 262144 ] 2>/dev/null; then
    warn "vm.max_map_count=$current_mmap < 262144，ES 会启动失败；本次先临时提高"
    run sysctl -w vm.max_map_count=262144 >/dev/null || warn "sysctl -w 失败，可能需要手动执行"
    info "持久化（重启仍生效）：echo 'vm.max_map_count=262144' > /etc/sysctl.d/99-elasticsearch.conf && sysctl --system"
  fi

  # 单节点 + 关安全：内网自用。开放公网前必须开 xpack.security 并改密码。
  container_apply elasticsearch "${ES_PORT}" "docker.elastic.co/elasticsearch/elasticsearch:${ES_VERSION}" \
    -p "${ES_PORT}:9200" -p "${ES_TRANSPORT_PORT}:9300" \
    -e "discovery.type=single-node" \
    -e "xpack.security.enabled=false" \
    -e "ES_JAVA_OPTS=${ES_JAVA_OPTS}" \
    -e "bootstrap.memory_lock=true" \
    -v "${vol}:/usr/share/elasticsearch/data" \
    --ulimit memlock=-1:-1 \
    --health-cmd "curl -fsS http://localhost:9200/_cluster/health" \
    --health-interval 15s --health-timeout 5s --health-retries 20
  info "单节点 + 关闭安全（内网自用）。公网暴露前请开启 xpack.security 并设置密码"
}

# ---------- 组件表 ----------
component_name() {
  case "$1" in
    docker)        echo "Docker（容器运行时）" ;;
    postgresql)    echo "PostgreSQL" ;;
    mysql)         echo "MySQL" ;;
    redis)         echo "Redis" ;;
    elasticsearch) echo "Elasticsearch" ;;
  esac
}
component_detect() {
  case "$1" in
    docker) detect_docker ;; postgresql) detect_postgresql ;; mysql) detect_mysql ;;
    redis) detect_redis ;; elasticsearch) detect_elasticsearch ;;
    *) return 1 ;;
  esac
}
component_running() {
  case "$1" in
    docker) docker_ready ;;
    postgresql) container_running postgresql || { systemd_available && systemctl is-active --quiet postgresql; } ;;
    mysql)      container_running mysql      || { systemd_available && { systemctl is-active --quiet mysql || systemctl is-active --quiet mariadb; }; } ;;
    redis)      container_running redis      || { systemd_available && systemctl is-active --quiet redis-server; } ;;
    elasticsearch) container_running elasticsearch ;;
    *) return 1 ;;
  esac
}
component_version() {
  case "$1" in
    docker) have docker && docker --version 2>/dev/null ;;
    postgresql)
      if container_running postgresql; then
        docker exec "$(container_name postgresql)" postgres --version 2>/dev/null
      else have psql && psql --version 2>/dev/null; fi ;;
    mysql)
      if container_running mysql; then
        docker exec "$(container_name mysql)" mysql --version 2>/dev/null
      else have mysql && mysql --version 2>/dev/null; fi ;;
    redis)
      if container_running redis; then
        docker exec "$(container_name redis)" redis-server --version 2>/dev/null | head -1
      else have redis-server && redis-server --version 2>/dev/null | head -1; fi ;;
    elasticsearch)
      if container_running elasticsearch; then
        docker inspect -f '{{.Config.Image}}' "$(container_name elasticsearch)" 2>/dev/null
      else have elasticsearch && elasticsearch --version 2>/dev/null | head -1; fi ;;
  esac
}
component_path() {
  case "$1" in
    docker) have docker && command -v docker ;;
    postgresql)
      if container_exists postgresql; then echo "container:$(container_name postgresql)  vol:$(volume_name postgresql)  port:${PG_PORT}"
      else echo "/var/lib/postgresql"; fi ;;
    mysql)
      if container_exists mysql; then echo "container:$(container_name mysql)  vol:$(volume_name mysql)  port:${MYSQL_PORT}"
      else echo "/var/lib/mysql"; fi ;;
    redis)
      if container_exists redis; then echo "container:$(container_name redis)  vol:$(volume_name redis)  port:${REDIS_PORT}"
      else echo "/var/lib/redis"; fi ;;
    elasticsearch)
      if container_exists elasticsearch; then echo "container:$(container_name elasticsearch)  vol:$(volume_name elasticsearch)  port:${ES_PORT}"
      else echo "${ES_DATA_DIR}"; fi ;;
  esac
}
component_install() {
  case "$1" in
    docker) install_docker_native ;;
    postgresql) install_postgresql ;;
    mysql) install_mysql ;;
    redis) install_redis ;;
    elasticsearch) install_elasticsearch ;;
    *) err "未知中间件：$1"; return 1 ;;
  esac
}
component_action() { # component_action <动作> <组件>
  local act="$1" c="$2"
  # docker 本体没有容器，动作直接打到 systemd
  if [ "$c" = "docker" ]; then docker_action "$act"; return $?; fi
  # 装了容器就用容器动作；否则退回 systemd（原生方式），再不行就报错。
  if container_exists "$c"; then container_action "$act" "$c"; return $?; fi
  case "$act" in
    logs)
      case "$c" in
        postgresql) have journalctl && journalctl -u postgresql -n 100 --no-pager ;;
        mysql)      have journalctl && journalctl -u mysql -n 100 --no-pager ;;
        redis)      have journalctl && journalctl -u redis-server -n 100 --no-pager ;;
        *) err "$c 不是容器方式装的，没有容器日志"; return 1 ;;
      esac ;;
    upgrade) err "$c 是原生方式装的，请用发行版包管理器升级（apt upgrade / dnf upgrade）"; return 1 ;;
    start|stop|restart)
      case "$c" in
        postgresql) if [ "$act" = stop ]; then unit_stop postgresql PostgreSQL; else unit_start postgresql PostgreSQL; fi ;;
        mysql)      if [ "$act" = stop ]; then unit_stop mysql MySQL; else unit_start mysql MySQL; fi ;;
        redis)      if [ "$act" = stop ]; then unit_stop redis-server Redis; else unit_start redis-server Redis; fi ;;
        *) err "$c 没有找到容器，也不支持该原生动作"; return 1 ;;
      esac ;;
    remove)
      err "$c 是原生方式装的，脚本不替使用者卸载系统包"
      info "确需卸载：apt remove --purge $c（或 dnf remove $c）；数据目录不会被删除"
      return 1 ;;
    *) return 1 ;;
  esac
}

# ---------- 选择组件 ----------
in_list() { # in_list <needle> <space separated list>
  case " $2 " in *" $1 "*) return 0 ;; *) return 1 ;; esac
}

SELECTED=""
if [ -n "$ONLY" ]; then
  if [ "$ONLY" = "all" ]; then ONLY="$ALL_COMPONENTS"; fi
  for c in ${ONLY//,/ }; do
    in_list "$c" "$ALL_COMPONENTS" || { err "--only 里的未知中间件：$c（可选：$ALL_COMPONENTS）"; exit 2; }
    SELECTED="$SELECTED $c"
  done
else
  SELECTED="$ALL_COMPONENTS"
fi
SELECTED="$(echo "$SELECTED" | xargs)"

if [ "$LIST_ONLY" = "1" ]; then
  printf '\n%-14s %-26s %-8s %-8s %-34s %s\n' "中间件" "名称" "已装" "在跑" "版本" "位置"
  for c in $ALL_COMPONENTS; do
    if component_detect "$c"; then s="是"; else s="否"; fi
    if component_running "$c"; then r="是"; else r="否"; fi
    printf '%-14s %-26s %-8s %-8s %-34s %s\n' "$c" "$(component_name "$c")" "$s" "$r" \
      "$(component_version "$c" 2>/dev/null | head -1)" "$(component_path "$c")"
  done
  exit 0
fi

section "中间件 $ACTION $SCRIPT_VERSION"
info "系统：$OS_ID $OS_VER / $ARCH    包管理器：$PKG"
info "中间件：$SELECTED"
info "方式：${MODE_OVERRIDE:-按各中间件默认（docker 优先）}    镜像：$MIRROR$([ "$DRY_RUN" = "1" ] && printf '    模式：dry-run（不做任何修改）')"
[ "$PKG" = "unknown" ] && { err "不支持的发行版：$OS_ID"; exit 1; }

FAILED=""
RESULTS=""
for c in $SELECTED; do
  section "[$c] $(component_name "$c")"
  okflag=0
  case "$ACTION" in
    install)
      if component_install "$c"; then okflag=1; fi ;;
    *)
      if component_action "$ACTION" "$c"; then okflag=1; fi ;;
  esac
  if [ "$okflag" = "1" ]; then
    RESULTS="$RESULTS$c|ok|$(component_version "$c" 2>/dev/null | head -1)|$(component_path "$c")\n"
  else
    RESULTS="$RESULTS$c|failed||\n"
    FAILED="$FAILED $c"
  fi
done

section "汇总（做了什么 / 装到哪）"
printf '%-14s %-8s %-34s %s\n' "中间件" "结果" "版本" "位置"
printf '%b' "$RESULTS" | while IFS='|' read -r c s v p; do
  [ -z "$c" ] && continue
  if [ "$s" = "ok" ]; then
    [ "$DRY_RUN" = "1" ] && v="(dry-run，未变更)"
    printf '%-14s %-8s %-34s %s\n' "$c" "成功" "$v" "$p"
  else
    printf '%-14s %-8s %-34s %s\n' "$c" "失败" "-" "-"
  fi
done

if [ -n "$FAILED" ]; then
  err "以下中间件处理失败：$FAILED"
  info "可只重试失败的：bash $0 --action $ACTION --only $(echo $FAILED | tr ' ' ',')"
  info "若要单独给某一项换方式：--mode-of <组件>=native"
  exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
  ok "预演结束：上面是将会执行的动作，未做任何修改"
  exit 0
fi

ok "全部完成。默认 --restart unless-stopped，服务器重启后容器会自动起来。"
info "回平台侧边栏「环境自检」点「重新检测」查看结果。"

# 连接信息放在最后：PG_PASSWORD / MYSQL_ROOT_PASSWORD 是各 installer 在随机生成时
# 赋给全局变量的，放在安装循环之前打印会永远是空的。
# 命令回显里密码是 ***，这里是唯一一次明文出现 —— 所以要明确提示。
if [ "$ACTION" = "install" ]; then
  HOSTIP="$(hostname -I 2>/dev/null | awk '{print $1}')"
  section "连接信息（密码请立即保存；命令回显里的 *** 就是它）"
  case " $SELECTED " in *" postgresql "*)
    info "PostgreSQL    host:${HOSTIP:-<服务器IP>}  port:${PG_PORT}  user:${PG_USER}  password:${PG_PASSWORD:-（未生成）}" ;;
  esac
  case " $SELECTED " in *" mysql "*)
    info "MySQL         host:${HOSTIP:-<服务器IP>}  port:${MYSQL_PORT}  user:root  password:${MYSQL_ROOT_PASSWORD:-（未生成）}" ;;
  esac
  case " $SELECTED " in *" redis "*)
    info "Redis         host:${HOSTIP:-<服务器IP>}  port:${REDIS_PORT}  password:${REDIS_PASSWORD:-（无密码）}" ;;
  esac
  case " $SELECTED " in *" elasticsearch "*)
    info "Elasticsearch http://${HOSTIP:-<服务器IP>}:${ES_PORT}  （安全已关闭，内网自用）" ;;
  esac
  info "容器名前缀 $CONTAINER_PREFIX、数据卷前缀 $VOLUME_PREFIX；密码也可从容器环境变量回看："
  info "  docker inspect deploy-mw-<组件> --format '{{.Config.Env}}'"
fi

exit 0
