<div align="center">

# CashStratum

### Open-source **Bitcoin Cash (BCH) stratum server** and **solo mining pool** software

**The CKPool fork built for Bitcoin Cash** — native CashAddr, per-address on-chain payouts,
a configurable operator fee, sub-100 ms multi-node failover, and out-of-the-box
NiceHash / MiningRigRentals compatibility.

[![Bitcoin Cash](https://img.shields.io/badge/BITCOIN%20CASH-BCH-0AC18E?style=for-the-badge&logo=bitcoincash&logoColor=white&labelColor=030711)](https://bitcoincash.org)
[![Stratum Server](https://img.shields.io/badge/STRATUM-V1%20SERVER-07D1FA?style=for-the-badge&labelColor=030711)](#architecture)
[![Mainnet Blocks](https://img.shields.io/badge/MAINNET%20BLOCKS-68%2B-0281F5?style=for-the-badge&labelColor=030711)](#proven-in-production)
[![Built in C](https://img.shields.io/badge/BUILT%20IN-C-5EE7FF?style=for-the-badge&logo=c&logoColor=white&labelColor=030711)](#architecture)
[![License GPLv3](https://img.shields.io/badge/LICENSE-GPL%20v3-9FEEFF?style=for-the-badge&labelColor=030711)](COPYING)

**[cashstratum.com](https://cashstratum.com)** · proofs, benchmarks and operator guides

</div>

---

> ### 📦 Status — source lands with `v1.0.0`
>
> CashStratum has been running on BCH mainnet since 2025 and has found **68+ blocks**. The code
> is being prepared for its first public release under this name: renaming, doc pass, reproducible
> build. **Watch the repo** to be notified when `v1.0.0` is pushed — it is a matter of days, not
> months. Every release here is a complete, buildable source tree; there is no hidden core.

---

## Why not stock CKPool?

If you are looking for **CKPool for Bitcoin Cash**, **BCH pool software**, or a **BCH stratum
server** you can put real money behind — this is it. Upstream CKPool is excellent *Bitcoin*
software: it does not understand CashAddr, it has no operator fee mechanism, its node failover
takes seconds, and rental services trip over its difficulty handling.

CashStratum is a **BCH-first fork** that fixes all four, with 68 blocks found on mainnet to
show for it.

| You want to… | Stock CKPool | **CashStratum** |
|---|---|---|
| Let miners use a `bitcoincash:q…` address as their username | ❌ Rejected | ✅ Native CashAddr, all prefixes |
| Pay every miner **on-chain, directly, in the block they found** | ❌ | ✅ Per-address dual-output coinbase |
| Take a pool fee without patching the source | ❌ Donation code only | ✅ Configurable, 0–50% |
| Survive a node restart without dropping miners | ⚠️ 4+ seconds | ✅ **<100 ms**, sync-aware |
| Accept NiceHash / MiningRigRentals hashrate | ⚠️ Manual, fragile | ✅ Auto-detected by useragent |
| Brand your own coinbase tag | ❌ Hardcoded `ckpool` | ✅ Configurable, up to 38 bytes |

## Mine directly to your own BCH address

In solo mode, a miner's **username is their payout address**. When that miner finds a block, the
coinbase pays them on-chain in the same block — no accounts, no withdrawals, no custody.

```bash
# CashAddr, with or without the prefix
cgminer -o stratum+tcp://your-pool:3333 -u bitcoincash:qr95sy3j9xwd2ap32xkykttr4cvcu7as4y0qverfuy -p x
cgminer -o stratum+tcp://your-pool:3333 -u qr95sy3j9xwd2ap32xkykttr4cvcu7as4y0qverfuy -p x

# Legacy Base58 still works
cgminer -o stratum+tcp://your-pool:3333 -u 1A1z7agoat8Bt8ZVUUxkKvWAWgHtdNi3nn -p x

# Multiple rigs on one address
cgminer -o stratum+tcp://your-pool:3333 -u bitcoincash:qr95…verfuy.rig01 -p x
```

A mistyped address is **rejected at authorize time** with an explicit error — you cannot
accidentally donate a block to an address that does not exist.

## Key capabilities

- **Native CashAddr** — pure C, zero external dependencies; `bitcoincash:`, `bchtest:`, `bchreg:`,
  plus legacy Base58. Network is auto-detected from the node.
- **Per-address on-chain payouts** — dual-output coinbase splits the reward between the finder and
  the operator; dust-safe (a fee below 546 sats is dropped, never lost).
- **Configurable operator fee** — 0–50%, no donation code, no source patching.
- **Sub-100 ms multi-node failover** — sync-aware, so the pool stays on a healthy backup instead of
  flapping back to a node that is still catching up.
- **Three ways to set difficulty** — password (`-p d=500000`), useragent auto-detection
  (NiceHash, MiningRigRentals), and worker-name patterns.
- **Rental-service ready** — NiceHash and MiningRigRentals work without special configuration.
- **Configurable coinbase tag** — your pool's signature in every block you find.
- **CKPool's foundation** — multi-process, multi-threaded, ultra-low overhead, seamless restarts
  with socket handover, ASICBoost support, advanced vardiff.

## Proven in production

- **68+ blocks found on Bitcoin Cash mainnet**, continuously since 2025.
- The most recent block was found with **under 400 TH/s** of pool hashrate — fully logged.
- Battle-tested against real hardware, from Bitaxe-class devices to rented industrial hashrate.
- Millions of shares validated with native CashAddr usernames and automatic fee splitting.

Raw logs, block explorer links, hashrate charts and reproducible benchmark runs are published at
**[cashstratum.com](https://cashstratum.com)** and mirrored in [`docs/proofs/`](docs/proofs/).
Every claim on this page has a receipt there.

## Pools running CashStratum

| Pool | Endpoint | Mode | Fee | Notes |
|---|---|---|---|---|
| [BlockSniper](https://blocksniper.ai) | `stratum+tcp://solo.blocksniper.ai:3333` | solo | 2% | Reference deployment · formerly **EloPool.cloud** — historical `elopool.cloud` coinbase tags refer to this same pool |

Machine-readable source: [`pools.yml`](pools.yml).

**Running CashStratum?** Open an [Add your pool](https://github.com/cashstratum/cashstratum/issues/new?template=add-your-pool.yml)
issue and we will verify and list you. Listings are verified, not self-declared — see
[CONTRIBUTING.md](CONTRIBUTING.md#getting-your-pool-listed).

## Architecture

CashStratum keeps CKPool's process model: a small set of cooperating processes (stratum,
connector, generator) over shared memory and unix sockets, written in C for predictable latency
under load. A single instance handles tens of thousands of connections on modest hardware, and
restarts hand sockets over so miners never see a disconnect.

Deployment modes: **pool**, **solo**, **proxy**, **passthrough**, **node**.

Requirements: a synced Bitcoin Cash full node (BCHN or compatible) with RPC and ZMQ enabled,
Linux, and a C toolchain.

## Contributing

Issues and pull requests are welcome — bug reports with a reproducible case, node/hardware
compatibility reports, documentation fixes and performance work especially. Start with
[CONTRIBUTING.md](CONTRIBUTING.md).

Security issues go to [SECURITY.md](SECURITY.md), never a public issue.

## License and credits

GPLv3 — see [COPYING](COPYING).

CashStratum is a fork of **CKPool** by Con Kolivas, which remains the foundation of everything
here. If this software is useful to you, the upstream project deserves the credit for the parts
it got right first.

---

<div align="center">

**Keywords:** bitcoin cash mining pool software · BCH stratum server · ckpool fork · solo mining
BCH · CashAddr pool · bitcoin cash solo pool · ASIC mining pool · Bitaxe BCH · BCH pool operator

</div>
