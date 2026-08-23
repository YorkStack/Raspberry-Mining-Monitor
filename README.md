# Raspberry-Mining-Monitor

A lightweight, open-source Bitcoin solo-mining operations dashboard for
AxeOS-compatible miners.

Designed for small always-on systems such as the Raspberry Pi and optimized
for ultrawide touch displays.

Solo Mining Deck combines miner telemetry, pool statistics and Bitcoin network
data into a single real-time dashboard.

> This project is currently under development.

## What it will show

### Miner telemetry

Monitor multiple AxeOS / ESP-Miner compatible devices:

- Hashrate
- ASIC temperature
- VRM temperature
- Power consumption
- Efficiency (J/TH)
- Frequency and voltage
- Fan speed
- Uptime
- Accepted / rejected shares
- Best share / best difficulty
- Pool connection status

### Bitcoin network

Display relevant Bitcoin network information:

- Current block height
- Network difficulty
- Estimated network hashrate
- Time since last block
- Block subsidy
- Solo-mining probability

### Combined mining statistics

Multiple miners can be aggregated into one mining operation:

    NerdOctaxe       ~12.0 TH/s
    Bitaxe Gamma     ~1.3 TH/s
    ----------------------------
    TOTAL            ~13.3 TH/s

The dashboard calculates combined hashrate, power consumption, efficiency
and estimated solo block probabilities.

## Target Setup

Initial reference hardware:

### Dashboard

- Raspberry Pi 4 Model B
- 1 GB RAM
- Raspberry Pi OS
- Waveshare 9.3" capacitive touchscreen
- 1600 × 600 resolution

### Miners

- NerdOctaxe Rev 3.1
- Bitaxe Gamma 602
- Other AxeOS-compatible miners planned

The miners connect directly to their configured mining pool.

Solo Mining Deck is monitoring infrastructure only and is **not part of the
mining path**.

If the dashboard or Raspberry Pi is offline, mining continues normally.

## Architecture

    Bitcoin Network
          │
          │
       Solo Pool
          │
       Stratum
          │
     ┌────┴─────┐
     │          │
 NerdOctaxe   Bitaxe
     │          │
     └────┬─────┘
          │
       AxeOS API
          │
          ▼
 ┌───────────────────┐
 │ Solo Mining Deck  │
 │                   │
 │ Collector         │
 │ Metrics           │
 │ Probability       │
 │ Dashboard         │
 └─────────┬─────────┘
           │
           ▼
   1600 × 600 Display


## Dashboard Concept

The primary screen is designed as a compact Bitcoin mining operations center:

    ┌───────────────────────────────────────────────────────────────┐
    │ SOLO MINING DECK                       BLOCK 963xxx  ● ONLINE │
    ├───────────────────┬───────────────────┬───────────────────────┤
    │ NERDOCTAXE        │ GAMMA 602         │ TOTAL                 │
    │                   │                   │                       │
    │ 12.10 TH/s        │ 1.27 TH/s         │ 13.37 TH/s            │
    │ 62°C              │ 55°C              │ 176 W                 │
    │ 158 W             │ 18 W              │ 13.2 J/TH             │
    ├───────────────────┴───────────────────┼───────────────────────┤
    │ SOLO MINING                           │ BITCOIN NETWORK       │
    │ Best share       18.7 G               │ Difficulty   xx.x T   │
    │ Accepted          1,284               │ Hashrate     xxx EH/s │
    │ Rejected              2               │ Last block   03:42    │
    │ Block probability ...                 │ Subsidy      3.125 BTC│
    └───────────────────────────────────────┴───────────────────────┘


## Design Goals

The project follows five priorities:

1. Security
2. Reliability
3. Low resource usage
4. Clear observability
5. Simple, attractive UI

The application is intentionally lightweight enough to run 24/7 on a
Raspberry Pi with limited memory.

## Security

Solo Mining Deck is designed as a **read-only monitoring system**.

It does not require or store:

- Bitcoin private keys
- Seed phrases
- Wallet passwords
- Hardware-wallet secrets

A miner never needs access to Bitcoin private keys.

Only public mining information and public Bitcoin addresses may be used where
required by pool APIs.

No telemetry, analytics or developer-fee functionality is planned.

## Development

Primary development platform:

    MacBook Pro
    Apple Silicon
    macOS

Production target:

    Raspberry Pi 4
    ARM64
    Raspberry Pi OS

A demo mode will allow development without physical miners:

    ./solo-mining-deck --demo

Demo mode will simulate realistic miner, pool and Bitcoin network data.

## Roadmap

### Phase 0 — Architecture
- Technology selection
- AxeOS API research
- Pool API research
- Bitcoin data provider
- UI design
- Raspberry Pi resource budget

### Phase 1 — Demo
- 1600 × 600 dashboard
- Simulated miners
- Simulated Bitcoin network
- Solo probability calculations

### Phase 2 — AxeOS
- Read-only AxeOS integration
- Multiple miners
- Health and stale-data detection

### Phase 3 — Bitcoin
- Live Bitcoin network data
- Difficulty
- Network hashrate
- Block information

### Phase 4 — Solo Pools
- Public Pool
- solo.ckpool.org
- Shares and best difficulty where available

### Phase 5 — History
- Hashrate history
- Temperature history
- Power history
- Lightweight local storage

### Phase 6 — Raspberry Pi
- systemd service
- Automatic startup
- Chromium kiosk mode
- Waveshare 1600 × 600 optimization

### Future

Potential future extensions include:

- Local Bitcoin Core integration
- Bitcoin Core RPC data provider
- Own Stratum proxy / solo pool
- Prometheus exporter
- Grafana integration
- Additional AxeOS miners
- Alerts
- Mobile dashboard

## Why?

Solo mining is often reduced to a single number: hashrate.

Solo Mining Deck is intended to make the complete process more visible:

    Bitcoin Network
           ↓
       Solo Pool
           ↓
      Stratum Job
           ↓
       ASIC Miner
           ↓
       SHA-256d
           ↓
         Shares
           ↓
      Difficulty
           ↓
    Block Probability

The goal is not to make solo mining predictable.

It is to make it **observable and understandable**.

## Status

🚧 Early development / architecture phase.

The first milestone is a fully simulated dashboard running on macOS before
deployment to Raspberry Pi hardware.

## License

License to be determined before the first public release.
