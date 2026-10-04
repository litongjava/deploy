#!/usr/bin/env bash
# 在服务器上验证 deploy-cli 的 Linux 版本能对着真实服务端跑通。
#
# 覆盖：login → whoami → project list → run -f（跟随实时日志）→ runs list → doctor
# 以及退出码语义（错误 token → 3）。
set -uo pipefail

CLI=/usr/local/bin/deploy-cli
BASE=http://127.0.0.1:10055

[ -x "$CLI" ] || { echo "缺少 $CLI"; exit 1; }

echo "=== 0) 版本 ==="
"$CLI" version
echo "退出码=$?"

echo
echo "=== 1) login ==="
"$CLI" login -s "$BASE" -u admin -p admin12345
echo "退出码=$?"

echo
echo "=== 2) whoami ==="
"$CLI" whoami
echo "退出码=$?"

echo
echo "=== 3) project list ==="
"$CLI" project list
echo "退出码=$?"

echo
echo "=== 4) run mosskb-website -f（跟随实时日志）==="
"$CLI" run mosskb-website -f
echo "退出码=$? （成功应为 0）"

echo
echo "=== 5) runs list ==="
"$CLI" runs list --project mosskb-website
echo "退出码=$?"

echo
echo "=== 6) doctor（服务端自检）==="
"$CLI" doctor | head -20
echo "退出码=$?"

echo
echo "=== 7) 退出码语义：错误 token 应为 3 ==="
"$CLI" project list --token dk_this_token_is_bogus >/dev/null 2>&1
code=$?
echo "退出码=$code $([ "$code" = "3" ] && echo '（正确）' || echo '（期望 3）')"

echo
echo "=== 8) 退出码语义：服务端不可达应为 4 ==="
"$CLI" project list --server http://127.0.0.1:59999 >/dev/null 2>&1
code=$?
echo "退出码=$code $([ "$code" = "4" ] && echo '（正确）' || echo '（期望 4）')"

echo
echo CLI_VERIFY_DONE
