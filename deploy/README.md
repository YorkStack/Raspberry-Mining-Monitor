# Deploy

Everything needed to run the monitor on a Raspberry Pi.

## Files

- `config.example.yaml` — copy to `config.yaml` and fill in your miners.
- `rmm.service` — hardened systemd unit. Install to `/etc/systemd/system/`.
- `deploy.sh` — cross-compile the ARM64 binary and copy it to the Pi.
- `deploy.env` — local host details for `deploy.sh` (gitignored, never committed).
- `launcher/` — a console chooser for a Pi that runs more than one project.
- `kiosk/` — full-screen Chromium kiosk (cage) for the Pi's own display.

## Deploy the binary

The Pi needs no Go toolchain. Set the target once in `deploy.env`:

    RMM_SSH=my-pi
    RMM_TARGET=~/rmm

then `./deploy.sh` (add `--restart` once the service is installed).

## Console launcher

On a Pi shared between projects (for example this monitor and a separate
display installation), `launcher/pi-launcher.sh` shows a menu on console login
so you can pick which program to start. It only takes over `tty1`, so SSH
sessions are unaffected.

Install on the Pi:

    scp launcher/*.sh my-pi:~/deploy-launcher/
    ssh my-pi 'bash ~/deploy-launcher/install-launcher.sh'

To remove it, delete the marked block from `~/.bash_profile` on the Pi.
