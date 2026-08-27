# Build Scripts

This folder contains everything needed to build and package the ASCOM Alpaca Proxy, including
compiling and bundling the matching ESP32 firmware.

## Table of Contents

- [Single Source of Truth: `release_version.json`](#single-source-of-truth-release_versionjson)
- [Windows (actively maintained)](#windows-actively-maintained)
- [Linux (not actively maintained)](#linux-not-actively-maintained)
- [GitHub Action: `release-linux.yml`](#github-action-release-linuxyml)
- [Prerequisites](#prerequisites)

## Single Source of Truth: `release_version.json`

The Proxy app and the ESP32 firmware are versioned independently (e.g. Proxy `0.9.17`, firmware
`0.9.16` - they don't have to match). Both version numbers used to be edited by hand in several
places; now there's exactly one place to edit:

```json
{
    "proxyVersion": "0.9.17",
    "firmwareVersion": "0.9.16"
}
```

Every build script below reads this file first and propagates both values everywhere else they're
needed, via one of two equivalent sync scripts:

- **`sync_versions.ps1`** (Windows/PowerShell) - used by `build_exe.bat` and `build_linux.ps1`.
- **`sync_versions.sh`** (bash/`sed`) - used by `build_linux.sh`, for the case where the script
  runs natively on a Linux host where PowerShell isn't available.

Both do the same thing: patch `../versioninfo.json`'s four redundant version fields
(`FixedFileInfo.FileVersion`/`ProductVersion`'s `Major`/`Minor`/`Patch`/`Build`, and
`StringFileInfo.FileVersion`/`ProductVersion`'s string form) with `proxyVersion`, and
`../../src/config_manager.h`'s `#define FIRMWARE_VERSION` with `firmwareVersion`. Both use
targeted find/replace rather than a full JSON re-serialize, so a version bump produces a minimal,
easy-to-review diff instead of reformatting the whole file.

You should never need to run either sync script directly - the build scripts call them
automatically.

## Windows (actively maintained)

### `build_exe.bat`
Builds the plain `AscomAlpacaProxy.exe` (no installer). Steps:
1. Sync versions from `release_version.json` (see above).
2. Build the firmware with PlatformIO (`pio run`) and copy `bootloader.bin`/`partitions.bin`/
   `firmware.bin` into `frontend-vue/public/flasher/firmware/`, the in-app web flasher's asset
   folder. (This does **not** touch `docs/firmware/`, the separate GitHub Pages flasher - that
   stays a deliberate, manual release step.)
3. Build the Vue frontend (`npm install && npm run build`).
4. Extract the firmware version from `config_manager.h` into
   `frontend-vue/dist/flasher/firmware/version.json` (used by the flasher's "bundled firmware"
   version display).
5. Read the Proxy's `ProductVersion` from `versioninfo.json` for the Go build below.
6. Install/update `goversioninfo`.
7. Generate `resource.syso` (Windows icon/manifest) from `versioninfo.json` + `icon.ico`.
8. `go build` → `build/AscomAlpacaProxy.exe` (deleted `resource.syso` afterward).

Requires PlatformIO CLI on `PATH`, or installed at the default
`%USERPROFILE%\.platformio\penv\Scripts\pio.exe` location.

### `build_installer.bat`
Builds on top of `build_exe.bat`: runs it, then fills `installer.iss`'s version placeholders and
compiles the final Windows installer with Inno Setup 6
(`C:\Program Files (x86)\Inno Setup 6\ISCC.exe`, hardcoded path) →
`build/SV241-AscomAlpacaProxy-Setup-<version>.exe`.

### `installer.iss`
The Inno Setup template. Worth knowing:
- Force-kills a running `AscomAlpacaProxy.exe` before both install and uninstall.
- Auto-detects and silently uninstalls a previous version (checks HKLM32/64 and HKCU32/64), then
  force-deletes its old install folder - including configs/logs.
- Autostart is registered under `HKLM` (machine-wide), and an old `HKCU`-based autostart entry
  from earlier versions is actively removed during setup to avoid a double-start after migration.
- On uninstall, `DelTree`s the entire app directory after the normal uninstall step - aggressive
  cleanup, also removes logs/configs.
- Also installs `Helper/Create-Driver.bat` + `Helper/Create-AscomDriver.ps1` (the classic-ASCOM
  driver registration helper - separate from this build pipeline).

## Linux (not actively maintained)

> **Maintenance status:** the maintainer doesn't use Linux and can't test these scripts against a
> real Linux host or a Raspberry Pi. They're kept up to date with the version-sync mechanism
> above on a best-effort basis, but the actual `go build`/systemd/install behavior is
> **unverified**. If you use these and find a bug, PRs are welcome.

### `build_linux.ps1`
Runs on Windows, cross-compiles for Linux (no installer, just raw binaries). Same version-sync,
firmware-build, and frontend-build steps as `build_exe.bat` (firmware via PlatformIO, copied into
`frontend-vue/public/flasher/firmware/` - see `build_exe.bat`'s step 2 above; done here too so the
in-app flasher this produces always embeds firmware matching the version it claims, not whatever
happened to be last committed), plus a step that ensures a CGO-capable cross-compile toolchain is
present (see `ensure_linux_crosscompile_toolchain.ps1` below), then two `go build` passes:
`GOOS=linux GOARCH=amd64` → `build/AscomAlpacaProxy-linux-amd64`, and `GOOS=linux GOARCH=arm64`
(Raspberry Pi 4/5, 64-bit) → `build/AscomAlpacaProxy-linux-arm64`.
Useful for a local, ad-hoc Linux build from a Windows machine; the actual GitHub release binaries
are now produced by the [`release-linux.yml`](#github-action-release-linuxyml) GitHub Action
instead, which builds natively on real Linux runners rather than cross-compiling.

CGO is required because `internal/serial/ch340_linux.go` (Linux-only) talks to the SV241's CH340
chip directly over libusb instead of going through the kernel's `ch341` tty driver, which
otherwise resets the ESP32 on every connect - see that file for the full story. This was **not**
needed before that fix landed; a plain `CGO_ENABLED=0` cross-compile used to be enough.

### `ensure_linux_crosscompile_toolchain.ps1`
Helper called by `build_linux.ps1`, not meant to be run directly. On first run, downloads and
caches (under `build/crosscompile-cache/`, gitignored) a real cross C compiler
([Zig](https://ziglang.org/), used via `zig cc -target <arch>-linux-gnu.2.31`) plus libusb-1.0's
header and linkable `.so`, extracted directly from the official Debian 12 "bookworm"
`libusb-1.0-0-dev`/`libusb-1.0-0` packages, for both `amd64` and `arm64`. Later runs reuse the
cache. Full rationale, exact package versions/URLs, and what to do if the pinned Debian package
URL ever goes stale (old versions eventually get purged from the pool) are documented in the
script's own header comment.

**This path (Zig + the extracted sysroots) was verified end-to-end against real hardware**:
binaries built through it were copied to a Raspberry Pi and passed the same test as a
natively-built binary - an output left on, the process hard-killed and restarted, state preserved,
zero ESP32 reset-banner occurrences in the log.

### `build_linux.sh`
A native build script, meant to run directly on a Linux host (it relies on the host's own default
`GOOS`/`GOARCH` rather than setting them explicitly - running it via Git Bash on Windows will
silently produce a Windows binary, not a Linux one). Detects its own architecture (`uname -m`,
same `x86_64`→`amd64`/`aarch64`→`arm64` mapping `install_linux.sh` uses) and names its output
`build/AscomAlpacaProxy-linux-<amd64|arm64>`, matching `build_linux.ps1`'s naming - this is what
[`release-linux.yml`](#github-action-release-linuxyml) actually runs, on real `amd64`/`arm64`
GitHub-hosted Linux runners, to produce the release binaries.

Also builds the firmware itself (via PlatformIO, installed automatically via `pip` if the `pio`
CLI isn't already on `PATH`) and copies it into `frontend-vue/public/flasher/firmware/`, same as
`build_exe.bat`/`build_linux.ps1` - so the in-app flasher this produces always matches whatever
`release_version.json` currently says, not whatever `.bin` files happened to be last committed.

Needs a C compiler and libusb-1.0's headers on the build host for the same CGO reason as
`build_linux.ps1` above (e.g. `sudo apt-get install -y gcc libusb-1.0-0-dev` on
Debian/Raspberry Pi OS) - unlike the Windows script, no extra toolchain setup is needed here since
Go's cgo just uses the host's own compiler directly.

Its firmware-version extraction (`grep -oP`) needs a UTF-8 locale to work correctly; on a real
Linux distro that's normally the default, but if you ever see a
`grep: -P supports only unibyte and UTF-8 locales` warning, set e.g. `LC_ALL=C.UTF-8` before
running the script.

### `install_linux.sh`
A one-line installer for end users (`curl ... | sudo bash`): downloads the latest GitHub release
binary matching the host's architecture (`x86_64`→`amd64`, `aarch64`→`arm64`), installs it to
`/usr/local/bin`, sets up a `systemd` service (`sv241-alpaca-proxy`), adds the invoking user to
the `dialout` group for serial port access, and installs a udev rule
(`/etc/udev/rules.d/99-sv241-usb.rules`) granting the raw USB access the proxy needs to talk to
the SV241's CH340 chip directly via libusb (bypassing the kernel's ch341 tty driver - see
`internal/serial/ch340_linux.go` - which is what stops the ESP32 resetting on every connect).
Also makes sure the `libusb-1.0-0` runtime library is actually installed (via `apt-get` if
missing) before starting the service, since that's a hard runtime requirement for the same reason
and isn't guaranteed present on a minimal/headless image. Expects release assets named
`AscomAlpacaProxy-linux-<amd64|arm64>`, matching both `build_linux.ps1`'s and `build_linux.sh`'s
output naming.

## GitHub Action: `release-linux.yml`

`.github/workflows/release-linux.yml` - **manual only** (`workflow_dispatch`, no auto-trigger on
release publish, matching the "not actively maintained" caution above). Run it from the repo's
Actions tab ("Run workflow"), optionally giving a specific release tag to target (defaults to
whatever the current `latest` release is).

Builds `AscomAlpacaProxy-linux-amd64` and `-arm64` **natively** in parallel, on real
`ubuntu-24.04` and `ubuntu-24.04-arm` GitHub-hosted runners (Linux arm64 hosted runners are free
for public repos) - by installing `gcc`/`libusb-1.0-0-dev` and PlatformIO (`pip install
platformio`), then running `build_linux.sh` unmodified on each (which, among other things, also
compiles the actual firmware and bundles it into the flasher - see `build_linux.sh` above). No
cross-compile toolchain involved at all; that complexity is now specific to the optional local
`build_linux.ps1` path. Both binaries plus `install_linux.sh` are then
uploaded to the target release (`gh release upload ... --clobber`, so re-running it replaces
existing same-named assets - useful for fixing a bad upload, not just adding new ones). Includes a
CRLF line-ending sanity check on `install_linux.sh` right before upload, guarding against the
exact bug (a Windows working-tree copy uploaded by hand, crashing immediately on Linux) that
`.gitattributes` fixes at the source but this catches again just in case.

Windows assets (`AscomAlpacaProxy.exe` / the installer) are **not** covered by this workflow and
stay a manual `build_installer.bat` + manual upload step.

## Prerequisites

| Tool | Used by | Notes |
|---|---|---|
| [PlatformIO CLI](https://platformio.org/) | `build_exe.bat` | Firmware compile step. Default path: `%USERPROFILE%\.platformio\penv\Scripts\pio.exe` |
| [Go](https://go.dev/) | all build scripts | `go build` for the Proxy backend |
| [Node.js](https://nodejs.org/) / npm | all build scripts | Vue frontend build |
| [Inno Setup 6](https://jrsoftware.org/isinfo.php) | `build_installer.bat` | Hardcoded path: `C:\Program Files (x86)\Inno Setup 6\ISCC.exe` |
