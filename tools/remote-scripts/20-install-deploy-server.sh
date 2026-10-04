#!/usr/bin/env bash
# 在服务器上安装并启动 deploy-server。
#
# 前提：调用方已经把这两个文件传上来了
#   /tmp/deploy-server-linux-amd64
#   /tmp/deploy-server.service
#
# 说明：服务以 root 运行。这不是偷懒 —— 它的职责就是执行用户写的部署脚本
# （本机 rsync 到站点目录、reload nginx、重启服务），降权反而会让用户困惑
# 「为什么我写的命令说权限不够」。安全边界靠「谁能登录这个平台」来控制，
# 所以默认只监听内网、并且必须经过登录鉴权。
set -euo pipefail

BIN_SRC=/tmp/deploy-server-linux-amd64
BIN_DST=/usr/local/bin/deploy-server
UNIT_SRC=/tmp/deploy-server.service
UNIT_DST=/etc/systemd/system/deploy-server.service
DATA_DIR=/srv/deploy/data
WS_DIR=/srv/deploy/workspaces

[ -f "$BIN_SRC" ] || { echo "缺少 $BIN_SRC"; exit 1; }
[ -f "$UNIT_SRC" ] || { echo "缺少 $UNIT_SRC"; exit 1; }

echo "=== [1/5] 安装二进制 ==="
install -m 0755 "$BIN_SRC" "$BIN_DST"
"$BIN_DST" --version

echo "=== [2/5] 准备数据目录 ==="
mkdir -p "$DATA_DIR" "$WS_DIR"
chmod 750 "$DATA_DIR" "$WS_DIR"
ls -ld "$DATA_DIR" "$WS_DIR"

echo "=== [3/5] 安装 systemd 单元 ==="
install -m 0644 "$UNIT_SRC" "$UNIT_DST"

echo "=== [4/5] 启动服务 ==="
systemctl daemon-reload
systemctl enable deploy-server >/dev/null 2>&1 || true
systemctl restart deploy-server
sleep 2

echo "=== [5/5] 校验 ==="
systemctl is-active deploy-server
(ss -lnt | grep -q ':10055 ') && echo "已监听 10055"
curl -fsS http://127.0.0.1:10055/api/v1/healthz
echo
echo "最近日志："
journalctl -u deploy-server -n 8 --no-pager || true
echo INSTALL_OK
