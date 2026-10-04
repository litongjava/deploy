#!/usr/bin/env bash
# 校验 maxkb.xyz 站点的部署结果。
#
# 为什么用脚本文件而不是 sshpoke exec：exec 的命令串要过 PowerShell 的参数解析，
# 引号会被吃掉，"Host: maxkb.xyz" 这种带头部的 curl 一定跑歪（实测得到 400）。
# runscript 走 stdin，完全不经过 shell 的参数解析。
set -uo pipefail

SITE=/var/www/maxkb.xyz
HOSTHDR="Host: maxkb.xyz"

echo "=== [1/4] nginx 状态 ==="
systemctl is-active nginx
nginx -t 2>&1 | tail -2

echo
echo "=== [2/4] 站点目录 ==="
if [ -d "$SITE" ]; then
  echo "文件数: $(find "$SITE" -type f | wc -l)"
  echo "占用:   $(du -sh "$SITE" | cut -f1)"
  ls -la "$SITE" | head -20
else
  echo "目录不存在: $SITE"
fi

echo
echo "=== [3/4] 各页面 HTTP 状态（走 Host 头）==="
FAIL=0
for p in / /index.html /features /scenarios /cases /pricing /contact /404.html /assets/css/style.css /assets/js/main.js /favicon.svg /robots.txt /sitemap.xml; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "$HOSTHDR" "http://127.0.0.1${p}" || echo 000)
  printf '  %-28s %s\n' "$p" "$code"
  case "$p" in
    /404.html) ;;
    *) [ "$code" = "200" ] || FAIL=1 ;;
  esac
done
# 404 兜底：不存在的路径应当返回 404，且内容来自我们的 404.html
code404=$(curl -s -o /dev/null -w '%{http_code}' -H "$HOSTHDR" "http://127.0.0.1/definitely-not-here")
echo "  /definitely-not-here         $code404  (期望 404)"

echo
echo "=== [4/4] 首页内容抽样（确认不是默认页）==="
curl -s -H "$HOSTHDR" http://127.0.0.1/ | grep -oE '<title>[^<]*</title>' | head -1
curl -s -H "$HOSTHDR" http://127.0.0.1/ | grep -c 'maxkb.xyz' | sed 's/^/  首页出现 maxkb.xyz 次数: /'
curl -s -H "$HOSTHDR" http://127.0.0.1/ | grep -oE '沪ICP备[0-9]+号-[0-9]+' | head -1 | sed 's/^/  备案号: /'

echo
if [ "$FAIL" = "1" ]; then
  echo "VERIFY_FAILED 有页面没有返回 200"
  exit 1
fi
echo "VERIFY_OK"
