# Security

The design order is Security first, then Reliability, then low resource use.
This document describes the posture and the reasoning behind it.

## Threat model

The monitor runs on a home or small-operation LAN alongside the miners. The
realistic risks are: a compromised device on the same network, a malicious or
buggy response from a miner or an upstream API, and accidental exposure of the
payout address or miner addresses. The monitor is not designed to face the
public internet directly.

## Read-only by construction

There is no code path from the monitor to a miner other than HTTP GET. The AxeOS
collector only reads; it never sends a restart, a config write or a firmware
call. This is enforced in the router as well: the miner-facing side has no write
verbs at all. Even if the dashboard were fully compromised, it could not change
a miner's settings, because the capability does not exist in the binary.

## The browser only sees a projection

The `dashboard` package builds the browser document field by field. It never
re-serves a miner, pool or mempool response verbatim. Two consequences:

- The payout address is masked before it leaves the process (`bc1q…5mdq`); the
  full address is never sent to the page.
- A new or unexpected field in a future firmware response cannot reach the
  browser, because only allowlisted fields are copied.

## Network gating

Every response carries a strict Content-Security-Policy that forbids all
off-origin requests:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:;
connect-src 'self'; font-src 'self'; object-src 'none'; frame-ancestors 'none';
base-uri 'none'; form-action 'none'
```

Plus `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and
`X-Frame-Options: DENY`. Everything the page needs (fonts included) ships in the
binary, so nothing is fetched from a CDN.

`GET /metrics` (Prometheus) is served read-only and gated to the local network,
so Prometheus/Grafana on the same LAN can scrape it; `dashboard.metrics: false`
removes it. It carries operational figures only (hashrate, power, temperature,
difficulty, price), no addresses.

Operator alerts are opt-in and outbound only: nothing is sent unless
`alerts.webhook_url` is set, and then only to exactly that URL, which is a secret
held in the config and never exposed by the API or UI. The URL comes from the
operator's config, never from data observed on the network.

The read-only dashboard is served to the whole LAN. The **admin surface**
(`/settings`, the config APIs, `/healthz`) answers only to the local network:
loopback plus RFC1918 / unique-local / link-local addresses. Public addresses
get a `404`, not a `403`, so the route's existence is not advertised.

Two rules matter here:

- **`X-Forwarded-For` and `X-Real-IP` are deliberately ignored.** Honouring them
  would let any client claim a trusted source. As a result, **do not place this
  service behind a reverse proxy while the admin surface is enabled** — every
  proxied request would look local. If you must proxy it, set
  `dashboard.settings: false` so the admin routes disappear entirely.
- The admin surface only changes what the dashboard *shows* (thresholds, icons,
  which miners are visible). It cannot touch a miner, so trusted-LAN access is an
  acceptable trade-off for a wall-mounted kiosk with no login.

## Process sandbox (systemd)

The provided `deploy/rmm.service` runs the backend with no privileges and a
tight sandbox:

- `DynamicUser=yes` — a transient account, no fixed user to target.
- `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`,
  `PrivateDevices=yes` — the filesystem is read-only except a private
  `StateDirectory`.
- `NoNewPrivileges`, `RestrictNamespaces`, `LockPersonality`,
  `MemoryDenyWriteExecute` — standard hardening.
- `RestrictAddressFamilies=AF_INET AF_INET6` — network only, no unix sockets.
- `GOMEMLIMIT=96MiB`, `MemoryMax=128M` — a hard ceiling on the 1 GB Pi.

The service writes only to its `StateDirectory` (thresholds, miners, history).

## Secrets and addresses

Addresses live only in `config.yaml` (or, after first run, `miners.json`). Both
are gitignored, along with `deploy/deploy.env`, `thresholds.json`,
`history.gob`, `*.gob` and `handoff.md`. Nothing sensitive is hard-coded in
source. The payout address is masked in the UI; the full value stays in the
config file on the host.

Two kinds of token can appear in the config, and both are kept out of the
browser: the Braiins API token (`pool.token`) and a per-miner Bearer token
(`miners[].token`) for a miner behind a monitoring security contract, such as the
Mac metal miner. The admin API never returns either one, the settings page
cannot read them, and editing a miner preserves its token rather than sending it
to the browser and back. The monitor still reads only: the miner token buys it a
GET to `/api/system/info`, nothing more. Because that exporter is currently plain
HTTP, the Bearer token is not encrypted in transit, so keep such a miner on a
trusted management network and never place it on an untrusted LAN or the
internet.

## What is out of scope

- No authentication or TLS. The monitor expects a trusted LAN, not the public
  internet. If you need remote access, put it behind a VPN, not a public proxy.
- No secrets management beyond file permissions (0600 on state files). There are
  no API keys to store: the default providers are public and unauthenticated.
