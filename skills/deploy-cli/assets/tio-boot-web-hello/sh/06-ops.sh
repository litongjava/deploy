#!/usr/bin/env bash
# 6. 运行状态与日志（运维，用「单独执行」跑，不重新构建）
set -e

PORT="${APP_PORT:-10062}"
APP_DIR=/srv/apps/tio-boot-web-hello

echo '=== systemd 状态 ==='
systemctl status tio-boot-web-hello --no-pager -l | head -12
echo '=== 监听端口 ==='
ss -ltnp 2>/dev/null | grep -E ":${PORT}\b" || echo "$PORT 未监听"
echo '=== 发布目录 ==='
ls -lh "$APP_DIR"
echo '=== 外部配置 ==='
cat "$APP_DIR/app.properties" "$APP_DIR/my.txt"
echo '=== startup.log 尾部 ==='
tail -20 "$APP_DIR/logs/startup.log" 2>/dev/null || true
echo '=== startup-error.log 尾部 ==='
tail -10 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
