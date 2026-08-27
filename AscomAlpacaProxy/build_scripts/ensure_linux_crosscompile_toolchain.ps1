param(
    [Parameter(Mandatory = $true)][string]$CacheDir
)
$ErrorActionPreference = "Stop"

# ==============================================================================
# Ensures a CGO-capable Windows -> Linux cross-compile toolchain is present under
# $CacheDir, downloading and caching it on first run. Used by build_linux.ps1.
#
# Why this exists: internal/serial/ch340_linux.go (Linux-only) talks to the SV241's
# CH340 chip directly over libusb via github.com/google/gousb, to work around a Linux
# kernel tty-layer behavior that resets the ESP32 on every serial port open. That
# package needs cgo + a real libusb-1.0 C library (headers + linkable .so) for the
# *target* architecture, not the host - something `go build`'s normal Windows->Linux
# cross-compile (CGO_ENABLED=0) can't do on its own.
#
# What gets cached here (all under $CacheDir, safe to delete entirely to force a
# clean re-fetch - nothing here is hand-edited):
#   zig-x86_64-windows-<ver>\zig.exe   - Zig's bundled clang, used as `zig cc -target
#                                        <arch>-linux-gnu.2.31` - a real cross C
#                                        compiler + bundled glibc headers, no separate
#                                        Linux toolchain install needed on Windows.
#   sysroot-amd64\, sysroot-arm64\     - libusb-1.0's header + linkable .so, extracted
#                                        from the official Debian 12 "bookworm"
#                                        libusb-1.0-0-dev / libusb-1.0-0 packages.
#                                        Only the small header/lib files are used here
#                                        - the actual runtime libusb-1.0.so.0 that gets
#                                        loaded when the built binary runs comes from
#                                        the target Linux system's own installation
#                                        (see install_linux.sh), not from this sysroot.
#   pkgconfig-stub.bat                 - gousb's source declares "#cgo pkg-config:
#                                        libusb-1.0", so `go build` always shells out to
#                                        a `pkg-config` binary regardless of any
#                                        CGO_CFLAGS/CGO_LDFLAGS set directly. Real
#                                        pkg-config isn't available (or meaningfully
#                                        configurable for a foreign-arch sysroot) on a
#                                        plain Windows box, so this tiny stand-in
#                                        (pointed at via the PKG_CONFIG env var) just
#                                        echoes the right -I/-L flags for whichever
#                                        sysroot build_linux.ps1 is currently building.
#
# Verified end-to-end against real hardware (2026-08-27): binaries built through this
# exact toolchain were copied to a Raspberry Pi and passed the same reset-preservation
# test as a natively-built binary (dc1 left on, process hard-killed and restarted,
# state preserved, zero ESP32 reset-banner occurrences in the log).
#
# Maintenance note: like the rest of build_linux.*, this isn't covered by CI and the
# maintainer doesn't test it on an ongoing basis (see build_scripts/readme.md). The
# Debian package version below is pinned to a specific, already-verified URL for
# reproducibility; if it ever 404s (Debian's pool eventually purges old package
# versions once nothing references them), look up the current libusb-1.0-0-dev /
# libusb-1.0-0 package versions at https://packages.debian.org/libusb-1.0-0-dev and
# update $libusbVersion below, or pull an immutable historical copy of this exact
# version from https://snapshot.debian.org/package/libusb-1.0/ instead.
# ==============================================================================

$zigVersion = "0.16.0"
$zigDir = Join-Path $CacheDir "zig-x86_64-windows-$zigVersion"
$zigExe = Join-Path $zigDir "zig.exe"

# Debian 12 "bookworm" libusb-1.0 - any recent 1.0.x works fine (libusb's SONAME/ABI,
# libusb-1.0.so.0, has been stable for well over a decade), this version was the one
# actually built and verified against real hardware.
$libusbVersion = "1.0.26-1"
$debianPoolBase = "http://ftp.debian.org/debian/pool/main/libu/libusb-1.0"

# arch key -> [Debian arch tag, GNU triplet]
$archMap = @{
    "amd64" = @{ DebianArch = "amd64"; Triplet = "x86_64-linux-gnu" }
    "arm64" = @{ DebianArch = "arm64"; Triplet = "aarch64-linux-gnu" }
}

New-Item -ItemType Directory -Path $CacheDir -Force | Out-Null

# --- Zig (cross C compiler + bundled glibc headers) ---
if (Test-Path $zigExe) {
    Write-Host "  Zig $zigVersion already cached." -ForegroundColor DarkGray
} else {
    Write-Host "  Downloading Zig $zigVersion (one-time, ~100 MB)..." -ForegroundColor Yellow
    $zigZip = Join-Path $CacheDir "zig-$zigVersion.zip"
    $zigUrl = "https://ziglang.org/download/$zigVersion/zig-x86_64-windows-$zigVersion.zip"
    try {
        Invoke-WebRequest -Uri $zigUrl -OutFile $zigZip -UseBasicParsing
    } catch {
        throw "Failed to download Zig from $zigUrl - check your internet connection, or verify this URL still exists at https://ziglang.org/download/ (Zig version may need bumping). Original error: $_"
    }
    Expand-Archive -Path $zigZip -DestinationPath $CacheDir -Force
    Remove-Item $zigZip -Force
    if (-not (Test-Path $zigExe)) {
        throw "Zig archive extracted but $zigExe was not found - the archive layout may have changed."
    }
    Write-Host "  Zig $zigVersion ready." -ForegroundColor Green
}

# --- Extracts a Debian .deb package (an ar archive containing control.tar.* and
#     data.tar.*) into $DestDir, using only stock PowerShell (for the ar container)
#     and Windows' own bundled tar.exe (for the inner data.tar.xz) - no 7-Zip or other
#     third-party tool required. ---
function Expand-DebPackage {
    param(
        [Parameter(Mandatory = $true)][string]$DebPath,
        [Parameter(Mandatory = $true)][string]$DestDir
    )
    $bytes = [System.IO.File]::ReadAllBytes($DebPath)
    $magic = [System.Text.Encoding]::ASCII.GetString($bytes, 0, 8)
    if ($magic -ne "!<arch>`n") {
        throw "$DebPath does not look like a .deb (ar) archive (bad magic)."
    }

    $offset = 8
    $dataTarPath = $null
    while ($offset + 60 -le $bytes.Length) {
        $header = [System.Text.Encoding]::ASCII.GetString($bytes, $offset, 60)
        $name = $header.Substring(0, 16).Trim()
        $size = [int64]($header.Substring(48, 10).Trim())
        $contentStart = $offset + 60

        if ($name -like "data.tar*") {
            $ext = switch -Wildcard ($name) {
                "*.xz" { ".tar.xz" }
                "*.gz" { ".tar.gz" }
                "*.zst" { ".tar.zst" }
                default { ".tar" }
            }
            $dataTarPath = Join-Path $DestDir "data$ext"
            [System.IO.File]::WriteAllBytes($dataTarPath, $bytes[$contentStart..($contentStart + $size - 1)])
        }

        # ar members are padded to an even byte boundary.
        $advance = $size
        if ($advance % 2 -ne 0) { $advance++ }
        $offset = $contentStart + $advance
    }

    if (-not $dataTarPath) {
        throw "Could not find a data.tar.* member inside $DebPath - unexpected .deb layout."
    }

    New-Item -ItemType Directory -Path $DestDir -Force | Out-Null
    # These packages contain real symlinks (libusb-1.0.so -> libusb-1.0.so.0 ->
    # libusb-1.0.so.0.x.y), and creating an actual filesystem symlink on Windows requires an
    # elevated process or Developer Mode - something this script can't assume. tar.exe fails
    # (non-zero exit + a stderr line) on just those symlink entries but, verified, still
    # extracts every regular file in the same archive successfully - the caller below checks
    # for and repairs the specific file it actually needs, so that failure is expected and
    # intentionally not fatal here.
    #
    # Routed through "cmd /c ... 2>nul" rather than called directly: PowerShell 5.1 turns each
    # line a native command writes to stderr into a terminating NativeCommandError under
    # $ErrorActionPreference = "Stop" as soon as *anything* in the call chain captures stderr
    # (a bare, unredirected call is fine on its own, but this script can be invoked from inside
    # something like `build_linux.ps1 2>&1 | Tee-Object ...` for logging, which reintroduces
    # exactly that capture several stack frames up - verified this actually happens). Letting
    # cmd.exe apply the "2>nul" itself keeps tar's stderr from ever reaching a PowerShell stream
    # in the first place, independent of what any caller happens to do.
    cmd /c "tar -xf `"$dataTarPath`" -C `"$DestDir`" 2>nul"
    Remove-Item $dataTarPath -Force
}

# --- libusb-1.0 sysroots (header + linkable .so per target arch) ---
foreach ($archKey in $archMap.Keys) {
    $debianArch = $archMap[$archKey].DebianArch
    $triplet = $archMap[$archKey].Triplet
    $sysrootDir = Join-Path $CacheDir "sysroot-$archKey"
    $libusbSoPath = Join-Path $sysrootDir "usr\lib\$triplet\libusb-1.0.so"

    if (Test-Path $libusbSoPath) {
        Write-Host "  libusb-1.0 sysroot for $archKey already cached." -ForegroundColor DarkGray
        continue
    }

    Write-Host "  Fetching libusb-1.0 $libusbVersion for $archKey..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Path $sysrootDir -Force | Out-Null

    # Runtime package first, then -dev: the -dev package's unversioned "libusb-1.0.so"
    # is (nominally) a symlink to the runtime package's real libusb-1.0.so.0.x.y file,
    # so the target needs to already exist for that to resolve cleanly on extraction.
    foreach ($debName in @("libusb-1.0-0_${libusbVersion}_${debianArch}.deb", "libusb-1.0-0-dev_${libusbVersion}_${debianArch}.deb")) {
        $debUrl = "$debianPoolBase/$debName"
        $debPath = Join-Path $CacheDir $debName
        try {
            Invoke-WebRequest -Uri $debUrl -OutFile $debPath -UseBasicParsing
        } catch {
            throw "Failed to download $debUrl - this pinned Debian package version may have been purged from the pool. See the maintenance note at the top of this script for how to fix that. Original error: $_"
        }
        Expand-DebPackage -DebPath $debPath -DestDir $sysrootDir
        Remove-Item $debPath -Force
    }

    if (-not (Test-Path $libusbSoPath)) {
        # The .deb's own libusb-1.0.so symlink didn't get created (see the note in
        # Expand-DebPackage above) - materialize it as a plain file copy of the real, fully
        # versioned .so instead. Identical from the linker's point of view; only a real
        # filesystem symlink needed the privilege that just wasn't available.
        $libDir = Join-Path $sysrootDir "usr\lib\$triplet"
        $versionedSo = Get-ChildItem -Path $libDir -Filter "libusb-1.0.so.*" -File -ErrorAction SilentlyContinue |
            Sort-Object { $_.Name.Length } -Descending |
            Select-Object -First 1
        if ($versionedSo) {
            Copy-Item $versionedSo.FullName $libusbSoPath
        }
    }

    if (-not (Test-Path $libusbSoPath)) {
        throw "Extracted the libusb-1.0 packages for $archKey but could not produce $libusbSoPath - .deb layout may have changed."
    }
    Write-Host "  libusb-1.0 sysroot for $archKey ready." -ForegroundColor Green
}

# --- pkg-config stand-in (see the top-of-file comment) - cheap to write, always kept
#     in sync rather than only generated once. ---
$pkgConfigStub = Join-Path $CacheDir "pkgconfig-stub.bat"
@'
@echo off
rem Minimal pkg-config stand-in for cross-compiling gousb's "#cgo pkg-config: libusb-1.0"
rem directive from Windows. build_linux.ps1 points PKG_CONFIG at this file and sets
rem PKG_CONFIG_SYSROOT/PKG_CONFIG_TRIPLET per target arch before each go build.
if "%~1"=="--cflags" (
  echo -I%PKG_CONFIG_SYSROOT%/usr/include -I%PKG_CONFIG_SYSROOT%/usr/include/libusb-1.0
  goto :eof
)
if "%~1"=="--libs" (
  echo -L%PKG_CONFIG_SYSROOT%/usr/lib/%PKG_CONFIG_TRIPLET% -lusb-1.0
  goto :eof
)
'@ | Set-Content -Path $pkgConfigStub -Encoding ASCII -NoNewline

# --- Report back the resolved paths the caller needs, keyed by arch. ---
$result = @{
    ZigExe = $zigExe
    PkgConfigStub = $pkgConfigStub
    Arches = @{}
}
foreach ($archKey in $archMap.Keys) {
    $result.Arches[$archKey] = @{
        Triplet = $archMap[$archKey].Triplet
        SysrootDir = (Join-Path $CacheDir "sysroot-$archKey") -replace '\\', '/'
    }
}
return $result
