# Operator API

A read-only HTTP API over the pool's own logs and status files, shipped as a single static Go
binary. It lets an application — a dashboard, a monitoring system, a pool website — read pool
state without shell access to the mining host.

It is **read-only by design**. It never writes to the pool, never touches the pool's control
sockets, and cannot change pool state. Control operations (log level, disconnecting a worker,
shutting the pool down) live on the engine's own Unix-socket interface, which this service does
not speak.

The API parses the mining engine's output, so it lives in the same repository as the engine: when
a log line format or the coinbase layout changes, the parser changes in the same commit.

> **Names.** The binary, service unit, configuration paths and environment variables below are the
> CashStratum names that ship with the first release. An installation carried over from the
> earlier CKPool-based build keeps its previous names until it is migrated; the release notes
> carry the mapping. Endpoint paths, parameters and response shapes are unaffected either way.

---

## Authentication

Every endpoint requires a bearer token, **except `/ping`**:

```
Authorization: Bearer $CASHSTRATUM_API_KEY
```

A missing or wrong key returns **`401`** with the plain-text body `Unauthorized`. Error bodies
are plain text, not JSON — do not blindly decode a non-2xx response as JSON.

Generate a key with `openssl rand -hex 32`. The server refuses to start on an empty key **or** on
the placeholder constant compiled into it, so an operator who forgets the variable gets a failed
start rather than a service authenticated by a published value.

The key is the only authentication and it travels in cleartext over HTTP. **Do not expose the
port to the internet.** Scope it in the firewall to the consuming application's address, exactly
as the stratum port already is.

## Rate limiting

Every endpoint is throttled **per client IP**, counted in a **fixed 60-second window** that opens
on that IP's first request and resets exactly 60 s later — not a rolling average.

| Bucket | Endpoints | Default | Environment variable |
|---|---|---|---|
| Default | everything except `/shares` and `/ping` | 20 req / 60 s | `CASHSTRATUM_RATE_LIMIT` |
| Shares | `/shares` only | 60 req / 60 s | `CASHSTRATUM_SHARES_RATE_LIMIT` |
| Ping | `/ping` only, unauthenticated | 60 req / 60 s | `CASHSTRATUM_PING_RATE_LIMIT` |

The three buckets are independent: spending the `/shares` budget does not affect `/find-block` or
`/ping`. On authenticated routes the budget is charged **after** authentication, so a bad key
never consumes it — but **before** the response cache, so a cache hit still costs one request.

Every response advertises the budget:

| Header | On | Meaning |
|---|---|---|
| `X-RateLimit-Limit` | all responses | the bucket's budget for this route |
| `X-RateLimit-Remaining` | all responses | requests left in the window; `0` on a 429 |
| `X-RateLimit-Reset` | all responses | Unix seconds when the window resets |
| `Retry-After` | `429` only | whole seconds to wait; always ≥ 1, never `0` |

All four are listed in `Access-Control-Expose-Headers`, so a browser client can read them
cross-origin. A `429` carries the plain-text body `Too Many Requests`.

> **A `429` is not an outage.** A consumer that treats every non-2xx as a flat failure cannot
> tell "come back in 12 seconds" from "the pool is down", and will retry blind into a closed
> window. Branch on the status: sleep `Retry-After` on a 429, and reserve failure handling for
> everything else.

Most endpoints serve a cached body for up to 60 s. If a consumer needs to sweep many historical
heights, raise `CASHSTRATUM_RATE_LIMIT` for that host — or better, have the consumer cache **negative**
results too, since `/find-block` returns `found: false` permanently for any height that has aged
out of the log.

## Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /health` | liveness, log presence and size |
| `GET /stats` | parsed pool stats — hashrate, workers, users, plus the node's own chain view |
| `GET /tail?lines=N` | tail the main log (capped at 1000 lines) |
| `GET /grep?pattern=` | literal search over the main log |
| `GET /find-block?height=N` | the log burst for one solved height — finder, worker, hashrate at solve time, round shares |
| `GET /user-file?user=` | one miner's full parsed status |
| `GET /user-log?user=&lines=N` | tail one miner's status file as raw lines |
| `GET /shares?since=&user=&limit=` | poll new sharelog records with a resumable cursor |
| `GET /coinbase?user=` | decode the coinbase the miner is currently working on |
| `GET /metrics` | service-level counters |
| `GET /ping` | **unauthenticated** round-trip-time probe for a browser |

All examples below assume:

```bash
AUTH="Authorization: Bearer $CASHSTRATUM_API_KEY"
POOL="http://127.0.0.1:8888"
```

---

### `GET /health`

```bash
curl -H "$AUTH" "$POOL/health"
```

```json
{
  "status": "ok",
  "timestamp": 1788046652,
  "log_exists": true,
  "log_size": 2575937,
  "uptime": 2842,
  "version": "3.0.0-go"
}
```

`uptime` is the **API service's** uptime in seconds, not the pool's. `log_exists: false` with
`status: "ok"` means the API is up but cannot see the pool's log — treat that as degraded, not
healthy.

---

### `GET /stats`

```bash
curl -H "$AUTH" "$POOL/stats"
```

```json
{
  "hashrate": "0",
  "workers": 0,
  "users": 0,
  "transactions": 261,
  "block_height": 966262,
  "node": {
    "chain": "main",
    "blocks": 966262,
    "headers": 966262,
    "progress_pct": 99.9999,
    "difficulty": 449476611397.2,
    "ibd": false,
    "synced": true
  },
  "timestamp": 1788048342
}
```

- `hashrate` is the engine's SI-suffixed string (`"50.8P"`, `"2.31G"`, `"0"`). Parse it
  suffix-aware, never as a float — see [Hashrate strings](#hashrate-strings).
- `block_height` and `node` come from the BCH node's own `getblockchaininfo`, using the RPC
  credentials already present in the pool's configuration file.
- **Gate on `node.synced`**, not on `blocks`: it is `blocks >= headers && !ibd`, so a node one
  block behind its own headers reads as not synced however healthy `blocks` looks.
- `node.difficulty` is the current network difficulty and should match `networkdiff` in each
  miner's user file. A persistent divergence means the pool is working on a stale block template
  and needs a restart.
- `node` is **omitted** when the node cannot be reached, and `block_height` then falls back to a
  log scrape of the last 200 lines — often `null` on a freshly restarted pool. Treat a missing
  `node` as "height may be stale".
- Cached ~30 s, so the node is polled at most twice a minute; each call is bounded at 3 s.

---

### `GET /tail`

`?lines=N` — capped at 1000.

```bash
curl -H "$AUTH" "$POOL/tail?lines=3"
```

```json
{
  "lines": [
    "[2026-08-29 23:37:08.468] Pool:{\"hashrate1m\": \"0\", \"hashrate1hr\": \"1.99G\", \"hashrate1d\": \"2.29P\", \"hashrate7d\": \"868T\"}",
    "[2026-08-29 23:37:08.468] Pool:{\"diff\": 32.3, \"luck\": 32.3, \"accepted\": 145146117994, \"rejected\": 34929688, \"bestshare\": 39410469345}",
    "[2026-08-29 23:37:23.454] Stored local workbase with 85 transactions"
  ],
  "timestamp": 1788046652
}
```

---

### `GET /grep`

`?pattern=` — literal (regex-escaped) match over the whole log, last 500 hits, cached 60 s.

```bash
curl -H "$AUTH" "$POOL/grep?pattern=Network%20diff"
```

```json
{
  "lines": [
    "[2026-08-29 23:26:16.467] Network diff set to 449476611397.2",
    "[2026-08-29 23:28:08.445] Network diff set to 449476611397.2"
  ],
  "pattern": "Network diff",
  "timestamp": 1788046652
}
```

---

### `GET /find-block`

`?height=N` (required, numeric). Returns the log burst the engine emits when it confirms a solve,
so a consumer can recover **who found the block and at what hashrate** — the "mining power when
found" figure on a block card.

```bash
curl -H "$AUTH" "$POOL/find-block?height=966569"
```

```json
{
  "found": true,
  "height": "966569",
  "timestamp": 1788251130,
  "lines": [
    "[…] BLOCK ACCEPTED!",
    "[…] ZMQ block hash 00000000000000000021bb…4f3f from endpoint 0",
    "[…] Solved and confirmed block 966569 by bitcoincash:qzxq…4u.nh",
    "[…] User bitcoincash:qzxq…4u:{\"hashrate1m\": \"443T\", \"hashrate5m\": \"992T\", \"hashrate1hr\": \"8.47P\", \"shares\": 565974752369, \"authorised\": 1787906856}",
    "[…] Worker bitcoincash:qzxq…4u.nh:{\"hashrate1m\": \"443T\", …}",
    "[…] Block solved after 565977752369 shares at 128.0% diff"
  ]
}
```

`lines` is **raw log text**, deliberately — the endpoint is a context grep, not a block parser, so
a new log line shows up here without a server change. The shape is stable because it comes from
one function in the engine, which emits these lines back to back with nothing interleaved:

| Pattern | Yields |
|---|---|
| `ZMQ block hash ([0-9a-f]{64})` | block hash |
| `Solved and confirmed block (\d+) by (\S+)` | height, workername |
| `^\[.*?\] User (\S+):(\{.*\})$` | account-wide hashrate snapshot (JSON) |
| `^\[.*?\] Worker (\S+):(\{.*\})$` | winning rig's hashrate snapshot (JSON) |
| `Block solved after ([\d.]+) shares at ([\d.]+)% diff` | round shares, luck % |
| `Possible block solve diff ([\d.]+)` | winning share difficulty |

The two JSON objects carry `hashrate1m`, `hashrate5m`, `hashrate1hr`, `hashrate1d`, `hashrate7d`;
`User` adds `shares` (lifetime accepted share difficulty) and `authorised` (Unix seconds). These
are the decaying averages **at confirmation time**, which is why they cannot be reconstructed
later from `/user-file` — that endpoint only ever reports *now*.

> **A username contains a colon.** `bitcoincash:qzxq…4u` is one CashAddr identity, and the stats
> line then appends a *second* colon before the JSON: `User bitcoincash:qzxq…4u:{…}`. A character
> class like `[\w.]+` stops at the first colon, so the pattern never matches and the hashrates
> come back empty — or worse, the block is attributed to a miner named `bitcoincash`. Use `(\S+)`
> as above (greedy, so it backtracks to the last colon), or widen the class to include `:`.

**Caveats.**

- It greps the live log only. The engine ships no log rotation, so a height stays resolvable
  exactly as long as that file retains the lines. Treat the call as a one-shot enrichment fired
  soon after the solve, and persist the parsed row yourself.
- `found: false` therefore means *"not in the current log"*, never *"no such block"*. When
  `found` is false, `lines` is **`null`, not `[]`** — guard the iteration.
- Found responses are cached in-process for the life of the service.
- Three search patterns run and their context windows are concatenated, so `lines` may repeat a
  line. Deduplicate, or key each field off its first match.

---

### `GET /user-file`

`?user=` — the full parsed status of one miner. **This is the endpoint for per-user dashboards.**

```bash
curl -H "$AUTH" "$POOL/user-file?user=bitcoincash:qzxq…4u"
```

```json
{
  "username": "bitcoincash:qzxq…4u",
  "content": {
    "authorised": 1787906856,
    "bestever": 39410469345,
    "bestshare": 39410469345.60201,
    "hashrate1m": "0",
    "hashrate5m": "0",
    "hashrate1hr": "23.1G",
    "hashrate1d": "2.51P",
    "hashrate7d": "881T",
    "lastshare": 1787993774,
    "luck": 32.29,
    "roundshares": 145146117994,
    "shares": 145146117994,
    "worker": [
      {
        "worker": "nh",
        "workername": "bitcoincash:qzxq…4u.nh",
        "hashrate1m": "0",
        "hashrate1hr": "23.1G",
        "hashrate1d": "2.51P",
        "shares": 145146117994,
        "bestshare": 39410469345.60201,
        "lastshare": 1787993774
      }
    ],
    "workers": 0
  }
}
```

- **`worker` is the field to render** — see [Worker names](#worker-names).
- `luck` / `roundshares` / `networkdiff` are that user's **independent** round progress:
  accumulated share difficulty since the last block solve **by that miner**, as a percentage of
  network difficulty. This is separate from the pool-wide `luck` in `/stats` — one miner's solve
  does not reset another's round. All three may be **entirely absent** for a miner who has not
  authorised since the last engine restart, so null-check for field presence rather than
  comparing `lastshare` freshness. `lastshare` is the "as of" timestamp for all three.
- `user` accepts both CashAddr spellings and resolves either to the file that exists; the
  response's `username` is the canonical on-disk identity.
- A miss returns `{"error": "User not found", "username": "…"}` with **HTTP 200**.

**Probe worker filtering.** `/coinbase` creates a temporary phantom worker (`<user>.cashstratum-api`)
to fetch the current template. `/user-file` filters those out of the `worker` array and subtracts
exactly the number filtered from the file's own `workers` count — never recomputing it from the
array length, since that counter decays independently and can read 0 while workers are listed.
Every other field passes through untouched.

---

### `GET /user-log`

`?user=&lines=N` — tails the same per-user file as raw lines.

Because that file is indented JSON, a small `lines` value returns fragments (closing brackets),
which is useless:

```json
{ "lines": ["]", "}"], "exists": true, "username": "bitcoincash:qzxq…4u", "timestamp": 1788046652 }
```

**Prefer `/user-file`**, which returns the same data parsed. Keep `/user-log` for raw-dump
debugging with a large `lines` value.

---

### `GET /shares`

Polls the per-block sharelog as a **resumable stream** rather than a fixed snapshot.

**Parameters** (all optional):

| Param | Meaning |
|---|---|
| `since` | Opaque resume cursor, copied verbatim from a previous response's `next_cursor`. Omit for a bootstrap read of the most recent records. |
| `user` | Username filter — resolves both CashAddr spellings. Legacy base58 addresses are never folded into this set, since the engine keeps them as distinct accounting identities. |
| `limit` | Max records. Default and hard cap **1000**; larger is silently clamped, less than 1 is silently raised to 1. |

```bash
curl -H "$AUTH" "$POOL/shares?limit=1"
```

```json
{
  "shares": [
    {
      "workinfoid": 7679001410389671985,
      "clientid": 10,
      "enonce1": "234b916a",
      "nonce2": "df71f0e107350100",
      "nonce": "3e8b9c52",
      "ntime": "6a914e37",
      "diff": 500000,
      "sdiff": 961690.4039235928,
      "hash": "0000000000001171fdfb2e4feac1e55e9a2a5b7f44677578a666c9e377da5188",
      "result": true,
      "reject-reason": null,
      "errn": 0,
      "createdate": "1787907641,54226544",
      "createby": "code",
      "createcode": "parse_submit",
      "createinet": "0.0.0.0:3333",
      "workername": "bitcoincash:qzxq…4u.nh",
      "worker": "nh",
      "username": "bitcoincash:qzxq…4u",
      "address": "203.0.113.10",
      "agent": "NiceHash/1.0.0"
    }
  ],
  "height": 966263,
  "next_cursor": "MDAwZWJkOTR8NmE5MTRiMTkwMDAwMDAzMS5zaGFyZWxvZ3wxMjI5",
  "timestamp": 1788047121
}
```

Field names are kept **verbatim** from the engine's own sharelog output rather than
Go/JSON-cased, so a record matches the on-disk `.sharelog` line exactly — with the single
exception of `worker`, which the engine does not write and this API derives. Every field the
engine writes is passed through, including `reject-reason` on rejected shares.

- `diff` is the credited difficulty (what payout math uses); `sdiff` is the share's actual hash
  quality (display and best-share only).
- `createdate` is `"<unix-seconds>,<nanoseconds>"`.
- `result: false` records carry a `reject-reason` string — count them separately, never toward
  work totals.
- `address` is the miner's IP and `agent` its useragent. Both are useful for per-connection
  displays and both are personal data — **do not render raw IPs on a public page.**
- `height` is the sharelog's own current height; it should agree with `/stats.block_height`. A
  persistent gap means the pool has not seen new work.

**Cursor semantics.** Poll again with the exact `next_cursor` from the last response to receive
each share exactly once, in order — never a repeat, never a gap. The cursor is anchored to a
`(height dir, file, byte offset)` triple rather than a bare counter, so it survives a block
rollover: shares before and after the pool moves to a new height directory come back seamlessly
across the same poll loop.

An **empty `shares` array with `next_cursor` unchanged** just means nothing new has landed yet.
That is not an error — keep polling with the same cursor.

The cursor decodes as base64 over `<height dir>|<sharelog file>|<byte offset>`:

```text
MDAwZWJlNzZ8fDA=                                      ->  000ebe76||0
MDAwZWJkOTR8NmE5MTRiMTkwMDAwMDAzMS5zaGFyZWxvZ3wxMjI5  ->  000ebd94|6a914b1900000031.sharelog|1229
```

An **empty file component is normal and valid** (`<dir>||0`): the engine creates the height
directory for a new workbase before writing any share into it, so on an idle pool there is
nothing to anchor on yet. Hand it straight back like any other cursor.

Treat the cursor as opaque and pass it back verbatim. The base64 keeps the internal shape out of
callers' hands; it is **not** a security boundary. A corrupt or hand-edited cursor is not an
error — it silently falls back to a bootstrap read. It also survives sharelog pruning: the read
resumes at the oldest surviving height directory **at or above** that height, never below.

> **Not for direct browser use.** Like every authenticated endpoint here, `/shares` is protected
> by the bearer key alone. Proxy it from your application rather than letting a browser call it,
> so the key never ships to a client that could leak it.

---

### `GET /coinbase`

`?user=` — decodes the coinbase transaction of the **current candidate block template**. This is
not proof of a found block.

Each uncached call authorises a temporary probe worker against the pool and waits for the pool to
broadcast a mining job. The response is cached for 10 seconds per username. On an idle pool this
can take up to the engine's `update_interval` (default 30 s) plus slack, and returns **`504`**
with a JSON body if the job never arrives.

```bash
curl -H "$AUTH" "$POOL/coinbase?user=anonymous"
```

```json
{
  "username": "anonymous",
  "coinbase_hex": "0100000001…00000000",
  "coinbase_message": "",
  "outputs": [
    { "value": 309779438, "value_bch": "3.09779438", "address": "1AGQ…7cS", "type": "miner" },
    { "value": 3129085,   "value_bch": "0.03129085", "address": "1AGQ…7cS", "type": "pool_fee" }
  ],
  "total_value": 312908523,
  "total_value_bch": "3.12908523",
  "block_height": 966267,
  "network_bits": "1802762c",
  "timestamp": 1788053906
}
```

Addresses come back in **legacy Base58** — convert them for CashAddr display.

`CASHSTRATUM_CASHADDR_PREFIX` sets the CashAddr network prefix (no trailing `:`) that the probe
prepends to a bare CashAddr username, so it authorises under the spelling the pool already knows
the address by instead of minting a second, bare-spelling shadow user.

---

### `GET /metrics`

Service-level counters for monitoring dashboards — not pool statistics.

```json
{
  "uptime": 2855,
  "requests_total": 167,
  "errors_total": 125,
  "rate_limit_entries": 2,
  "cache_entries": 28,
  "timestamp": 1788046665
}
```

`errors_total` counts 401s and 429s too, so a rising value under normal operation usually means a
misconfigured consumer key rather than pool trouble.

---

### `GET /ping`

The one deliberate exception to everything above: **unauthenticated**, so a browser can call it
directly with no key of its own. It exists so a visitor's browser can measure its own round-trip
time to the pool host.

```
GET /ping  →  204 No Content
```

- **Body:** always empty. **Headers:** `Cache-Control: no-store` (a cached response measures
  nothing) and `Access-Control-Allow-Origin: *` (no data, no credentials, so an open CORS policy
  is safe here).
- **Methods:** `GET` and `HEAD` only — anything else gets `405` with `Allow: GET, HEAD`.
- **Query string is ignored entirely** — a caller appends `?t=<ts>` only to defeat HTTP caching,
  and the value is never read.
- **No redirects.** It answers on the exact requested path; a 301/302 would double every
  measurement.
- **Does zero work per request:** no body read, no disk I/O, no log line before responding. Any
  of those would be added to the number this endpoint exists to measure.
- **Its own rate-limit bucket**, so a burst of browser samples can never eat the budget the
  authenticated routes depend on.
- **Client IP for the limiter** is `RemoteAddr`, except when `RemoteAddr` is loopback (a request
  arriving through a local reverse proxy), in which case the first entry of `X-Forwarded-For` is
  used instead. `X-Forwarded-For` is trusted **only** in that loopback case, never from a direct
  caller, so a client cannot spoof its own bucket.

**What this is not:** it is not a liveness probe for the stratum port — it proves the host and
network path are reachable and how far away they are, nothing about the mining daemon. It must
never carry data, and it is not a general CORS opening of the other routes.

---

## Shared conventions

### Worker names

The engine stores a worker's name as the whole stratum auth string — `bitcoincash:qzxq….nh` —
because authorisation splits it on the first `.` or `_` to derive the username (which is why a
username can never contain either character). Repeating a 54-character address on every worker
row is useless to a dashboard, so `/user-file` and `/shares` both add a **`worker`** field
carrying just the segment after that separator:

| `workername` | `worker` |
|---|---|
| `bitcoincash:qzxq….nh` | `nh` |
| `bitcoincash:qzxq…_nh` | `nh` |
| `bitcoincash:qzxq….rig.1` | `rig.1` |
| `bitcoincash:qzxq…` (no suffix) | `""` |

This is **additive** — `workername` is still the verbatim string the engine wrote, so existing
consumers are unaffected. `worker` is always derived at read time. A miner who authorised with a
bare address gets `""` rather than the address echoed back, so a single unnamed rig renders
without re-matching the identity.

### CashAddr resolution

`/user-log`, `/user-file` and `/shares` resolve both CashAddr spellings of a username. The pool
names each stats file after the exact string the miner authorised with, so a query for `qzxq…`
also finds `bitcoincash:qzxq…` (and `bchtest:` / `bchreg:`), and vice versa. An exact match always
wins, and the response's `username` field carries the name the stats actually live under.

Legacy base58 addresses are kept separate — only CashAddr spellings are treated as equivalent,
matching the engine's own accounting.

### Hashrate strings

Hashrates are formatted by the engine as a value plus a decimal (1000-based) SI suffix — `K M G T
P E` — to three significant digits. Parse the suffix: `"443T"` is 443 TH/s, `"8.47P"` is
8.47 PH/s.

**Below 1000 H/s there is no suffix at all** — an idle worker logs a bare `"0"`, so a parser that
assumes a trailing letter will mis-read it.

---

## Configuration

Environment only. There is no configuration file.

| Variable | Default | Notes |
|---|---|---|
| `CASHSTRATUM_API_KEY` | — | **required**; the server aborts without it |
| `CASHSTRATUM_LOG_PATH` | `~/cashstratum/logs/cashstratum.log` | must match the pool's `logdir` |
| `CASHSTRATUM_USER_LOGS_PATH` | `~/cashstratum/logs/users` | ditto |
| `CASHSTRATUM_API_PORT` | `8888` | |
| `CASHSTRATUM_CASHADDR_PREFIX` | `bitcoincash` | CashAddr network prefix (no trailing `:`) used by `/coinbase`'s probe; a regtest/testnet rig sets `bchreg` / `bchtest` |
| `CASHSTRATUM_CONF_PATH` | `<logdir>/../cashstratum.conf`, then `<logdir>/cashstratum.conf` | the pool's own configuration, read once at startup for the node RPC endpoint |
| `CASHSTRATUM_RATE_LIMIT` | `20` | per-IP budget for all endpoints except `/shares` and `/ping`, per 60 s window |
| `CASHSTRATUM_SHARES_RATE_LIMIT` | `60` | per-IP budget for `/shares` only |
| `CASHSTRATUM_PING_RATE_LIMIT` | `60` | per-IP budget for `/ping` only |
| `DAYS_TO_KEEP` | `60` | sharelog retention in days, used by the pruning timer |

### Node RPC credentials come from the pool's own configuration

`/stats` reports `block_height` and the `node` object from the BCH node itself, via
`getblockchaininfo`. Those RPC credentials are **not** configured here — they are read once at
startup from the pool's own configuration file, from the first `btcd` entry that has a `url`:

```json
{ "btcd": [{ "url": "127.0.0.1:8332", "auth": "…", "pass": "…", "notify": true }] }
```

The engine cannot build work without those credentials, so on any host running a pool they are
already present, already correct, and rotate with the node — whereas a second copy in this
service's environment is one more thing to drift.

That file is typically mode `0640` owned by the pool account, so **run the API as that account**
or grant its group read access.

Every failure on this path is **non-fatal**: an unreadable, absent or node-less configuration, or
a node that is down, only makes `/stats` less informative. `node` is omitted and `block_height`
falls back to the log scrape. The RPC call is bounded at 3 s and happens at most once per 30 s.

---

## Build

The HTTP server itself is standard-library-only. The block notifier adds one direct dependency
(a ZeroMQ client) plus three indirects, and **all of it is vendored**, so the build needs no
network and no module proxy.

```bash
cd api
go build -mod=vendor -ldflags="-s -w" -o cashstratum-api .
```

Binaries are not committed; build them on demand.

### Cross-compiling is the practical path

The module requires a current Go toolchain, newer than the one most stable Linux distributions
ship. Since Go 1.21 the default `GOTOOLCHAIN=auto` would fetch a newer toolchain automatically —
but that needs proxy access a hardened pool host usually does not have, so it fails rather than
rescuing the build. Either upgrade the toolchain on the host, or cross-compile:

```bash
cd api
gofmt -l . && go vet ./... && go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath \
    -ldflags="-s -w" -o cashstratum-api-linux-amd64 .
```

`CGO_ENABLED=0` is safe: the cgo-backed ZeroMQ binding sits behind a build tag and is never
compiled, so the result is a **statically linked** ELF with no libc or libzmq dependency on the
pool host. Swap `GOARCH=arm64` for an ARM host.

> **Checksum both ends.** A truncated transfer over a slow link is a live failure mode, not a
> theoretical one. Compare `sha256sum` locally and on the host before installing anything.

```bash
sha256sum cashstratum-api-linux-amd64
scp cashstratum-api-linux-amd64 pool-host:/tmp/cashstratum-api
ssh pool-host 'sha256sum /tmp/cashstratum-api'   # must match before proceeding
```

---

## Install

```bash
# 1. Binary
sudo install -d -m 0755 /opt/cashstratum-api
sudo install -m 0755 cashstratum-api-linux-amd64 /opt/cashstratum-api/cashstratum-api

# 2. Key, root-only
sudo install -d -m 0755 /etc/cashstratum-api
printf 'CASHSTRATUM_API_KEY=%s\n' "$(openssl rand -hex 32)" \
    | sudo tee /etc/cashstratum-api/cashstratum-api.env >/dev/null
sudo chmod 0600 /etc/cashstratum-api/cashstratum-api.env

# 3. Point it at the pool's logs
sudo tee -a /etc/cashstratum-api/cashstratum-api.env >/dev/null <<'EOF'
CASHSTRATUM_LOG_PATH=/home/cashstratum/cashstratum/logs/cashstratum.log
CASHSTRATUM_USER_LOGS_PATH=/home/cashstratum/cashstratum/logs/users
CASHSTRATUM_API_PORT=8888
EOF

# 4. Unit — edit User=, WorkingDirectory= and ExecStart= first
sudo cp cashstratum-api.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cashstratum-api
```

Keep the previous binary as a dated backup so a rollback is a `mv` and a restart, never a rebuild.
Restarting the API does **not** touch the mining engine: miners stay connected and no shares are
lost. Confirm that after any deploy by checking that the stratum unit's start timestamp is
unchanged.

**Two things that reliably go wrong:**

- **Run it as the user that runs the pool.** The engine creates its log directory mode `0750`, so
  any other account gets `permission denied` on every endpoint while `/health` still answers.
- **Do not expose the port to the internet.** The key is the only authentication and it travels
  in cleartext over HTTP. Scope it in the firewall to the consuming application's address.

### Exposing only `/ping` over TLS

If you want browsers to measure latency to the pool host, put a TLS reverse proxy in front and
proxy **exactly one path**, `GET /ping`, to the API on `127.0.0.1:8888`, returning 404 for
everything else. A sample Caddy configuration ships in `api/caddy/`.

This opens 443 on the machine that runs your pool. That is a deliberate trade — make it knowingly,
keep the proxy's own admin surface disabled, and never widen the proxied path set to include an
authenticated route.

---

## Sharelog retention

The engine creates a new per-block sharelog directory on every new workbase and leaves old ones in
place indefinitely. A systemd timer runs a pruning script daily to remove directories older than
`DAYS_TO_KEEP` (default 60 days). The window is sized so `/shares` cursors resume comfortably
within a normal poll interval.

```bash
sudo install -m 0644 cashstratum-logprune.timer /etc/systemd/system/
sudo install -m 0644 cashstratum-logprune.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cashstratum-logprune.timer
```

The shipped units are **templates carrying one host's paths and user**. Edit `User=`, `Group=`,
`WorkingDirectory=`, `Environment=` and `ExecStart=`, then run the service **once by hand** and
read the journal before trusting the timer. A bad `ExecStart` fails at the first firing, hours
later, with nobody watching. The script accepts `--dry-run` to preview deletions.

**Solve protection.** The pruning script builds a persistent, append-only index of solved heights
before it prunes anything, skips every height in it, and **refuses to delete at all** if the index
cannot be built. The sharelog of a height you solved is the per-share record behind a block you
were paid for, and it has no second copy.

The index is persistent by design: the main log has a bounded window, so a solve from months ago
is no longer greppable anywhere. An index rebuilt from scratch each run would silently shrink, and
the next prune would take exactly the directories the protection exists for.

**Verify protection on the deployed copy, not the one in your checkout** — and note that *how* you
verify depends on history:

- *On a pool with at least one solve*, the dry run is real evidence. Protection is live only if
  the output names the solved height (`PROTECTED (solved): 000ebfa9`).
- *On a pool that has never solved*, **the dry run proves nothing** — a protected script and an
  unprotected one produce identical output when there is no solve to protect. Confirm the deployed
  file's provenance instead. A check that could not have failed is not evidence.

---

## Block notifier (optional companion)

A second Go binary in the same directory delivers **sub-second block notifications** as signed
webhooks. It subscribes directly to the node's ZMQ `hashblock` socket and posts within
milliseconds of a new block, and it tails the pool log to emit events for the three solve moments
the engine records: submission, confirmation and rejection. It sends a heartbeat every five
minutes to prove liveness.

It is a **latency layer, not a source of truth**: if a webhook is lost or delayed, your own
periodic polling remains the safety net.

```bash
cd api && go build -mod=vendor -o notifier ./notifier
```

It reads its configuration from a single root-only env file (mode `0600`): the webhook URL, a
32-byte shared secret used to sign deliveries (`openssl rand -hex 32`), and the paths to the pool
configuration and log. It **defaults to dry-run** — it will not POST anywhere until you explicitly
disable that.

---

## Status codes

| Code | When |
|---|---|
| `200` | success, including "user not found" on `/user-file` |
| `204` | `/ping` only |
| `401` | missing or wrong bearer key — plain-text body `Unauthorized` |
| `405` | `/ping` called with a method other than `GET` or `HEAD` |
| `429` | rate limit exceeded — plain-text body, `Retry-After` header |
| `504` | `/coinbase` timed out waiting for the pool to broadcast a job |
