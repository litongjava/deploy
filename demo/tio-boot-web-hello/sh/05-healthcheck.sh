#!/usr/bin/env bash
# 5. 健康检查（决定本次部署成败）
# 判据是「应用真的应答了」，不是「systemctl 说 active」。
set -e

PORT="${APP_PORT:-10062}"
APP_DIR=/srv/apps/tio-boot-web-hello
BASE="http://127.0.0.1:$PORT"

echo '=== 等待端口起来 ==='
CODE=000
UP=0
for _ in $(seq 1 30); do
  CODE=$(curl -s -o /tmp/tbwh-body -w '%{http_code}' "$BASE/" || true)
  if [ "$CODE" = "200" ]; then UP=1; break; fi
  sleep 2
done

echo "GET / -> HTTP $CODE"
echo -n '响应体: '
cat /tmp/tbwh-body 2>/dev/null || true
echo
echo '=== 响应头 ==='
curl -s -D - -o /dev/null "$BASE/" | head -8 || true

echo '=== 启动日志（前 30 行）==='
head -30 "$APP_DIR/logs/startup.log" 2>/dev/null || true

if [ "$UP" != "1" ]; then
  echo '健康检查失败'
  echo '--- startup.log 尾部 ---'; tail -60 "$APP_DIR/logs/startup.log" 2>/dev/null || true
  echo '--- startup-error.log 尾部 ---'; tail -40 "$APP_DIR/logs/startup-error.log" 2>/dev/null || true
  exit 1
fi
echo '健康检查通过'
