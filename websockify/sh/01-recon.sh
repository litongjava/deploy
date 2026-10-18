#!/usr/bin/env bash
# 1. 环境勘察（只读）
# 目的：动手之前确认主机、工具链、源码提交、目标端口与 VNC 端口是否空闲。
set -e

WORKDIR="${WORKDIR:-/srv/worksapce/tio-websockify}"
PORT="${APP_PORT:-10063}"
VNC_SERVER="${VNC_SERVER:-127.0.0.1:5901}"
VNC_PORT="${VNC_SERVER##*:}"

echo '=== 主机 ==='
hostname
uname -srm
echo "cores: $(nproc)"
free -m | head -2

echo
echo '=== 工具链 ==='
for t in java mvn git curl ss; do
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
grep -nE '<final.name>|<main.class>|<tio-boot.version>' pom.xml || true

echo
echo '=== 目标端口 ==='
for p in "$PORT" "$VNC_PORT"; do
  if ss -ltn | grep -qE ":${p}\b"; then
    echo "  警告：$p 已被占用"
    ss -ltnp 2>/dev/null | grep -E ":${p}\b" || true
  else
    echo "  $p 空闲"
  fi
done
echo -n ' 当前所有监听端口: '
ss -ltn | tail -n +2 | awk '{print $4}' | sed 's/.*://' | sort -n -u | tr '\n' ' '
echo