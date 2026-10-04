# 用平台的 REST API 完成 mosskb-website 的部署。
#
# 存在的意义有两个：
#   1. 可复现 —— 浏览器点一遍是给人看的，脚本跑一遍是给 CI 与回归用的。
#   2. 后备路径 —— 万一 Web 表单在某个浏览器上填不动，部署本身不能因此停摆。
#
# 流程严格对应用户描述的三步：登录 → 创建项目（写 sh 命令）→ 点击执行。
#
# 三个必须注意的坑（都实际踩过）：
#   1. 本机 PowerShell 是 5.1，字符串请求体会按默认代码页编码（中文变 '?'），
#      所以所有请求体一律先转成 UTF-8 字节再发。
#   2. 平台的业务错误是 **HTTP 200 + success=false**（见 docs/API设计.md §0.1），
#      所以**不能用 try/catch 判断业务失败**，必须显式检查 success 字段。
#      这里所有请求都走 Api()，它在 success=false 时抛出带 errorCode 的异常。
#   3. shell 脚本一律用单引号 here-string（@'...'@）。双引号 here-string 里
#      $(...) 会被 PowerShell 当成子表达式先执行一遍，脚本内容会被改坏。
param(
    [string]$Base = "http://192.168.31.97:10055/api/v1",
    [string]$User = "admin",
    [string]$Pass = "admin12345",
    [string]$SiteRoot = "/var/www/maxkb.xyz",
    [string]$HostName = "maxkb.xyz",
    [switch]$SkipRun
)

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# Api 是所有请求的唯一入口。
#
# 关键点：先看 HTTP 状态码，再解析 body 并检查 success。
# 绝不能依赖 Invoke-RestMethod 抛异常来判断业务失败 —— 业务失败是 HTTP 200。
function Api {
    param(
        [string]$Method,
        [string]$Url,
        [hashtable]$Headers = @{},
        $Body = $null
    )
    $params = @{
        Uri             = $Url
        Method          = $Method
        Headers         = $Headers
        UseBasicParsing = $true
        TimeoutSec      = 60
    }
    if ($null -ne $Body) {
        $json = if ($Body -is [string]) { $Body } else { $Body | ConvertTo-Json -Depth 10 -Compress }
        $params.Body = [System.Text.Encoding]::UTF8.GetBytes($json)
        $params.ContentType = "application/json; charset=utf-8"
    }

    $resp = Invoke-WebRequest @params
    $parsed = $null
    if ($resp.Content) { $parsed = $resp.Content | ConvertFrom-Json }

    if ($resp.StatusCode -ge 400 -or ($parsed -and $parsed.success -eq $false)) {
        $code = if ($parsed) { $parsed.errorCode } else { $resp.StatusCode }
        $msg = if ($parsed) { $parsed.errorMessage } else { "$($resp.StatusCode)" }
        throw "[$Method $Url] HTTP $($resp.StatusCode) errorCode=$code : $msg"
    }
    if ($parsed) { return $parsed.data }
    return $null
}

Write-Host "==== 1) 登录 ====" -ForegroundColor Cyan
$status = Api -Method GET -Url "$Base/auth/bootstrap-status"
if (-not $status.initialized) {
    Write-Host "    系统尚未初始化，创建管理员 $User"
    $auth = Api -Method POST -Url "$Base/auth/bootstrap" -Body @{ username = $User; password = $Pass }
}
else {
    $auth = Api -Method POST -Url "$Base/auth/login" -Body @{ username = $User; password = $Pass }
}
$H = @{ Authorization = $auth.token }
$me = Api -Method GET -Url "$Base/auth/me" -Headers $H
Write-Host "    当前用户: $($me.username) / 角色 $($me.role)"

Write-Host "==== 2) 准备项目定义 ====" -ForegroundColor Cyan

# ---- 第 1 条：把 Git 拉下来的工作区同步到站点根目录 ----
$scriptSync = @'
set -e
SITE_ROOT="${SITE_ROOT:-/var/www/maxkb.xyz}"
mkdir -p "$SITE_ROOT"
rsync -a --delete --exclude '.git/' "${WORKDIR}/" "$SITE_ROOT"/
chown -R www-data:www-data "$SITE_ROOT"
echo "已同步 $(find "$SITE_ROOT" -type f | wc -l) 个文件到 $SITE_ROOT"
ls -la "$SITE_ROOT" | head -12
'@

# ---- 第 2 条：先校验配置再 reload，避免把线上打挂 ----
$scriptReload = @'
set -e
nginx -t
systemctl reload nginx
echo "nginx 已重载"
'@

# ---- 第 3 条：自己健康检查；任一页面不是 200 就让本次部署失败 ----
$scriptCheck = @'
set -e
HOSTNAME_="${SITE_HOSTNAME:-maxkb.xyz}"
FAIL=0
for p in / /index.html /features /scenarios /cases /pricing /contact /assets/css/style.css /favicon.svg /robots.txt /sitemap.xml; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Host: $HOSTNAME_" "http://127.0.0.1$p")
  printf '  %-26s %s\n' "$p" "$code"
  [ "$code" = "200" ] || FAIL=1
done
title=$(curl -s -H "Host: $HOSTNAME_" http://127.0.0.1/ | grep -oE '<title>[^<]*</title>' | head -1)
echo "  首页标题: $title"
[ "$FAIL" = "0" ] || { echo "有页面没有返回 200"; exit 1; }
echo "健康检查通过"
'@

$commands = @(
    @{ name = "同步站点文件"; sortOrder = 1; enabled = $true; failStrategy = "abort"; script = $scriptSync },
    @{ name = "校验并重载 nginx"; sortOrder = 2; enabled = $true; failStrategy = "abort"; script = $scriptReload },
    @{ name = "健康检查"; sortOrder = 3; enabled = $true; failStrategy = "abort"; script = $scriptCheck }
)

$project = @{
    key              = "mosskb-website"
    name             = "MossKB 官网"
    description      = "纯静态站点：Git 拉取 → rsync 到站点目录 → reload nginx → 健康检查"
    workdir          = '${WORKSPACE_ROOT}/${PROJECT_KEY}'
    shell            = "auto"
    timeoutSec       = 300
    concurrency      = "queue"
    hookEnabled      = $true
    archived         = $false
    allowEnvOverride = $true
    useGit           = $true
    gitUrl           = "/srv/git/mosskb-website.git"
    gitBranch        = "main"
    gitSubdir        = ""
    gitDepth         = 0
    gitClean         = $false
    gitSubmodule     = $false
    commands         = $commands
    envs             = @(
        @{ key = "SITE_ROOT"; value = $SiteRoot; isSecret = $false },
        @{ key = "SITE_HOSTNAME"; value = $HostName; isSecret = $false }
    )
}

# 已存在就更新，不存在就创建 —— 脚本可以反复跑。
# 判断「存在」不能靠异常：不存在时服务端返回 HTTP 200 + errorCode 40400，
# Invoke-RestMethod 根本不报错。
$exists = $false
try {
    $null = Api -Method GET -Url "$Base/projects/mosskb-website" -Headers $H
    $exists = $true
}
catch {
    if ($_.Exception.Message -notmatch 'errorCode=40400') { throw }
    $exists = $false
}

if ($exists) {
    Write-Host "    项目已存在，改为更新"
    $p = Api -Method PUT -Url "$Base/projects/mosskb-website" -Headers $H -Body $project
}
else {
    Write-Host "    项目不存在，新建"
    $p = Api -Method POST -Url "$Base/projects" -Headers $H -Body $project
}
Write-Host "    项目: $($p.key) / $($p.name)"
Write-Host "    Git : $($p.gitUrl) @ $($p.gitBranch)  useGit=$($p.useGit)"
Write-Host "    命令: $($p.commands.Count) 条"
foreach ($c in $p.commands) { Write-Host "      - [$($c.sortOrder)] $($c.name)" }

Write-Host "==== 3) 预演（确认将要执行的脚本）====" -ForegroundColor Cyan
$preview = Api -Method POST -Url "$Base/projects/mosskb-website/preview" -Headers $H -Body @{ branch = "main" }
Write-Host "    工作目录: $($preview.workdir)"
($preview.script -split "`n") | Select-Object -First 10 | ForEach-Object { Write-Host "    | $_" }

if ($SkipRun) { Write-Host "`n已跳过执行（-SkipRun）"; exit 0 }

Write-Host "==== 4) 点击执行 ====" -ForegroundColor Cyan
$run = Api -Method POST -Url "$Base/projects/mosskb-website/run" -Headers $H -Body @{ branch = "main" }
$runId = $run.runId
Write-Host "    runId=$runId buildNumber=$($run.buildNumber)"

$final = $null
for ($i = 0; $i -lt 240; $i++) {
    Start-Sleep -Milliseconds 500
    $r = Api -Method GET -Url "$Base/runs/$runId" -Headers $H
    if ($r.status -notin @("queued", "running")) { $final = $r; break }
}

Write-Host "==== 5) 执行日志 ====" -ForegroundColor Cyan
$log = Api -Method GET -Url "$Base/runs/$runId/log" -Headers $H
Write-Host $log.data

if ($null -eq $final) { Write-Host "超时：运行没有在 120 秒内结束" -ForegroundColor Red; exit 1 }

Write-Host "---- 摘要 ----"
Write-Host "    状态    : $($final.status)"
Write-Host "    退出码  : $($final.exitCode)"
Write-Host "    耗时    : $([math]::Round($final.durationMs / 1000, 1))s"
Write-Host "    git     : $($final.gitCommitShort) @ $($final.gitBranchResolved)"
Write-Host "    prepare : $($final.prepareStatus) ($($final.prepareDurationMs)ms)"
if ($final.errorMsg) { Write-Host "    错误    : $($final.errorMsg)" }

if ($final.status -ne "success") {
    Write-Host "`n部署失败" -ForegroundColor Red
    exit 1
}
Write-Host "`n部署成功" -ForegroundColor Green
exit 0
