#!/usr/bin/env bash
# 3. 安装并启动本地 VNC 桌面（TigerVNC + XFCE）
# tio-websockify 的 vnc-server 是必填项：没有真实后端时 /websockify 一连就断。
# 这里在服务器本机装一个 :1 显示，只监听 127.0.0.1，由 systemd 常驻。
set -e

APP_DIR=/srv/apps/tio-websockify
VNC_SERVER="${VNC_SERVER:-127.0.0.1:5901}"
VNC_HOST="${VNC_SERVER%:*}"
VNC_PORT="${VNC_SERVER##*:}"
VNC_PASSWORD="${VNC_PASSWORD:-tio12345}"

echo '=== 安装 VNC / 桌面组件 ==='
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  tigervnc-standalone-server tigervnc-tools \
  xfce4 xfce4-terminal dbus-x11 x11-utils

echo
echo '=== 生成 VNC 密码文件 ==='
install -d -m 0700 "$APP_DIR/vnc" "$APP_DIR/logs"
printf '%s\n' "$VNC_PASSWORD" | tigervncpasswd -f > "$APP_DIR/vnc/passwd"
chmod 0600 "$APP_DIR/vnc/passwd"
ls -l "$APP_DIR/vnc/passwd"

echo
echo '=== 写 XFCE 启动脚本 ==='
cat > /usr/local/bin/tio-websockify-xfce.sh <<'XFCE'
#!/bin/bash
# 等 X 显示就绪再启动 XFCE，逻辑与项目 docker/start-desktop.sh 一致。
set -euo pipefail
for attempt in {1..60}; do
  if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
    exec dbus-run-session -- startxfce4
  fi
  sleep 1
done
echo "VNC display did not become ready." >&2
exit 1
XFCE
chmod 0755 /usr/local/bin/tio-websockify-xfce.sh

echo
echo '=== 注册 VNC systemd 服务 ==='
cat > /etc/systemd/system/tio-websockify-vnc.service <<UNIT
[Unit]
Description=TigerVNC server for tio-websockify (display :1, ${VNC_SERVER})
After=network-online.target

[Service]
Type=simple
User=root
Environment=HOME=/root
ExecStart=/usr/bin/Xtigervnc :1 -geometry 1600x900 -depth 24 -rfbport ${VNC_PORT} -localhost yes -SecurityTypes VncAuth -PasswordFile ${APP_DIR}/vnc/passwd -AlwaysShared
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/logs/vnc.log
StandardError=append:${APP_DIR}/logs/vnc-error.log

[Install]
WantedBy=multi-user.target
UNIT

echo '=== 注册 XFCE systemd 服务 ==='
cat > /etc/systemd/system/tio-websockify-xfce.service <<UNIT
[Unit]
Description=XFCE desktop session for tio-websockify (display :1)
After=tio-websockify-vnc.service
Requires=tio-websockify-vnc.service

[Service]
Type=simple
User=root
Environment=HOME=/root
Environment=DISPLAY=:1
Environment=LANG=C.UTF-8
ExecStart=/usr/local/bin/tio-websockify-xfce.sh
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/logs/xfce.log
StandardError=append:${APP_DIR}/logs/xfce-error.log

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable tio-websockify-vnc tio-websockify-xfce >/dev/null 2>&1 || true
systemctl restart tio-websockify-vnc
systemctl restart tio-websockify-xfce

echo
echo '=== 等待 VNC 监听 ==='
for i in $(seq 1 30); do
  if ss -ltn | grep -qE ":${VNC_PORT}\b"; then
    break
  fi
  sleep 1
done

if ! ss -ltn | grep -qE ":${VNC_PORT}\b"; then
  echo "端口 ${VNC_PORT} 没有监听起来"
  systemctl status tio-websockify-vnc --no-pager -l || true
  tail -40 "$APP_DIR/logs/vnc-error.log" 2>/dev/null || true
  tail -40 "$APP_DIR/logs/vnc.log" 2>/dev/null || true
  exit 1
fi

echo "VNC 服务 : $(systemctl is-active tio-websockify-vnc)"
echo "桌面会话 : $(systemctl is-active tio-websockify-xfce)"
ss -ltnp 2>/dev/null | grep -E ":${VNC_PORT}\b" || true

echo
echo '=== 校验 RFB 握手 ==='
python3 - "$VNC_HOST" "$VNC_PORT" <<'PY'
import socket, sys
host, port = sys.argv[1], int(sys.argv[2])
with socket.create_connection((host, port), timeout=5) as s:
    banner = s.recv(12).decode('ascii', 'replace').strip()
print('  banner:', banner)
if not banner.startswith('RFB '):
    raise SystemExit('unexpected banner: ' + banner)
PY