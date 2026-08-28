$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$projectRoot = Resolve-Path "$scriptDir\..\.."
$proxyRoot = Resolve-Path "$scriptDir\.."

Write-Host "--- Building SV241 Ascom Alpaca Proxy (Linux/Windows Cross-Compile) ---" -ForegroundColor Cyan

# 1. Sync versions from release_version.json into versioninfo.json and config_manager.h.
#    Single source of truth for both version numbers - edit release_version.json only, this
#    script keeps every other spot in sync automatically. Runs on Windows/PowerShell here (this
#    script itself is a Windows cross-compile script), so it can call sync_versions.ps1 directly.
Write-Host "`n[1/8] Syncing versions from release_version.json..." -ForegroundColor Yellow
# No explicit success check needed here: $ErrorActionPreference = "Stop" (top of this script)
# already turns any throw() inside sync_versions.ps1 into a terminating error that halts this
# script too. (Checking $LASTEXITCODE here would be unreliable anyway - it's only set by native
# executables, not by invoking another .ps1 via `&`.)
& "$scriptDir\sync_versions.ps1" -ProjectRoot $projectRoot -ProxyRoot $proxyRoot

# 2. Build Firmware (PlatformIO) and copy the flashable artifacts into the in-app flasher's asset
#    folder - mirrors build_exe.bat's step 2 exactly. Must happen after the version sync above
#    (so the compiled firmware embeds the just-synced FIRMWARE_VERSION) and before the version
#    string gets extracted into version.json below - otherwise that file would claim a firmware
#    version that doesn't match what's actually embedded in firmware.bin.
#
# $env:SKIP_FIRMWARE_BUILD = "1" skips this entirely - set only by release-windows.yml, which
# downloads an already-built firmware.zip (from release-firmware.yml) and extracts it into
# $flasherFwDir itself before calling this script. Never set for local/manual runs. Deliberately
# not a file-existence check: $flasherFwDir is gitignored, so a stale .bin from an earlier local
# build would otherwise cause this to silently skip rebuilding after a real source change.
$flasherFwDir = "$proxyRoot\frontend-vue\public\flasher\firmware"
if ($env:SKIP_FIRMWARE_BUILD -eq "1") {
    Write-Host "`n[2/8] Skipping firmware build (SKIP_FIRMWARE_BUILD=1 - using pre-built firmware)..." -ForegroundColor Yellow
} else {
    Write-Host "`n[2/8] Building Firmware..." -ForegroundColor Yellow
    $pioExe = "$env:USERPROFILE\.platformio\penv\Scripts\pio.exe"
    if (-not (Test-Path $pioExe)) { $pioExe = "pio" }
    Push-Location $projectRoot
    try {
        & $pioExe run
        if ($LASTEXITCODE -ne 0) {
            throw "Firmware build failed (is PlatformIO installed? pioExe=$pioExe)"
        }
    } finally {
        Pop-Location
    }

    Write-Host "Copying firmware artifacts to in-app flasher..." -ForegroundColor Yellow
    $fwBuildDir = "$projectRoot\.pio\build\Firmware_ESP32"
    New-Item -ItemType Directory -Path $flasherFwDir -Force | Out-Null
    foreach ($f in @("bootloader.bin", "partitions.bin", "firmware.bin")) {
        Copy-Item -Path "$fwBuildDir\$f" -Destination "$flasherFwDir\$f" -Force
    }
}
# Note: docs/firmware/ (the separate GitHub Pages flasher) is deliberately NOT touched here -
# publishing there is release-webflasher.yml's job.

# 3. Ensure the CGO cross-compile toolchain (Zig + libusb-1.0 sysroots) is present.
#    internal/serial/ch340_linux.go (Linux-only) needs cgo + real libusb-1.0 headers/libs for
#    the target architecture - see ensure_linux_crosscompile_toolchain.ps1 for the full story on
#    why and what gets downloaded/cached here (nothing is committed to the repo; first run
#    downloads ~100 MB, later runs reuse the cache).
Write-Host "`n[3/8] Ensuring Linux cross-compile toolchain (Zig + libusb-1.0)..." -ForegroundColor Yellow
$crosscompileCache = "$proxyRoot\build\crosscompile-cache"
$toolchain = & "$scriptDir\ensure_linux_crosscompile_toolchain.ps1" -CacheDir $crosscompileCache

# 4. Build Frontend
Write-Host "`n[4/8] Building Frontend..." -ForegroundColor Yellow
Push-Location "$proxyRoot\frontend-vue"
try {
    npm install
    npm run build
} finally {
    Pop-Location
}

# 5. Extract Firmware Version into dist (must happen after npm build, before Go build)
Write-Host "`n[5/8] Extracting Firmware Version from config_manager.h..." -ForegroundColor Yellow
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

# 6. Get Product Version for Go Build
Write-Host "`n[6/8] Reading ProductVersion from versioninfo.json..." -ForegroundColor Yellow
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
    # CGO is required on both passes below: ch340_linux.go (Linux-only) uses cgo to talk to
    # libusb directly, bypassing the kernel's ch341 tty driver that otherwise resets the ESP32
    # on every connect (see ch340_linux.go and ensure_linux_crosscompile_toolchain.ps1 for why).
    $env:CGO_ENABLED = "1"
    $env:PKG_CONFIG = $toolchain.PkgConfigStub

    # 7. Compile Linux AMD64 (Standard PC/Server)
    Write-Host "`n[7/8] Compiling Linux AMD64 (PC/Server)..." -ForegroundColor Yellow
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CC = "$($toolchain.ZigExe) cc -target x86_64-linux-gnu.2.31"
    $env:PKG_CONFIG_SYSROOT = $toolchain.Arches["amd64"].SysrootDir
    $env:PKG_CONFIG_TRIPLET = $toolchain.Arches["amd64"].Triplet
    $outputAmd64 = "build/AscomAlpacaProxy-linux-amd64"
    if (Test-Path $outputAmd64) { Remove-Item $outputAmd64 }
    go build -ldflags="-X main.AppVersion=$appVersion" -o $outputAmd64 .
    Write-Host "Created: $outputAmd64" -ForegroundColor Green

    # 8. Compile Linux ARM64 (Raspberry Pi 4/5 64-bit)
    Write-Host "`n[8/8] Compiling Linux ARM64 (Raspberry Pi)..." -ForegroundColor Yellow
    $env:GOOS = "linux"
    $env:GOARCH = "arm64"
    $env:CC = "$($toolchain.ZigExe) cc -target aarch64-linux-gnu.2.31"
    $env:PKG_CONFIG_SYSROOT = $toolchain.Arches["arm64"].SysrootDir
    $env:PKG_CONFIG_TRIPLET = $toolchain.Arches["arm64"].Triplet
    $outputArm64 = "build/AscomAlpacaProxy-linux-arm64"
    if (Test-Path $outputArm64) { Remove-Item $outputArm64 }
    go build -ldflags="-X main.AppVersion=$appVersion" -o $outputArm64 .
    Write-Host "Created: $outputArm64" -ForegroundColor Green

} finally {
    # Reset environment variables so we don't mess up future Windows builds in this console
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    Remove-Item Env:\CC -ErrorAction SilentlyContinue
    Remove-Item Env:\PKG_CONFIG -ErrorAction SilentlyContinue
    Remove-Item Env:\PKG_CONFIG_SYSROOT -ErrorAction SilentlyContinue
    Remove-Item Env:\PKG_CONFIG_TRIPLET -ErrorAction SilentlyContinue
    Pop-Location
}

Write-Host "`n--- Build Complete ---" -ForegroundColor Cyan
