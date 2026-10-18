<#
把本目录下的项目定义（project.json + sh/*.sh）写到 deploy-server 上的既有项目。

deploy-cli 没有 project update，所以更新走 REST：POST /auth/login 取 JWT，
GET /projects/{key} 读出既有字段（保留 Git 配置），PUT /projects/{key} 提交
内联的 commands 与 envs（不带 id 即新建，缺失的旧命令会被删除）。

用法：
  powershell -ExecutionPolicy Bypass -File .\apply-project.ps1 `
      -Base http://127.0.0.1:10055 -User admin -Password <你的密码>
#>
param(
    [string]$Base = 'http://100.113.197.53:10055',
    [string]$User = 'admin',
    [string]$Password,
    [string]$ProjectKey = 'tio-websockify',
    [string]$Token
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

function New-Session {
    if ($Token) { return $Token }
    if (-not $Password) { throw 'provide -Password or -Token' }
    $body = @{ username = $User; password = $Password } | ConvertTo-Json -Compress
    $res = Invoke-RestMethod -Method Post -Uri "$Base/api/v1/auth/login" `
        -ContentType 'application/json' -Body $body -TimeoutSec 25
    if (-not $res.success) { throw "login failed: $($res.errorMessage)" }
    return $res.data.token
}

$jwt = New-Session
$headers = @{ Authorization = "Bearer $jwt" }

$spec = Get-Content (Join-Path $root 'project.json') -Raw | ConvertFrom-Json
$current = (Invoke-RestMethod -Uri "$Base/api/v1/projects/$ProjectKey" -Headers $headers -TimeoutSec 25).data

$commands = @()
$order = 1
foreach ($c in $spec.commands) {
    $scriptPath = Join-Path $root ($c.scriptFile -replace '/', '\')
    if (-not (Test-Path $scriptPath)) { throw "scriptFile not found: $($c.scriptFile)" }
    $script = [System.IO.File]::ReadAllText($scriptPath).TrimStart([char]0xFEFF) -replace "`r`n", "`n"
    if ([string]::IsNullOrWhiteSpace($script)) { throw "empty script: $($c.scriptFile)" }
    $timeout = if ($null -ne $c.timeoutSec) { [int]$c.timeoutSec } else { 0 }
    $commands += [ordered]@{
        name         = $c.name
        script       = $script
        sortOrder    = $order
        enabled      = $true
        failStrategy = 'abort'
        timeoutSec   = $timeout
    }
    $order++
}

$envs = @()
foreach ($prop in $spec.env.PSObject.Properties) {
    $envs += [ordered]@{ key = $prop.Name; value = [string]$prop.Value; isSecret = $false }
}

# 未在 project.json 里显式给出的字段沿用线上现值，避免误清空。
$payload = [ordered]@{
    key              = $current.key
    name             = $spec.name
    description      = $spec.description
    workdir          = $spec.workdir
    shell            = $spec.shell
    timeoutSec       = 0
    concurrency      = $spec.concurrency
    hookEnabled      = [bool]$spec.hookEnabled
    useGit           = [bool]$spec.useGit
    gitUrl           = $spec.gitUrl
    gitBranch        = $spec.gitBranch
    gitCredentialId  = $current.gitCredentialId
    gitSubdir        = $current.gitSubdir
    gitDepth         = [int]$current.gitDepth
    gitClean         = [bool]$current.gitClean
    gitSubmodule     = [bool]$current.gitSubmodule
    allowEnvOverride = [bool]$current.allowEnvOverride
    kind             = $current.kind
    commands         = $commands
    envs             = $envs
}

$json = $payload | ConvertTo-Json -Depth 8 -Compress
$bytes = [System.Text.Encoding]::UTF8.GetBytes($json)

$res = Invoke-RestMethod -Method Put -Uri "$Base/api/v1/projects/$ProjectKey" `
    -Headers $headers -ContentType 'application/json; charset=utf-8' -Body $bytes -TimeoutSec 60

if (-not $res.success) { throw "update failed: $($res.errorMessage)" }

$d = $res.data
Write-Output "project  : $($d.key) (id=$($d.id))"
Write-Output "workdir  : $($d.workdir)"
Write-Output "git      : $($d.gitUrl) @ $($d.gitBranch)"
Write-Output "envs     : $(($d.envs | ForEach-Object { "$($_.key)=$($_.value)" }) -join ', ')"
Write-Output "commands :"
foreach ($c in $d.commands) {
    Write-Output ("  [{0}] id={1,-4} timeout={2,-5} {3}" -f $c.sortOrder, $c.id, $c.timeoutSec, $c.name)
}