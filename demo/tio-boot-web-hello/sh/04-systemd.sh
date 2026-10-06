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
