@echo off
setlocal EnableDelayedExpansion

REM Get script directory
cd /d "%~dp0"
set "SCRIPT_DIR=%CD%"
set "PROJECT_ROOT=%SCRIPT_DIR%\..\.."
set "PROXY_ROOT=%SCRIPT_DIR%\.."

echo --- Building SV241 Ascom Alpaca Proxy EXE ---

REM 0. Cleanup previous build
if exist "..\build\AscomAlpacaProxy.exe" (
    echo [0/8] Cleaning previous build...
    del "..\build\AscomAlpacaProxy.exe"
)

REM 1. Sync versions from release_version.json into versioninfo.json and config_manager.h.
REM    Single source of truth for both version numbers - edit release_version.json only, this
REM    script keeps every other spot (the 4 redundant fields goversioninfo needs in
REM    versioninfo.json, plus the firmware's own FIRMWARE_VERSION define) in sync automatically.
echo [1/8] Syncing versions from release_version.json...
powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%\sync_versions.ps1" -ProjectRoot "%PROJECT_ROOT%" -ProxyRoot "%PROXY_ROOT%"
if %ERRORLEVEL% NEQ 0 (
    echo Error syncing versions!
    exit /b 1
)

REM 2. Build Firmware (PlatformIO) and copy the flashable artifacts into the in-app flasher's
REM    asset folder - replaces the previous manual "compile, then copy 3 files by hand" step.
REM
REM SKIP_FIRMWARE_BUILD=1 skips this entirely - set only by release-windows.yml, which downloads
REM an already-built firmware.zip (from release-firmware.yml) and extracts it into FLASHER_FW_DIR
REM itself before calling this script. Never set for local/manual runs. Deliberately not a
REM file-existence check: FLASHER_FW_DIR is gitignored, so a stale .bin from an earlier local
REM build would otherwise cause this to silently skip rebuilding after a real source change.
set "FLASHER_FW_DIR=%PROXY_ROOT%\frontend-vue\public\flasher\firmware"
if "%SKIP_FIRMWARE_BUILD%"=="1" (
    echo [2/8] Skipping firmware build ^(SKIP_FIRMWARE_BUILD=1 - using pre-built firmware^)...
) else (
    echo [2/8] Building Firmware...
    REM Delayed expansion (!VAR!) required for every variable set-then-read within this same
    REM parenthesized block - %VAR% would otherwise resolve to its value from BEFORE the block
    REM started (parsed once, up front), not the value just set a few lines earlier. Bit this
    REM project once already: PIO_EXE ended up empty (""), ERRORLEVEL stayed 0 no matter what
    REM `pio run` actually returned, and FW_BUILD_DIR was empty in the copy loop below.
    set "PIO_EXE=%USERPROFILE%\.platformio\penv\Scripts\pio.exe"
    if not exist "!PIO_EXE!" set "PIO_EXE=pio"
    pushd "%PROJECT_ROOT%"
    "!PIO_EXE!" run
    if !ERRORLEVEL! NEQ 0 (
        echo Error building firmware! ^(is PlatformIO installed? PIO_EXE=!PIO_EXE!^)
        popd
        exit /b 1
    )
    popd

    echo Copying firmware artifacts to in-app flasher...
    set "FW_BUILD_DIR=%PROJECT_ROOT%\.pio\build\Firmware_ESP32"
    if not exist "%FLASHER_FW_DIR%" mkdir "%FLASHER_FW_DIR%"
    for %%F in (bootloader.bin partitions.bin firmware.bin) do (
        copy /Y "!FW_BUILD_DIR!\%%F" "%FLASHER_FW_DIR%\%%F" >nul
        if !ERRORLEVEL! NEQ 0 (
            echo Error copying %%F from PlatformIO build output!
            exit /b 1
        )
    )
)
REM Note: docs/firmware/ (the separate GitHub Pages flasher) is deliberately NOT touched here -
REM publishing there is release-webflasher.yml's job.

REM 3. Build Frontend
echo [3/8] Building Frontend...
pushd "%PROXY_ROOT%\frontend-vue"
call npm install
if %ERRORLEVEL% NEQ 0 (
    echo Error during npm install!
    popd
    exit /b 1
)
call npm run build
if %ERRORLEVEL% NEQ 0 (
    echo Error during npm run build!
    popd
    exit /b 1
)
popd

REM 4. Extract Firmware Version
set "CONFIG_H=%PROJECT_ROOT%\src\config_manager.h"
set "VERSION_JSON_DIR=%PROXY_ROOT%\frontend-vue\dist\flasher\firmware"
set "VERSION_JSON=%VERSION_JSON_DIR%\version.json"

if not exist "%VERSION_JSON_DIR%" mkdir "%VERSION_JSON_DIR%"

echo [4/8] Extracting Firmware Version from config_manager.h...
powershell -Command "$line = Get-Content -Path '%CONFIG_H%' | Select-String 'FIRMWARE_VERSION'; if($line) { $parts = $line.ToString().Split([char]34); if($parts.Length -ge 2) { $v = $parts[1]; Write-Host 'Found Firmware Version:' $v; $j = '{\"version\": \"' + $v + '\"}'; Set-Content -Path '%VERSION_JSON%' -Value $j -Encoding UTF8 } else { Write-Host 'Warning: Could not parse version from line.' } } else { Write-Host 'Warning: FIRMWARE_VERSION not found in header!' }"

REM 5. Get Product Version for Go Build
set "VERSION_INFO=%PROXY_ROOT%\versioninfo.json"
echo [5/8] Reading ProductVersion from versioninfo.json...
for /f "usebackq delims=" %%I in (`powershell -Command "$json = Get-Content -Raw -Path '%VERSION_INFO%'; $obj = ConvertFrom-Json -InputObject $json; $obj.StringFileInfo.ProductVersion"`) do set "APP_VERSION=%%I"

if "%APP_VERSION%"=="" (
    echo Error: Could not extract ProductVersion!
    exit /b 1
)
echo App Version: %APP_VERSION%

REM 6. Prepare Go Environment
echo [6/8] Installing/Updating goversioninfo...
pushd "%PROXY_ROOT%"
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
popd

REM 7. Generate Resources
echo [7/8] Generating Windows Resources (Icon/Manifest)...
REM Run from Project Root so "AscomAlpacaProxy/icon.ico" path in json is valid
pushd "%PROJECT_ROOT%"
"%USERPROFILE%\go\bin\goversioninfo.exe" -64 -o AscomAlpacaProxy/resource.syso AscomAlpacaProxy/versioninfo.json
if %ERRORLEVEL% NEQ 0 (
    echo Error generating resources!
    popd
    exit /b 1
)
popd

REM 8. Build EXE
echo [8/8] Compiling Go executable...
pushd "%PROXY_ROOT%"
if not exist "build" mkdir build
go build -ldflags="-H=windowsgui -X main.AppVersion=%APP_VERSION%" -o build/AscomAlpacaProxy.exe .
if exist resource.syso del resource.syso
popd
echo --- Build Complete: build/AscomAlpacaProxy.exe ---
endlocal
