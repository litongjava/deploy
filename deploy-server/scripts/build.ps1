# Windows 下的构建脚本：一条命令产出可以扔到 Linux 服务器上的二进制。
#
# 【仅用于 Windows】它产出的是 Linux 二进制，但脚本自身只能在 Windows 上跑：
# 里面的路径拼接用的是 "\"，而 PowerShell 在 Linux 上不会把 "\" 当分隔符，
# 跑起来会拷错目录。所以脚本开头直接拒绝在非 Windows 上运行。
# Linux/macOS 请用同目录的 Makefile：make ui && make linux
# （即使装了 PowerShell Core（pwsh）也不要用这个脚本。）
#
# 为什么还要写它：开发机是 Windows，make 不一定装了；
# 而且 PowerShell 里跑 Go 交叉编译要设一堆环境变量，脚本里写清楚更省事。
#
# 用法（在任意目录都能跑，脚本内部会自己切到 deploy-server）：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version 1.2.3 -Local
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -SkipUI
#
# 三个 PowerShell 陷阱（都踩过）：
#   1. 本文件含中文，PS 5.1 会按 ANSI 解码导致语法错误 —— 必须带 UTF-8 BOM。
#   2. 参数名不能叫 $Host / $args，它们是只读自动变量。
#   3. `go build ./cmd/server` 里的相对路径是相对**当前目录**的，
#      所以下面用 Push-Location 固定到 deploy-server，保证在任意目录都能调用。
param(
    # 写进二进制的版本号（-ldflags -X main.version），会显示在 /api/v1/healthz 与 doctor 里
    [string]$Version = "0.1.0",

    # 跳过「把前端 dist 拷进 embed 目录」这一步。
    # 只在前端**没改动**、只想重编后端时用；
    # 如果你刚跑完 pnpm build 就直接带上它，新前端不会被编进去，二进制里还是旧 UI。
    [switch]$SkipUI,

    # 额外构建 Windows 版本，用于本机起服务调试。
    # 不加这个只产出 Linux/amd64（生产部署用）。
    [switch]$Local,

    # 前端仓库（deploy-server-ui）的位置。
    # 不传就按候选路径自动探测，见下面的 $uiCandidates。
    [string]$UiDir
)

$ErrorActionPreference = "Stop"

# 平台守卫：显式拒绝在 Linux/macOS 上运行，而不是跑一半拷错目录。
# $PSVersionTable.Platform 只在 PowerShell 6+ 存在，Windows PowerShell 5.1 上没有这个键，
# 所以用 -and 短路，5.1 会直接跳过这个检查（它本来就只在 Windows 上）。
if ($PSVersionTable.Platform -and $PSVersionTable.Platform -ne 'Win32') {
    throw @"
scripts/build.ps1 只能用于 Windows（当前平台：$($PSVersionTable.Platform)）。
它产出的确实是 Linux 二进制，但脚本自身的路径处理是按 Windows 写的。

Linux/macOS 请改用 Makefile：
    cd deploy-server && make ui && make linux
"@
}

$root = Split-Path -Parent $PSScriptRoot   # deploy-server/
$embedDir = Join-Path $root "internal\web\dist"
$binDir = Join-Path $root "bin"

# 定位前端仓库 deploy-server-ui。
#
# 目录约定是「前端仓库与平台仓库同级」（和 java-mosskb / java-mosskb-ui 一样），
# 但平台自身可能再套一层（例如仓库根是 deploy/，而前端在它的上一级），
# 所以按候选顺序探测，而不是写死一层 ../。用 -UiDir 可以显式覆盖。
$uiCandidates = @()
if ($UiDir) { $uiCandidates += $UiDir }
$uiCandidates += @(
    (Join-Path $root "..\deploy-server-ui")       # 前端与 deploy-server 同级
    (Join-Path $root "..\..\deploy-server-ui")    # 前端与「平台仓库」同级（当前布局）
    (Join-Path $root "deploy-server-ui")          # 前端被放在 deploy-server 里面
    (Join-Path $root "..\..\..\deploy-server-ui")
)

$uiRepo = $null
foreach ($cand in $uiCandidates) {
    if (Test-Path (Join-Path $cand "package.json")) { $uiRepo = (Resolve-Path $cand).Path; break }
}
if (-not $uiRepo) {
    throw @"
找不到前端仓库 deploy-server-ui（判定依据：目录里有 package.json）。
已尝试：
$($uiCandidates | ForEach-Object { "  - $_" } | Out-String)
请用 -UiDir <路径> 显式指定，例如：
  powershell -File .\scripts\build.ps1 -UiDir D:\code\project\project-deploy\deploy-server-ui
"@
}
$uiDist = Join-Path $uiRepo "dist"

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
Write-Host "前端仓库: $uiRepo" -ForegroundColor DarkGray

# 1) 把前端产物拷进 embed 目录
if (-not $SkipUI) {
    if (-not (Test-Path (Join-Path $uiDist "index.html"))) {
        throw "前端还没构建（找不到 $uiDist\index.html）。请先执行: cd '$uiRepo'; pnpm.cmd install; pnpm.cmd build"
    }
    Step "拷贝前端产物到内嵌目录"
    if (Test-Path $embedDir) { Remove-Item -Recurse -Force $embedDir }
    New-Item -ItemType Directory -Path $embedDir -Force | Out-Null

    # 逐个复制而不是整目录 Copy-Item：要顺手排掉两类不该进生产的东西。
    #
    #   *.map          source map，实测在 dist 里能占到一半体积（24 个文件 20.8MB），
    #                  打进二进制既浪费又不必要地暴露源码结构。
    #   *hot-update*   `max dev` 的开发态热更新产物。它**直接往 dist/ 里写**，
    #                  一旦误拷进去，二进制里就混进了开发构建，行为难以预料。
    $copied = 0; $skippedMap = 0; $skippedHot = 0
    Get-ChildItem -Recurse -File $uiDist | ForEach-Object {
        if ($_.Extension -eq ".map") { $skippedMap++; return }
        if ($_.Name -match 'hot-update') { $skippedHot++; return }
        $rel = $_.FullName.Substring($uiDist.Length).TrimStart('\')
        $dest = Join-Path $embedDir $rel
        $destDir = Split-Path -Parent $dest
        if (-not (Test-Path $destDir)) { New-Item -ItemType Directory -Path $destDir -Force | Out-Null }
        Copy-Item -Force $_.FullName $dest
        $copied++
    }
    Write-Host "    已拷入 $copied 个文件（跳过 $skippedMap 个 .map、$skippedHot 个热更新产物）"

    # 没拷到入口文件说明前端产物不完整，早点报错比部署出白屏好。
    if (-not (Test-Path (Join-Path $embedDir "index.html"))) {
        throw "embed 目录里没有 index.html —— 前端产物不完整。请先 cd '$uiRepo'; pnpm.cmd build"
    }
}
else {
    Step "跳过前端拷贝（-SkipUI）"
}

# 切到 deploy-server 再编译：`go build ./cmd/server` 的相对路径是相对当前目录的。
Push-Location $root
try {
    # 2) 交叉编译 Linux 二进制（CGO_ENABLED=0 是必须的：
    #    glebarez/sqlite 是纯 Go 驱动，关掉 CGO 才能免依赖交叉编译）
    Step "构建 Linux/amd64"
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -ldflags "-s -w -X main.version=$Version" -o (Join-Path $binDir "deploy-server-linux-amd64") ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "Linux 构建失败" }

    # 3) 可选：本机版本，方便本地起服务验证
    if ($Local) {
        Step "构建 Windows/amd64（本地调试用）"
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        go build -ldflags "-s -w -X main.version=$Version" -o (Join-Path $binDir "deploy-server.exe") ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw "Windows 构建失败" }
    }
}
finally {
    # 4) 恢复现场：切回原目录，并清掉交叉编译用的环境变量，
    #    免得影响同一个终端里的后续命令（比如后面想 go run 本机版本）。
    Pop-Location
    Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

Step "完成"
Get-ChildItem $binDir | Select-Object Name, @{n = "MB"; e = { [math]::Round($_.Length / 1MB, 1) } } | Format-Table -AutoSize
