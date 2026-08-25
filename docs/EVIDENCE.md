# Mining Evidence & Tax Documentation

A separate service, `rmm-evidence`, that collects, preserves and (in later
phases) exports objective evidence about mining activity. It is independent of
the live monitor: its own binary, its own database, its own config.

## Legal boundary

The module documents **objective technical facts only**. It does not determine
that mining is private or commercial, that a reward is taxable or tax-free, that
a cost is deductible, that a timestamp is the authoritative receipt date, or
that a later sale is tax-free. Every report carries:

> Technical factual documentation only. This report does not determine the legal
> or tax classification of the mining activity.

The collected material is intended for review by a German tax adviser or
authority, who makes the legal and tax determinations.

## Architecture decisions

- **Separate binary** `cmd/rmm-evidence`, sharing the repo but not the monitor's
  runtime. The monitor binary `rmm` links **zero** evidence dependencies and
  stays lean.
- **Storage:** pure-Go SQLite (`modernc.org/sqlite`, no cgo), so the binary is a
  single static file and cross-compiles for the Pi. Foreign keys on, WAL
  journalling (crash-safe, fewer SD-card writes), busy timeout.
- **Integrity:** append-only tables and a hash-chained audit log. Money is euro
  cents, Bitcoin is satoshi, energy is Wh, hashrate is H/s — all integers, never
  floating point.
- **PDF/A** (later phase) will use an external tool (Ghostscript / veraPDF) for
  true PDF/A-2b/3b and validation.
- **Backups** (later phase): local + NAS/SSD, and optionally an encrypted
  off-device copy. Any off-device target holds only client-side-encrypted data
  over a secure channel; a public web host is never an evidence store.

## Phased delivery

1. **Foundation (this phase):** SQLite store + migrations, versioned miner
   inventory, document management (hashed, versioned, validated), append-only
   audit log with a hash chain.
2. Telemetry persistence + hourly/daily aggregates + network snapshots +
   immutable contemporaneous expected-value snapshots.
3. Watch-only reward detection + EUR valuation policy + configuration history.
4. Energy measurement + cost records.
5. Reporting-period close + CSV exports + evidence/final manifests + integrity.
6. PDF/A report + digital signing + printing + backup + annual package.
7. UI section "Tax & Evidence" + full operator documentation.

## What the foundation provides

- `internal/evidence/store` — SQLite open + embedded migrations (idempotent,
  data-preserving).
- `internal/evidence/audit` — hash-chained append-only log; `Verify` detects any
  edit or deletion.
- `internal/evidence/miner` — versioned inventory: a change supersedes the
  current version and inserts a new one; history is preserved.
- `internal/evidence/document` — file storage with SHA-256, versioning (never
  overwrites), and validation against path traversal, filename injection,
  executables and MIME spoofing.
- `internal/evidence/config` — the `evidence:` config block.
- `cmd/rmm-evidence` — `init`, `miner-add`, `miner-list`, `doc-add`,
  `audit-verify`, `version`.

## Configuration

The evidence service reads its own YAML file (default `evidence.yaml`). The
foundation uses this subset; later phases extend the block:

```yaml
evidence:
  enabled: true
  data_directory: /data/mining-evidence   # DB + documents live here (use an SSD)
  timezone: Europe/Berlin                  # local time recorded alongside UTC
  report_language: de
```

## Usage (foundation)

```bash
rmm-evidence --config evidence.yaml init
rmm-evidence --config evidence.yaml --actor york miner-add \
  --id NERD-01 --manufacturer BitAxe --model NerdOctaxe --serial SN123 \
  --price-cents 29900 --hashrate-hs 12000000000000 --power-w 158
rmm-evidence --config evidence.yaml --actor york doc-add \
  --type miner_invoice --file ./invoice.pdf --description "NerdOctaxe invoice"
rmm-evidence --config evidence.yaml miner-list
rmm-evidence --config evidence.yaml audit-verify
```

Build for the Pi (pure Go, no cgo):

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o rmm-evidence-linux-arm64 ./cmd/rmm-evidence
```

## Never stored

Bitcoin private keys, wallet seed phrases, wallet or exchange passwords, or
unencrypted API credentials. Watch-only addresses only (from a later phase).
