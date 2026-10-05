#!/usr/bin/env bash
# 部署平台「环境工具链一键安装」脚本。
#
# 这个文件被 go:embed 进 deploy-server 二进制（见 internal/toolchain/toolchain.go），
# 页面上点「一键安装」时由平台把它落盘到 <dataDir>/toolchain/install.sh，
# 再以普通 Run 的形式执行 —— 所以日志、取消、重跑、下载都复用平台既有机制。
# 也可以脱离平台手动跑：
#
#   bash install.sh                      # 装全部（已装的跳过）
#   bash install.sh --only java,maven    # 只装指定组件
#   bash install.sh --skip rust          # 装除 rust 之外的全部
#   bash install.sh --dry-run            # 只打印将要做什么，不做任何修改
#   bash install.sh --list               # 只列出组件与检测结果
#
# 组件：cc(gcc/g++/make) java maven nvm node pnpm go rust
#
# 设计原则：
#   1. 幂等：已装好的默认跳过（--force 强制重装），重复点「一键安装」不会把环境搞乱。
#   2. 一个组件失败不影响其它组件：逐个执行、最后汇总，退出码反映是否有失败。
#   3. 默认走国内镜像（--mirror off 关闭），交付现场直连 github/nodejs.org 往往很慢。
#   4. 只写 /usr/local、/etc/profile.d 和工具自己的配置；不碰 apt 源，不覆盖已有配置。
set -uo pipefail

SCRIPT_VERSION="1.0.1"
ALL_COMPONENTS="cc java maven nvm node pnpm go rust"
NODE_MIRROR_CN="https://npmmirror.com/mirrors/node"

# ---------- 默认参数（同名环境变量可覆盖） ----------
JAVA_VERSION="${JAVA_VERSION:-17}"
NVM_VERSION="${NVM_VERSION:-v0.40.1}"
NODE_VERSION="${NODE_VERSION:---lts}"
PNPM_VERSION="${PNPM_VERSION:-latest}"
GO_VERSION="${GO_VERSION:-1.24.2}"
RUST_TOOLCHAIN="${RUST_TOOLCHAIN:-stable}"
MIRROR="${MIRROR:-cn}"
GO_DIR="${GO_DIR:-/usr/local/go}"
NVM_DIR="${NVM_DIR:-/usr/local/nvm}"
RUSTUP_HOME="${RUSTUP_HOME:-/usr/local/rustup}"
CARGO_HOME="${CARGO_HOME:-/usr/local/cargo}"

ONLY=""; SKIP=""; DRY_RUN=0; FORCE=0; LIST_ONLY=0

if [ -t 1 ]; then C_HEAD=$'\033[1;36m'; C_OK=$'\033[1;32m'; C_WARN=$'\033[1;33m'; C_ERR=$'\033[1;31m'; C_OFF=$'\033[0m'
else C_HEAD=""; C_OK=""; C_WARN=""; C_ERR=""; C_OFF=""; fi

section() { printf '\n%s==> %s%s\n' "$C_HEAD" "$*" "$C_OFF"; }
info() { printf '    %s\n' "$*"; }
ok()   { printf '%s    ✓ %s%s\n' "$C_OK" "$*" "$C_OFF"; }
warn() { printf '%s    ! %s%s\n' "$C_WARN" "$*" "$C_OFF"; }
err()  { printf '%s    ✗ %s%s\n' "$C_ERR" "$*" "$C_OFF"; }
have() { command -v "$1" >/dev/null 2>&1; }
# login shell 里有没有这个命令（nvm 装的 node 只在 login shell 可见）
have_login() { [ -n "$(bash -lc "command -v $1" 2>/dev/null)" ]; }

# run 是唯一执行「有副作用」动作的入口：dry-run 时只打印。
run() {
  if [ "$DRY_RUN" = "1" ]; then printf '    [dry-run] %s\n' "$*"; return 0; fi
  "$@"
}

usage() {
  cat <<'EOF'
部署平台「环境工具链一键安装」

用法：bash install.sh [选项]

  --only a,b        只安装这些组件（cc java maven nvm node pnpm go rust）
  --skip a,b        跳过这些组件
  --mirror cn|off   是否使用国内镜像（默认 cn）
  --java 17         JDK 大版本（默认 17，用发行版的 openjdk 包）
  --node --lts|20   Node 版本（默认 --lts，经 nvm 安装）
  --go 1.24.2       Go 版本
  --rust stable     Rust toolchain
  --dry-run         预演：只打印将要做的事，不做任何修改
  --force           已安装的组件也重装
  --list            只列出组件与检测结果
  -h, --help        显示本帮助
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --only)    ONLY="${2:-}"; shift 2 ;;
    --skip)    SKIP="${2:-}"; shift 2 ;;
    --mirror)  MIRROR="${2:-cn}"; shift 2 ;;
    --java)    JAVA_VERSION="${2:-17}"; shift 2 ;;
    --node)    NODE_VERSION="${2:---lts}"; shift 2 ;;
    --go)      GO_VERSION="${2:-}"; shift 2 ;;
    --rust)    RUST_TOOLCHAIN="${2:-stable}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --force)   FORCE=1; shift ;;
    --list)    LIST_ONLY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) err "未知参数：$1"; usage; exit 2 ;;
  esac
done

# ---------- 平台识别 ----------
if [ "$(id -u)" != "0" ]; then
  if have sudo; then exec sudo -E bash "$0" "$@"; fi
  err "需要 root 权限（要写 /usr/local、/etc/profile.d 并调用包管理器）"; exit 1
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
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  armv7l) GOARCH=armv6l ;;
  *) GOARCH="$ARCH" ;;
esac

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
  for t in curl git tar; do have "$t" || need+=("$t"); done
  if [ ${#need[@]} -gt 0 ]; then
    section "安装基础依赖：${need[*]}"
    case "$PKG" in
      apt)     pkg_install curl git ca-certificates tar xz-utils || return 1 ;;
      dnf|yum) pkg_install curl git ca-certificates tar xz || return 1 ;;
      *)       return 1 ;;
    esac
  fi
  have xz || pkg_install xz-utils >/dev/null 2>&1 || true
  return 0
}

profile_write() { # profile_write <文件名> <内容>
  local f="/etc/profile.d/$1"
  if [ "$DRY_RUN" = "1" ]; then info "[dry-run] 写 $f"; return 0; fi
  printf '%s\n' "$2" > "$f"
  chmod 0644 "$f"
}

# require_installed <检测函数> <组件名>：装完后的自检。
# dry-run 下什么都没装，跳过验证（否则会被误判成「安装失败」）。
require_installed() {
  if [ "$DRY_RUN" = "1" ]; then return 0; fi
  if ! "$1"; then err "$2 安装后仍不可用"; return 1; fi
  return 0
}

# dry_run_note <目标路径>：dry-run 时只报「将安装到哪」。
# 否则 `gcc -dumpversion` 这类命令会去打机器上**已有**的版本（甚至 command not found），
# 让预演结果看起来像是真的装过。
dry_run_note() {
  if [ "$DRY_RUN" = "1" ]; then ok "预演：将安装到 $1"; return 0; fi
  return 1
}

# ---------- C 工具链 ----------
detect_cc() { have gcc && have make; }
install_cc() {
  if detect_cc && [ "$FORCE" != "1" ] && [ -z "${CC_EXTRA_PACKAGES:-}" ]; then
    ok "已安装：gcc $(gcc -dumpversion) / make $(make -v 2>/dev/null | head -1 | awk '{print $3}')（路径 $(command -v gcc)）"
    return 0
  fi
  case "$PKG" in
    apt)     pkg_install build-essential pkg-config libssl-dev || return 1 ;;
    dnf|yum) pkg_install gcc gcc-c++ make pkgconfig openssl-devel || return 1 ;;
    *)       return 1 ;;
  esac
  require_installed detect_cc "gcc/make" || return 1
  if [ -n "${CC_EXTRA_PACKAGES:-}" ]; then
    info "额外安装：$CC_EXTRA_PACKAGES"
    # 这里就是要把参数按空格拆成多个包名
    pkg_install $CC_EXTRA_PACKAGES || warn "额外软件包安装失败（不影响基础工具链）"
  fi
  dry_run_note "$(command -v gcc 2>/dev/null || echo /usr/bin/gcc)" && return 0
  ok "gcc $(gcc -dumpversion) / make $(make -v | head -1 | awk '{print $3}') → $(command -v gcc)"
}

# ---------- Java ----------
detect_java() { have java; }

# 用压缩包安装（JDK 8 这类 apt 里没有的版本走这条）
install_java_tarball() {
  local dir="${JAVA_DIR:-/usr/local/jdk}" tmp="/tmp/jdk.tar.gz"
  ensure_prereqs || return 1
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] curl -fL $JDK_URL -o $tmp && tar -xf $tmp -C $dir --strip-components=1（JAVA_HOME=$dir）"
    return 0
  fi
  info "下载 $JDK_URL"
  curl -fL --connect-timeout 20 --retry 2 -o "$tmp" "$JDK_URL" || { err "JDK 压缩包下载失败"; return 1; }
  rm -rf "$dir" && mkdir -p "$dir"
  if ! tar -xf "$tmp" -C "$dir" --strip-components=1; then
    err "解压失败（确认是 .tar.gz 且带一层顶层目录）"; rm -f "$tmp"; return 1
  fi
  rm -f "$tmp"
  [ -x "$dir/bin/java" ] || { err "解压后没有找到 $dir/bin/java（检查压缩包结构）"; return 1; }
  profile_write "java.sh" "# Java（由平台「工具链一键安装」写入，压缩包方式）
export JAVA_HOME=$dir
export PATH=\$JAVA_HOME/bin:\$PATH"
  for b in java javac jar javap keytool; do
    [ -x "$dir/bin/$b" ] && ln -sf "$dir/bin/$b" "/usr/local/bin/$b"
  done
  PATH="$dir/bin:$PATH"
  ok "$(java -version 2>&1 | head -1) → $dir（JAVA_HOME，软链在 /usr/local/bin/java）"
}

install_java() {
  if detect_java && [ "$FORCE" != "1" ] && [ -z "${JDK_URL:-}" ]; then
    ok "已安装：$(java -version 2>&1 | head -1)（路径 $(command -v java)）"
    return 0
  fi
  if [ -n "${JDK_URL:-}" ]; then
    install_java_tarball || return 1
    return 0
  fi
  case "$PKG" in
    apt) pkg_install "openjdk-${JAVA_VERSION}-jdk-headless" || {
           err "apt 里没有 openjdk-${JAVA_VERSION}-jdk-headless。可用的版本："
           apt-cache search --names-only '^openjdk-[0-9]+-jdk' 2>/dev/null | awk '{print "        "$1}' | sort -u
           return 1
         } ;;
    dnf|yum) pkg_install "java-${JAVA_VERSION}-openjdk-devel" || return 1 ;;
    *) return 1 ;;
  esac
  require_installed detect_java "java" || return 1
  local jh
  jh="$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")"
  profile_write "java.sh" "# Java（由平台「工具链一键安装」写入）
export JAVA_HOME=$jh
export PATH=\$JAVA_HOME/bin:\$PATH"
  dry_run_note "$jh（JAVA_HOME）" && return 0
  ok "$(java -version 2>&1 | head -1) → $jh（JAVA_HOME）"
}

# ---------- Maven ----------
detect_maven() { have mvn; }

install_maven_tarball() {
  local dir="${MAVEN_DIR:-/usr/local/maven}" tmp="/tmp/maven.tar.gz"
  ensure_prereqs || return 1
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] curl -fL $MAVEN_URL -o $tmp && tar -xf $tmp -C $dir --strip-components=1"
    return 0
  fi
  info "下载 $MAVEN_URL"
  curl -fL --connect-timeout 20 --retry 2 -o "$tmp" "$MAVEN_URL" || { err "Maven 压缩包下载失败"; return 1; }
  rm -rf "$dir" && mkdir -p "$dir"
  if ! tar -xf "$tmp" -C "$dir" --strip-components=1; then
    err "解压失败（确认是 apache-maven-*-bin.tar.gz）"; rm -f "$tmp"; return 1
  fi
  rm -f "$tmp"
  [ -x "$dir/bin/mvn" ] || { err "解压后没有 $dir/bin/mvn"; return 1; }
  ln -sf "$dir/bin/mvn" /usr/local/bin/mvn
  profile_write "maven.sh" "# Maven（由平台「工具链一键安装」写入，压缩包方式）
export M2_HOME=$dir
export PATH=$dir/bin:\$PATH"
  ok "$(mvn -v | head -1) → $dir（软链在 /usr/local/bin/mvn）"
}

install_maven() {
  if detect_maven && [ "$FORCE" != "1" ] && [ -z "${MAVEN_URL:-}" ]; then
    ok "已安装：$(mvn -v 2>/dev/null | head -1)（路径 $(command -v mvn)）"
    return 0
  fi
  if [ -n "${MAVEN_URL:-}" ]; then
    install_maven_tarball || return 1
  else
    case "$PKG" in
      apt)     pkg_install maven || return 1 ;;
      dnf|yum) pkg_install maven || return 1 ;;
      *)       return 1 ;;
    esac
    require_installed detect_maven "mvn" || return 1
    dry_run_note "$(command -v mvn 2>/dev/null || echo /usr/bin/mvn)" && return 0
    ok "$(mvn -v | head -1) → $(command -v mvn)"
  fi

  # 国内镜像：只在没有 settings.xml 时写入，绝不覆盖用户已有配置。
  if [ "$MIRROR" = "cn" ]; then
    local m2="$HOME/.m2"
    if [ -f "$m2/settings.xml" ]; then
      warn "已有 $m2/settings.xml，跳过镜像配置（要改请自行合并）"
    else
      if [ "$DRY_RUN" = "1" ]; then
        info "[dry-run] 写 $m2/settings.xml（阿里云镜像）"
      else
        mkdir -p "$m2"
        cat > "$m2/settings.xml" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<settings xmlns="http://maven.apache.org/SETTINGS/1.0.0">
  <mirrors>
    <mirror>
      <id>aliyun</id>
      <name>Aliyun Maven</name>
      <url>https://maven.aliyun.com/repository/public</url>
      <mirrorOf>central</mirrorOf>
    </mirror>
  </mirrors>
</settings>
XML
      fi
      ok "已配置 Maven 阿里云镜像（$m2/settings.xml）"
    fi
  fi
}

# ---------- nvm ----------
detect_nvm() { [ -s "$NVM_DIR/nvm.sh" ]; }
install_nvm() {
  if detect_nvm && [ "$FORCE" != "1" ]; then
    ok "已安装：$NVM_DIR（nvm $(bash -c ". '$NVM_DIR/nvm.sh' && nvm --version" 2>/dev/null)）"
    return 0
  fi
  ensure_prereqs || return 1
  local url="${NVM_GIT_URL:-https://github.com/nvm-sh/nvm.git}"
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] git clone --depth 1 --branch $NVM_VERSION $url $NVM_DIR"
  else
    rm -rf "$NVM_DIR"
    if ! git clone --depth 1 --branch "$NVM_VERSION" "$url" "$NVM_DIR" >/dev/null 2>&1; then
      err "克隆 nvm 失败（$url）；可设 NVM_GIT_URL 指向可达镜像后重试"
      return 1
    fi
  fi
  local body="# nvm（由平台「工具链一键安装」写入）
export NVM_DIR=$NVM_DIR
[ -s \"\$NVM_DIR/nvm.sh\" ] && . \"\$NVM_DIR/nvm.sh\""
  [ "$MIRROR" = "cn" ] && body="$body
export NVM_NODEJS_ORG_MIRROR=$NODE_MIRROR_CN"
  profile_write "nvm.sh" "$body"
  ok "nvm $NVM_VERSION → $NVM_DIR（已加入 /etc/profile.d/nvm.sh）"
}

# ---------- Node（经 nvm） ----------
detect_node() { have_login node; }
install_node() {
  detect_nvm || install_nvm || return 1
  if detect_node && [ "$FORCE" != "1" ]; then
    ok "已安装：node $(bash -lc 'node -v' 2>/dev/null)（nvm 默认版本）"
    return 0
  fi
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] bash -lc 'nvm install $NODE_VERSION && nvm alias default <该版本>'"
    return 0
  fi
  local ver
  ver="$(bash -lc "nvm install '$NODE_VERSION' >/dev/null 2>&1 && nvm current && nvm alias default \"\$(nvm current)\" >/dev/null && echo ok" 2>/dev/null | head -1)"
  if [ -z "$ver" ] || [ "$ver" = "none" ] || [ "$ver" = "system" ]; then
    err "nvm install $NODE_VERSION 失败（可设 NVM_NODEJS_ORG_MIRROR 后重试）"
    return 1
  fi
  # profile.d 里显式 nvm use default：否则 login shell 里未必有 node
  local shim="# nvm（由平台「工具链一键安装」写入）
export NVM_DIR=$NVM_DIR
[ -s \"\$NVM_DIR/nvm.sh\" ] && . \"\$NVM_DIR/nvm.sh\"
nvm use default --silent >/dev/null 2>&1 || true"
  [ "$MIRROR" = "cn" ] && shim="$shim
export NVM_NODEJS_ORG_MIRROR=$NODE_MIRROR_CN"
  profile_write "nvm.sh" "$shim"
  ok "node $ver（nvm default，nvm --version $(bash -lc 'nvm --version' 2>/dev/null)）"
  warn "nvm 装的 node 只在 login shell（平台默认的 bash -lc）可见；项目 Shell 选 sh 时请改用 bash"
}

# ---------- pnpm ----------
detect_pnpm() { have_login pnpm; }
install_pnpm() {
  if detect_pnpm && [ "$FORCE" != "1" ]; then
    ok "已安装：pnpm $(bash -lc 'pnpm -v' 2>/dev/null)"
    return 0
  fi
  detect_node || install_node || return 1
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] corepack enable --install-directory /usr/local/bin && corepack prepare pnpm@$PNPM_VERSION --activate"
    return 0
  fi
  # corepack enable 会在 /usr/local/bin 生成 shim，所以任何 shell 都能找到 pnpm
  bash -lc "corepack enable --install-directory /usr/local/bin >/dev/null 2>&1 || true"
  if ! bash -lc "corepack prepare pnpm@$PNPM_VERSION --activate" >/dev/null 2>&1; then
    warn "corepack 准备 pnpm 失败，退回 npm i -g pnpm"
    bash -lc "npm i -g pnpm@$PNPM_VERSION" >/dev/null 2>&1 || { err "pnpm 安装失败"; return 1; }
  fi
  if [ "$MIRROR" = "cn" ]; then
    bash -lc "pnpm config set registry https://registry.npmmirror.com >/dev/null 2>&1 || true"
    bash -lc "npm config set registry https://registry.npmmirror.com >/dev/null 2>&1 || true"
    ok "已把 npm/pnpm registry 设为 registry.npmmirror.com"
  fi
  ok "pnpm $(bash -lc 'pnpm -v' 2>/dev/null || echo '?')（shim 在 /usr/local/bin/pnpm）"
}

# ---------- Go ----------
detect_go() { have go || [ -x "$GO_DIR/bin/go" ]; }
install_go() {
  if detect_go && [ "$FORCE" != "1" ]; then
    ok "已安装：$( (have go && go version) || "$GO_DIR/bin/go version")"
    return 0
  fi
  ensure_prereqs || return 1
  local file="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
  local urls=()
  # 显式给了下载地址就优先用它（内网镜像 / 指定小版本）
  [ -n "${GO_URL:-}" ] && urls+=("$GO_URL")
  if [ "$MIRROR" = "cn" ]; then
    urls+=("https://mirrors.aliyun.com/golang/$file")
    urls+=("https://golang.google.cn/dl/$file")
  fi
  urls+=("https://go.dev/dl/$file")

  local tmp="/tmp/$file" got=0
  for u in "${urls[@]}"; do
    if [ "$DRY_RUN" = "1" ]; then info "[dry-run] curl -fL $u -o $tmp"; got=1; break; fi
    info "下载 $u"
    if curl -fL --connect-timeout 15 --retry 2 -o "$tmp" "$u"; then got=1; break; fi
    warn "下载失败，换下一个源"
  done
  [ "$got" = "1" ] || { err "Go $GO_VERSION 下载失败（确认该版本/$GOARCH 的包存在）"; return 1; }

  if [ "$DRY_RUN" != "1" ]; then
    rm -rf "$GO_DIR" && mkdir -p "$GO_DIR"
    tar -C "$(dirname "$GO_DIR")" -xzf "$tmp" || { err "解压失败"; return 1; }
    rm -f "$tmp"
    ln -sf "$GO_DIR/bin/go" /usr/local/bin/go
    ln -sf "$GO_DIR/bin/gofmt" /usr/local/bin/gofmt
  fi
  local body="# Go（由平台「工具链一键安装」写入）
export PATH=$GO_DIR/bin:\$PATH"
  if [ "$MIRROR" = "cn" ]; then
    body="$body
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn"
  fi
  profile_write "go.sh" "$body"
  ok "go $("$GO_DIR/bin/go" version 2>/dev/null | awk '{print $3}') → $GO_DIR（软链在 /usr/local/bin/go）"
}

# ---------- Rust ----------
detect_rust() { have rustc || [ -x "$CARGO_HOME/bin/rustc" ]; }
install_rust() {
  if detect_rust && [ "$FORCE" != "1" ]; then
    ok "已安装：$( (have rustc && rustc --version) || "$CARGO_HOME/bin/rustc --version")"
    return 0
  fi
  ensure_prereqs || return 1
  if [ "$DRY_RUN" = "1" ]; then
    info "[dry-run] curl https://sh.rustup.rs | RUSTUP_HOME=$RUSTUP_HOME CARGO_HOME=$CARGO_HOME sh -s -- -y --no-modify-path --profile minimal --default-toolchain $RUST_TOOLCHAIN"
    return 0
  fi
  local init="/tmp/rustup-init.sh"
  if ! curl -fL --connect-timeout 15 --retry 2 -o "$init" https://sh.rustup.rs; then
    err "下载 rustup 安装脚本失败"
    return 1
  fi
  local -a rtenv=(env "RUSTUP_HOME=$RUSTUP_HOME" "CARGO_HOME=$CARGO_HOME")
  if [ "$MIRROR" = "cn" ]; then
    rtenv+=("RUSTUP_DIST_SERVER=https://rsproxy.cn" "RUSTUP_UPDATE_ROOT=https://rsproxy.cn/rustup")
  fi
  if ! "${rtenv[@]}" sh "$init" -y --no-modify-path --profile minimal --default-toolchain "$RUST_TOOLCHAIN" >/dev/null 2>&1; then
    err "rustup 安装失败"; rm -f "$init"; return 1
  fi
  rm -f "$init"
  for b in rustc cargo rustup rustfmt cargo-fmt; do
    [ -x "$CARGO_HOME/bin/$b" ] && ln -sf "$CARGO_HOME/bin/$b" "/usr/local/bin/$b"
  done
  profile_write "rust.sh" "# Rust（由平台「工具链一键安装」写入）
export RUSTUP_HOME=$RUSTUP_HOME
export CARGO_HOME=$CARGO_HOME
export PATH=$CARGO_HOME/bin:\$PATH"

  if [ "$MIRROR" = "cn" ]; then
    if [ -f "$CARGO_HOME/config.toml" ]; then
      warn "已有 $CARGO_HOME/config.toml，跳过 crates 镜像配置"
    else
      cat > "$CARGO_HOME/config.toml" <<'TOML'
# 由平台「工具链一键安装」写入：rsproxy 稀疏索引加速 crates 下载
[source.crates-io]
replace-with = "rsproxy-sparse"

[source.rsproxy-sparse]
registry = "sparse+https://rsproxy.cn/index/"

[registries.rsproxy]
index = "sparse+https://rsproxy.cn/index/"

[net]
git-fetch-with-cli = true
TOML
      ok "已配置 crates 国内镜像（$CARGO_HOME/config.toml）"
    fi
  fi
  ok "$("$CARGO_HOME/bin/rustc" --version 2>/dev/null)（软链在 /usr/local/bin/rustc）"
}

# ---------- 组件表 ----------
component_name() {
  case "$1" in
    cc) echo "C 工具链（gcc/g++/make）" ;;
    java) echo "Java (OpenJDK)" ;;
    maven) echo "Maven" ;;
    nvm) echo "nvm" ;;
    node) echo "Node.js（经 nvm）" ;;
    pnpm) echo "pnpm" ;;
    go) echo "Go" ;;
    rust) echo "Rust（rustup）" ;;
  esac
}
component_detect() {
  case "$1" in
    cc) detect_cc ;; java) detect_java ;; maven) detect_maven ;; nvm) detect_nvm ;;
    node) detect_node ;; pnpm) detect_pnpm ;; go) detect_go ;; rust) detect_rust ;;
    *) return 1 ;;
  esac
}
component_version() {
  case "$1" in
    cc)    have gcc && gcc -dumpversion ;;
    java)  have java && java -version 2>&1 | head -1 ;;
    maven) have mvn && mvn -v 2>/dev/null | head -1 ;;
    nvm)   detect_nvm && bash -c ". '$NVM_DIR/nvm.sh' && nvm --version" ;;
    node)  bash -lc 'command -v node >/dev/null 2>&1 && node -v' ;;
    pnpm)  bash -lc 'command -v pnpm >/dev/null 2>&1 && pnpm -v' ;;
    go)    { have go && go version; } 2>/dev/null || { [ -x "$GO_DIR/bin/go" ] && "$GO_DIR/bin/go" version; } 2>/dev/null ;;
    rust)  { have rustc && rustc --version; } 2>/dev/null || { [ -x "$CARGO_HOME/bin/rustc" ] && "$CARGO_HOME/bin/rustc" --version; } 2>/dev/null ;;
  esac
}
component_path() { # 这个组件装到哪 / 在哪能找到（写进报告，便于回看）
  case "$1" in
    cc)    command -v gcc 2>/dev/null ;;
    java)  if [ -n "${JDK_URL:-}" ]; then echo "${JAVA_DIR:-/usr/local/jdk}"; else command -v java 2>/dev/null; fi ;;
    maven) if [ -n "${MAVEN_URL:-}" ]; then echo "${MAVEN_DIR:-/usr/local/maven}"; else command -v mvn 2>/dev/null; fi ;;
    nvm)   echo "$NVM_DIR" ;;
    node)  bash -lc 'command -v node' 2>/dev/null ;;
    pnpm)  bash -lc 'command -v pnpm' 2>/dev/null ;;
    go)    echo "$GO_DIR" ;;
    rust)  echo "$CARGO_HOME" ;;
  esac
}
component_install() {
  case "$1" in
    cc) install_cc ;; java) install_java ;; maven) install_maven ;; nvm) install_nvm ;;
    node) install_node ;; pnpm) install_pnpm ;; go) install_go ;; rust) install_rust ;;
    *) err "未知组件：$1"; return 1 ;;
  esac
}

# ---------- 选择组件 ----------
# 列表是**空格**分隔的（"cc java maven ..."），所以两边都补空格再匹配；
# 早先写成逗号匹配（*",$1,"*），结果 --only 永远报「未知组件」。
in_list() { # in_list <needle> <space separated list>
  case " $2 " in *" $1 "*) return 0 ;; *) return 1 ;; esac
}

SELECTED=""
if [ -n "$ONLY" ]; then
  for c in ${ONLY//,/ }; do
    in_list "$c" "$ALL_COMPONENTS" || { err "--only 里的未知组件：$c（可选：$ALL_COMPONENTS）"; exit 2; }
    SELECTED="$SELECTED $c"
  done
else
  SELECTED="$ALL_COMPONENTS"
fi
if [ -n "$SKIP" ]; then
  kept=""
  for c in $SELECTED; do in_list "$c" "$SKIP" && continue; kept="$kept $c"; done
  SELECTED="$kept"
fi
SELECTED="$(echo "$SELECTED" | xargs)"

section "环境工具链安装 $SCRIPT_VERSION"
info "系统：$OS_ID $OS_VER / $ARCH    包管理器：$PKG"
info "组件：$SELECTED"
info "镜像：$MIRROR$([ "$DRY_RUN" = "1" ] && printf '    模式：dry-run（不做任何修改）')"
[ "$PKG" = "unknown" ] && { err "不支持的发行版：$OS_ID"; exit 1; }

if [ "$LIST_ONLY" = "1" ]; then
  printf '\n%-6s %-24s %-8s %-30s %s\n' "组件" "名称" "状态" "版本" "位置"
  for c in $ALL_COMPONENTS; do
    if component_detect "$c"; then s="已安装"; else s="缺失"; fi
    printf '%-6s %-24s %-8s %-30s %s\n' "$c" "$(component_name "$c")" "$s" \
      "$(component_version "$c" | head -1)" "$(component_path "$c")"
  done
  exit 0
fi

FAILED=""
RESULTS=""
for c in $SELECTED; do
  section "[$c] $(component_name "$c")"
  if component_install "$c"; then
    # 记录「装了什么版本、装到哪」：这条汇总本身就是安装记录的一部分（运行日志可回看）
    RESULTS="$RESULTS$c|ok|$(component_version "$c" | head -1)|$(component_path "$c")\n"
  else
    RESULTS="$RESULTS$c|failed||\n"
    FAILED="$FAILED $c"
  fi
done

section "汇总（装了什么 / 装到哪）"
printf '%-6s %-8s %-34s %s\n' "组件" "结果" "版本" "位置"
printf '%b' "$RESULTS" | while IFS='|' read -r c s v p; do
  [ -z "$c" ] && continue
  if [ "$s" = "ok" ]; then
    # dry-run 下「版本」是机器上已有的东西，会误导，直接标注
    [ "$DRY_RUN" = "1" ] && v="(dry-run，未变更)"
    printf '%-6s %-8s %-34s %s\n' "$c" "成功" "$v" "$p"
  else
    printf '%-6s %-8s %-34s %s\n' "$c" "失败" "-" "-"
  fi
done

if [ -n "$FAILED" ]; then
  err "以下组件安装失败：$FAILED"
  info "可只重试失败的组件：bash $0 --only $(echo $FAILED | tr ' ' ',')"
  exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
  ok "预演结束：上面是将会执行的动作，未做任何修改"
else
  ok "全部完成。已写入 /etc/profile.d/*.sh，新开的 login shell（平台默认的 bash -lc）即可使用。"
  info "回到平台侧边栏「环境自检」点「重新检测」查看结果。"
fi
exit 0
