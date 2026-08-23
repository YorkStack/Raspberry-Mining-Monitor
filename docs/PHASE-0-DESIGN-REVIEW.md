# Phase 0 — Design Review

Status: **approved. Phase 1 built against it.**
Date: 2026-08-23
Scope: research and architecture. Kept as the reference for later phases.

Every API fact below was checked against upstream source or against a live
response on 2026-08-23. Sources are listed in the appendix. Anything that could
not be verified is marked UNVERIFIED rather than guessed.

---

## Decisions confirmed

| Question | Answer |
|---|---|
| Solo pool | Public Pool. ckpool stays designed but unbuilt until later. |
| Product and binary name | `raspberry-mining-monitor`, binary `rmm` |
| Payout addresses | Different address per miner |
| Licence | MIT |

Two of these change the design.

A separate payout address per miner makes the pool adapter simpler, not harder.
Each address maps to exactly one miner, so per-miner attribution is direct and
there is no need to parse a `address.workername` suffix to work out which device
a worker belongs to. The cost is one `GET /api/client/{address}` call per miner
instead of one shared call, so the poll budget against public-pool.io goes from
one request per minute to three (two clients plus `/api/pool`).

Choosing Public Pool means the MVP ships one pool adapter rather than two. The
`PoolAdapter` interface and its `Capabilities()` method still go in, because
they are what let the ckpool adapter land later without touching the UI.

### Configuration shape

```yaml
miners:
  - name: NerdOctaxe
    host: 192.168.1.51          # no hard-coded IPs in source; set here
    type: axeos
    payout_address: bc1q...     # this miner's Public Pool address
    interval: 2s
    warn_temp_c: 70
    crit_temp_c: 80
  - name: Gamma 602
    host: 192.168.1.52
    type: axeos
    payout_address: bc1q...
    interval: 2s

bitcoin:
  provider: public
  base_url: https://mempool.space
  interval: 30s
  timeout: 8s

pool:
  provider: publicpool
  base_url: https://public-pool.io:40557
  interval: 60s
  timeout: 8s

dashboard:
  bind: 0.0.0.0
  port: 8080
  admin_bind: 127.0.0.1

history:
  enabled: true
  path: /var/lib/rmm/history.db
  retention_days: 7
```

The payout address lives on the miner rather than on the pool, which is what
makes the one-call-per-miner mapping fall out of the config instead of needing
a separate lookup table.

---

## 0. Research finding that shapes the design

The two miners do not run the same firmware.

| Miner | Firmware | API surface |
|---|---|---|
| Bitaxe Gamma 602 | `bitaxeorg/ESP-Miner` (upstream AxeOS) | `/api/system/info`, `/api/system/statistics`, `/api/system/scoreboard`, `/api/ws/live` |
| NerdOctaxe Rev 3.1 | `shufps/ESP-Miner-NerdQAxePlus` (fork) | `/api/system/info` (different fields), `/api/v2/dashboard`, `/api/v2/system`, no `/api/system/statistics` |

Both are called AxeOS and both look similar at a glance, but they do not share a
contract. Field names overlap roughly 70%, and the fork has moved its best data
into a nested `/api/v2/dashboard` document while upstream keeps everything flat.
Section 5 has the full comparison.

So the AxeOS integration needs a variant-detecting adapter rather than a single
JSON struct. That costs a little extra work now and avoids a lot of rework
later, which is why it belongs in the MVP instead of a follow-up phase.

---

## 1. Technology stack

### Backend — Go 1.23+

| Candidate | Idle RSS | Deploy | Verdict |
|---|---|---|---|
| Go | 20–40 MB | one static ARM64 binary | recommended |
| Python + FastAPI | 60–110 MB | interpreter + venv + wheels | too heavy for 1 GB alongside Chromium |
| Node + Fastify | 55–90 MB | runtime + `node_modules` | same problem, plus a large dependency tree |
| Rust + axum | 8–20 MB | one static binary | leanest, but slower to write and a smaller pool of contributors |

Go wins on the combination that matters here. It has a small resident
footprint, needs no runtime installed on the Pi, cross-compiles from the M2 Pro
in seconds with `GOOS=linux GOARCH=arm64 go build`, and its standard library
covers HTTP clients with context timeouts, JSON and TLS without third-party
code.

Proposed dependencies, deliberately few:

- `modernc.org/sqlite`, a pure-Go SQLite with no cgo, so cross-compilation stays trivial
- `gopkg.in/yaml.v3` for config parsing
- nothing else in the hot path

### Frontend — no framework

The dashboard is roughly forty numbers that change on a timer. React, Vue and
Svelte all cost more than they return here: bundle size, a build toolchain, an
npm dependency tree to audit, and extra Chromium heap.

Proposal: TypeScript compiled by esbuild into one file of about 12 KB, plain DOM
updates, CSS Grid for layout. Charts on the detail screens use
[uPlot](https://github.com/leeoniya/uPlot), around 45 KB, canvas-based, built
for exactly this. It gets vendored rather than pulled from a CDN.

Phase 1 ships this as plain ES-module JavaScript with no build step at all.
At roughly 250 lines the TypeScript toolchain would add an npm dependency tree
to audit for very little return. The esbuild step goes in when the charts
arrive in Phase 5, which is the first point where it earns its place.

If a component model later proves necessary, Preact plus htm adds about 4 KB and
can be introduced without touching the backend.

### Transport — Server-Sent Events

The data flow is one-way, server to browser. SSE gives that with automatic
reconnection built into the browser, no handshake and no ping/pong bookkeeping,
and it survives a backend restart without any frontend logic. WebSocket would be
more machinery for no gain.

- `GET /api/v1/stream`, SSE, one `snapshot` event per second
- `GET /api/v1/snapshot`, the same document as a plain GET, for polling clients,
  debugging, and the initial page paint

### Kiosk — Wayland with cage and Chromium

Raspberry Pi OS Lite 64-bit, plus `cage`, a single-window Wayland kiosk
compositor of a few MB, rather than the full LXDE/labwc desktop. Chromium runs
as cage's only client.

---

## 2. Repository structure

```
raspberry-mining-monitor/
├── cmd/
│   └── rmm/                     main(), flag parsing, wiring, graceful shutdown
├── internal/
│   ├── config/                  YAML load, defaults, validation
│   ├── miner/
│   │   ├── miner.go             Miner interface + normalised MinerSnapshot
│   │   ├── axeos/
│   │   │   ├── client.go        HTTP GET with timeout, retry, backoff
│   │   │   ├── variant.go       firmware detection: upstream vs nerdqaxe
│   │   │   ├── upstream.go      bitaxeorg field mapping
│   │   │   ├── nerdqaxe.go      shufps fork field mapping (v2 + legacy)
│   │   │   └── testdata/        captured real JSON responses
│   │   └── demo/                synthetic miner with realistic drift
│   ├── pool/
│   │   ├── pool.go              PoolAdapter interface + PoolSnapshot
│   │   ├── publicpool/
│   │   ├── ckpool/
│   │   └── demo/
│   ├── bitcoin/
│   │   ├── provider.go          BitcoinDataProvider interface + NetworkSnapshot
│   │   ├── mempool/             PublicApiProvider (mempool.space compatible)
│   │   ├── corerpc/             stub only in MVP; interface proven, not wired
│   │   └── demo/
│   ├── subsidy/                 height → block subsidy, pure function
│   ├── probability/             Poisson block-probability math
│   ├── aggregate/               combined hashrate, power, weighted J/TH
│   ├── state/                   in-memory snapshot store + SSE fan-out
│   ├── store/                   SQLite history, batched writer, retention
│   ├── httpapi/                 REST + SSE handlers, static file serving
│   └── health/                  /healthz, /readyz, source freshness
├── web/
│   ├── src/                     TypeScript, CSS
│   ├── vendor/uplot/            vendored, pinned, checksummed
│   └── dist/                    esbuild output, embedded via go:embed
├── deploy/
│   ├── rmm.service              systemd unit, hardened
│   ├── rmm-kiosk.service        cage + Chromium
│   ├── install.sh
│   └── config.example.yaml
├── docs/
│   ├── PHASE-0-DESIGN-REVIEW.md
│   ├── ARCHITECTURE.md
│   ├── SECURITY.md
│   ├── INSTALL-RASPBERRY-PI.md
│   └── CONFIGURATION.md
└── Makefile
```

The whole frontend ships inside the binary via `go:embed`. Deployment is one
file plus one config file.

Three packages were added while building Phase 1 that this outline did not
anticipate:

- `internal/model` holds the `Source` freshness primitive that `miner`, `pool`
  and `bitcoin` all embed. Putting it in any one of them would have created an
  import cycle.
- `internal/dashboard` holds the allowlist projection that turns collector
  snapshots into the browser document. It started inside `httpapi` and moved
  out because it is the security boundary and deserves its own test suite.
- `internal/collect` holds the poll loops and the backoff policy, keeping the
  timing concerns out of the adapters themselves.

---

## 3. Runtime RAM budget — Pi 4 Model B, 1 GB

| Component | Expected RSS |
|---|---|
| Kernel, Raspberry Pi OS Lite 64-bit, systemd, journald, networking | 120–160 MB |
| cage compositor + Mesa/DRM | 40–60 MB |
| Chromium: browser + gpu + zygote + one renderer at 1600×600 | 280–400 MB |
| `rmm` backend: Go heap, runtime, SQLite page cache | 25–45 MB |
| Total | 465–665 MB |
| Headroom | ~360–560 MB |

It fits with room to spare, but only under specific choices.

Use Pi OS Lite, not Desktop. The full desktop alone costs 200 MB or more and
would make the budget marginal.

Set `GOMEMLIMIT=96MiB` and `GOGC=50` on the backend. That turns a soft memory
target into a hard one and stops the Go heap drifting upward over weeks of
uptime.

Chromium flags that matter on 1 GB:
`--disk-cache-dir=/dev/shm/chromium --disk-cache-size=33554432`,
`--disable-features=Translate,MediaRouter,OptimizationHints`,
`--renderer-process-limit=1`, `--no-first-run`.

Add 512 MB of zram swap. Compressed swap in RAM absorbs Chromium's allocation
spikes without touching the SD card.

Leave GPU memory at the default under the KMS driver. The old `gpu_mem=` tuning
does not apply to the Pi 4 KMS path.

The risk here is that Chromium's footprint depends on version and can grow
across Pi OS updates. Pinning the Chromium version helps, and the health
endpoint exposes system memory so a regression shows up as a number rather than
as a mystery.

---

## 4. Data flow and component architecture

```
  ┌─────────────────┐  HTTP GET   ┌──────────────────────────────────────┐
  │ NerdOctaxe      │◄────2 s─────┤ collector: axeos (nerdqaxe variant)  │
  │ AxeOS fork      │             └──────────────────┬───────────────────┘
  └─────────────────┘                                │
  ┌─────────────────┐  HTTP GET   ┌──────────────────┴───────────────────┐
  │ Bitaxe Gamma    │◄────2 s─────┤ collector: axeos (upstream variant)  │
  │ AxeOS upstream  │             └──────────────────┬───────────────────┘
  └─────────────────┘                                │
  ┌─────────────────┐  HTTPS      ┌──────────────────┴───────────────────┐
  │ Solo pool API   │◄───60 s─────┤ collector: pool adapter              │
  └─────────────────┘             └──────────────────┬───────────────────┘
  ┌─────────────────┐  HTTPS      ┌──────────────────┴───────────────────┐
  │ mempool.space   │◄───30 s─────┤ collector: bitcoin provider          │
  └─────────────────┘             └──────────────────┬───────────────────┘
                                                     ▼
                                    ┌────────────────────────────────┐
                                    │ state.Store (RWMutex)          │
                                    │  · latest snapshot per source  │
                                    │  · per-source freshness stamp  │
                                    │  · 1 h ring buffer in RAM      │
                                    └───────┬────────────────┬───────┘
                                            │                │
                            derive (1 Hz)   │                │ 60 s batch
                                            ▼                ▼
                              ┌──────────────────┐   ┌──────────────┐
                              │ aggregate +      │   │ SQLite       │
                              │ probability      │   │ (24 h / 7 d) │
                              └────────┬─────────┘   └──────────────┘
                                       ▼
                        ┌──────────────────────────────┐
                        │ httpapi: SSE + REST + static │
                        └──────────────┬───────────────┘
                                       ▼
                            Chromium kiosk / LAN browsers
```

Four design rules make this reliable.

Every collector runs as an independent goroutine with its own ticker, its own
`context.WithTimeout` and its own backoff. A dead miner cannot stall the pool
collector, and an unreachable mempool.space cannot stall anything.

Collectors never block on the store. They write a snapshot under a short write
lock and return.

The store tracks freshness rather than discarding data. Every snapshot carries
`fetchedAt` and `ok`. Stale data is marked stale, not deleted, and the UI
decides how to render it. The last known hashrate shown greyed out is more
useful than a blank tile.

Derived values are computed on read, once per second, from whatever the store
currently holds. There is no cache to invalidate incorrectly.

Backoff: on failure, retry at twice the base interval, capped at 60 s, reset on
the first success. Miner timeouts 2 s, external APIs 8 s.

---

## 5. AxeOS / ESP-Miner API, verified

### 5a. Bitaxe Gamma 602 — `bitaxeorg/ESP-Miner`

Routes registered in `main/http_server/http_server.c` and documented in
`main/http_server/openapi.yaml`:

| Route | Method | Use |
|---|---|---|
| `/api/system/info` | GET | everything we need |
| `/api/system/asic` | GET | ASIC model, frequency/voltage option lists |
| `/api/system/statistics` | GET | on-device history (requires logging enabled) |
| `/api/system/scoreboard` | GET | top 20 best shares |
| `/api/system/logs` | GET | device logs |
| `/api/ws/live` | GET | WebSocket live telemetry |
| `/api/system` | PATCH | write, never called by this project |
| `/api/system/restart`, `/pause`, `/resume`, `/OTA`, `/OTAWWW` | POST | write, never called |

`/api/system/info` fields relevant to the dashboard, with exact names:

| Requirement | Field | Unit / note |
|---|---|---|
| model | `ASICModel`, `boardVersion` | e.g. `BM1370` |
| hostname | `hostname`, `fullHostname`, `mdnsHostname` | |
| firmware | `version`, `axeOSVersion`, `idfVersion` | |
| uptime | `uptimeSeconds`, `totalUptimeSeconds` | seconds |
| current hashrate | `hashRate` | GH/s |
| average hashrate | `hashRate_1m`, `hashRate_10m`, `hashRate_1h` | GH/s |
| expected hashrate | `expectedHashrate` | GH/s |
| frequency | `frequency`, `actualFrequency` | MHz |
| core voltage | `coreVoltage`, `coreVoltageActual` | mV |
| input voltage | `voltage` | mV |
| power | `power`, `maxPower` | W |
| current | `current` | mA |
| ASIC temp | `temp`, `temp2` | °C |
| VRM temp | `vrTemp` | °C |
| fan | `fanrpm`, `fan2rpm`, `fanspeed` | RPM, RPM, % |
| shares | `sharesAccepted`, `sharesRejected`, `sharesRejectedReasons` | counts, array |
| best share | `bestDiff`, `bestSessionDiff` | difficulty |
| pool difficulty | `poolDifficulty` | |
| pool | `stratumURL`, `stratumPort`, `stratumUser`, `pools[]` | |
| failover state | `isUsingFallbackStratum` | 0/1 |
| pool latency | `responseTime` | ms |
| device network | `wifiStatus`, `wifiRSSI`, `ipv4` | |
| device health | `freeHeap`, `cpuUsage`, `power_fault`, `hardware_fault` | |
| chain context | `blockHeight`, `networkDifficulty`, `blockFound` | miner's own view |

J/TH is not reported by the firmware. It is derived as
`power / (hashRate / 1000)`.

### 5b. NerdOctaxe Rev 3.1 — `shufps/ESP-Miner-NerdQAxePlus`

Routes registered in `main/http_server/http_server.cpp`:

| Route | Method | Use |
|---|---|---|
| `/api/system/info` | GET | legacy flat document, still populated |
| `/api/v2/dashboard` | GET | preferred, nested, purpose-built for exactly this |
| `/api/v2/system` | GET | system detail |
| `/api/system/asic` | GET | ASIC options |
| `/api/v2/can/nodes` | GET | multi-board CAN chaining |
| `/api/system`, `/api/v2/settings`, `/OTA`, `/restart`, `/shutdown`, `/reset-stats` | PATCH/POST | write, never called |

`/api/v2/dashboard` structure, read from `handler_v2_dashboard.cpp`:

```
system      { uptime, shutdown, boardError, overheatTemp }
performance { hashRateTimestamp, hashRate, hashRate1m, hashRate10m,
              hashRate1h, hashRate1d, bestDiff, bestSessionDiff,
              sharesAccepted, sharesRejected, frequency, asicCount,
              smallCoreCount }
power       { watts, min, max, voltage (V), voltageMin, voltageMax,
              currentA (A), currentAMin, currentAMax, coreVoltageActual (V) }
thermal     { asicTemp, vrTemp, vrTempInt, asicTemps[], fans[{speed, rpm}] }
stratum     { pools[{ host, port, user, ... }], ... }
can         { hasExtension, enabled }
coinbase    { blockHeaders[{ pool, blockHeight, networkDifficulty, scriptSig,
              ... }], pools[] }
history     { ... }   // only when ?ts= is supplied
```

Seven differences the adapter has to absorb:

1. Naming differs. Upstream uses `hashRate_1m`; the fork uses `hashRate1m` in v2
   and `hashRate_1m` in its legacy endpoint. Both spellings must be handled.
2. Units differ. Upstream reports `voltage` and `coreVoltage` in mV and
   `current` in mA. The fork's v2 document reports `voltage` and
   `coreVoltageActual` in V and `currentA` in A. Mixing these silently produces
   power figures off by a factor of 1000.
3. Fans are an array on the fork (`thermal.fans[]`) and scalars upstream
   (`fanrpm`, `fanspeed`). The NerdOctaxe-Gamma is a six-phase board with
   several fans, so the array is the correct model.
4. Temperature is per-ASIC on the fork (`thermal.asicTemps[]` plus an
   `asicTemp` maximum). Display the maximum and keep the array for the detail
   screen.
5. `expectedHashrate` does not exist on the fork. Compute it from
   `frequency × smallCoreCount × asicCount`, or omit the field.
6. There is no `/api/system/statistics` on the fork. On-device history comes
   from the `?ts=&limit=&historySpan=` query parameters on `/api/v2/dashboard`.
7. The fork gates reads behind `is_network_allowed()` and has optional OTP. If
   either is enabled, the Pi's IP must be permitted. That is a configuration
   step rather than a code problem, but it belongs in the install guide.

### 5c. Adapter design

```go
type MinerSnapshot struct {
    Name        string
    Model       string        // ASICModel / deviceModel
    Firmware    string
    Variant     Variant       // upstream | nerdqaxe
    Uptime      time.Duration

    HashrateTHs        float64    // normalised to TH/s regardless of source
    HashrateAvg1hTHs   *float64
    ExpectedHashTHs    *float64

    PowerW      *float64
    VoltageV    *float64        // always volts
    CurrentA    *float64        // always amps
    FreqMHz     *float64
    CoreVoltageV *float64

    ASICTempC   *float64        // max across chips
    VRMTempC    *float64
    Fans        []Fan           // scalar sources produce a one-element slice

    SharesAccepted, SharesRejected *uint64
    BestDiff, BestSessionDiff      *float64
    PoolURL, PoolUser              string
    UsingFallback                  *bool

    FetchedAt   time.Time
    OK          bool
    Err         string
}
```

Every optional value is a pointer. A missing field becomes `nil`, and `nil`
renders as `—` rather than as a confident `0`. That rule is what makes a mixed
fleet degrade gracefully, and it is worth the small syntactic cost.

Variant detection: probe `/api/v2/dashboard` once at startup. HTTP 200 with
valid JSON means the fork, 404 means upstream. Cache the result and re-probe
after any run of consecutive failures, so a firmware upgrade gets picked up.

---

## 6. Bitcoin network data provider

### Recommendation: mempool.space REST, behind `BitcoinDataProvider`

```go
type BitcoinDataProvider interface {
    Network(ctx context.Context) (NetworkSnapshot, error)
    SourceKind() SourceKind   // SourcePublic | SourceLocalNode
}
```

`SourceKind()` drives the required `NETWORK SOURCE: PUBLIC | LOCAL NODE` badge,
so the UI reads it from the interface rather than from config. The badge then
cannot disagree with what is actually being queried.

Endpoints, all verified live on 2026-08-23:

| Need | Endpoint | Interval |
|---|---|---|
| tip height, timestamp, difficulty, latest blocks | `GET /api/blocks` | 30 s |
| tip height only (cheap) | `GET /api/blocks/tip/height` | |
| network hashrate + current difficulty | `GET /api/v1/mining/hashrate/3d` | 5 min |
| retarget estimate | `GET /api/v1/difficulty-adjustment` | 5 min |
| fee recommendations (optional) | `GET /api/v1/fees/recommended` | 5 min |

`GET /api/blocks` returns the last 10 blocks including `height`, `timestamp` and
`difficulty` in a single request, which covers block height, time since last
block and difficulty together. That is the primary poll. The others move slowly
enough to run every 5 minutes.

Live values captured while writing this document:

```
height            963692
difficulty        125,807,076,547,197.5      (~125.81 T)
network hashrate  907,782,986,431,433,900,000 H/s   (~907.8 EH/s)
next retarget     height 965,664, estimated -2.45%
```

Block subsidy is computed locally rather than fetched. It is a deterministic
function of height, so an API call for it would be a needless dependency:

```go
func Subsidy(height uint32) uint64 {  // satoshis
    halvings := height / 210_000
    if halvings >= 64 { return 0 }
    return 5_000_000_000 >> halvings
}
```

At height 963,692 that is four halvings, so 3.125 BTC, with the next halving at
height 1,050,000.

Configuration keeps the base URL open, so a self-hosted mempool instance or a
mirror such as `mempool.emzy.de` is a one-line change:

```yaml
bitcoin:
  provider: public
  base_url: https://mempool.space
  timeout: 8s
```

On rate limits and terms: the mempool.space public instance asks automated
clients to stay reasonable. The schedule above is about 2 requests per minute in
steady state, well inside any sane limit, and every response is cached with its
own TTL. The provider must also honour HTTP 429 with exponential backoff instead
of retrying immediately.

`BitcoinCoreRpcProvider` stays a stub in the MVP. The interface exists and is
exercised by the demo provider, so Phase 7 becomes an implementation rather than
a refactor. Bitcoin Core is never required for mining or for the dashboard.

---

## 7. Solo-pool API findings

### 7a. Public Pool, documented and clean, and the one being built

Source: `benjamin-wilson/public-pool`, NestJS, with `app.setGlobalPrefix('api')`
confirmed in `src/main.ts`. The hosted instance base URL comes from
`public-pool-ui/src/environments/environment.prod.ts`:

```
https://public-pool.io:40557
```

Note the non-standard port. It is not a typo, and a restrictive outbound
firewall will block it.

| Endpoint | Returns |
|---|---|
| `GET /api/client/{address}` | `bestDifficulty`, `workersCount`, `workers[]` |
| `GET /api/client/{address}/{workerName}` | `name`, `bestDifficulty`, `chartData` |
| `GET /api/client/{address}/{workerName}/{sessionId}` | per-session detail incl. `startTime` |
| `GET /api/client/{address}/chart` | hashrate chart series |
| `GET /api/pool` | `totalHashRate`, `blockHeight`, `totalMiners`, `blocksFound`, `fee` |
| `GET /api/network` | Bitcoin `getmininginfo` passthrough from the pool's node |
| `GET /api/info` | `blockData`, `userAgents`, `highScores`, `uptime` |

Each entry in `workers[]`:

```
{ sessionId, name, bestDifficulty, hashRate, startTime, lastSeen }
```

UNVERIFIED: the unit and type of `workers[].hashRate`. The controller passes it
through from the ORM without conversion, and `bestDifficulty` comes back as a
string in the same object. The first integration task is to capture a real
response and pin both down in `testdata/`.

Public Pool's API does not expose per-worker rejected-share counts or assigned
pool difficulty. Those are documented as unavailable rather than approximated.
The rejected count on the dashboard therefore comes from the miners' own
`sharesRejected` field, and the panel labels it as miner-reported so it is not
mistaken for a pool-side figure.

Because each miner has its own payout address, the collector runs one
`GET /api/client/{address}` per miner plus one `GET /api/pool`, all inside the
same 60 s tick. Each call gets its own timeout and its own failure state, so one
address returning 404 while the pool is still learning about a new miner does
not blank the other miner's panel. Combined best difficulty is the maximum
across addresses, never a sum.

### 7b. solo.ckpool.org, working but undocumented, deferred

Not part of the MVP. The research below stays in the document so the second
adapter is a build rather than another round of investigation.

No official API documentation exists. The endpoints below are confirmed by
reading `mrv777/ckstats` (`lib/ckpool.ts`, `scripts/updateUsers.ts`) and by a
live fetch of the pool status document.

| Endpoint | Format |
|---|---|
| `GET https://solo.ckpool.org/users/{address}` | single JSON object |
| `GET https://solo.ckpool.org/pool/pool.status` | newline-delimited JSON, five concatenated objects, not an array |

The `/users/{address}` shape, taken from the `UserData` interface in ckstats:

```
{ authorised, hashrate1m, hashrate5m, hashrate1hr, hashrate1d, hashrate7d,
  lastshare, workers, shares, bestshare, bestever,
  worker: [ { workername, hashrate1m, hashrate5m, hashrate1hr, hashrate1d,
              hashrate7d, lastshare, shares, bestshare, bestever } ] }
```

Two parsing traps, both confirmed against live data:

1. Hashrates are SI-suffixed strings, not numbers. The live pool status returned
   `"hashrate1m":"126P"`. A parser has to handle the `K M G T P E` suffix set
   plus an empty or absent value. ckstats carries a `convertHashrate()` helper
   for exactly this reason.
2. `pool.status` is not valid JSON as a whole. ckstats concatenates the objects
   with a regex before parsing. Splitting on newlines and decoding each line
   independently is cleaner and does not break if the pool adds a sixth object.

Live `pool.status` sample from 2026-08-23:

```json
{"runtime":1563463,"lastupdate":1787468041,"Users":23550,"Workers":38949,"Idle":13527,"Disconnected":4911}
{"hashrate1m":"126P","hashrate5m":"125P","hashrate15m":"126P","hashrate1hr":"150P","hashrate6hr":"228P","hashrate1d":"218P","hashrate7d":"152P"}
{"diff":46.3,"accepted":58298669080738,"rejected":90660679997221218,"bestshare":15712779548735,...}
```

ckpool does not expose per-user rejected shares (only a pool-wide figure),
per-user assigned difficulty (`diff` is pool-wide), or any explicit connection
status. Connection state has to be inferred from the age of `lastshare`, and the
UI must label it as inferred rather than presenting it as a reported status.

Rate limiting is undocumented. The design polls at 60 s, sends a descriptive
`User-Agent`, treats 404 as "address not yet known to the pool" rather than an
error, and backs off on anything else. There is no HTML scraping anywhere.

### 7c. Adapter interface

```go
type PoolAdapter interface {
    Fetch(ctx context.Context) (PoolSnapshot, error)
    Name() string
    Capabilities() Capabilities   // which fields this pool can actually supply
}
```

`Capabilities()` lets the UI hide a metric the configured pool genuinely cannot
provide, instead of showing a permanent `—` that looks like a bug.

---

## 8. Derived metrics and probability

### Per miner

```
hashrate_ths       = hashRate_GHs / 1000
efficiency_j_th    = power_W / hashrate_ths          // undefined if hashrate == 0
acceptance_ratio   = accepted / (accepted + rejected) // undefined if both 0
```

### Combined

```
total_hashrate = Σ hashrate_ths        (only miners with ok == true)
total_power    = Σ power_W             (only miners reporting power)
weighted_j_th  = total_power / total_hashrate
```

Weighted efficiency is total power over total hashrate, not the mean of the
per-miner values. Averaging J/TH across a 12 TH/s miner and a 1.3 TH/s miner
would badly misrepresent the operation.

Aggregates also record which miners contributed. If the NerdOctaxe is offline,
"TOTAL 1.27 TH/s" is technically correct and still misleading unless the tile
says how many miners are in the total.

### Block probability

Solo mining is a Poisson process. Expected hashes per block is `D · 2³²`, so:

```
λ  = H / (D · 2³²)                    blocks per second
E[blocks in T]  = λ · T
P(≥1 block in T) = 1 − e^(−λT)
E[time to block] = 1 / λ              mean
median time      = ln(2) / λ          ≈ 0.693 · mean
```

Numerical stability matters here. λT is on the order of 10⁻⁶, where
`1 - math.Exp(-x)` loses most of its significant digits to cancellation. Use:

```go
p := -math.Expm1(-lambda * seconds)
```

`Expm1` stays accurate for small arguments, and that is the difference between a
correct probability and numeric mush. This gets a required unit test.

Worked example at the reference setup, 13.37 TH/s against difficulty 125.81 T:

| Window | P(≥1 block) |
|---|---|
| 1 day | 0.000214 % |
| 1 week | 0.001497 % |
| 30 days | 0.006413 % |
| 1 year | 0.078055 % |

```
expected time to a block   ≈ 1,280 years   (mean)
median time to a block     ≈   888 years
share of network hashrate  ≈ 0.0000015 %
```

Three presentation rules:

- The label is probability, never estimate, forecast or expected reward.
- Show the median alongside the mean. The mean of an exponential distribution is
  the figure people quote and the one that misleads them; the median is the more
  honest headline.
- Never imply that elapsed time without a block changes the odds. The process is
  memoryless, and the dashboard should not suggest otherwise through progress
  bars or countdowns.

---

## 9. Historical storage

### Two tiers

The first tier is a RAM ring buffer covering 1 hour: a fixed-size circular
buffer, one sample per second per miner, allocated once at startup. 3600 samples
× 2 miners × roughly 64 bytes comes to about 0.5 MB. It costs nothing in SD-card
writes and serves the 1-hour view directly.

The second tier is SQLite, covering 24 hours and 7 days, with one row per miner
per minute written in a single batched transaction every 60 seconds.

```sql
CREATE TABLE miner_samples (
  ts          INTEGER NOT NULL,     -- unix seconds, minute-aligned
  miner       TEXT    NOT NULL,
  hashrate    REAL,
  power       REAL,
  asic_temp   REAL,
  vrm_temp    REAL,
  shares_acc  INTEGER,
  shares_rej  INTEGER,
  PRIMARY KEY (ts, miner)
) WITHOUT ROWID;
```

`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`

Volume: 2 miners × 1440 rows/day is about 2,880 rows/day, roughly 20,000 rows
for 7 days, a few MB including indexes. Retention runs daily, deletes anything
beyond 7 days, and follows with an incremental vacuum.

### Why SQLite over a rolling binary file

A hand-rolled ring file would be marginally smaller, but SQLite already ships in
the Pi OS image, gives crash-safe writes through WAL, and makes the detail-screen
queries trivial. The pure-Go driver keeps cross-compilation clean. The size
difference does not justify a custom format.

### SD-card write budget

| Writer | Writes/day |
|---|---|
| SQLite batched inserts | 1,440 transactions |
| WAL checkpoints | ~50 |
| journald (capped at 32 MB, or volatile) | bounded |
| Chromium cache | 0, redirected to `/dev/shm` |

That comes to roughly 2 MB/day of actual flash writes, far below anything that
meaningfully shortens the life of a decent A2 card.

### Failure isolation

The history writer runs in its own goroutine behind a buffered channel. If
SQLite fails through corruption, a full card or a read-only filesystem, the
writer logs the error, increments `history_write_errors_total`, and the channel
drains. The dashboard keeps working with RAM-tier history only. A history
failure must never be able to take down the live view.

---

## 10. UI — 1600 × 600 wireframe

### Physical reality check

The Waveshare 9.3" panel puts 1600 × 600 pixels across roughly 221 mm × 83 mm,
so the pixel pitch is about 0.138 mm. That constrains readability more than any
design choice:

| Tier | Font size | Digit height | Readable to |
|---|---|---|---|
| Primary metric | 96 px | ~9.3 mm | ~2 m |
| Secondary metric | 34 px | ~3.3 mm | ~0.8 m |
| Label / unit | 18 px | ~1.7 mm | ~0.4 m |

At 2 metres only the primary tier is legible. The layout should therefore commit
to that: one big number per tile, with everything else deliberately treated as a
close-up tier. Trying to make the whole screen readable at 2 m on an 83 mm-tall
panel would fail at both distances.

### Layout, 600 px vertical budget

```
┌──────────────────────────────────────────────────────────────────────────┐ 60
│ RASPBERRY MINING MONITOR      BLOCK 963,692   NETWORK: PUBLIC   ● ONLINE │
├────────────────────────┬────────────────────────┬────────────────────────┤ 220
│ NERDOCTAXE          ●  │ GAMMA 602           ●  │ TOTAL                  │
│                        │                        │                        │
│   12.10 TH/s           │    1.27 TH/s           │   13.37 TH/s           │
│                        │                        │                        │
│  62 °C   158 W         │   55 °C    18 W        │  176 W    13.2 J/TH    │
│  13.1 J/TH  4820 rpm   │   14.2 J/TH  5100 rpm  │  2 of 2 miners online  │
├────────────────────────┴───────────┬────────────┴────────────────────────┤ 268
│ SOLO MINING          public-pool   │ BITCOIN NETWORK                     │
│                                    │                                     │
│ Best share      18.7 G             │ Difficulty      125.81 T            │
│ Best ever      124.3 G             │ Network hashrate  907.8 EH/s        │
│ Accepted / rejected  1,284 / 2     │ Last block      00:03:42 ago        │
│ Last share      12 s ago           │ Subsidy         3.125 BTC           │
│                                    │ Next retarget   −2.45 % in 13 d     │
│ P(block) 30 d    0.0064 %          │                                     │
│ Median time to block   888 years   │ Your share      0.0000015 %         │
├────────────────────────────────────┴─────────────────────────────────────┤ 28
│ miners 2s · pool 41s · network 12s          [ MINERS ] [ CHAIN ] [ PI ]  │
└──────────────────────────────────────────────────────────────────────────┘
   60 + 8 + 220 + 8 + 268 + 8 + 28 = 600 px, no scrolling
```

### Behaviour

A dark sci-fi operations palette on a deep-space navy ground (`#050B12`), with
cyan panel chrome (`#00E5FF`), ice-blue values (`#E0F4FF`), muted cyan labels
(`#6592A6`), and a gold fleet-total tile (`#FFD54F`). Status uses three colours
only: green `#00E676`, amber `#FFB300`, red `#FF3D00`.

Panel chrome, corner brackets, glow, badge pills and the wireframe icons are
drawn in CSS and inline SVG rather than baked into a background image. That is
deliberate. A static background cannot dim a panel when its data goes stale,
cannot switch a status ring from filled to hollow, cannot turn a badge red at a
threshold, and cannot grow a fourth tile when a third miner is added. All four
are requirements elsewhere in this document, so the chrome has to be live.

State is shown by colour and by shape, never by colour alone. Offline is a
hollow ring plus dimmed values rather than only a red dot, which survives both
colour-blindness and a glance from across the room.

Staleness is visible. A source older than three times its poll interval dims its
panel and shows the age. Values are never silently frozen.

Temperature thresholds are per miner and adjustable at runtime.

The band is anchored to the firmware rather than picked by eye. Both AxeOS
variants trigger their own thermal protection at 70 °C: the NerdQAxe fork ships
`overheat_temp=70`, upstream ships `selftest_max=70`. Red therefore sits exactly
on that line, because a critical level above it would colour the tile red only
after the firmware had already intervened. Amber at 64 gives six degrees of
warning first, which leaves a NerdOctaxe idling at 62 to 63 °C green. The
NerdQAxe fan controller targets 55 °C, so that idle range is the expected
steady state rather than a problem. The VRM band stays amber at 80 and red at 90.

A `/settings` page changes these per miner without a restart. It is a display
threshold and nothing more: no value from that page ever reaches a miner.

The page and `/healthz` answer only to requests whose remote address is
loopback, which on the Pi means the kiosk itself. The LAN dashboard therefore
stays strictly read-only, and the settings route is not merely refused there but
invisible, answering 404. `X-Forwarded-For` and `X-Real-IP` are deliberately not
honoured, since trusting them would let any LAN client claim to be local. The
consequence is stated in the config: do not put this service behind a reverse
proxy with the settings surface enabled, or set `dashboard.settings` to false.

Overrides persist to a small JSON file written atomically at mode 0600. A
missing file is the normal first-run state; a corrupt one is logged and the
defaults stay in force, because a bad settings file must not blank the
dashboard.

Touch targets are at least 64 px. The three footer buttons open detail screens
for miner history, chain detail and Pi health, each with an obvious back
control.

No secrets appear on screen. The payout address in `stratumUser` is truncated to
`bc1q…f4k2`. It is public information, but there is no reason to display it in
full on a screen a visitor can photograph.

Refresh is SSE-driven at 1 Hz. On disconnect the browser reconnects
automatically and the header badge shows `● RECONNECTING` until a snapshot
arrives.

The screen stays awake through the kiosk compositor's idle settings rather than
a JavaScript wake lock.

---

## 11. Security

### What the system will not do

| Constraint | Enforcement |
|---|---|
| No private keys or seed words | Nothing in the design reads or stores key material. |
| No wallet files | No filesystem access outside its own config and data directory. |
| No wallet passwords | No concept of a wallet exists in the code. |
| No miner configuration changes | The AxeOS client exposes GET only. `PATCH`/`POST` helpers are not implemented, so misuse would require writing new code rather than flipping a flag. |
| No credentials in the frontend | The snapshot document is built by an explicit allowlist projection, not by re-serialising the upstream JSON. |
| No telemetry or analytics | No outbound calls beyond the configured miner, pool and Bitcoin endpoints. |
| No developer fee | None. |
| No runtime code download | Everything ships in the binary, and CSP forbids external script sources. |

The allowlist projection deserves a note. The obvious implementation, proxying
the miner's JSON straight to the browser, would leak whatever a future firmware
version decides to add to `/api/system/info`. Building the response field by
field means new upstream fields stay invisible until someone deliberately adds
them.

### Network posture

```
Miners  ──► trusted VLAN, no internet route needed for the monitor's purposes
Pi      ──► binds dashboard to LAN interface, port 8080
        ──► binds /healthz, /metrics, /debug to 127.0.0.1 only
        ──► outbound allowlist: configured pool host + Bitcoin provider host
```

The AxeOS HTTP API has no authentication. Anyone on the same network can
`PATCH /api/system` and change a miner's pool, frequency or WiFi settings. That
is a property of the miners rather than of this project, but the install guide
has to say so plainly and recommend a separate VLAN, or at minimum a wireless
network the miners share with nothing else. The NerdQAxe fork's
`is_network_allowed()` and OTP options are worth enabling.

The dashboard itself is read-only and has no login in the MVP. That is
acceptable only because it exposes nothing an attacker on the LAN could not read
directly from the miners. It is stated here as an explicit assumption, and the
config allows binding to `127.0.0.1` only for anyone who disagrees.

### Hardening

- `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'`
- Plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
- systemd unit: `DynamicUser=yes`, `NoNewPrivileges=yes`, `ProtectSystem=strict`,
  `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`,
  `RestrictAddressFamilies=AF_INET AF_INET6`, `MemoryMax=128M`,
  `StateDirectory=rmm`.
- Config file `0640`, owned by the service user. No credentials belong in it
  today. If a future private pool needs one, it gets read from a separate file
  or an environment variable and never echoed to logs or API responses.
- Repository hygiene: `.gitignore` covers `config.yaml`, `*.db` and `data/`.
  Only `config.example.yaml` with placeholder values is committed. A pre-commit
  secret scan is worth adding.
- Dependencies pinned in `go.mod` with checksums in `go.sum`, `govulncheck` in
  CI, and the vendored uPlot build checksummed.

---

## 12. Risks and unknowns

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| 1 | Chromium RAM growth on 1 GB across Pi OS updates | dashboard OOMs at 3 a.m. | pin Chromium version; zram; `MemoryMax` on the backend; expose free memory on the health endpoint and the Pi detail screen |
| 2 | ckpool API is undocumented and unversioned | pool panel breaks silently | parse defensively, treat every field as optional, capability-gate the UI, alert on parse failure instead of showing zeros |
| 3 | NerdQAxe fork is moving fast (v1.0.34 added `/api/v2/*`) | field names shift under us | variant detection at runtime; captured `testdata/` fixtures per firmware version; never assume a field exists |
| 4 | mempool.space rate limits or terms change | network panel goes dark | configurable base URL, cached responses, 429 backoff, documented alternative instances, Core RPC path already designed |
| 5 | `workers[].hashRate` unit from Public Pool is unconfirmed | hashrate off by 10³ or 10⁹ | capture a real response before writing the parser; add a sanity check that rejects values implausible against the miners' own reported hashrate |
| 6 | Waveshare 1600×600 needs custom HDMI timings | blank or letterboxed screen | verify the exact `cmdline.txt`/`config.txt` lines on the real panel during Phase 6 and document them |
| 7 | `.local` mDNS resolution is unreliable on some clients | LAN access fails | document the IP fallback and a static DHCP lease as the primary route |
| 8 | Pi clock skew after a power cut without NTP | "last block 4 hours ago" nonsense | use monotonic clocks for freshness; warn if wall-clock time is implausible against the tip block timestamp |
| 9 | Probability figures get misread as predictions | user disappointment, or worse, financial decisions | strict wording, median shown next to mean, an explanatory detail screen |
| 10 | SD-card wear or corruption | history loss | batched writes, tmpfs for caches, WAL, and history isolated so its failure is not fatal |
| 11 | Two payout addresses means two Public Pool calls per tick, and a new address returns 404 until the pool sees its first share | a miner panel looks broken on first run | treat 404 as "not yet known to the pool" and show a distinct "awaiting first share" state rather than an error |
| 12 | Both payout addresses appear in the config file and in outbound request URLs | addresses are public but still identifying | config file `0640`; addresses truncated on screen; no logging of full addresses at default log level |

### Open questions

All four are answered. Recorded here for traceability.

1. Solo pool: Public Pool. The ckpool adapter is designed but deferred.
2. Product and binary name: `raspberry-mining-monitor`, binary `rmm`.
3. Payout addresses: one per miner, so the pool collector makes one call per
   address and per-miner attribution is direct.
4. Licence: MIT.

---

## 13. Recommended MVP scope

### In

Phase 1, simulated dashboard, developed on macOS first:

- Go backend, config loading, SSE and REST, `go:embed` frontend
- the full 1600×600 dashboard rendering correctly in a browser window
- `--demo` mode generating realistic, drifting miner, pool and network data with
  occasional simulated dropouts, so error states are visible during development
- probability and aggregation packages, fully unit-tested
- subsidy calculation, unit-tested across halving boundaries

Phase 2, real AxeOS, read-only — DONE:

- variant-detecting adapter for both firmwares
- per-miner timeout, backoff and staleness
- fixture-based tests from captured real responses

Phase 3, Bitcoin network: the mempool.space provider behind the interface, with
the source badge — DONE.

Phase 4, solo pool: the Public Pool adapter, polling one address per miner plus
the pool-wide endpoint, with capability gating so the missing rejected-share and
pool-difficulty fields are hidden rather than shown empty — DONE. Worker
hashRate resolved to H/s from the pool source and normalised to TH/s.

Phase 5, history: RAM ring buffer plus SQLite tier, retention, and detail
screens using uPlot.

Phase 6, Pi deployment: systemd units, cage and Chromium kiosk, install guide,
panel timings.

### Out of the MVP, deliberately

- the solo.ckpool.org adapter (researched, interface ready, not built)
- Bitcoin Core RPC (interface only)
- Prometheus, Grafana, alerting
- any write path to the miners
- authentication on the dashboard
- mobile-specific layout
- Stratum proxy or self-hosted pool
- multi-language UI

### Definition of done for Phase 0 → 1

The demo build runs on macOS with `./rmm --demo`, renders the full dashboard at
1600×600 without scrolling, and every number on screen is either live-simulated
or explicitly `—`. No placeholder text and no hard-coded values pretending to be
data.

---

## Appendix — sources

Firmware:
- [`bitaxeorg/ESP-Miner` — `main/http_server/http_server.c`](https://github.com/bitaxeorg/ESP-Miner/blob/master/main/http_server/http_server.c), route registration
- [`bitaxeorg/ESP-Miner` — `main/http_server/openapi.yaml`](https://github.com/bitaxeorg/ESP-Miner/blob/master/main/http_server/openapi.yaml), SystemInfo schema
- [`shufps/ESP-Miner-NerdQAxePlus` — `main/http_server/http_server.cpp`](https://github.com/shufps/ESP-Miner-NerdQAxePlus/blob/master/main/http_server/http_server.cpp), route registration
- [`shufps/ESP-Miner-NerdQAxePlus` — `main/http_server/v2/handler_v2_dashboard.cpp`](https://github.com/shufps/ESP-Miner-NerdQAxePlus/blob/master/main/http_server/v2/handler_v2_dashboard.cpp), v2 dashboard document
- [`shufps/ESP-Miner-NerdQAxePlus` — `main/http_server/handler_system.cpp`](https://github.com/shufps/ESP-Miner-NerdQAxePlus/blob/master/main/http_server/handler_system.cpp), legacy info document
- [Bitaxe API endpoints, OSMU wiki](https://osmu.wiki/bitaxe/api/)

Pools:
- [`benjamin-wilson/public-pool` — `src/main.ts`](https://github.com/benjamin-wilson/public-pool/blob/master/src/main.ts), `api` global prefix
- [`benjamin-wilson/public-pool` — `src/controllers/client/client.controller.ts`](https://github.com/benjamin-wilson/public-pool/blob/master/src/controllers/client/client.controller.ts), client routes and response shape
- [`benjamin-wilson/public-pool` — `src/app.controller.ts`](https://github.com/benjamin-wilson/public-pool/blob/master/src/app.controller.ts), `/api/pool`, `/api/network`, `/api/info`
- [`benjamin-wilson/public-pool-ui` — `environment.prod.ts`](https://github.com/benjamin-wilson/public-pool-ui), hosted API base URL and port
- [`mrv777/ckstats` — `lib/ckpool.ts`](https://github.com/mrv777/ckstats/blob/main/lib/ckpool.ts), ckpool endpoints and NDJSON handling
- [`mrv777/ckstats` — `scripts/updateUsers.ts`](https://github.com/mrv777/ckstats/blob/main/scripts/updateUsers.ts), `UserData` and `WorkerData` field names
- [solo.ckpool.org](https://solo.ckpool.org/), live `pool/pool.status` fetched 2026-08-23

Bitcoin data:
- [mempool.space REST API documentation](https://mempool.space/docs/api/rest)
- Live values fetched 2026-08-23 from `/api/blocks/tip/height`,
  `/api/v1/difficulty-adjustment` and `/api/v1/mining/hashrate/3d`
