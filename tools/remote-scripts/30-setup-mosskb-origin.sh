#!/usr/bin/env bash
# 在服务器上把上传上来的 mosskb-website 源码做成一个本地裸仓库（origin）。
#
# 为什么要这一步：本平台按需求 D2 支持「Git 准备阶段」——
# 项目里填仓库地址，平台自己 clone/fetch 后再跑命令。
# mosskb-website 的 Gitee 仓库是私有的（匿名克隆 401），
# 所以这里在服务器本地建一个 origin，让整条 Git 流程能被真实地跑一遍，
# 而不是把平台降级成「本地 rsync 搬运工」。
set -euo pipefail

SRC=/srv/git-src/mosskb-website
BARE=/srv/git/mosskb-website.git

[ -d "$SRC" ] || { echo "缺少源目录 $SRC（需要先 sshpoke push 上去）"; exit 1; }
[ -f "$SRC/index.html" ] || { echo "$SRC 里没有 index.html，内容不对"; exit 1; }

echo "=== [1/3] 在源码目录建立提交历史 ==="
cd "$SRC"
# 每次都从当前内容重建：上传的是最新版，历史不重要，内容一致才重要。
rm -rf .git
git init -q -b main
git config user.email "deploy@localhost"
git config user.name "deploy-server"
git add -A
git commit -q -m "import mosskb-website $(date '+%F %T')" || echo "（没有变化，仍继续）"
echo "提交：$(git log --oneline -1)"

echo "=== [2/3] 生成裸仓库 ==="
rm -rf "$BARE"
mkdir -p "$(dirname "$BARE")"
git clone -q --bare . "$BARE"
echo "裸仓库：$BARE"
git --git-dir="$BARE" log --oneline -1

echo "=== [3/3] 校验可克隆 ==="
TMP=$(mktemp -d)
git clone -q "$BARE" "$TMP/probe"
test -f "$TMP/probe/index.html" || { echo "克隆出来的目录里没有 index.html"; exit 1; }
echo "克隆校验通过，文件数：$(find "$TMP/probe" -type f -not -path '*/.git/*' | wc -l)"
rm -rf "$TMP"
echo ORIGIN_OK
