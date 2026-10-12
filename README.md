<div align="center">

<img src=".github/assets/cashstratum-bch-transparent-v2.png" alt="CashStratum" width="640">

# Mining infrastructure for Bitcoin Cash

**A BCH-focused Stratum server and solo mining pool engine, built on CKPool.**

Direct miner payouts · Native CashAddr · Per-address round statistics · Operator tooling

[![Release](https://img.shields.io/github/v/release/cashstratum/cashstratum?color=009E61&label=release)](https://github.com/cashstratum/cashstratum/releases)
[![Blocks found](https://img.shields.io/badge/BCH_mainnet_blocks-78-009E61)](docs/proofs/)
[![BCH mined](https://img.shields.io/badge/BCH_mined-243.94-009E61)](docs/proofs/VALUATION.md)
[![Verified on chain](https://img.shields.io/badge/verified-on_chain-0B7A4B)](#verify-it-yourself)
[![License: GPL v3 or later](https://img.shields.io/badge/license-GPLv3%2B-009E61)](COPYING)
[![Bitcoin Cash](https://img.shields.io/badge/Bitcoin_Cash-BCH-009E61)](https://bitcoincash.org)

[Releases](https://github.com/cashstratum/cashstratum/releases) · [Install](docs/installation.md) · [Proofs](docs/proofs/) · [Contribute](CONTRIBUTING.md) · [Report an issue](https://github.com/cashstratum/cashstratum/issues) · [cashstratum.com](https://cashstratum.com)

</div>

---

<div align="center">

| ⛏️ Blocks found | 🪙 BCH mined | 💵 Historical value | 🚀 On the released build |
|:---:|:---:|:---:|:---:|
| **[78](docs/proofs/)** | **[243.94151949](docs/proofs/VALUATION.md)** | **[≈ $129,968.85](docs/proofs/VALUATION.md)** | **[9 blocks on v1.2.1](#production-proof)** |
| BCH mainnet, every hash on the main chain | gross coinbase rewards, incl. fees | USD at historical minute prices, before costs | 2026-10-06 → 2026-10-12 |

**Latest block: [972517](https://www.blockchain.com/explorer/blocks/bch/972517)** · 2026-10-12 06:26 UTC · [redacted solve log](docs/proofs/logs/972517-context.log)

</div>

---

## Contents

- [Release status](#release-status)
- [Production proof](#production-proof) · [Verify it yourself](#verify-it-yourself)
- [Built for BCH operators](#built-for-bch-operators) · [How direct payouts work](#how-direct-payouts-work)
- [Architecture](#c-engine-go-api-shell-tooling) · [Operator API](#operator-api)
- [Documentation](#documentation)
- [Join the project](#join-the-project) · [License and acknowledgments](#license-and-acknowledgments)

## Release status

**CashStratum 1.2.3** (2026-10-12) is the current release. It changes no code: it publishes the
updated production proofs, 78 blocks including the first ones found by a released CashStratum
build. **1.2.2** added BCHD and Flowee the Hub as supported nodes, and **1.2.0** (2026-09-13) was
the first public release. See [CHANGELOG.md](CHANGELOG.md) for what each release carries, and
what has and has not been deployed.

This tree contains the C mining engine, Go operator services, installers, unit tests and the
regtest money gate. Start with [Installing CashStratum](docs/installation.md) for Linux
prerequisites, building this checkout, a staged installation and configuring an existing node
([supported nodes](docs/installation.md#supported-nodes): BCHN, BCHD, Flowee the Hub). Use an
explicit release tag when installing from the public repository. The installers serve BCH over
Stratum V1; they do not provision a node or automatically enable SV2.

CashStratum descends from [CKPool](https://bitbucket.org/ckolivas/ckpool/) and the
BCH fork previously published as [skaisser/ckpool](https://github.com/skaisser/ckpool).

## Production proof

**78 BCH mainnet blocks from this engine's lineage, every one verified against public chain
data** — from the EloPool deployment in 2025 to the released CashStratum build today.

```text
 2025-07 ────────────── 2025-12        2026-09        2026-10-06 ──── 2026-10-12
 ◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆◆           ◆              ◆◆◆◆◆◆◆◆◆
 EloPool · ckpool fork · 68 blocks      BlockSniper    CashStratum v1.2.1 · 9 blocks
```

| Era | Software | Blocks | Dates (UTC) | Gross BCH | Solve excerpts |
|---|---|---:|---|---:|---:|
| **EloPool** | modified CKPool fork | 68 | 2025-07-15 → 2025-12-06 | 212.67746187 | 35 |
| **BlockSniper** | modified CKPool fork | 1 | 2026-09-01 | 3.12542747 | 1 |
| **CashStratum** | **release v1.2.1** | **9** | **2026-10-06 → 2026-10-12** | **28.13863015** | **9** |
| | | **78** | | **243.94151949** | **45** |

**The 9 blocks of October 2026 are the first found by a tagged CashStratum release**, running
on the reference deployment: heights
[971726](https://www.blockchain.com/explorer/blocks/bch/971726),
[971744](https://www.blockchain.com/explorer/blocks/bch/971744),
[971770](https://www.blockchain.com/explorer/blocks/bch/971770),
[971817](https://www.blockchain.com/explorer/blocks/bch/971817),
[971860](https://www.blockchain.com/explorer/blocks/bch/971860),
[972310](https://www.blockchain.com/explorer/blocks/bch/972310),
[972313](https://www.blockchain.com/explorer/blocks/bch/972313),
[972318](https://www.blockchain.com/explorer/blocks/bch/972318) and
[972517](https://www.blockchain.com/explorer/blocks/bch/972517). Each has a redacted solve log
showing the candidate, the submission, `BLOCK ACCEPTED!` and the ZMQ tip change, with SHA-256
checksums for the excerpt and its source.

Every record in the **[proof index](docs/proofs/)** carries the height, hash, UTC block date,
coinbase transaction and script, recognised coinbase tag and verification time, plus an
explorer link. The **[valuation](docs/proofs/VALUATION.md)** prices each block's gross coinbase
reward at the BCH/USD minute close before its header time, from two independent exchanges.

> **What this proves, and what it does not.** It proves the blocks exist on the BCH main chain
> and carry this deployment's coinbase tags, and that solve-time logs exist for 45 of them. A
> coinbase tag is self-declared: it does not cryptographically attest a software build. The
> `software` field on the CashStratum-era records comes from the operator's deployment record.
> None of this establishes hashrate, luck, or a performance advantage over other engines.

CashStratum is the software. BlockSniper is a reference deployment. Other operators can run
the engine under their own pool name and branding.

### Verify it yourself

Everything needed is in [`docs/proofs/`](docs/proofs/), and both scripts use only the Python 3
standard library:

```bash
git clone https://github.com/cashstratum/cashstratum && cd cashstratum/docs/proofs

python3 verify.py --offline   # record structure + SHA-256 of every solve excerpt
python3 verify.py             # re-fetch all 78 blocks and coinbases from a public BCH API
python3 verify-values.py      # re-do every reward sum and price-boundary check (offline)
```

`verify.py` requires each stored hash to be on the main chain at its height, the first
transaction to be a coinbase with the recorded script, and every excerpt to match its checksum.
`verify-values.py` requires each coinbase's outputs to equal subsidy plus fees and every price
candle to close **before** the block's header time. Prefer an explorer?
[blocks.csv](docs/proofs/blocks.csv) links every block.

## Built for BCH operators

| Capability | What it provides |
|---|---|
| **Native CashAddr** | BCH address parsing and payout-script construction, with network and checksum validation. |
| **Direct solo payouts** | In per-address solo mode, the winning miner's address receives its share of the reward in the block's coinbase transaction. |
| **Operator fee** | A configurable fee output alongside the miner's payout. |
| **Per-address rounds** | Separate round-share and luck statistics for each payout identity; one miner's solve does not reset everyone else's round. |
| **Rental compatibility** | Difficulty handling for NiceHash and MiningRigRentals, alongside conventional ASIC clients. |
| **Node choice and resilience** | BCHN, BCHD or Flowee the Hub; multi-node RPC/ZMQ integration and failover handling. |
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
PPS guarantee or a shared-reward accounting system. See the [configuration guide](README-CASHSTRATUM.md#-configuration) for address handling.

## C engine, Go API, shell tooling

The mining engine retains CKPool's C foundation and cooperating connector, generator and
Stratum-processing components. The Go API supplies operator-facing read access; it does not
replace the mining engine. Shell tools support installation and terminal monitoring.

Authenticated API routes expose operational data. The lightweight `/ping` route is deliberately
unauthenticated and separately rate-limited. The full endpoint reference, configuration table and
deployment procedure — including network boundaries and TLS — are documented in
**[docs/operator-api.md](docs/operator-api.md)** and summarized below.

The installer uses `cashstratum`, `cashstratum.conf`, `cashstratum.service` and
`cashstratum.log`. Source builds retain `src/ckpool`; pass `-n cashstratum` explicitly
when launching it. See the [installation guide](docs/installation.md) before migrating an existing pool.

## Operator API

A read-only HTTP API over the pool's own logs and status files, shipped as a single static Go
binary. It lets a dashboard, a monitoring system or a pool website read pool state without shell
access to the mining host.

It exposes no administrative mutation endpoints. The `/coinbase` endpoint opens a
short-lived Stratum probe connection, which can appear in the engine's worker statistics;
the API filters its probe worker from `/user-file`. The HTTP server is Go standard library only, and its one
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

Build the API from this checkout with `cd api && go build -mod=vendor -o cashstratum-api .`.
Use the [operator deployment guide](docs/operator-api.md#install) for service templates,
explicit log/configuration paths and secret-file permissions.

Two things reliably go wrong: **run it as the account that runs the pool** (the engine creates its
log directory mode `0750`, so any other account gets permission denied on every endpoint while
`/health` still answers), and **do not expose the port to the internet** — the bearer key is the
only authentication and it travels in cleartext over HTTP. If you want browsers to measure latency
to the pool host, put a TLS proxy in front of `/ping` alone.

Full reference — every parameter, response, cursor semantics, caveat and the retention timer:
**[docs/operator-api.md](docs/operator-api.md)**.

## Documentation

| I want to… | Read |
|---|---|
| Install and run a pool | [Installing CashStratum](docs/installation.md) — prerequisites, staged install, [supported nodes](docs/installation.md#supported-nodes) |
| Configure solo mining and addresses | [Configuration guide](README-CASHSTRATUM.md) · [Solo mining](README-SOLOMINING.md) · [Pool fee](POOL_FEE.md) |
| Understand the modes | [CashStratum modes](README-CS_MODES.md) · [Proxy mode](README-CSPROXY.md) |
| Harden a deployment | [Security hardening](docs/security-hardening.md) · [Security notes](README-SECURITY.md) · [Share-inflation analysis](SECURITY-share-inflation.md) |
| Read pool data from a dashboard | [Operator API](docs/operator-api.md) · [API guide](CASHSTRATUM_API_GUIDE.md) |
| Check the production evidence | [Proof index](docs/proofs/) · [Valuation](docs/proofs/VALUATION.md) · [Verify it yourself](#verify-it-yourself) |
| See what changed | [CHANGELOG](CHANGELOG.md) · [Releases](https://github.com/cashstratum/cashstratum/releases) |
| Report a vulnerability | [SECURITY.md](SECURITY.md) — privately, please |

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
