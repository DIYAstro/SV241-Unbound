$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$projectRoot = Resolve-Path "$scriptDir\..\.."
$proxyRoot = Resolve-Path "$scriptDir\.."

Write-Host "--- Building SV241 Ascom Alpaca Proxy (Linux/Windows Cross-Compile) ---" -ForegroundColor Cyan

# 1. Build Frontend
Write-Host "`n[1/4] Building Frontend..." -ForegroundColor Yellow
Push-Location "$proxyRoot\frontend-vue"
try {
    npm install
    npm run build
} finally {
    Pop-Location
}

# 2. Extract Firmware Version into dist (must happen after npm build, before Go build)
Write-Host "`n[2/5] Extracting Firmware Version from config_manager.h..." -ForegroundColor Yellow
$configH = "$projectRoot\src\config_manager.h"
$versionJsonDir = "$proxyRoot\frontend-vue\dist\flasher\firmware"
$versionJson = "$versionJsonDir\version.json"

if (Test-Path $configH) {
    New-Item -ItemType Directory -Path $versionJsonDir -Force | Out-Null
    $line = Get-Content $configH | Select-String "FIRMWARE_VERSION"
    if ($line) {
        $parts = $line.ToString().Split('"')
        if ($parts.Length -ge 2) {
            $fwVersion = $parts[1]
            $jsonContent = '{"version": "' + $fwVersion + '"}'
            Set-Content -Path $versionJson -Value $jsonContent -Encoding UTF8
            Write-Host "Embedded Firmware Version: $fwVersion" -ForegroundColor Green
        } else {
            Write-Host "Warning: Could not parse FIRMWARE_VERSION from line." -ForegroundColor Yellow
        }
    } else {
        Write-Host "Warning: FIRMWARE_VERSION not found in config_manager.h!" -ForegroundColor Yellow
    }
} else {
    Write-Host "Note: config_manager.h not found at '$configH', skipping firmware version extraction." -ForegroundColor Yellow
}

# 3. Get Product Version for Go Build
Write-Host "`n[3/5] Reading ProductVersion from versioninfo.json..." -ForegroundColor Yellow
$versionInfoPath = "$proxyRoot\versioninfo.json"
$appVersion = "dev"
if (Test-Path $versionInfoPath) {
    $json = Get-Content -Raw -Path $versionInfoPath
    $obj = ConvertFrom-Json $json
    $appVersion = $obj.StringFileInfo.ProductVersion
}
if ([string]::IsNullOrWhiteSpace($appVersion)) {
    Write-Host "Error: Could not extract ProductVersion!" -ForegroundColor Red
    exit 1
}
Write-Host "App Version: $appVersion"

# Create build directory
$buildDir = "$proxyRoot\build"
if (-not (Test-Path $buildDir)) {
    New-Item -ItemType Directory -Path $buildDir | Out-Null
}

Push-Location $proxyRoot
try {
    # 4. Compile Linux AMD64 (Standard PC/Server)
    Write-Host "`n[4/5] Compiling Linux AMD64 (PC/Server)..." -ForegroundColor Yellow
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $outputAmd64 = "build/AscomAlpacaProxy-linux-amd64"
    if (Test-Path $outputAmd64) { Remove-Item $outputAmd64 }
    go build -ldflags="-X main.AppVersion=$appVersion" -o $outputAmd64 .
    Write-Host "Created: $outputAmd64" -ForegroundColor Green

    # 5. Compile Linux ARM64 (Raspberry Pi 4/5 64-bit)
    Write-Host "`n[5/5] Compiling Linux ARM64 (Raspberry Pi)..." -ForegroundColor Yellow
    $env:GOOS = "linux"
    $env:GOARCH = "arm64"
    $outputArm64 = "build/AscomAlpacaProxy-linux-arm64"
    if (Test-Path $outputArm64) { Remove-Item $outputArm64 }
    go build -ldflags="-X main.AppVersion=$appVersion" -o $outputArm64 .
    Write-Host "Created: $outputArm64" -ForegroundColor Green

} finally {
    # Reset Environment variables so we don't mess up future Windows builds in this console
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    Pop-Location
}

Write-Host "`n--- Build Complete ---" -ForegroundColor Cyan
