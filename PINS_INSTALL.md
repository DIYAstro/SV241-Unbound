# Installing the SV241 Proxy on a PINS Raspberry Pi

> **Experimental, not regularly tested.** The Linux install path (this guide, `install_linux.sh`,
> and the CH340 driver it relies on) has been verified end-to-end against real Raspberry Pi
> hardware, but the maintainer doesn't run Linux day-to-day and this isn't part of any ongoing
> test routine. It should work, but a regression could go unnoticed until someone hits it - if
> you do, please [open an issue](https://github.com/DIYAstro/SV241-Unbound/issues) or a PR.

Six steps, all from a terminal: log into the Raspberry Pi running PINS over SSH, then let one
command download, install, and start the proxy as a background service.

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

## 5. Confirm it's running

```bash
sudo systemctl status sv241-alpaca-proxy
```

Look for `active (running)`. Then open the printed URL (port `32241`) in a browser on the same
network to confirm the SV241's dashboard loads.

## 6. Everyday commands

Keep these handy - no need to memorize, they're printed again at the end of every install:

| Command | What it does |
|---|---|
| `sudo systemctl status sv241-alpaca-proxy` | is it running right now? |
| `sudo journalctl -u sv241-alpaca-proxy -f` | watch the live log |
| `sudo systemctl restart sv241-alpaca-proxy` | restart the service |
| `sudo systemctl stop sv241-alpaca-proxy` | stop it |

## Worth doing once

`pi` / `pins` is a well-known default. If this Pi is reachable beyond your own home network,
change the password with `passwd` before leaving it running unattended.
