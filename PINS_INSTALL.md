# Installing the SV241 Proxy on a PINS Raspberry Pi

> **Experimental, not regularly tested.** The Linux install path (this guide, `install_linux.sh`,
> and the CH340 driver it relies on) has been verified end-to-end against real Raspberry Pi
> hardware, but the maintainer doesn't run Linux day-to-day and this isn't part of any ongoing
> test routine. It should work, but a regression could go unnoticed until someone hits it - if
> you do, please [open an issue](https://github.com/DIYAstro/SV241-Unbound/issues) or a PR.
>
> This guide is written for PINS since that's what's actually been tested, but nothing about
> `install_linux.sh` is PINS-specific - it should work on most systemd-based Linux distributions
> (Debian, Ubuntu, Raspberry Pi OS, etc.), just untested there so far.

Nine steps, all from a terminal: log into the Raspberry Pi running PINS over SSH, then let one
command download, install, and start the proxy as a background service.

## Contents

- [1. Find the Pi's address](#1-find-the-pis-address)
- [2. Connect over SSH](#2-connect-over-ssh)
- [3. Run the installer](#3-run-the-installer)
- [4. What the installer does](#4-what-the-installer-does)
- [5. Confirm it's running](#5-confirm-its-running)
- [6. Connect from PINS](#6-connect-from-pins)
- [7. Everyday commands](#7-everyday-commands)
- [8. Updating the Proxy](#8-updating-the-proxy)
- [9. Updating the firmware](#9-updating-the-firmware)
- [Manually setting the serial port (advanced)](#manually-setting-the-serial-port-advanced)
- [Installing a beta build](#installing-a-beta-build)
- [Uninstalling](#uninstalling)

## 1. Find the Pi's address

You need either its IP address or its network name. PINS images typically set the hostname to
`pins-XXXXX` (check the label on the Pi, or your router's device list):

```bash
# from another machine on the same network
ping pins-XXXXX.local   # replace with your Pi's actual hostname
```

No luck? Check your router's admin page for a device named `pins-*` or `raspberrypi`, and note
its IP.

## 2. Connect over SSH

```bash
ssh pi@<pi-ip-or-hostname>
```

Default PINS credentials: user `pi`, password `pins`.

**On Windows**, if you'd rather not use the `ssh` command (built into PowerShell/cmd on modern
Windows), [PuTTY](https://www.putty.org/) works too and is what the PINS manual itself points to:
open PuTTY, enter the Pi's IP or hostname under "Host Name", leave the port at `22`, click "Open",
then log in as `pi` with the same password when prompted.

First connection from this machine? SSH will ask you to confirm the Pi's host key fingerprint -
type `yes` to continue.

## 3. Run the installer

One command, once you're logged in. It downloads the release binary matching your Pi's
architecture, installs it, and sets it up as a service:

```bash
curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo bash
```

## 4. What the installer does

Seven quick steps, all logged to the terminal as they happen:

1. Downloads the binary for your Pi's CPU (amd64 or arm64)
2. Installs the `libusb-1.0` runtime library if it's missing
3. Adds your user to the `dialout` group for serial access
4. Installs a udev rule granting the SV241's USB chip direct access
5. Installs the binary to `/usr/local/bin`
6. Registers and starts the `sv241-alpaca-proxy` systemd service

It finishes by printing the web interface URL - that's your ASCOM Alpaca endpoint for PINS to
connect to.

**A note on other USB-serial devices:** the SV241 is found by USB chip type, not by name - if you
have other CH340-based USB-serial devices plugged into the Pi too (common; it's a very widely used
chip, found in plenty of cheap USB-serial adapters and Arduino-compatible boards), the very first
connection (or the first one after moving the SV241 to a different USB port) checks whatever's
currently connected to find it, without disturbing anything that answers normally. Once it's found
the SV241 successfully, it remembers exactly which physical USB port that was and reconnects
straight to it from then on, without touching anything else - as long as it stays in that same
port. Practical takeaway: once it's working, leave the SV241 plugged into the same USB port.

If you've got other CH340-based gear plugged into the SV241's own switchable USB outputs (`USB
3+4+5` / `USB-C 1+2`) specifically, you can guarantee an unambiguous very first connection instead
of just relying on the above: set those outputs' startup state to **Off** (Switches tab in the web
interface, or the firmware config). With them off at boot, whatever's plugged into them is
electrically absent - not enumerated on the USB bus at all - so the Proxy's first-ever scan can
only ever see the SV241 itself. Once it's connected once (and so has a port pinned), it's safe to
turn those outputs back on; only a fresh, un-pinned scan (e.g. after moving the SV241 to a
different USB port) would need this trick again. Doesn't help with CH340 devices plugged into some
other USB port on the Pi entirely, outside the SV241's own outputs - only ones routed through the
box itself.

## 5. Confirm it's running

```bash
sudo systemctl status sv241-alpaca-proxy
```

Look for `active (running)`. Then open the printed URL (port `32241`) in a browser on the same
network to confirm the SV241's dashboard loads.

## 6. Connect from PINS

PINS should find the proxy automatically via Alpaca discovery. If it doesn't show up, try the
reload button next to Switch and Weather in the pins-alpaca plugin.

## 7. Everyday commands

Keep these handy - no need to memorize, they're printed again at the end of every install:

| Command | What it does |
|---|---|
| `sudo systemctl status sv241-alpaca-proxy` | is it running right now? |
| `sudo journalctl -u sv241-alpaca-proxy -f` | watch the live log |
| `sudo systemctl restart sv241-alpaca-proxy` | restart the service |
| `sudo systemctl stop sv241-alpaca-proxy` | stop it |

## 8. Updating the Proxy

Re-run the exact same command from step 3 - it downloads whatever's currently latest, stops the
running service, replaces the binary, and starts it back up:

```bash
curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo bash
```

Your saved settings and the SV241 itself are untouched - the installer only ever replaces the
binary and the service definition, never `~/.config/SV241AlpacaProxy/`. Confirm the update landed
with `sudo systemctl status sv241-alpaca-proxy` (its recent log lines show the version that just
started) or the version shown in the web interface.

## 9. Updating the firmware

The Proxy has a built-in web flasher, but it needs a browser running **on the Pi itself** - the
browser talks to the SV241 directly over USB, so it only works from the same machine the box is
physically plugged into. **Opening it from another computer on the network will not work,** even
though the Proxy's own web interface otherwise works fine remotely.

To get a browser on the Pi's own desktop, install PINS' VNC plugin first, then connect to the Pi
over VNC. From inside that VNC session, open Chromium and go to:

```
http://localhost:32241/flasher/
```

(`localhost`, not the Pi's IP - you're already running the browser on the Pi itself.) From there,
click Connect and follow the on-screen instructions.

## Manually setting the serial port (advanced)

Normally you never need this - the Proxy finds the SV241 on its own and, from then on, remembers
exactly which USB port it was on (see the note in step 4). But if you'd rather set it explicitly -
say, you've got other CH340-based USB-serial gear on the Pi and want to skip relying on
auto-detection even once - here's how.

**Easiest way: let it find the SV241 once, then lock that in.** After a normal install and a
successful connect (steps 1-6), the Proxy has already written the right value into its config.
Open `~/.config/SV241AlpacaProxy/proxy_config.json` and look at `serialPortName` - it'll be
something like `"ch340-usb:bus1:1.1.4.4"`, not a `/dev/ttyUSB0`-style path (see
`internal/serial/ch340_linux.go` for why - the short version: this identifies the SV241 by its
physical USB port, not by a device node name that can change). Set `"autoDetectPort": false` (via
the web interface's Proxy Settings tab, or directly in this file) and you're done - the Proxy will
only ever try that exact port from now on.

**Setting it yourself, without ever letting auto-detect run:**

1. Stop the service, so nothing is holding the port while you do this:
   ```bash
   sudo systemctl stop sv241-alpaca-proxy
   ```
2. Clear the kernel log, then physically unplug and reconnect the SV241's USB cable (a `dmesg
   --clear` alone does nothing here - you need a fresh "device appeared" event to log):
   ```bash
   sudo dmesg --clear
   ```
3. Find the line announcing it. Note that `dmesg` logs the vendor/product ID as two separate
   fields, not as the `1a86:7523` pair `lsusb` would show - `idVendor=1a86` alone is enough to
   find it:
   ```bash
   dmesg | grep 'idVendor=1a86'
   ```
   You'll see something like:
   ```
   usb 1-1.3: New USB device found, idVendor=1a86, idProduct=7523, bcdDevice= 2.54
   ```
   The `1-1.3` part is the bit you need - it's `<bus number>-<physical port path>` (here: bus `1`,
   port path `1.3`; on a Pi behind extra hubs/adapters it'll have more segments, e.g. `1-1.4.2`).
4. Turn that into the Proxy's format by replacing the first `-` with `:bus` and adding a `:` after
   the bus number - `1-1.3` becomes `ch340-usb:bus1:1.3`.
5. Edit `~/.config/SV241AlpacaProxy/proxy_config.json` (create the directory/file first if the
   Proxy has never run yet):
   ```json
   "serialPortName": "ch340-usb:bus1:1.3",
   "autoDetectPort": false,
   ```
6. Restart the service and confirm it connected straight to that port:
   ```bash
   sudo systemctl restart sv241-alpaca-proxy
   sudo journalctl -u sv241-alpaca-proxy -n 20
   ```
   Look for a line like `CH340 (ch340-usb:bus1:1.3): gentle open succeeded`.

Got the format wrong, or the SV241 later moves to a different USB port? Nothing breaks - an
unrecognized or non-matching value is simply treated the same as no preference at all, and the
Proxy falls back to its normal auto-detection.

## Installing a beta build

If a maintainer has asked you to test a pre-release, set `SV241_RELEASE_TAG` when installing
instead of using step 3's plain command:

```bash
curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo SV241_RELEASE_TAG=v0.9.21-beta.1 bash
```

(Replace `v0.9.21-beta.1` with whatever tag you were given. Note the env var comes right after
`sudo`, not before the whole command - `sudo` doesn't pass through your shell's environment
variables otherwise.)

## Uninstalling

Same one-liner, with `SV241_UNINSTALL=1` instead:

```bash
curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo SV241_UNINSTALL=1 bash
```

Removes the service, the udev rule, and the binary. Leaves your `dialout` group membership, the
`libusb-1.0-0` package, and any saved config under `~/.config/SV241AlpacaProxy/` in place - other
things on the Pi may depend on the first two, and the config is worth keeping if you reinstall
later.
