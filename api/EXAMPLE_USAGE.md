# API example usage — real captured responses

Every response below was captured live from the production API (`solo`, master `464a7ac6`,
2026-08-29) — these are not hand-written examples. Written for the BlockSniper Laravel
integration; per-endpoint notes call out what the consumer should and should not rely on.

## Basics

- Base URL: `http://<pool-host>:8888` — never exposed to the internet or browsers; Laravel
  proxies everything.
- Every endpoint needs `Authorization: Bearer $CKPOOL_API_KEY`. Missing/wrong key → `401`
  with plain-text body `Unauthorized`.
- Rate limits per IP: **20 req/60s** on everything except **`/shares`, which gets 60 req/60s**
  (sized for a 2–3 s poll). Over budget → `429 Too Many Requests`. Cache: most endpoints serve
  a cached body for up to 60 s (per-endpoint freshness noted below).

```bash
curl -H "Authorization: Bearer $CKPOOL_API_KEY" "http://$POOL:8888/<endpoint>"
```

## `GET /health`

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

`uptime` is the **API service's** uptime (seconds), not the pool's. `log_exists:false` with
`status:"ok"` means the API is up but cannot see the pool's log — treat as degraded.

## `GET /stats`

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

- `hashrate` is ckpool's SI-suffixed string (`"50.8P"`, `"2.31G"`, `"0"`) — parse suffix-aware,
  never as a float.
- `block_height` and `node` come from the BCH node's own `getblockchaininfo`, using the RPC
  credentials already in `ckpool.conf`. The same capture from the host's `bch-status` helper
  agrees field for field:

  ```text
  state=up   chain=main   blocks=966262   headers=966262
  progress_pct=99.9999    ibd=false       peers=8
  ```

- Gate on **`node.synced`**, not on `blocks`: it is `blocks >= headers && !ibd`, so a node that
  is one block behind its own headers reads as not synced however healthy `blocks` looks.
- `node.difficulty` is the current network difficulty. This value should match `networkdiff` in
  each miner's user file (via `/user-file?user=…`). A persistent divergence means ckpool is
  working on a stale block template and requires a restart.
- `node` is **omitted** when the node cannot be reached, and `block_height` then falls back to
  the old log-scrape — which is often `null` on a freshly restarted pool, because it only sees
  whatever height appeared in the last 200 log lines. Treat a missing `node` as "height may be
  stale"; `/shares`' `height` remains a good cross-check.
- Cached ~30 s, so the node is polled at most twice a minute; each call is bounded at 3 s.

## `GET /tail?lines=N`

```json
{
    "lines": [
        "[2026-08-29 23:37:08.468] Pool:{\"hashrate1m\": \"0\", \"hashrate5m\": \"0\", \"hashrate15m\": \"0\", \"hashrate1hr\": \"1.99G\", \"hashrate6hr\": \"484T\", \"hashrate1d\": \"2.29P\", \"hashrate7d\": \"868T\"}",
        "[2026-08-29 23:37:08.468] Pool:{\"diff\": 32.3, \"luck\": 32.3, \"accepted\": 145146117994, \"rejected\": 34929688, \"bestshare\": 39410469345, \"SPS1m\": 0.0, \"SPS5m\": 0.0, \"SPS15m\": 0.0, \"SPS1h\": 2.33e-9}",
        "[2026-08-29 23:37:23.454] Stored local workbase with 85 transactions"
    ],
    "timestamp": 1788046652
}
```

`lines` caps at 1000. The `Pool:{"diff"` line is the one `PoolDataCollectionService` greps;
since master `464a7ac6` it also carries `"luck"` (same value — `diff` is the deprecated alias
and stays FIRST on the line by contract).

## `GET /grep?pattern=...`

Literal (regex-escaped) match over the whole log, last 500 hits, cached 60 s.

```bash
curl -H "$AUTH" "http://$POOL:8888/grep?pattern=Network%20diff"
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

## `GET /find-block?height=N`

```json
{
    "found": false,
    "lines": null,
    "height": "966262",
    "timestamp": 1788046652
}
```

⚠️ When `found` is `false`, `lines` is **`null`, not `[]`** — guard the iteration. Found
blocks are cached forever.

## `GET /user-file?user=...`

The full parsed status of one miner — **this is the endpoint for per-user dashboards**:

```json
{
    "username": "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u",
    "content": {
        "authorised": 1787906856,
        "bestever": 39410469345,
        "bestshare": 39410469345.60201,
        "hashrate1d": "2.51P",
        "hashrate1hr": "23.1G",
        "hashrate1m": "0",
        "hashrate5m": "0",
        "hashrate7d": "881T",
        "lastshare": 1787993774,
        "luck": 32.29,
        "roundshares": 145146117994,
        "shares": 145146117994,
        "worker": [
            {
                "bestever": 39410469345,
                "bestshare": 39410469345.60201,
                "hashrate1d": "2.51P",
                "hashrate1hr": "23.1G",
                "hashrate1m": "0",
                "hashrate5m": "0",
                "hashrate7d": "881T",
                "lastshare": 1787993774,
                "shares": 145146117994,
                "worker": "nh",
                "workername": "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u.nh"
            }
        ],
        "workers": 0
    }
}
```

- **`worker` is the field to render.** ckpool stores the whole stratum auth string in
  `workername` (it splits on the first `.` or `_` to derive the username), so repeating the
  54-character address on every row is useless to a dashboard — `worker` carries just the
  segment after that separator. `workername` is left byte-identical, so nothing that already
  reads it breaks. A miner who authorised with a bare address and no suffix gets `""`.

- `luck` / `roundshares` / `networkdiff` (new in master `464a7ac6`): the user's **independent**
  round progress — accumulated share difficulty since the last **block solve by that miner**,
  as % of network difficulty. This is **separate** from the pool-wide `luck` (in `/stats`):
  each user's luck resets only when **that user** solves a block, not when someone else on the
  pool does. ⚠️ All three fields may be **entirely absent** from a user's file if they have not
  authorised since a ckpool restart — the file may be frozen from an older build. Consumers
  must null-check for field presence, not just compare `lastshare` freshness. `lastshare`
  is the "as of" timestamp for all three.
  - **Note:** `networkdiff` (in the user file) should match `/stats.node.difficulty`; a
    persistent divergence means ckpool is working on a stale template and needs a restart.
- `user` accepts both CashAddr spellings (prefixed and bare) and resolves either to the file
  that exists; the response's `username` is the canonical (on-disk) identity.
- Miss → `{"error": "User not found", "username": "..."}` with HTTP 200.

## `GET /user-log?user=...&lines=N`

Tails the same per-user file as raw lines. Since that file is indented JSON, a small `lines`
value returns fragments (closing brackets), which is useless:

```json
{
    "lines": [ "]", "}" ],
    "exists": true,
    "username": "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u",
    "timestamp": 1788046652
}
```

**Prefer `/user-file`** — it returns the same data parsed. Keep `/user-log` only for raw-dump
debugging with a large `lines` value.

## `GET /shares` — the incremental share stream (feeds the transparency page)

Full parameter/cursor semantics in [README.md](README.md#get-shares). Budget: 60 req/min/IP;
responses served from cache for up to 2 s per (cursor, user, limit) key — a 2–3 s poll always
sees fresh data.

**Empty state** (no miners connected, nothing new — this is NOT an error, keep polling):

```json
{
    "shares": [],
    "height": 966262,
    "next_cursor": "MDAwZWJlNzZ8fDA=",
    "timestamp": 1788046662
}
```

### What `next_cursor` actually encodes

Base64 over `<height dir>|<sharelog file>|<byte offset>`. Decoding the two cursors on this page:

```text
MDAwZWJlNzZ8fDA=                                      ->  000ebe76||0
MDAwZWJkOTR8NmE5MTRiMTkwMDAwMDAzMS5zaGFyZWxvZ3wxMjI5  ->  000ebd94|6a914b1900000031.sharelog|1229
```

- `000ebe76` is hex **966262** — the per-block sharelog directory, matching this response's
  own `height`.
- The middle is the `.sharelog` file inside it. **Empty is normal and valid**: it means "the
  start of this height dir", which is what an idle pool produces — ckpool has created the
  directory for the new workbase but has not written a share into it yet.
- The last number is the byte offset just past the last line you were handed.

Treat it as opaque and pass it back verbatim. The base64 only keeps the internal shape out of
callers' hands; it is **not** a security boundary — a corrupt or hand-edited cursor is not an
error, it silently falls back to a bootstrap read. It also survives `clean-old-blocks.sh`
pruning the directory out from under it: the read resumes at the oldest surviving height dir
**at or above** that height, never below.

**With records** — real NiceHash-order shares (`limit=2` shown):

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
            "error": null,
            "errn": 0,
            "createdate": "1787907641,54226544",
            "createby": "code",
            "createcode": "parse_submit",
            "createinet": "0.0.0.0:3333",
            "workername": "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u.nh",
            "worker": "nh",
            "username": "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u",
            "address": "34.22.147.138",
            "agent": "NiceHash/1.0.0"
        }
    ],
    "height": 966263,
    "next_cursor": "MDAwZWJkOTR8NmE5MTRiMTkwMDAwMDAzMS5zaGFyZWxvZ3wxMjI5",
    "timestamp": 1788047121
}
```

Laravel notes:

- Poll loop: call with no `since` once (bootstrap: most recent records), then always pass the
  previous response's `next_cursor` verbatim. Each share arrives exactly once, in order, across
  block rollovers.
- `diff` is the credited difficulty (what payout math uses); `sdiff` is the share's actual
  hash quality (display/bestshare only). `createdate` is `"<unix-seconds>,<nanoseconds>"`.
- `result:false` records carry `reject-reason` (string) — count them separately, never toward
  work totals.
- `user` filter resolves both CashAddr spellings (bare `qz…` and `bitcoincash:qz…` forms),
  just like `/user-file` and `/user-log`. Legacy base58 is kept separate — only CashAddr
  spellings are treated as equivalent, matching ckpool's own accounting.
- `address` is the miner's IP, `agent` its useragent — useful for per-connection displays but
  PII-ish: don't render raw IPs on the public page.
- `height` here is the sharelog's own current height. `/stats.block_height` now comes from the
  node itself, so the two should agree; a persistent gap means the pool has not seen new work.

## `GET /coinbase?user=...`

Returns the **current candidate block template's coinbase** — not proof of a found block.
Each uncached call authorizes a temporary probe worker against the pool and waits for the
pool to broadcast a mining job. The response is cached for 10 seconds per username; a cache
miss costs one short-lived stratum authorisation. On an idle pool, this can take up to
`update_interval` (default 30 s) + 5 s slack and returns HTTP 504 (Gateway Timeout) with a
JSON body if the job never arrives. Reference response shape:

```json
{
    "username": "anonymous",
    "coinbase_hex": "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff31037bbe0e00049889936a04ed2e391a0c4068936a000000000000000014456c6f506f6f6c2e636c6f75642f5b536f6c6f5dffffffff02eedb7612000000001976a91401c09ad61cb2ef44f812441153318c332d3b651088acfdbe2f00000000001976a91401c09ad61cb2ef44f812441153318c332d3b651088ac00000000",
    "coinbase_message": "",
    "outputs": [
        { "value": 309779438, "value_bch": "3.09779438", "address": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", "type": "miner" },
        { "value": 3129085,  "value_bch": "0.03129085", "address": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", "type": "pool_fee" }
    ],
    "total_value": 312908523,
    "total_value_bch": "3.12908523",
    "block_height": 966267,
    "network_bits": "1802762c",
    "timestamp": 1788053906
}
```

Addresses come back in **legacy Base58** — convert for CashAddr display
(see [COINBASE_API.md](COINBASE_API.md)).

## `GET /metrics`

Service-level counters for monitoring dashboards (not pool stats):

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

`errors_total` counts 401s and 429s too — a rising value under normal operation usually means
a misconfigured consumer key, not pool trouble.

## Auth failures

```text
GET /shares  (no Authorization header)      -> HTTP 401, body "Unauthorized"
GET /health  (Authorization: Bearer wrong)  -> HTTP 401, body "Unauthorized"
```

Same behavior on every endpoint. 401 bodies are plain text, not JSON — don't json-decode
error responses blindly.
