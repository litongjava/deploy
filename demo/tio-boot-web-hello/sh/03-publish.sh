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
