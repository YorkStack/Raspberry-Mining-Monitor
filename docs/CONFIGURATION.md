# Configuration

There are two layers:

1. **`config.yaml`** — the startup seed, read once at launch. It sets the initial
   miners, providers, network binding and defaults.
2. **Runtime state** — after first run, miners, provider URLs and display
   preferences are edited live on the `/settings` page and stored next to the
   config path (`miners.json`, `thresholds.json`, `history.gob`). These win over
   `config.yaml` on later starts.

So `config.yaml` seeds the first boot; the admin page owns everything after that.
Running with `--demo` ignores the file entirely and uses a built-in simulated
fleet.

## config.yaml

Copy `deploy/config.example.yaml` to `config.yaml` and edit. The file is
gitignored — it is the only place addresses belong.

### miners

```yaml
miners:
  - name: NerdOctaxe
    host: 192.168.1.51        # IP or hostname of the miner's AxeOS web UI
    type: axeos               # axeos | demo
    payout_address: bc1q…
    interval: 2s              # how often to poll
    timeout: 2s
    warn_temp_c: 64           # amber at or above this
    crit_temp_c: 70           # red at or above this
```

`host` is the miner's own web UI address, reachable from the Pi. `type: axeos`
covers both upstream Bitaxe and NerdQAxe firmware; the collector detects which
one automatically. `warn_temp_c` / `crit_temp_c` are optional and default to
64 / 70 °C. Both AxeOS variants trip their own thermal protection at 70 °C, so
red sits on that line and amber gives a few degrees of warning first.

### bitcoin

```yaml
bitcoin:
  provider: public            # public | core | demo
  base_url: https://mempool.space
  interval: 30s
  timeout: 8s
```

`public` uses mempool.space (or a self-hosted mempool instance if you change
`base_url`). `demo` simulates it. `core` (direct Bitcoin Core RPC) is reserved;
see [Optional extensions](#optional-extensions).

### pool

```yaml
pool:
  provider: publicpool        # publicpool | ckpool | none | demo
  base_url: https://public-pool.io:40557   # note the non-standard port
  interval: 60s
  timeout: 8s
```

`none` disables pool stats (the panel shows em-dashes). `ckpool` is reserved.

### dashboard

```yaml
dashboard:
  bind: 0.0.0.0               # 127.0.0.1 to keep it off the LAN
  port: 8080
  admin_bind: 127.0.0.1
  settings: true              # false disables /settings and /healthz entirely
  settings_path: /var/lib/rmm/thresholds.json
  screensaver_minutes: 15     # 0 disables the burn-in screensaver
```

`settings: false` removes the admin surface completely — use it if the service
is ever reachable from outside the LAN. `settings_path` also determines where
`miners.json` and `history.gob` live (same directory). See
[SECURITY.md](SECURITY.md) for why the admin surface must not sit behind a
reverse proxy.

### history

```yaml
history:
  enabled: true
  retention_days: 7
```

Rolling fleet totals kept in RAM and persisted to `history.gob`.

## Runtime settings (the `/settings` page)

Reachable from the local network only. No restart is needed for any of these:

- **Miners** — add, edit or remove miners and set their poll interval. A newly
  added AxeOS miner is polled at once and shows offline until it answers.
- **Data providers** — the Bitcoin and pool base URLs.
- **Appearance** — the animated mark per miner and for the fleet total.
- **Screensaver** — off / floating panel / blank, and the idle timeout.
- **Monitoring** — switch a miner off to hide it and stop polling it (the miner
  keeps mining; only the monitor stops looking).
- **Temperature thresholds** — per-miner amber/red levels, or the fleet default.

## Command-line flags

```
--config <path>     path to config.yaml (default: config.yaml)
--demo              simulated miners, pool and network; ignores config.yaml
--addr host:port   override the listen address
--log-level        debug | info | warn | error
--version          print the version and exit
```

## Versioning

The version shown in the header (`v0.8.2`) comes from `var version` in
`cmd/rmm/main.go`, stamped into the build by the Makefile. It is bumped on every
change: the patch digit for fixes, the minor digit for features. `GET
/api/v1/version` returns it; a `404` there means an old binary is still running.

## Optional extensions

Interfaces exist for these but they are not built yet:

- **`bitcoin: core`** — read network data straight from your own Bitcoin Core
  node over RPC instead of mempool.space. Fully local, no third-party API.
- **`pool: ckpool`** — an adapter for solo.ckpool.org alongside Public Pool.
- Per-miner history and detail charts, and a Prometheus metrics endpoint.

See the main open-items list for status.
