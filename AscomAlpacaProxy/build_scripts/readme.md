# Build Scripts

[← Back to main readme](../../readme.md)

This folder contains everything needed to build and package the SV241 Alpaca Proxy, including
compiling and bundling the matching ESP32 firmware.

## Table of Contents

- [Project Structure](#project-structure)
- [Single Source of Truth: `release_version.json`](#single-source-of-truth-release_versionjson)
- [Firmware isn't committed to the repo](#firmware-isnt-committed-to-the-repo)
- [Windows (actively maintained)](#windows-actively-maintained)
- [Local Development (Hot Reload)](#local-development-hot-reload)
- [Linux (not actively maintained)](#linux-not-actively-maintained)
- [GitHub Actions: `release-*.yml`](#github-actions-release-yml)
- [Cutting a beta / pre-release](#cutting-a-beta--pre-release)
- [Prerequisites](#prerequisites)

## Project Structure

```
AscomAlpacaProxy/
├── frontend-vue/     # Vue 3 SPA (web interface)
│   ├── src/          # Vue components & stores
│   ├── public/       # Static assets (flasher, favicon)
│   └── dist/         # Build output (generated)
├── internal/         # Go backend modules
├── build/            # Compiled .exe (generated)
└── install/          # Installer output (generated)
```

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

## Firmware isn't committed to the repo

`frontend-vue/public/flasher/firmware/*.bin` (the in-app flasher's asset folder) and
`webflasher/firmware/*.bin` (the standalone GitHub Pages flasher's) are `.gitignore`d, not committed.
Every build path below regenerates them from `src/` before it needs them - there's exactly one
place firmware bytes come from (compiling the C++ source, pinned to a specific `platformio.ini`
platform version for reproducibility), never a hand-maintained binary that can silently drift out
of sync with what `release_version.json` claims.

Locally, `build_exe.bat`/`build_linux.sh`/`build_linux.ps1` each just compile the firmware
themselves via PlatformIO, same as always. In CI, `release-firmware.yml` builds it **once** and
publishes it as `firmware.zip` on the GitHub release; `release-windows.yml`/`release-linux.yml`/
`release-webflasher.yml` all download that same zip instead of each compiling their own copy
(faster, and guarantees byte-identical firmware across every release asset). Set the
`SKIP_FIRMWARE_BUILD=1` environment variable to make `build_exe.bat`/`build_linux.sh`/
`build_linux.ps1` skip their own build step and use whatever's already sitting in the flasher
asset folder - only ever set by the CI workflows, never for a local build (deliberately not a
"does the file already exist" check - that gitignored folder can easily still hold a stale `.bin`
from an earlier local build, which would silently skip rebuilding after a real source change).

## Windows (actively maintained)

### `build_exe.bat`
Builds the plain `AscomAlpacaProxy.exe` (no installer). Steps:
1. Sync versions from `release_version.json` (see above).
2. Build the firmware with PlatformIO (`pio run`) and copy `bootloader.bin`/`partitions.bin`/
   `firmware.bin` into `frontend-vue/public/flasher/firmware/`, the in-app web flasher's asset
   folder. (This does **not** touch `webflasher/firmware/`, the separate GitHub Pages flasher -
   that stays a deliberate, manual release step.)
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

## Local Development (Hot Reload)

```bash
cd AscomAlpacaProxy/frontend-vue
npm run dev     # Dev server: http://localhost:5173
```
> [!NOTE]
> The dev server proxies API requests to the running Go proxy on port 32241.

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
are now produced by the [`release-linux.yml`](#github-actions-release-yml) GitHub Action
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
[`release-linux.yml`](#github-actions-release-yml) actually runs, on real `amd64`/`arm64`
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

Since the CH340's VID/PID (`1a86:7523`) isn't unique to the SV241 - the same chip shows up in
countless other cheap USB-serial adapters and Arduino clones - `ch340_linux.go` pins to the
specific USB bus/port path of whichever device it last connected to successfully (persisted
through the existing `conf.SerialPortName` mechanism), falling back to checking whatever's
currently connected only on the very first connect, or after the SV241 moves to a different port.
That fallback tries every candidate gently (no DTR/RTS) before forcing a reset on any of them, and
only escalates to a DTR/RTS release, one candidate at a time, if none respond gently - verified
against real hardware with two CH340 devices connected, both with and without a prior pin: the
SV241 is found within one connection attempt either way, and an unrelated candidate that answers
normally is never touched. See that file's `ch340PathID`/`ch340Candidates`/`tryOpenCH340` for the
full mechanism; see `PINS_INSTALL.md` for the user-facing version of this note.

Set `SV241_RELEASE_TAG` to install from a specific release instead of whatever's current
`latest` - the only way to point someone at a beta/pre-release, since GitHub itself never treats
a pre-release as `latest`:
```bash
curl -sSL .../install_linux.sh | sudo SV241_RELEASE_TAG=v0.9.21-beta.1 bash
```
(As a `sudo` argument, not before the pipeline - `sudo` doesn't pass through the invoking shell's
environment variables otherwise.)

Set `SV241_UNINSTALL=1` (same argument-position rule) to remove everything the script itself
installed - service, udev rule, binary - and exit, instead of installing:
```bash
curl -sSL .../install_linux.sh | sudo SV241_UNINSTALL=1 bash
```
Leaves the `dialout` group membership, the `libusb-1.0-0` package, and
`~/.config/SV241AlpacaProxy/` (saved config/logs) alone on purpose - other things on the system
may depend on the first two, and the config is worth keeping in case of a reinstall.

## GitHub Actions: `release-*.yml`

Five workflows, all **manual only** (`workflow_dispatch`, no auto-trigger on release publish,
matching the "not actively maintained" caution above for the Linux-related ones). All take an
optional `release_tag` input (blank = whatever's current `latest`) - none of them *create*
releases, they only upload assets to one that already exists (create it by hand first: tag,
release notes, mark as pre-release if it's a beta).

- **`release-firmware.yml`** - builds the firmware (PlatformIO, same pinned platform as every
  other path) and publishes it as `firmware.zip` (bootloader/partitions/firmware `.bin` + a
  `version.json`) on the target release. Run this first - everything else downloads from here
  instead of compiling its own copy.
- **`release-windows.yml`** - downloads `firmware.zip`, builds the Windows installer
  (`windows-latest` runner: Go, Node, Inno Setup via Chocolatey, `build_installer.bat` with
  `SKIP_FIRMWARE_BUILD=1`), uploads it. **New, first time this project has used a Windows Actions
  runner** - untested beyond local review, most likely to need a follow-up fix on its first real
  run (Inno Setup's Chocolatey install path matching `build_installer.bat`'s hardcoded `ISCC.exe`
  path is the main risk).
- **`release-linux.yml`** - downloads `firmware.zip`, builds `AscomAlpacaProxy-linux-amd64`/
  `-arm64` **natively** in parallel on real `ubuntu-24.04`/`ubuntu-24.04-arm` runners (Linux arm64
  runners are free for public repos) via `build_linux.sh` with `SKIP_FIRMWARE_BUILD=1`, uploads
  them plus `install_linux.sh`. Includes a CRLF line-ending sanity check on `install_linux.sh`
  right before upload, guarding against the exact bug (a Windows working-tree copy uploaded by
  hand, crashing immediately on Linux) that `.gitattributes` fixes at the source but this catches
  again just in case.
- **`release-webflasher.yml`** - downloads `firmware.zip`, substitutes its version into
  `webflasher/index.html`'s `__FIRMWARE_VERSION__` placeholder, deploys the standalone flasher
  page via GitHub's Actions-based Pages deployment (`actions/upload-pages-artifact` +
  `actions/deploy-pages`) - **not** by committing to `webflasher/`. Requires the repo's Pages
  source set to "GitHub Actions" in Settings > Pages (already done), not "Deploy from a branch".
- **`release-all.yml`** - runs all four in order, Firmware → Windows → Linux → Webflasher,
  strictly sequential (`needs:` chain, not parallel) so a failure stops the chain before anything
  downstream builds against a bad artifact. **For a normal stable release only** - see below for
  why betas should run the first three individually instead.

`gh release upload ... --clobber` throughout, so re-running any of these replaces existing
same-named assets on the target release - useful for fixing a bad upload, not just adding new
ones.

## Cutting a beta / pre-release

Don't use `release-all.yml` for this: it always runs `release-webflasher.yml` too, which deploys
the single, public, always-live GitHub Pages site - that should stay on stable firmware, not
whatever the latest beta happens to be. Instead:

1. Bump `release_version.json` to the beta version (e.g. `"0.9.21-beta.1"`), commit - doesn't need
   to be on `main`; `workflow_dispatch` lets you pick the source branch from a dropdown when
   triggering a workflow.
2. Create the GitHub release by hand, tag e.g. `v0.9.21-beta.1`, marked as a **pre-release** (not
   "Set as the latest release") - via the web UI or `gh release create v0.9.21-beta.1 --prerelease`.
3. From the Actions tab, run `release-firmware` → `release-windows` → `release-linux` individually
   (same branch, `release_tag: v0.9.21-beta.1` each time). **Skip `release-webflasher`.**
4. Point testers at the release page directly, or for Linux, at the `SV241_RELEASE_TAG` override
   documented above.

## Prerequisites

| Tool | Used by | Notes |
|---|---|---|
| [PlatformIO CLI](https://platformio.org/) | `build_exe.bat` | Firmware compile step. Default path: `%USERPROFILE%\.platformio\penv\Scripts\pio.exe` |
| [Go](https://go.dev/) | all build scripts | `go build` for the Proxy backend |
| [Node.js](https://nodejs.org/) / npm | all build scripts | Vue frontend build |
| [Inno Setup 6](https://jrsoftware.org/isinfo.php) | `build_installer.bat` | Hardcoded path: `C:\Program Files (x86)\Inno Setup 6\ISCC.exe` |
