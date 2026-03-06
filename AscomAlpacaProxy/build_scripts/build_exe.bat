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
    echo [0/5] Cleaning previous build...
    del "..\build\AscomAlpacaProxy.exe"
)

REM 1. Build Frontend
echo [1/6] Building Frontend...
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

REM 2. Extract Firmware Version
set "CONFIG_H=%PROJECT_ROOT%\src\config_manager.h"
set "VERSION_JSON_DIR=%PROXY_ROOT%\frontend-vue\dist\flasher\firmware"
set "VERSION_JSON=%VERSION_JSON_DIR%\version.json"

if not exist "%VERSION_JSON_DIR%" mkdir "%VERSION_JSON_DIR%"

echo [2/6] Extracting Firmware Version from config_manager.h...
powershell -Command "$line = Get-Content -Path '%CONFIG_H%' | Select-String 'FIRMWARE_VERSION'; if($line) { $parts = $line.ToString().Split([char]34); if($parts.Length -ge 2) { $v = $parts[1]; Write-Host 'Found Firmware Version:' $v; $j = '{\"version\": \"' + $v + '\"}'; Set-Content -Path '%VERSION_JSON%' -Value $j -Encoding UTF8 } else { Write-Host 'Warning: Could not parse version from line.' } } else { Write-Host 'Warning: FIRMWARE_VERSION not found in header!' }"

REM 3. Get Product Version for Go Build
set "VERSION_INFO=%PROXY_ROOT%\versioninfo.json"
echo [3/6] Reading ProductVersion from versioninfo.json...
for /f "usebackq delims=" %%I in (`powershell -Command "$json = Get-Content -Raw -Path '%VERSION_INFO%'; $obj = ConvertFrom-Json -InputObject $json; $obj.StringFileInfo.ProductVersion"`) do set "APP_VERSION=%%I"

if "%APP_VERSION%"=="" (
    echo Error: Could not extract ProductVersion!
    exit /b 1
)
echo App Version: %APP_VERSION%

REM 4. Prepare Go Environment
echo [4/6] Installing/Updating goversioninfo...
pushd "%PROXY_ROOT%"
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
popd

REM 5. Generate Resources
echo [5/6] Generating Windows Resources (Icon/Manifest)...
REM Run from Project Root so "AscomAlpacaProxy/icon.ico" path in json is valid
pushd "%PROJECT_ROOT%"
"%USERPROFILE%\go\bin\goversioninfo.exe" -64 -o AscomAlpacaProxy/resource.syso AscomAlpacaProxy/versioninfo.json
if %ERRORLEVEL% NEQ 0 (
    echo Error generating resources!
    popd
    exit /b 1
)
popd

REM 6. Build EXE
echo [6/6] Compiling Go executable...
pushd "%PROXY_ROOT%"
if not exist "build" mkdir build
go build -ldflags="-H=windowsgui -X main.AppVersion=%APP_VERSION%" -o build/AscomAlpacaProxy.exe .
if exist resource.syso del resource.syso
popd
echo --- Build Complete: build/AscomAlpacaProxy.exe ---
endlocal
