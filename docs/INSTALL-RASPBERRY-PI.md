# Install on a Raspberry Pi

This walks through a production install: the backend as a hardened systemd
service, and an optional full-screen kiosk on the Pi's own display. The Pi needs
no Go toolchain — you cross-compile on your workstation and copy one binary.

Tested on a Raspberry Pi 4 (1 GB) with a Waveshare 9.3" 1600×600 HDMI touch
display, but nothing here is specific to that board.

## 1. Prepare the Pi

- Install Raspberry Pi OS (64-bit / arm64) or Debian arm64.
- Enable SSH and give your workstation key-based access.
- Note the Pi's address; set up an SSH alias in `~/.ssh/config` if you like.

Nothing else is required for the backend. The kiosk (step 5) needs two extra
packages, installed there.

## 2. Configure the deploy

On your workstation, in the repo, copy the deploy template and fill in your
Pi's details. `deploy/deploy.env` is gitignored, so host details never leave
your machine:

```
RMM_SSH=my-pi          # ssh target (alias or user@host)
RMM_TARGET=rmm         # path on the Pi, relative to the login home
```

## 3. Deploy the binary

```bash
RMM_SSH=my-pi RMM_TARGET=rmm ./deploy/deploy.sh
```

This cross-compiles the ARM64 binary, uploads it, verifies the SHA-256 on both
ends, and places it on the Pi. Re-run it any time to update; the version it
stamps is read from the source, so `rmm --version` on the Pi tells you exactly
what is deployed.

## 4. Run the backend as a service

Put your real configuration on the Pi (see [CONFIGURATION.md](CONFIGURATION.md)):

```bash
ssh my-pi 'sudo mkdir -p /etc/rmm && sudo tee /etc/rmm/config.yaml' < config.yaml
```

Install the hardened unit shipped in `deploy/rmm.service`. It expects the binary
at `/opt/rmm/rmm` and the config at `/etc/rmm/config.yaml`; adjust `ExecStart`
if you deployed elsewhere.

```bash
scp deploy/rmm.service my-pi:/tmp/
ssh my-pi 'sudo mv /tmp/rmm.service /etc/systemd/system/ \
  && sudo systemctl daemon-reload \
  && sudo systemctl enable --now rmm'
```

Check it:

```bash
ssh my-pi 'systemctl status rmm; curl -s localhost:8080/api/v1/version'
```

The unit runs fully sandboxed with a memory ceiling suited to the 1 GB Pi (see
[SECURITY.md](SECURITY.md)). Reach the dashboard from any LAN browser at
`http://<pi>:8080`; `/settings` and `/healthz` answer on the Pi's own network
only.

> Demo first: to see the UI before your miners are online, run the binary with
> `--demo` instead of `--config`. Everything is simulated and no config file is
> needed.

## 5. Kiosk on the Pi's display (optional)

To turn the Pi into a wall dashboard, run Chromium full-screen under `cage`
(a minimal Wayland compositor). Install both on the Pi:

```bash
ssh my-pi 'sudo apt-get update && sudo apt-get install -y cage chromium'
```

Copy the kiosk script from `deploy/kiosk/rmm-kiosk.sh` to the Pi and start it on
`tty1`. The shipped script launches `cage -- chromium --kiosk --app=http://localhost:8080`
with cache in `/dev/shm` so nothing wears the SD card.

For a Pi that runs more than one project, `deploy/launcher/` adds a console menu
on login so you can choose what to start; it only takes over `tty1`, leaving SSH
sessions alone. Install it with `deploy/launcher/install-launcher.sh`.

After deploying an update, refresh the kiosk so it drops its cached copy of the
old page:

```bash
ssh my-pi 'rm -rf /dev/shm/chromium && sudo systemctl restart getty@tty1.service'
```

> If you run the kiosk as a `systemd --user` service and control the Pi over
> SSH, enable lingering (`loginctl enable-linger <user>`) so the service is not
> killed when your SSH session ends.

## 6. Touchscreen

A USB HID touchscreen needs no configuration: the Linux `hid-multitouch` driver
binds it, and `cage`/libinput uses it automatically. After plugging it in,
confirm the kernel sees it:

```bash
ssh my-pi 'lsusb; ls /dev/input/by-id/'
```

You should see the touch controller listed (for the Waveshare panel it appears
as a `Waveshare` device). If nothing shows up:

- The Pi 4 has **no USB-C data port** — its USB-C is power only. The touch cable
  must reach one of the Pi's **USB-A** ports.
- Use a real USB-A-to-USB-C **data** cable. Charge-only cables enumerate
  nothing, which is the most common cause of "touch not detected".
- Test the same cable in a port you know works (where the keyboard enumerates).

Once the device appears, restart the kiosk (`sudo systemctl restart
getty@tty1.service`) so the compositor grabs it, then tap the screen.

## Updating

1. `./deploy/deploy.sh` from your workstation.
2. `ssh my-pi 'sudo systemctl restart rmm'` (or the user service).
3. If the kiosk is running: `rm -rf /dev/shm/chromium && sudo systemctl restart
   getty@tty1.service`.

Confirm the new build with `curl -s localhost:8080/api/v1/version` on the Pi.
