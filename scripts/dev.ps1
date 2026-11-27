param(
    [ValidateSet("vet", "test", "run", "build-server", "build-client", "all")]
    [string]$Task = "test",
    [string]$Addr = "127.0.0.1:8090",
    [string]$ReefBase = "http://127.0.0.1:8080",
    [string]$ReefDeviceID = $env:REEF_DEVICE_ID
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# 本机便携工具链（其他机器请把 go / gcc 放进 PATH，这里会自动跳过）
foreach ($p in @("C:\temp\gotool\go\bin", "C:\temp\mingw\mingw64\bin")) {
    if ((Test-Path $p) -and ($env:PATH -notlike "*$p*")) { $env:PATH = "$p;$env:PATH" }
}
$env:CGO_ENABLED = "1"
if (-not $env:CC) { $env:CC = "gcc" }
$env:GOPROXY = "https://goproxy.cn,direct"

function Invoke-All {
    Write-Host "== go vet" -ForegroundColor Cyan
    go vet ./...
    Write-Host "== go test" -ForegroundColor Cyan
    go test ./... -count=1
}

switch ($Task) {
    "vet"  { go vet ./... }
    "test" { Invoke-All }
    "all"  { Invoke-All; & $PSCommandPath -Task build-server }
    "build-server" {
        New-Item -ItemType Directory -Force -Path "bin" | Out-Null
        go build -trimpath -ldflags "-s -w -X main.version=dev" -o "bin\radar-server.exe" ./cmd/radar
        Write-Host "→ bin\radar-server.exe" -ForegroundColor Green
    }
    "build-client" {
        Write-Host "客户端（Wails）构建脚本待补：需要 wails.json、前端产物与图标。" -ForegroundColor Yellow
        Write-Host "参考 RFERP 的做法：npm run build:embed → go build -tags desktop,production。" -ForegroundColor Yellow
    }
    "run" {
        if (-not $ReefDeviceID) {
            Write-Warning "未设置 REEF_DEVICE_ID：连接 RFERP 2.x 时登录会返回 device_missing。"
            Write-Warning "取法：python -c \"import sqlite3;print(sqlite3.connect(r'..\RFERP-local\data\rferp.db').execute(\\\"select device_id_hash from devices where kind='host'\\\").fetchone()[0])\""
        }
        $env:REEF_BASE = $ReefBase
        $env:REEF_DEVICE_ID = $ReefDeviceID
        $env:RADAR_ADDR = $Addr
        $env:RADAR_DATA_DIR = "./data"
        $env:RADAR_SOURCE = "fixture"
        $env:RADAR_FIXTURE_DIR = "./fixtures"
        Write-Host "radar → http://$Addr  (REEF_BASE=$ReefBase)" -ForegroundColor Green
        go run ./cmd/radar
    }
}
