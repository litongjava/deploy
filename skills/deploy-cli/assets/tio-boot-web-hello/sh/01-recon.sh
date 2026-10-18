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
