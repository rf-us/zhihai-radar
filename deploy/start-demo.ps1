# 本地一键演示：起一个一次性 RFERP（SQLite）+ radar
#
# 用法：
#   .\deploy\start-demo.ps1 -ReefExe C:\path\to\rferp-server.exe
#
# 说明：
#   - RFERP 的数据落在 .\tmp\reef\reef.db，删掉目录即可重置，不会碰你的业务库；
#   - radar 的数据落在 .\data\radar.db；
#   - 两个进程都在当前窗口输出日志，Ctrl+C 结束。

param(
    [string]$ReefExe = "",
    [int]$ReefPort = 8080,
    [int]$RadarPort = 8090
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $ReefExe) {
    foreach ($candidate in @(".\rferp-server.exe", ".\RFERP.exe", ".\bin\rferp-server.exe")) {
        if (Test-Path $candidate) { $ReefExe = (Resolve-Path $candidate).Path; break }
    }
}
if (-not $ReefExe -or -not (Test-Path $ReefExe)) {
    throw "找不到 RFERP 可执行文件。请用 -ReefExe 指定 rferp-server.exe 的路径。"
}

$reefDir = Join-Path $root "tmp\reef"
New-Item -ItemType Directory -Force -Path $reefDir | Out-Null
$config = @{ storage = "sqlite"; sqlitePath = "./reef.db" } | ConvertTo-Json
Set-Content -Path (Join-Path $reefDir "config.json") -Value $config -Encoding UTF8

Write-Host "启动 RFERP  $ReefExe  :$ReefPort  (data: $reefDir)"
$reef = Start-Process -FilePath $ReefExe -ArgumentList "-addr", ":$ReefPort" `
    -WorkingDirectory $reefDir -PassThru -NoNewWindow

try {
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        Start-Sleep -Milliseconds 500
        try {
            $null = Invoke-RestMethod -Uri "http://127.0.0.1:$ReefPort/healthz" -TimeoutSec 2
            $ready = $true
            break
        } catch { }
    }
    if (-not $ready) { throw "RFERP 在 15 秒内没有就绪，检查端口 $ReefPort 是否被占用。" }

    Write-Host "RFERP 就绪。首次运行请先在 http://127.0.0.1:$ReefPort 创建管理员。" -ForegroundColor Green
    Write-Host "启动 radar  http://127.0.0.1:$RadarPort" -ForegroundColor Green

    $env:REEF_BASE = "http://127.0.0.1:$ReefPort"
    $env:RADAR_ADDR = ":$RadarPort"
    go run ./cmd/radar
}
finally {
    if ($reef -and -not $reef.HasExited) {
        Write-Host "停止 RFERP..."
        Stop-Process -Id $reef.Id -Force
    }
}
