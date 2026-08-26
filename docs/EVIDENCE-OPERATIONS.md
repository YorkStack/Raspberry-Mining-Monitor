# Mining Evidence: Operator Guide

This guide covers running the `rmm-evidence` service day to day: setting it up,
recording evidence, closing a month, finalising and signing a report, rolling up
a tax year, viewing status, and keeping backups. It assumes the Raspberry Pi
setup from the main README.

## What this service does, and what it does not

`rmm-evidence` records objective technical facts about your mining activity and
preserves them so they cannot be silently changed. That includes hashed
documents, an append-only audit log with a hash chain, immutable expected-value
and network snapshots, watch-only reward tracking, EUR valuations, energy and
cost records, signed monthly reports, and a signed annual package.

It does not decide anything about tax. It never determines whether your mining
is private or commercial, whether a reward is taxable or tax-free, whether a cost
is deductible, which timestamp counts as the receipt date, or whether a later
sale is tax-free. Every report carries the line:

> Technical factual documentation only. This report does not determine the
> legal or tax classification of the mining activity.

Those judgements belong to you and your tax adviser. The service gives you the
documented facts to make them from.

It also never stores secrets. Do not put private keys, seed phrases, wallet
passwords, exchange passwords, or unencrypted API credentials anywhere in the
evidence data or documents. Reward tracking is watch-only: it needs a payout
address, never a key.

## Install and configure

The evidence service is a separate binary from the monitor. Build it for the Pi
(no cgo, so a plain cross-compile works):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o rmm-evidence ./cmd/rmm-evidence
```

Copy it to the Pi and create `evidence.yaml` next to it:

```yaml
evidence:
  enabled: true
  data_directory: /data/mining-evidence   # database + documents live here; use the SSD, not the SD card
  timezone: Europe/Berlin                  # local time is recorded alongside UTC
  report_language: de
```

Put `data_directory` on the SSD. It holds the SQLite database, stored documents,
report packages, the annual packages, and the signing key. Initialise it once:

```bash
./rmm-evidence init
```

That creates the database, runs the migrations, and prints the schema version.

Every command takes `--config` (default `evidence.yaml`) and `--actor NAME`, the
person doing the action, which is recorded in the audit log. Set `--actor` to a
real name so the audit trail means something.

## The daily rhythm

### Record miners and their documents

Add each miner to the versioned inventory, then attach its purchase documents:

```bash
./rmm-evidence miner-add --id rig-01 --manufacturer Bitmain --model "Antminer S9" \
  --serial SN12345 --price-cents 45000 --hashrate-hs 13500000000000 --power-w 1350
./rmm-evidence doc-add --type invoice --file ./invoices/rig-01.pdf --description "Antminer S9 invoice"
```

A miner change (a repair or a firmware update, say) supersedes the current
version and keeps the old one, so history is never lost. Documents are hashed on
the way in and never overwritten; adding the same filename again stores a new
version.

### Watch your payout address

```bash
./rmm-evidence watch-add --address bc1qyourpayoutaddress
./rmm-evidence watch-list
```

This is watch-only. It records rewards that arrive at the address and has no
access to the wallet.

### Ingest a monitor snapshot

Point the evidence service at the running monitor's snapshot endpoint. Each
ingest records telemetry, the daily network snapshot, and the contemporaneous
expected value:

```bash
./rmm-evidence ingest --from http://127.0.0.1:8080/api/v1/snapshot
```

Run this on a schedule. A cron entry or a systemd timer every few minutes is
typical:

```cron
*/5 * * * * /opt/rmm/rmm-evidence --config /opt/rmm/evidence.yaml ingest --from http://127.0.0.1:8080/api/v1/snapshot
```

The daily network snapshot and its expected value are frozen on first
observation: the first reading of the day wins and is never recalculated, so the
figures your report rests on are the ones that were true at the time.

### Record energy and costs

Record measured energy where you have a meter, and estimates where you do not.
They stay separate, and an estimate must state its method and who made it. Costs
go in as integer euro cents:

```bash
./rmm-evidence cost-add --date 2026-08-03 --description "Grid electricity August" \
  --category ENERGY --gross-cents 6120
./rmm-evidence cost-summary --period 2026-08
```

The cost summary is a preliminary factual grouping. It does not apply
deductions, depreciation, or VAT treatment; that is the adviser's call.

## Closing a month

At the end of a reporting period, check for gaps first:

```bash
./rmm-evidence period-validate --period 2026-08
```

This lists warnings: a miner with no serial number, an unwatched address, still
unconfirmed rewards, hours with incomplete telemetry. Fix what you can, then
close the period:

```bash
./rmm-evidence period-close --period 2026-08
```

If warnings remain, the close is refused until you acknowledge them, and the
acknowledged warnings are stored in the report so nothing is hidden:

```bash
./rmm-evidence period-close --period 2026-08 --acknowledge
```

Closing writes the evidence package (one deterministic CSV per dataset plus an
`evidence-manifest.json` that hashes every file) and an evidence-bundle hash that
identifies the whole package. A closed original is never overwritten. If
something has to change later, create a revision that references the original,
which stays intact:

```bash
./rmm-evidence period-revise --period 2026-08 --reason "corrected August invoice amount"
```

Verify any package against its manifest at any time:

```bash
./rmm-evidence verify --dir /data/mining-evidence/reports/2026-08/original
```

## Finalising a report

Closing produces the package; finalising produces the authoritative, signed
record. `finalize` generates the human-readable PDF, validates it for PDF/A when
a validator is installed, then builds and signs the stage-2 final manifest with
the dedicated evidence key:

```bash
./rmm-evidence finalize --period 2026-08 --backup /mnt/nas/mining-evidence
```

You get `summary/report.pdf`, a signed `final-manifest.json` with its detached
`final-manifest.sig`, and, with `--backup`, a byte-verified copy on the target.

PDF/A validation is best-effort. With veraPDF on the PATH, the manifest and audit
log record a real verdict. Without it, they record "not validated (no PDF/A
validator installed)" rather than a false claim. To install the tools on the Pi:

```bash
sudo apt-get install ghostscript      # optional PDF/A conversion
# veraPDF: install from verapdf.org for validation
```

Verify a finalised report, and print it only through the gated command, which
refuses unless the manifest verifies and a PDF hash is on record:

```bash
./rmm-evidence verify-final --dir /data/mining-evidence/reports/2026-08/original
./rmm-evidence print --dir /data/mining-evidence/reports/2026-08/original            # shows the authoritative PDF path
./rmm-evidence print --dir /data/mining-evidence/reports/2026-08/original --cups BrotherHL   # sends it to a CUPS printer
```

There is no automatic printing. A tampered PDF is refused. The printed page says
so plainly: the signed PDF/A and signed final manifest in the archive are the
authoritative record, and paper is a copy.

## The annual package

Once a tax year's months are closed, roll them into one signed package for your
adviser:

```bash
./rmm-evidence annual --year 2026 --backup /mnt/nas/mining-evidence
```

This copies each month's latest revision into `annual/2026/`, re-verifies every
copied file against its own manifest and recorded bundle hash, records a factual
year-filtered summary (rewards, valuations, costs, measured versus estimated
energy, as amounts only), and signs `annual-manifest.json`. The result is one
self-contained, portable, signed directory. Verify it the same way:

```bash
./rmm-evidence verify-annual --dir /data/mining-evidence/annual/2026
```

The summary states figures, not conclusions. What they mean for your return is
for you and your adviser to decide.

## The read-only status view

`serve` runs a read-only "Tax & Evidence" page and JSON endpoint. It mutates
nothing, so it is safe to leave running and to show to your adviser:

```bash
./rmm-evidence serve --addr 127.0.0.1:8090
```

Then open `http://127.0.0.1:8090/`. It shows the audit-chain state, the reports
and their evidence-bundle hashes, annual packages, active miners, and watched
addresses. `GET /api/status` returns the same as JSON, and `GET /healthz` is a
health check. The page fetches no external assets.

Bind it to `127.0.0.1` unless you have deliberately put access control in front
of it. It is read-only, but it still shows your archive's contents.

## Backups

Keep at least two copies beyond the Pi:

1. A local copy on a separate disk or NAS. `finalize` and `annual` write one
   directly with `--backup`.
2. An off-site copy.

The `brownbeaver.de` site may be used only as a third, encrypted copy: client-side
encrypted, transferred over SFTP, and stored outside the web root. It must never
hold plaintext or publicly reachable evidence. Confirm the SFTP path and the
non-public location before using it for anything.

Every backup is byte-verified as it is written: each copied file's hash is
checked against the source, and the run is recorded. A backup that does not
verify fails loudly rather than leaving you with a bad copy you think is good.

Test that a backup actually restores. Copy an `annual/` or report package to a
scratch machine and run `verify-annual` or `verify-final` against it. A backup
you have never restored is not yet a backup you can rely on.

## The signing key

The dedicated evidence key lives at `<data_directory>/keys/evidence-signing.key`
with `0600` permissions. It is created automatically the first time you finalise
or build an annual package. The private key stays in that file, outside the
database; the public keys are kept in the database so old signatures still verify
after a rotation.

Back up the key file with the same care as the data, since without it you cannot
produce new signatures under the same identity. Keep it out of any shared or
public location. If it is ever exposed, rotate it; the old public key stays on
record so previously signed reports continue to verify.

## Integrity checks

Check the audit-log hash chain whenever you want reassurance that nothing has
been altered or deleted:

```bash
./rmm-evidence audit-verify
```

An intact chain means every recorded event is still exactly as it was written.
A break points to the first altered or missing entry.

## Troubleshooting

`period-close` refuses with warnings. That is the point. Read the warnings from
`period-validate`, fix what you can, and re-run with `--acknowledge` to close
with the rest recorded in the report.

`print` or `verify-final` refuses. The package is not finalised, or a file has
changed since it was signed. Re-run `verify-final` to see which file failed; if
the change was legitimate, create a revision and finalise that.

PDF/A shows "not validated". No validator is installed. The PDF is still produced
and legible; install veraPDF if you need the conformance check on record.

A backup reports FAILED verification. A copied file did not match its source. Do
not trust that copy. Check the disk and the target path, then run the backup
again.

`ingest` records nothing. Check that the monitor is running and its snapshot
endpoint is reachable from the Pi at the `--from` URL.

`serve` will not start. Another process holds the address. Pick a different
`--addr` or free the port.
