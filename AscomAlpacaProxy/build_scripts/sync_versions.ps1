# Reads release_version.json and syncs both version numbers everywhere else they're needed:
# - versioninfo.json's 4 redundant fields (FixedFileInfo.FileVersion/ProductVersion numeric
#   parts, StringFileInfo.FileVersion/ProductVersion strings) that goversioninfo requires.
# - src/config_manager.h's FIRMWARE_VERSION #define.
#
# Single source of truth: edit release_version.json only, this script (called from
# build_exe.bat) keeps everything else in sync automatically.
param(
    [Parameter(Mandatory = $true)][string]$ProjectRoot,
    [Parameter(Mandatory = $true)][string]$ProxyRoot
)
$ErrorActionPreference = "Stop"

# Writes text without a BOM and without appending an extra trailing newline, matching what
# Set-Content/-Encoding UTF8 does NOT do in Windows PowerShell 5.1 (it both adds a BOM and can
# append a newline) - keeps the diff to just the actual version-number change.
$Utf8NoBom = New-Object System.Text.UTF8Encoding $false
function Write-TextExact([string]$Path, [string]$Content) {
    [System.IO.File]::WriteAllText($Path, $Content, $Utf8NoBom)
}

$releaseVersionFile = Join-Path $PSScriptRoot "release_version.json"
if (-not (Test-Path $releaseVersionFile)) {
    throw "release_version.json not found at $releaseVersionFile"
}

$rel = Get-Content -Raw -Path $releaseVersionFile | ConvertFrom-Json
$proxyVer = $rel.proxyVersion
$fwVer = $rel.firmwareVersion
if ([string]::IsNullOrWhiteSpace($proxyVer) -or [string]::IsNullOrWhiteSpace($fwVer)) {
    throw "release_version.json must set both proxyVersion and firmwareVersion"
}

# --- versioninfo.json ---
# Targeted regex replace (not parse+re-serialize) to avoid ConvertTo-Json reformatting the file.
# Safe here because "Major"/"Minor"/"Patch"/"Build" only ever appear under FixedFileInfo's two
# version objects, both of which should always hold the same value - and the FileVersion/
# ProductVersion regexes only match the *string* form (StringFileInfo's), not the object form
# (FixedFileInfo's), because they require a quote immediately after the colon.
#
# FixedFileInfo's four fields are plain integers (PE resource format), so a prerelease suffix
# like "-daily.20260903+a1b2c3d" (proxyVersion can carry one, e.g. for daily builds) has to be
# stripped before splitting on '.' - only the numeric core goes into Major/Minor/Patch/Build.
# The *string* fields below (FileVersion/ProductVersion) keep the full $proxyVer, suffix and all.
$core = $proxyVer.Split('-')[0]
$parts = $core.Split('.')
$major = [int]$parts[0]
$minor = [int]$parts[1]
$patch = if ($parts.Length -ge 3) { [int]$parts[2] } else { 0 }
$build = if ($parts.Length -ge 4) { [int]$parts[3] } else { 0 }

$viPath = Join-Path $ProxyRoot "versioninfo.json"
$vi = Get-Content -Raw -Path $viPath
$vi = $vi -replace '"Major":\s*\d+', "`"Major`": $major"
$vi = $vi -replace '"Minor":\s*\d+', "`"Minor`": $minor"
$vi = $vi -replace '"Patch":\s*\d+', "`"Patch`": $patch"
$vi = $vi -replace '"Build":\s*\d+', "`"Build`": $build"
$vi = $vi -replace '"FileVersion":\s*"[^"]*"', "`"FileVersion`": `"$proxyVer`""
$vi = $vi -replace '"ProductVersion":\s*"[^"]*"', "`"ProductVersion`": `"$proxyVer`""
Write-TextExact $viPath $vi
Write-Host "versioninfo.json set to $proxyVer"

# --- src/config_manager.h ---
$chPath = Join-Path $ProjectRoot "src\config_manager.h"
$ch = Get-Content -Raw -Path $chPath
$ch = $ch -replace '#define FIRMWARE_VERSION "[^"]*"', "#define FIRMWARE_VERSION `"$fwVer`""
Write-TextExact $chPath $ch
Write-Host "config_manager.h FIRMWARE_VERSION set to $fwVer"
