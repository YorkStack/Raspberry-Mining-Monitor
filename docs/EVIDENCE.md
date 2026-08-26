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
2. **Telemetry persistence + aggregates + network snapshots + immutable
   expected-value (done):** `telemetry` (raw + hourly aggregate with
   completeness/gaps + retention prune), `network` (snapshots with raw response
   and SHA-256, append-only), `expected` (contemporaneous expected value,
   integer satoshi/cents, immutable and formula-versioned). The `ingest` command
   records a monitor snapshot into all three:
   `rmm-evidence ingest --from http://127.0.0.1:8080/api/v1/snapshot`.
   Daily network snapshots and their expected value are frozen: the first
   observation of the day wins and is never recalculated.
3. **Watch-only rewards + EUR valuation + configuration history (done):**
   `configlog` (append-only effective config; a change closes the previous and
   inserts a new one), `reward` (watch-only addresses; reward events with all
   timestamps, source classification, raw response + SHA-256, idempotent per
   txid/vout; confirmation tracking; reorg preserves the original and adds a
   status event; never stores keys/seeds), `valuation` (versioned per-year
   policy; EUR value in integer cents; primary→fallback with the reason
   recorded; manual correction inserts a new row preserving the original). CLI:
   `watch-add`, `watch-list`, `policy-set`. Live blockchain/price adapters are a
   thin follow-on; the domain logic is provider-agnostic and tested with
   injected data.
4. **Energy measurement + cost records (done):** `energy` (measurements keep
   physically measured and estimated consumption separate; an estimate must
   state its method and author; gaps are recorded via completeness, never
   interpolated; GRID/SOLAR/MIXED classification) and `cost` (integer euro
   cents; no automatic deduction/depreciation/VAT determination; adviser
   adjustments are separate records preserving the original; preliminary
   per-category summaries labelled as such). CLI: `cost-add`, `cost-summary`.
5. **Reporting-period close + CSV exports + evidence manifest + integrity
   (done):** `export` (one deterministic CSV per dataset — UTF-8, table-order
   columns, integer base units — plus `evidence-manifest.json` hashing every
   file and an evidence-bundle hash; `VerifyEvidencePackage` fails on any byte
   change) and `report` (pre-close validation warnings; `Close` refuses a
   period with warnings unless acknowledged and refuses to overwrite a closed
   original; `Revise` creates `MINING-YYYY-MM-REVISION-NNN` referencing the
   original, which stays intact). CLI: `period-validate`, `period-close`,
   `period-revise`, `verify`. Report ids: `MINING-2026-08-ORIGINAL` /
   `-REVISION-001`. The final manifest with the PDF hash and signing is phase 6.
6. **Digital signing + stage-2 final manifest + backup + PDF report (done):**
   - **6a (done):** `signing` (dedicated Ed25519 key in a 0600 file, kept for
     verification after rotation), `finalmanifest` (stage-2 manifest hashing the
     export files and the PDF, signed detached; the final PDF hash lives in the
     manifest, never inside the PDF, to avoid a circular reference), `backup`
     (byte-verified copy of the package with a recorded run). CLI: `finalize`,
     `verify-final`.
   - **6b (done):** `pdf` renders the self-contained A4 report — cover with the
     full evidence-bundle hash and a QR code, per-page footer (report id, period,
     revision, page x of {nb}, short hash), the "technical factual documentation
     only" disclaimer on the cover and a closing page, and one section per
     dataset (miners, rewards, valuations, costs by category, measured vs
     estimated energy, data gaps and corrections). `pdf/pdfa` validates PDF/A-2b
     with veraPDF when present and reports "not validated" (never a false claim)
     when it is absent; the verdict is recorded in the manifest and audit log.
     `finalize` now generates the PDF into `summary/report.pdf` before signing.
     The gated `print` command prints (or points to) the authoritative PDF only
     after the final manifest verifies and a PDF hash is recorded; there is no
     automatic printing, and a tampered PDF is refused.
7. **Annual package + "Tax & Evidence" view + operator documentation (done):**
   - `annual` (`internal/evidence/annual`) rolls a tax year's monthly evidence
     packages into one signed, self-contained annual package: it copies each
     closed month's latest revision, re-verifies every copy against its own
     manifest and recorded evidence-bundle hash, records a factual year-filtered
     summary (rewards, valuations, costs, measured vs estimated energy — amounts
     only, never a tax result), and signs `annual-manifest.json` with the same
     Ed25519 key. CLI: `annual --year YYYY [--backup DIR]`, `verify-annual`.
   - `serve` (`internal/evidence/serve`) is a read-only "Tax & Evidence" status
     server in the evidence binary — a JSON endpoint plus a self-contained page
     (no external assets) showing the audit-chain state, reports, annual
     packages, miners and watched addresses, with the disclaimer. It mutates
     nothing. The monitor stays untouched and keeps zero SQLite/evidence deps.
     CLI: `serve [--addr 127.0.0.1:8090]`.
   - Operator documentation: `docs/EVIDENCE-OPERATIONS.md` (install, daily
     ingest, monthly close, finalize, annual package, serve, backup/restore,
     key management, the legal boundary, troubleshooting).

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
