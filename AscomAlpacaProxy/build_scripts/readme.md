# Build Scripts

This folder contains everything needed to build and package the ASCOM Alpaca Proxy, including
compiling and bundling the matching ESP32 firmware.

## Table of Contents

- [Single Source of Truth: `release_version.json`](#single-source-of-truth-release_versionjson)
- [Windows (actively maintained)](#windows-actively-maintained)
- [Linux (not actively maintained)](#linux-not-actively-maintained)
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
Runs on Windows, cross-compiles for Linux (no installer, just raw binaries). Same version-sync +
frontend-build steps as `build_exe.bat`, then two `go build` passes:
`GOOS=linux GOARCH=amd64` → `build/AscomAlpacaProxy-linux-amd64`, and
`GOOS=linux GOARCH=arm64` (Raspberry Pi 4/5, 64-bit) → `build/AscomAlpacaProxy-linux-arm64`.
This is the script actually used to produce the binaries attached to GitHub releases.

### `build_linux.sh`
A native build script, meant to run directly on a Linux host (it relies on the host's own default
`GOOS`/`GOARCH` rather than setting them explicitly - running it via Git Bash on Windows will
silently produce a Windows binary, not a Linux one). Outputs the plain, unqualified
`build/AscomAlpacaProxy` - unlike `build_linux.ps1`'s `-linux-amd64`/`-linux-arm64` suffixed
names, so it's not a drop-in replacement for producing release assets as-is.

Its firmware-version extraction (`grep -oP`) needs a UTF-8 locale to work correctly; on a real
Linux distro that's normally the default, but if you ever see a
`grep: -P supports only unibyte and UTF-8 locales` warning, set e.g. `LC_ALL=C.UTF-8` before
running the script.

### `install_linux.sh`
A one-line installer for end users (`curl ... | sudo bash`): downloads the latest GitHub release
binary matching the host's architecture (`x86_64`→`amd64`, `aarch64`→`arm64`), installs it to
`/usr/local/bin`, sets up a `systemd` service (`sv241-alpaca-proxy`), and adds the invoking user to
the `dialout` group for serial port access. Expects release assets named
`AscomAlpacaProxy-linux-<amd64|arm64>` - i.e. `build_linux.ps1`'s output naming, not
`build_linux.sh`'s.

## Prerequisites

| Tool | Used by | Notes |
|---|---|---|
| [PlatformIO CLI](https://platformio.org/) | `build_exe.bat` | Firmware compile step. Default path: `%USERPROFILE%\.platformio\penv\Scripts\pio.exe` |
| [Go](https://go.dev/) | all build scripts | `go build` for the Proxy backend |
| [Node.js](https://nodejs.org/) / npm | all build scripts | Vue frontend build |
| [Inno Setup 6](https://jrsoftware.org/isinfo.php) | `build_installer.bat` | Hardcoded path: `C:\Program Files (x86)\Inno Setup 6\ISCC.exe` |
