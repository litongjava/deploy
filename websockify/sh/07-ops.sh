#!/usr/bin/env bash
# 7. 运行状态与日志（运维，只读）
# 单独执行用；不会重新构建、不会重启服务。
set -e

APP_DIR=/srv/apps/tio-websockify
PORT="${APP_PORT:-10063}"
VNC_SERVER="${VNC_SERVER:-127.0.0.1:5901}"
VNC_PORT="${VNC_SERVER##*:}"

echo '=== systemd ==='
for u in tio-websockify tio-websockify-vnc tio-websockify-xfce; do
  printf '  %-26s %s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null || echo unknown)"
done

echo
echo '=== 监听端口 ==='
ss -ltnp 2>/dev/null | grep -E ":(${PORT}|${VNC_PORT})\b" || echo '  未发现目标端口'

echo
echo '=== 发布目录 ==='
ls -la "$APP_DIR"
echo '--- app.properties ---'
cat "$APP_DIR/app.properties" 2>/dev/null || true

echo
echo '=== 探活 ==='
printf '  GET /vnc.html            -> %s\n' "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:${PORT}/vnc.html || echo fail)"
printf '  GET /websocketify/index  -> %s\n' "$(curl -s http://127.0.0.1:${PORT}/websocketify/index || echo fail)"

echo
echo '=== 应用日志尾部 ==='
tail -30 "$APP_DIR/logs/startup.log" 2>/dev/null || true

echo
echo '=== VNC / 桌面日志尾部 ==='
tail -15 "$APP_DIR/logs/vnc-error.log" 2>/dev/null || true
tail -15 "$APP_DIR/logs/xfce-error.log" 2>/dev/null || true