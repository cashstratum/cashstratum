# Production deployment — live dual-node topology

> Snapshot of the **live BlockSniper production deployment** as of 2026-08-31, so the
> multi-node configuration documented in [README.md](README.md#multi-node-configuration-highly-recommended-for-production)
> can be seen as actually run, not just as an example.
>
> **No credentials live in this repo.** All RPC users/passwords and the notifier shared
> secret are stored in the operator's 1Password (`ClaudeCode` vault) and in the operator's
> private infrastructure repo, and are carried in the operator's saved Claude instructions.
> Ask the operator; never commit them here.
>
> **Deeper runbooks** (bring-up, hardening, SSH mesh, sizing evidence, incident history)
> live in the operator's private repo `skaisser/private-homelab`, folder `blocksniper.ai/`
> — start at `blocksniper.ai/README.md` there.

## The three boxes (Vultr, Silicon Valley, shared VPC `blocksniper` 10.12.128.0/20)

| Box | Role | Public IP | VPC IP |
|---|---|---|---|
| `us` (solo) | **Primary** — BCHN node + ckpool (this repo, solo `-B` mode, `:3333`) + Go API (`:8888`) + blocksniper-notifier | `45.63.84.63` | `10.12.128.4` |
| `node2` | **Second node, ACTIVE-ACTIVE** — BCHN node only (no stratum by decision) | `149.28.194.23` | `10.12.128.5` |
| `stratum` | Future stratum front (proxy tier) — provisioned, software not yet installed | `45.32.136.93` | `10.12.128.3` |

Both nodes run **at the same time** — this is not primary/passive-backup. Each node has its
own RPC credentials and its own ZMQ publisher; ckpool subscribes to **both** ZMQ feeds and
acts on whichever `hashblock` notification arrives first, with sync-aware <100 ms RPC
failover between the nodes (see README §Multi-Node Configuration for the mechanism).

## ckpool.conf `btcd` — as deployed on `us`

```json
"btcd": [
    {
        "url": "127.0.0.1:8332",
        "auth": "<in 1Password>",
        "pass": "<in 1Password>",
        "notify": true,
        "zmqnotify": "tcp://127.0.0.1:28332"
    },
    {
        "url": "10.12.128.5:8332",
        "auth": "<in 1Password>",
        "pass": "<in 1Password>",
        "notify": true,
        "zmqnotify": "tcp://10.12.128.5:28332"
    }
]
```

- Entry 0 = the local node over loopback (always preferred while alive).
- Entry 1 = node2 over the **VPC** (free bandwidth, ~2–11 ms behind loopback in measured
  block races on 2026-08-31 — both endpoints delivered every block; ckpool deduped).
- node2's side: `rpcallowip` covers the VPC subnet, ZMQ bound on `0.0.0.0:28332` with ufw
  restricting it to the VPC. Nodes also `addnode` each other over the VPC.

## Laravel (blocksniper.ai) node wiring

The web app fails over across the same two nodes via `config/bchrpc.php`:

| Env (Forge, site blocksniper.ai) | Value |
|---|---|
| `BCH_RPC_PRIMARY_HOST` | `45.63.84.63` |
| `BCH_RPC_BACKUP_HOST` | `149.28.194.23` |
| `BCH_RPC_FAILOVER_ENABLED` | `true` |

Both nodes' ufw allow `8332` from the web server's IP only. Credentials: 1Password, as above.

## blocksniper-notifier

Runs on `us` (live, `NOTIFY_DRY_RUN=false`) posting signed webhooks to
`https://blocksniper.ai/api/pool/notify`; a dry-run twin runs on the operator's LAN test
box. Deployment runbook: [api/NOTIFIER.md](api/NOTIFIER.md). ⚠️ Its journal logs failures
only — a silent journal with the process running means deliveries are succeeding; verify on
the Laravel side (`Notifier heartbeat received` in `laravel.log`).
