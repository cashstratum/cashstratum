<div align="center">

<img src=".github/assets/cashstratum-bch-transparent-v2.png" alt="CashStratum" width="640">

# Mining infrastructure for Bitcoin Cash

**A BCH-focused Stratum server and solo mining pool engine, built on CKPool.**

**[69 BCH mainnet blocks](docs/proofs/) · 215.80288934 BCH mined**

**[≈ $121,575.15 USD in historical gross block rewards](docs/proofs/VALUATION.md)**

Valued per block at historical BCH/USD minute prices; includes transaction fees, before costs.

Direct miner payouts · Native CashAddr · Per-address round statistics · Operator tooling

[![License: GPL v3 or later](https://img.shields.io/badge/license-GPLv3%2B-009E61)](COPYING)
[![Bitcoin Cash](https://img.shields.io/badge/Bitcoin_Cash-BCH-009E61)](https://bitcoincash.org)

[Releases](https://github.com/cashstratum/cashstratum/releases) · [Contribute](CONTRIBUTING.md) · [Report an issue](https://github.com/cashstratum/cashstratum/issues) · [cashstratum.com](https://cashstratum.com)

</div>

## Release status

**The first CashStratum source release is being prepared. This repository currently contains
project documentation, not an installable mining engine.**

CashStratum grows out of the BCH fork previously published as [skaisser/ckpool](https://github.com/skaisser/ckpool).
That remains the existing public source while the next release is integrated, tested and
rebranded. Features described below cover the development line intended for CashStratum;
consult the release notes of the version you actually deploy.

Watch **Releases** on this repository for the first tagged source release. It will include the
engine, build instructions, tests and operator documentation. No release date is promised.

## Built for BCH operators

| Capability | What it provides |
|---|---|
| **Native CashAddr** | BCH address parsing and payout-script construction, with network and checksum validation. |
| **Direct solo payouts** | In per-address solo mode, the winning miner's address receives its share of the reward in the block's coinbase transaction. |
| **Operator fee** | A configurable fee output alongside the miner's payout. |
| **Per-address rounds** | Separate round-share and luck statistics for each payout identity; one miner's solve does not reset everyone else's round. |
| **Rental compatibility** | Difficulty handling for NiceHash and MiningRigRentals, alongside conventional ASIC clients. |
| **Node resilience** | Multi-node RPC/ZMQ integration and failover handling. |
| **Go operator API** | Read-only access to pool statistics, miner records, shares, coinbase details and block-find logs. |
| **Browser latency probe** | A lightweight `/ping` endpoint for measuring HTTP round-trip time to the API host. |
| **Terminal monitoring** | A custom `monitor.sh` view highlighting pool events, shares, hashrate and block discoveries. |

The API's latency probe measures the HTTP path. It is not a measurement of Stratum job-delivery
latency, ASIC performance or block-propagation speed.

## How direct payouts work

A miner connects using a BCH payout address, optionally followed by a worker suffix. In
per-address solo mode, that identity determines the miner output in the coinbase. When the
miner finds an accepted block, the reward goes directly to that address, less the configured
operator fee. Coinbase rewards remain subject to network maturity rules.

This avoids an operator-managed withdrawal balance for that solo payout. It does not imply a
PPS guarantee or a shared-reward accounting system. Username fallback and address-rejection
rules will be documented with the released configuration.

## C engine, Go API, shell tooling

The mining engine retains CKPool's C foundation and cooperating connector, generator and
Stratum-processing components. The Go API supplies operator-facing read access; it does not
replace the mining engine. Shell tools support installation and terminal monitoring.

Authenticated API routes expose operational data. The lightweight `/ping` route is deliberately
unauthenticated and separately rate-limited. The full endpoint reference, configuration table and
deployment procedure — including network boundaries and TLS — are documented in
**[docs/operator-api.md](docs/operator-api.md)** and summarized below.

The first release will document CashStratum binary, configuration, service and log names,
including migration from existing CKPool-based installations. Do not rename files in a running
installation without updating their consumers.

## Operator API

A read-only HTTP API over the pool's own logs and status files, shipped as a single static Go
binary. It lets a dashboard, a monitoring system or a pool website read pool state without shell
access to the mining host.

It is **read-only by design**: it never writes to the pool, never touches the engine's control
sockets, and cannot change pool state. The HTTP server is Go standard library only, and its one
dependency (used by the optional block notifier) is vendored, so it builds with no network access.

| Endpoint | Purpose |
|---|---|
| `GET /health` | liveness, log presence and size |
| `GET /stats` | pool hashrate, workers, users, plus the node's own chain view |
| `GET /tail?lines=N` | tail the main log (capped at 1000 lines) |
| `GET /grep?pattern=` | literal search over the main log |
| `GET /find-block?height=N` | the log burst for one solved height — finder, worker, hashrate at solve time, round shares |
| `GET /user-file?user=` | one miner's full parsed status |
| `GET /user-log?user=&lines=N` | tail one miner's status file as raw lines |
| `GET /shares?since=&user=&limit=` | poll new sharelog records through a resumable cursor |
| `GET /coinbase?user=` | decode the coinbase the miner is currently working on |
| `GET /metrics` | service-level counters |
| `GET /ping` | **unauthenticated** round-trip-time probe for a browser |

Every route except `/ping` requires `Authorization: Bearer $CASHSTRATUM_API_KEY`. Requests are
rate limited per client IP in a fixed 60-second window — 20 requests by default, with independent
budgets of 60 for `/shares` (sized for a 2–3 s poll loop) and 60 for `/ping`. Every response
carries `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset`, so a client never
has to guess its budget.

```bash
AUTH="Authorization: Bearer $CASHSTRATUM_API_KEY"
curl -H "$AUTH" "http://127.0.0.1:8888/stats"
```

```json
{
  "hashrate": "50.8P",
  "workers": 12,
  "users": 4,
  "block_height": 966262,
  "node": { "chain": "main", "blocks": 966262, "headers": 966262, "ibd": false, "synced": true },
  "timestamp": 1788048342
}
```

Hashrates are SI-suffixed strings (`"50.8P"` is 50.8 PH/s, and an idle worker reports a bare
`"0"`), so parse them suffix-aware rather than as floats.

**Configuration is environment-only** — no config file. `CASHSTRATUM_API_KEY` is required and the
service refuses to start without it; `CASHSTRATUM_LOG_PATH`, `CASHSTRATUM_USER_LOGS_PATH` and
`CASHSTRATUM_API_PORT` (default `8888`) cover the rest of a normal deployment. Node RPC
credentials are **not** configured here: they are read at startup from the pool's own
configuration file, so they cannot drift from the node the pool is already using.

**Install**, once the release binary is in hand:

```bash
sudo install -d -m 0755 /opt/cashstratum-api
sudo install -m 0755 cashstratum-api-linux-amd64 /opt/cashstratum-api/cashstratum-api

sudo install -d -m 0755 /etc/cashstratum-api
printf 'CASHSTRATUM_API_KEY=%s\n' "$(openssl rand -hex 32)" \
    | sudo tee /etc/cashstratum-api/cashstratum-api.env >/dev/null
sudo chmod 0600 /etc/cashstratum-api/cashstratum-api.env

sudo cp cashstratum-api.service /etc/systemd/system/   # edit User= and ExecStart= first
sudo systemctl daemon-reload
sudo systemctl enable --now cashstratum-api
```

Two things reliably go wrong: **run it as the account that runs the pool** (the engine creates its
log directory mode `0750`, so any other account gets permission denied on every endpoint while
`/health` still answers), and **do not expose the port to the internet** — the bearer key is the
only authentication and it travels in cleartext over HTTP. If you want browsers to measure latency
to the pool host, put a TLS proxy in front of `/ping` alone.

Full reference — every parameter, response, cursor semantics, caveat and the retention timer:
**[docs/operator-api.md](docs/operator-api.md)**.

## Production lineage and evidence

**69 BCH mainnet blocks from this modified CKPool lineage, verified against public chain data.**
The BlockSniper / EloPool deployment is the production history behind CashStratum.

Browse the **[69-block proof index](docs/proofs/)** for heights, UTC block dates, explorer links,
coinbase tags and verification timestamps. All 69 hashes match the BCH main chain; sanitized
local solve excerpts are included for 35. The remaining 34 are explicitly marked without a
recovered local excerpt. JSON, CSV and a public re-verification script accompany the index.

**Last verified: 2026-09-05 (UTC).** This evidence establishes historical blocks and deployment
tags, not the exact software commit behind each block or a performance advantage over other
engines. It does not imply that the first CashStratum-branded release has already shipped.

CashStratum is the software. BlockSniper is a reference deployment. Other operators can run
the engine under their own pool name and branding.

## Join the project

We welcome reproducible bug reports, payout and address-validation regression cases, ASIC and
rental compatibility reports, documentation improvements, and performance measurements with
commands and hardware details.

- Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.
- Follow [SECURITY.md](SECURITY.md) for privately reporting security issues.
- To propose a pool listing, use the [Add your pool](https://github.com/cashstratum/cashstratum/issues/new?template=add-your-pool.yml) issue template. Listings require verification.

## License and acknowledgments

**GNU GPL version 3 or later** — see [COPYING](COPYING).

CashStratum is derived from **CKPool by Con Kolivas and its contributors**. Their work remains
an essential part of this engine. BCH development originated in the BlockSniper / EloPool fork
and continues here with community contributions. Original copyright and license notices are
retained; the CashStratum name identifies this project's maintenance and BCH direction.
