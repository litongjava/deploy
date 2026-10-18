#!/usr/bin/env bash
# 4. 发布产物与外部配置
# app.properties 与 my.txt 各写一份：不同版本的 tio-boot 读的外部配置文件名不一致，
# 两份都放可以避免「配置没生效、进程偷偷监听默认端口」这种最难查的情况。
# 进程的工作目录（systemd WorkingDirectory）必须与发布目录一致，外部配置才生效。
set -e

WORKDIR="${WORKDIR:-/srv/worksapce/tio-websockify}"
APP_DIR=/srv/apps/tio-websockify
PORT="${APP_PORT:-10063}"
VNC_SERVER="${VNC_SERVER:-127.0.0.1:5901}"

JAR=$(ls -t "$WORKDIR"/target/tio-websockify-*.jar 2>/dev/null | grep -vE -- '-(sources|javadoc)\.jar$' | head -1)
if [ -z "$JAR" ]; then
  echo '未找到构建产物，请先执行「2. Maven 构建生产包」'
  exit 1
fi

mkdir -p "$APP_DIR/logs"
cp -f "$JAR" "$APP_DIR/app.jar"

for f in app.properties my.txt; do
  {
    printf 'server.port=%s\n' "$PORT"
    printf 'vnc-server=%s\n' "$VNC_SERVER"
  } > "$APP_DIR/$f"
done

echo "来源 jar : $JAR"
echo "发布 jar : $APP_DIR/app.jar ($(du -h "$APP_DIR/app.jar" | cut -f1))"
echo "监听端口 : $PORT"
echo "VNC 后端 : $VNC_SERVER"
cat "$APP_DIR/app.properties"
ls -la "$APP_DIR"