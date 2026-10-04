# 验证「取消运行后进程树被完整终止」。
#
# 为什么单独写一个脚本：这是部署工具最容易出错的地方 ——
# 只杀掉直接子进程（shell）会把后台起的孙子进程留成孤儿，
# 下次部署时它们还占着端口/文件锁，排查起来非常痛苦。
#
# 注意：本机 PowerShell 是 5.1，用字符串做请求体时会按默认代码页编码，
# 中文会变成 '?'。所以这里一律先 [Text.Encoding]::UTF8.GetBytes() 再发。
param(
    [string]$Base = "http://127.0.0.1:8080/api/v1",
    [string]$User = "admin",
    [string]$Pass = "admin12345"
)

$ErrorActionPreference = "Stop"

function Post-Json($url, $headers, $obj) {
    $json = if ($obj -is [string]) { $obj } else { $obj | ConvertTo-Json -Depth 8 -Compress }
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    return Invoke-RestMethod $url -Method Post -ContentType "application/json; charset=utf-8" -Headers $headers -Body $bytes
}
function Get-Json($url, $headers) { return Invoke-RestMethod $url -Headers $headers }
function Delete-Json($url, $headers) { return Invoke-RestMethod $url -Method Delete -Headers $headers }

Write-Host "== 1) 登录 ==" -ForegroundColor Cyan
$login = Post-Json "$Base/auth/login" @{} (@{ username = $User; password = $Pass })
$H = @{ Authorization = $login.data.token }
Write-Host "   登录成功: $($login.data.user.username)"

# 清理上一次留下的同名项目
try { Delete-Json "$Base/projects/tree-test" $H | Out-Null } catch { }

Write-Host "== 2) 建项目：命令会派生一个孙子进程，然后长睡 ==" -ForegroundColor Cyan
# 这段脚本：起一个隐藏的子 PowerShell（睡 300 秒）→ 打印它的 PID → 自己也睡 300 秒。
# 如果 killTree 只杀直接子进程，那个子 PowerShell 就会活下来。
#
# 用 powershell 而不是 pwsh：本机只装了 Windows PowerShell 5.1，
# pwsh 不在 PATH 上（探测过）。
$exe = (Get-Command powershell).Source
$script = @"
`$child = Start-Process -PassThru -WindowStyle Hidden -FilePath '$exe' -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 300'
Write-Output "CHILD_PID=`$(`$child.Id)"
Start-Sleep -Seconds 300
"@
$proj = @{
    key       = "tree-test"
    name      = "进程树测试"
    workdir   = '${WORKSPACE_ROOT}/tree-test'
    commands  = @(@{ name = "派生孙子进程并长睡"; script = $script })
}
$p = Post-Json "$Base/projects" $H $proj
Write-Host "   项目已创建: $($p.data.key)"

Write-Host "== 3) 触发执行 ==" -ForegroundColor Cyan
$run = Post-Json "$Base/projects/tree-test/run" $H "{}"
$runId = $run.data.runId
Write-Host "   runId=$runId"

# 等日志里出现 CHILD_PID
$childPid = 0
for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    $log = Get-Json "$Base/runs/$runId/log" $H
    if ($log.data.data -match 'CHILD_PID=(\d+)') {
        $childPid = [int]$Matches[1]
        break
    }
}
if ($childPid -eq 0) { throw "没能从日志里拿到孙子进程 PID" }
Write-Host "   孙子进程 PID = $childPid"

$aliveBefore = $null -ne (Get-Process -Id $childPid -ErrorAction SilentlyContinue)
Write-Host "   取消前该进程存活: $aliveBefore"
if (-not $aliveBefore) { throw "孙子进程没起来，测试前提不成立" }

Write-Host "== 4) 取消运行 ==" -ForegroundColor Cyan
$sw = [System.Diagnostics.Stopwatch]::StartNew()
Post-Json "$Base/runs/$runId/cancel" $H "{}" | Out-Null

$final = $null
for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    $r = Get-Json "$Base/runs/$runId" $H
    if ($r.data.status -notin @("queued", "running")) { $final = $r.data; break }
}
$sw.Stop()
if ($null -eq $final) { throw "取消后运行没有结束" }
Write-Host "   状态: $($final.status)，耗时 $([math]::Round($sw.Elapsed.TotalSeconds,1))s"

Write-Host "== 5) 检查孙子进程是否被清理 ==" -ForegroundColor Cyan
# 给 SIGTERM→SIGKILL 的兜底逻辑留点时间
Start-Sleep -Seconds 3
$aliveAfter = $null -ne (Get-Process -Id $childPid -ErrorAction SilentlyContinue)
Write-Host "   取消后该进程存活: $aliveAfter"

Write-Host ""
if ($aliveAfter) {
    Write-Host "失败：孙子进程成了孤儿，进程树没有被完整终止" -ForegroundColor Red
    exit 1
}
if ($final.status -ne "canceled") {
    Write-Host "失败：状态应为 canceled，实际 $($final.status)" -ForegroundColor Red
    exit 1
}
Write-Host "通过：取消后进程树被完整终止，状态为 canceled" -ForegroundColor Green
exit 0
