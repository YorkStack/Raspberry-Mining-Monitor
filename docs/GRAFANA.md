# Grafana Cloud dashboards

The monitor can send its metrics to a Grafana Cloud dashboard so you can watch
your fleet from anywhere, without ever exposing the miners or the Pi to the
internet. The monitor only makes outbound HTTPS calls to Grafana; nothing
listens for inbound connections.

There are two ways to get the metrics to Grafana. Pick one.

- **Built-in push (recommended).** `rmm` writes its metrics straight to Grafana
  Cloud. One binary, no extra process. This is what the rest of this document
  sets up.
- **Grafana Alloy.** A separate collector scrapes the monitor's `/metrics`
  endpoint and forwards it. Use this only if you already run Alloy or prefer a
  standalone agent. A ready config is in
  [grafana/alloy-config.alloy](grafana/alloy-config.alloy).

Either way, the metric names are the same, so the dashboard works with both.

## What you need from Grafana Cloud

In the Grafana Cloud Portal, open your stack, then **Prometheus → Send Metrics**.
Three values are shown there:

- the **Remote Write Endpoint** (a URL ending in `/api/prom/push`),
- the **Username / Instance ID** (a number),
- an **API token**, created with **Generate now**.

The URL and the instance ID are not secret. The token is. Create the token when
you are on the Pi, and keep it in a file readable only by the service, not in a
shared config.

## Built-in push

Add a `grafana:` block to the monitor's `config.yaml`:

```yaml
grafana:
  enabled: true
  url: https://prometheus-prod-65-prod-eu-west-2.grafana.net/api/prom/push
  user: "3561754"
  token_file: /etc/rmm/grafana.token   # a file containing only the token
  interval: 30s                         # optional, defaults to 30s
  timeout: 10s                          # optional, defaults to 10s
```

The `url` and `user` above are the values from the coralblackberry327 stack;
change them if you use a different Grafana Cloud account.

Put the token in its own file so it never sits in the main config:

```bash
sudo install -d -m 700 /etc/rmm
printf '%s' 'PASTE_YOUR_TOKEN_HERE' | sudo tee /etc/rmm/grafana.token >/dev/null
sudo chmod 600 /etc/rmm/grafana.token
```

You can instead put the token inline with `token: "..."`, but a file with tight
permissions is safer. If you prefer an environment variable, point `token_file`
at a path your deployment writes.

Restart the monitor. The log prints `grafana metrics push enabled`, and within a
minute or two the data shows up in Grafana. If a push fails, the monitor logs a
warning and carries on; a dropped push never affects the local dashboard.

## Import the dashboard

1. In Grafana (`https://coralblackberry327.grafana.net`), go to
   **Dashboards → New → Import**.
2. Upload [grafana/rmm-dashboard.json](grafana/rmm-dashboard.json).
3. When asked, pick your Prometheus data source (Grafana Cloud creates one named
   `grafanacloud-<stack>-prom`).

The dashboard has fleet hashrate, power and online count, per-miner hashrate,
temperature and power, the accepted/rejected share rate, network difficulty and
height, and the solo odds. A **Miner** dropdown filters the per-miner panels.

## Staying on the free plan

The free plan allows 10,000 active metric series with 14 days of retention. Two
miners produce roughly 27 series, so you are far under the limit and will not be
charged. You do not need to add a card. A fresh account starts on a 14-day
unlimited trial and then drops to the free plan on its own.

## What is sent, and what is not

Only the operational metrics go to Grafana: per-miner hashrate, power,
temperature, shares and online state, plus the fleet totals, pool workers,
network figures and the solo probability. These are the same series as the local
`/metrics` endpoint.

Payout addresses are never sent. The Mining Evidence and tax records are a
separate system and stay entirely local; nothing from that archive is pushed to
any cloud.
