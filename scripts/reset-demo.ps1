param(
    # 要重置的数据目录；默认与 radar 的 RADAR_DATA_DIR 一致。
    [string]$DataDir = $env:RADAR_DATA_DIR
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($DataDir)) {
    $DataDir = Join-Path (Split-Path -Parent $PSScriptRoot) "data"
}
$dbPath = Join-Path $DataDir "radar.db"

Write-Host "即将重置演示数据：" -ForegroundColor Yellow
Write-Host "  $dbPath"
Write-Host "只删除 radar 自有库（候选/证据/成本/供应商/采纳记录），不影响 RFERP 中已建的产品与 BOM。"

$answer = Read-Host "确认删除并重建空库？输入 yes 继续"
if ($answer -ne "yes") {
    Write-Host "已取消，未删除任何文件。"
    exit 0
}

# WAL/SHM 是 SQLite 的附属文件，必须一起删，否则旧数据会被回放。
foreach ($suffix in @("", "-wal", "-shm")) {
    $target = "$dbPath$suffix"
    if (Test-Path $target) {
        Remove-Item -Force $target
        Write-Host "  已删除 $target"
    }
}

if (-not (Test-Path $DataDir)) {
    New-Item -ItemType Directory -Path $DataDir | Out-Null
}

Write-Host "完成。下次启动 radar 会自动重建空库与表结构。" -ForegroundColor Green
