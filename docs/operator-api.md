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
*(With `$CKPOOL_API_KEY` supported for backwards compatibility).*

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

| Bucket | Endpoints | Default | Environment variable (Primary / Fallback) |
|---|---|---|---|
| Default | everything except `/shares` and `/ping` | 20 req / 60 s | `CASHSTRATUM_RATE_LIMIT` (`CKPOOL_RATE_LIMIT`) |
| Shares | `/shares` only | 60 req / 60 s | `CASHSTRATUM_SHARES_RATE_LIMIT` (`CKPOOL_SHARES_RATE_LIMIT`) |
| Ping | `/ping` only, unauthenticated | 60 req / 60 s | `CASHSTRATUM_PING_RATE_LIMIT` (`CKPOOL_PING_RATE_LIMIT`) |

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
| `GET /round` | pro-rata pool only: the live round, per address — see [Round API](#round-api-pro-rata-pool) |
| `GET /rounds/solved?since=` | pro-rata pool only: solved blocks above a height, oldest first |
| `GET /round/{height}` | pro-rata pool only: one frozen round, with its finder and coinbase outputs |
| `GET /round/history?hours=&bucket_minutes=` | pro-rata pool only: hashrate and best share per time bucket |

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

### `GET /users`

Pool totals and every miner's status file in one response, read from the files the engine already
writes. It makes **no node RPC**. Use it for a fleet view; use `/user-file` for one miner.

```bash
curl -H "$AUTH" "$POOL/users?active_since=1793638000&workers=1"
```

| Param | Meaning |
|---|---|
| `active_since=<unix>` | Keep only users whose `lastshare` is ≥ the value. A user with no `lastshare` is dropped when this filter is set. A non-integer or negative value returns **`400`**. |
| `workers=1` or `workers=true` | Include each user's `worker` array. Without it, `worker` is removed from every user. |

```json
{
  "pool": {
    "runtime": 98463,
    "lastupdate": 1793638507,
    "Users": 6,
    "Workers": 11,
    "Idle": 3,
    "Disconnected": 9,
    "hashrate1m": "112P",
    "hashrate5m": "107P",
    "hashrate15m": "111P",
    "hashrate1hr": "121P",
    "hashrate6hr": "98.6P",
    "hashrate1d": "83.2P",
    "hashrate7d": "49.3P",
    "diff": 83.04,
    "luck": 83.04,
    "accepted": 451593215252,
    "rejected": 127629841,
    "bestshare": 471171079910,
    "SPS1m": 1.29,
    "SPS5m": 1.28,
    "SPS15m": 1.3,
    "SPS1h": 1.36
  },
  "users": [
    {
      "hashrate1m": "102",
      "hashrate5m": "84.7M",
      "hashrate1hr": "9.25G",
      "hashrate1d": "3.1G",
      "hashrate7d": "519M",
      "lastshare": 1793635713,
      "workers": 0,
      "shares": 80000,
      "roundshares": 80000,
      "bestshare": 445222.83518990397,
      "luck": 0.000014710457495446335,
      "networkdiff": 543830808965.41956,
      "authorised": 1793608663,
      "rejected": 10000,
      "rejectedcount": 2,
      "user": "bitcoincash:qzxq…4u",
      "worker": []
    }
  ],
  "skipped": 0,
  "timestamp": 1793638600
}
```

The example is abridged. Values are illustrative; the shape is what matters.

- **`pool`** is the three lines of `pool.status` merged into one object. Keys pass through **as the
  engine writes them**, so the casing is mixed (`Users`, `Workers`, `SPS1m`, `hashrate1m`). Do not
  normalise them in a client that matches on names.
- **`users`** holds each user's status file content, the same fields `/user-file` returns in
  `content`, plus **`user`**, the file name. The name is the canonical on-disk identity, including
  any `bitcoincash:` prefix. Users are sorted by name.
- **Hashrates are suffixed strings** (`"102"`, `"84.7M"`, `"9.25G"`), not numbers. Parse the suffix
  yourself, as with `/user-file`.
- **`luck` is a percent**, unrounded per user. Its precision is the engine's, so do not compare
  it with equality.
- **`worker`** appears only with `workers=1`. Probe workers (`<user>.cashstratum-api`) are filtered
  out, and the rest are annotated the same way as on `/user-file`. The top-level **`workers`** count
  is always the file's own value, unchanged by `workers=1`. It is never recomputed from the array,
  so it **can differ from `len(worker)`** once probe workers are filtered out, and it can be larger.
  A count of `0` next to a non-empty `worker` is expected when the engine's counter has decayed.
- **`skipped`** counts files left out of this response. A file that is being rewritten is re-read
  once; if it still cannot be read, the last good copy of that file is used. A file is counted in
  `skipped` only when there is no good copy yet, or when it is larger than 1 MB.
- **Freshness.** The snapshot is reused for at most 30 s. It is rebuilt sooner when `pool.status`
  changes on disk, which is checked on every request. `timestamp` is when the response was built.
- **Cost.** One request in the default rate-limit bucket, including a cache hit. The budget is
  charged after authentication. No node RPC is made.
- **Errors.** **`503`** with `{"error": "pool status unavailable"}` when `pool.status` cannot be read
  or parsed and no earlier good copy exists, or `{"error": "users directory unavailable"}` when the
  users directory cannot be listed. The response never includes file paths; the detail is written
  to the server log. Retry after a short wait; a 503 means the files are unavailable, not that the
  pool has no miners.

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

**Per-height template.** `/coinbase` now answers from one cached job per block height. It probes
the pool once for that height, under a fixed identity, and keeps that job as the template. Every
address is then answered by splicing its own payout output into the template, so a viewer does not
cost a probe of its own. The configured pool fee address (`pooladdress` in the engine config) is
the probe identity when it is set. Without it, the first requesting address is used.

The per-user probe described below is only the **fallback**. It runs when the username is not a
CashAddr, decodes to a different network than the pool's, when `candidate_height` is older than the
template, or when no template can be had. The response fields are the same on both paths.
`extranonce1` in a spliced answer is a sample from the template probe, and `extranonce2` is zeros,
exactly as in a probe answer. A block that is actually found carries the finding miner's own bytes.

Each uncached fallback call authorises a temporary probe worker against the pool. The probe returns
as soon as it holds both the mining job and the authorise reply, so it no longer waits for the next
broadcast. The response is cached for 10 seconds per username. If no job has arrived yet, the probe
waits up to the engine's `update_interval` (default 30 s) plus slack, and returns **`504`** with a
JSON body if the job never arrives.

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

## Round API (pro-rata pool)

Four routes describe a pro-rata (PROP) pool's round: the live round, the solved blocks, one frozen
round and a time-bucketed history. They answer `503` when the round index cannot open, while the rest
of the API keeps working. The index opens at startup from the pool's own state directory (see
[Configuration](#configuration)).

They take the same `Authorization: Bearer` key and the **default** rate budget (20 requests per 60 s
per IP, `CASHSTRATUM_RATE_LIMIT`). A consumer that polls `GET /round` every 15 s uses four of those
twenty requests a minute. Successful bodies are cached for **5 s**.

> **Errors here are JSON.** The round routes answer `{"error": "<message>"}`, unlike the plain-text
> bodies elsewhere in this API. A `401` or a `429` still comes from the shared middleware, as plain
> text.

### Encoding rules

- **Times** are unix seconds as JSON integers, in UTC. A time that is not known is `null`, never `0`.
  There are no ISO-8601 strings.
- **Work, total work, shares and every `*_sats` field** are JSON **strings**. Work is a decimal, not
  an integer: an assigned difficulty can be fractional on a low-difficulty network, so do not round
  it or parse it as a number you cannot represent.
- **Hashrates** are decimal strings in H/s, `null` when the figure is unknown. These are raw values,
  not the suffixed form used by the log-based routes (see [Hashrate strings](#hashrate-strings)).
- **`best_share`** and **`best_of_net`** are JSON numbers. `best_of_net` is a percentage of the
  network difficulty and is `null` when that difficulty is unknown.
- **Addresses** are the bare, lowercase CashAddr, with no `bitcoincash:` prefix. A legacy Base58
  address is converted first. `username` keeps the string the miner authorised with. Two spellings
  of one address merge into a single row, both in the live sums and in a frozen round.
- An address with zero work is omitted from a round.

### `GET /round`

The live round since the last accepted block. The round starts after that block, so
`last_block_height` and `started_at` describe the window that `total_work` covers.

```bash
curl -H "$AUTH" "$POOL/round"
```

```json
{
  "timestamp": 1762650000,
  "last_block_height": 976535,
  "started_at": 1762635660,
  "total_work": "682330000000",
  "network_difficulty": 527345678901.2,
  "coinbase_value_sats": "312990000",
  "hashrate_1m": "108000000000000000",
  "hashrate_1h": "105000000000000000",
  "addresses": [
    {
      "address": "qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh",
      "username": "qqnwep7p0802q2e4mpkeze7afer9zj3l8qryyg3chh",
      "work": "609912000000",
      "shares": "1342",
      "last_share_at": 1762649997,
      "best_share": 187000000000.0,
      "best_of_net": 35.5,
      "online": true,
      "hashrate_1h": "2000000000000"
    }
  ]
}
```

| Field | Type | Notes |
|---|---|---|
| `timestamp` | integer | when the response was built |
| `last_block_height` | integer or `null` | the last accepted solve the round starts after |
| `started_at` | integer or `null` | start of the round window |
| `total_work` | string | the round's total work |
| `network_difficulty` | number, or null when unknown (for example right after a restart while the node is unreachable) | the network difficulty `best_of_net` is measured against |
| `coinbase_value_sats` | string or `null` | the node's current block template value; `null` when the node cannot be reached |
| `hashrate_1m`, `hashrate_1h` | string or `null` | the pool's one-minute and one-hour hashrate, H/s |
| `addresses` | array | one row per address with work, in no particular order |

Each `addresses` row:

| Field | Type | Notes |
|---|---|---|
| `address` | string | canonical lowercase CashAddr, no prefix |
| `username` | string | the string the miner authorised with |
| `work` | string | the address's work in this round |
| `shares` | string | accepted shares in this round |
| `last_share_at` | integer or `null` | time of its last accepted share |
| `best_share` | number | the best share this round, in difficulty |
| `best_of_net` | number or `null` | `best_share` as a percentage of the network difficulty |
| `online` | boolean | at least one accepted share in the last 10 minutes |
| `hashrate_1h` | string or `null` | the address's one-hour hashrate, H/s |

Addresses that have disconnected stay in the list until the round closes.

### `GET /rounds/solved?since=<height>`

Solved blocks with a height **strictly greater** than `since`, oldest first. `since` is optional, a
non-negative integer, and defaults to `0` (every solve). A non-numeric or negative `since` is `400`.
A solve the daemon rejected never appears here.

```json
{
  "solves": [
    { "height": 976535, "hash": "0000…", "solved_at": 1762635660, "status": "accepted" }
  ],
  "timestamp": 1762650000
}
```

| Field | Type | Notes |
|---|---|---|
| `height` | integer | the solved block's height |
| `hash` | string | the block hash the index holds; it can be provisional until verified |
| `solved_at` | integer or `null` | when the pool found it |
| `status` | string | `accepted` or `orphaned` |

### `GET /round/{height}`

One frozen round. A frozen round is kept indefinitely. Its outputs and totals do not change once
they are filled, with one exception: `status` can move from `accepted` to `orphaned`. The `orphaned`
status is terminal for a payout consumer, which should re-check the hash at that height against its
own node before it pays anything.

- `400` for a height that is not a positive integer.
- `404` (`"not a solve height"`) for a height that is not a PROP solve.
- An orphaned height returns `200` with `"status": "orphaned"`.

```json
{
  "height": 976535,
  "hash": "0000…",
  "status": "accepted",
  "solved_at": 1762635660,
  "prev_block_height": 976456,
  "started_at": 1762589400,
  "finder_address": "qrfc9t…eshhfq",
  "finder_worker": "qrfc9t…eshhfq.doctor95",
  "coinbase_value_sats": "312990000",
  "finder_output":  { "address": "qrfc9t…eshhfq", "vout": 0, "value_sats": "156495000" },
  "payroll_output": { "address": "qpayroll…",    "vout": 1, "value_sats": "156495000" },
  "total_work": "990000000000",
  "addresses": [
    { "address": "…", "username": "…", "work": "…", "shares": "…", "last_share_at": 1762635650 }
  ]
}
```

| Field | Type | Notes |
|---|---|---|
| `height`, `hash`, `status`, `solved_at`, `started_at` | as in the live round | `status` is `accepted` or `orphaned` |
| `prev_block_height` | integer or `null` | the accepted block the round started after; for a rolled-forward round it is older than the orphan |
| `finder_address`, `finder_worker` | string | the address and the full worker name that found the block |
| `coinbase_value_sats` | string or `null` | the block's coinbase value, taken from the node |
| `finder_output` | object or `null` | coinbase output `vout` 0: `address`, `vout`, `value_sats` (string) |
| `payroll_output` | object or `null` | coinbase output `vout` 1, the pool's payroll output |
| `total_work` | string | the round's total work |
| `addresses` | array | `address`, `username`, `work`, `shares`, `last_share_at`, as in the live round |

The outputs and the coinbase value come from the node's record of the solved block, not from a
block template. They are `null` until the node has filled them.

### `GET /round/history?hours=&bucket_minutes=`

Time buckets of pool hashrate and best share.

- `hours`: `1` to `72`, default `24`. Anything else is `400`.
- `bucket_minutes`: one of `5`, `10`, `15`, `30`, `60`, default `60`. Anything else is `400`.

Buckets are aligned to UTC, oldest first. The newest bucket has `"partial": true` while it is still
filling. A bucket with no data has `null` for `hashrate` and `best_share`.

```json
{
  "bucket_minutes": 60,
  "buckets": [
    { "bucket_start": 1762635600, "hashrate": "105000000000000000", "best_share": "187000000000", "partial": false },
    { "bucket_start": 1762639200, "hashrate": null, "best_share": null, "partial": false },
    { "bucket_start": 1762642800, "hashrate": "108000000000000000", "best_share": "9000000000", "partial": true }
  ],
  "timestamp": 1762645000
}
```

| Field | Type | Notes |
|---|---|---|
| `bucket_minutes` | integer | the bucket width you asked for |
| `buckets[].bucket_start` | integer | start of the bucket, unix seconds, UTC-aligned |
| `buckets[].hashrate` | string or `null` | the mean of the pool's per-minute hashrate points in the bucket, H/s |
| `buckets[].best_share` | string or `null` | the highest accepted share difficulty in the bucket |
| `buckets[].partial` | boolean | `true` for the bucket still filling |
| `timestamp` | integer | when the response was built |

### How the round is kept

- **Ownership decides the orphan check.** The index asks the node, about once a minute, what block
  is at each solve's height. A block that pays this pool is ours: its hash is recorded, its coinbase
  value and outputs are filled, and the solve is `accepted`. A block at that height that pays someone
  else, once the chain has moved past it, makes the solve `orphaned`. A pool with a `pooladdress`
  recognises its blocks by the output at vout 1. A pool without one recognises them by its `btcsig`
  in the coinbase. Solves 100 or more blocks deep are settled and no longer re-checked.
- **An orphan returns its work to the round.** When the newest solve is orphaned, the live round
  falls back to the previous accepted block, so the orphaned round's work joins the current one. A
  later `accepted` status undoes that.
- **A failed node call changes nothing.** The index keeps the last state it had.

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

Environment only. There is no configuration file. Primary environment variables use
the `CASHSTRATUM_*` prefix, with `CKPOOL_*` supported for backwards compatibility.

| Variable | Default | Notes |
|---|---|---|
| `CASHSTRATUM_API_KEY` | — | **required**; the server aborts without it (fallback: `CKPOOL_API_KEY`) |
| `CASHSTRATUM_LOG_PATH` | `~/cashstratum/logs/cashstratum.log` | must match the pool's `logdir` (fallback: `CKPOOL_LOG_PATH` / `~/ckpool/logs/ckpool.log`) |
| `CASHSTRATUM_USER_LOGS_PATH` | `~/cashstratum/logs/users` | ditto (fallback: `CKPOOL_USER_LOGS_PATH` / `~/ckpool/logs/users`) |
| `CASHSTRATUM_API_PORT` | `8888` | (fallback: `CKPOOL_API_PORT`) |
| `CASHSTRATUM_CASHADDR_PREFIX` | `bitcoincash` | CashAddr network prefix (no trailing `:`) used by `/coinbase`'s probe; a regtest/testnet rig sets `bchreg` / `bchtest` (fallback: `CKPOOL_CASHADDR_PREFIX`) |
| `CASHSTRATUM_CONF_PATH` | `<logdir>/../cashstratum.conf`, then `<logdir>/cashstratum.conf` | the pool's own configuration, read once at startup for the node RPC endpoint (fallback: `CKPOOL_CONF_PATH` / `ckpool.conf`) |
| `CASHSTRATUM_RATE_LIMIT` | `20` | per-IP budget for all endpoints except `/shares` and `/ping`, per 60 s window (fallback: `CKPOOL_RATE_LIMIT`) |
| `CASHSTRATUM_SHARES_RATE_LIMIT` | `60` | per-IP budget for `/shares` only (fallback: `CKPOOL_SHARES_RATE_LIMIT`) |
| `CASHSTRATUM_PING_RATE_LIMIT` | `60` | per-IP budget for `/ping` only (fallback: `CKPOOL_PING_RATE_LIMIT`) |
| `DAYS_TO_KEEP` | `60` | sharelog retention in days, used by the pruning timer (`cashstratum-logprune.timer`) |
| `CASHSTRATUM_INDEX_DIR` | `<root>/api-state/index` | round index cache (fallback: `CKPOOL_INDEX_DIR`) |
| `CASHSTRATUM_ROUNDS_DIR` | `<root>/api-state/rounds` | frozen round store, kept indefinitely; back it up with the pool (fallback: `CKPOOL_ROUNDS_DIR`) |

`<root>` is the directory above the log directory: for `CASHSTRATUM_LOG_PATH=~/cashstratum/logs/cashstratum.log`,
the defaults are `~/cashstratum/api-state/index` and `~/cashstratum/api-state/rounds`. Two pool
instances whose log directories share a parent also share this state directory, so give each
instance its own pair of variables.

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

From the checkout root after building `api/cashstratum-api-linux-amd64`, adapt the
paths and service account below. Preserve existing secret files on upgrades.

```bash
# 1. Binary
sudo install -d -m 0755 /opt/cashstratum-api
sudo install -m 0755 api/cashstratum-api-linux-amd64 /opt/cashstratum-api/cashstratum-api

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
sudo cp api/cashstratum-api.service /etc/systemd/system/
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

`clean-old-blocks.sh` prunes aged height directories only after a verified historical
solve index is present. No automatic scan can recover solve records whose logs have
already disappeared. Review a complete historical index before initializing protection.
Keep the old timer disabled throughout migration and back up the index and log tree.

The seed contains one eight-digit lowercase hexadecimal height per line, for example
`000ebd93`. An empty seed explicitly declares that the pool has **never** solved a block;
it must not be used as a shortcut for missing history. Import an existing legacy index
only after checking its completeness against your historical evidence.

Run as the account that owns the pool files, using the actual deployed script and paths:

```bash
CASHSTRATUM_DIR=/opt/cashstratum bash /opt/cashstratum/clean-old-blocks.sh \
    --seed-index /path/to/verified-heights.idx
CASHSTRATUM_DIR=/opt/cashstratum bash /opt/cashstratum/clean-old-blocks.sh --dry-run
```

Seeding merges the verified baseline and all current/rotated `cashstratum.log*` and
`ckpool.log*` records, writes a verification marker and exits **without pruning**.
Normal runs refuse missing, empty or unverified legacy indexes, missing logs, invalid
entries and read/decompression failures. Plain and gzip logs are supported. The index
only gains heights; it is never rebuilt from current logs alone. `--dry-run` does not
modify the index or logs. `DAYS_TO_KEEP` defaults to 60; `ROTATED_LOG_DAYS` defaults to 30.

Before enabling `api/cashstratum-logprune.timer`, edit its service's `User`, `Group`,
`WorkingDirectory`, `CASHSTRATUM_DIR` and `ExecStart`. Compare pre/post height and
sharelog-directory sets; no historical entry may disappear. Run the configured service
once and review its journal after the dry-run passes. The timer uses `Persistent=true`,
so enabling it may immediately trigger a missed run. Confirm the deployed script matches
the reviewed version. Production hosts using a different pruner need their own migration;
installing this repository's script does not patch an external retention service.

---

## Block notifier (optional companion)

A second Go binary in the same directory delivers **sub-second block notifications** as signed
webhooks. It subscribes directly to the node's ZMQ `hashblock` socket and posts within
milliseconds of a new block, and it tails the pool log to emit events for the three solve moments
the engine records: submission, confirmation and rejection. It sends a heartbeat every five
minutes to prove liveness.

It is a **latency layer, not a source of truth**: if a webhook is lost or delayed, your own
periodic polling remains the safety net. Managed by `cashstratum-notifier.service`.

```bash
cd api && go build -mod=vendor -o cashstratum-notifier ./notifier
```

It reads its configuration from a single root-only env file `/etc/cashstratum-notifier/cashstratum-notifier.env`
(mode `0600`): the webhook URL (`CASHSTRATUM_NOTIFY_URL`, fallback `BLOCKSNIPER_NOTIFY_URL`), a
32-byte shared secret used to sign deliveries (`CASHSTRATUM_NOTIFY_SECRET`, fallback `BLOCKSNIPER_NOTIFY_SECRET`),
and the paths to the pool configuration and log (`CASHSTRATUM_CONF_PATH`, `CASHSTRATUM_LOG_PATH`).
It **defaults to dry-run** (`NOTIFY_DRY_RUN=true`) — it will not POST anywhere until you explicitly
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
| `400` | round routes only: a bad `since`, `hours`, `bucket_minutes` or `height` — JSON body `{"error": …}` |
| `404` | `/round/{height}` only: the height is not a PROP solve — JSON body |
| `405` | round routes only: a method other than `GET` — JSON body |
| `503` | round routes only: the round index is unavailable — JSON body |
