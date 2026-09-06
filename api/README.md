# ckpool-api — read-only HTTP API over ckpool's logs

A single-binary Go service that exposes ckpool's log tree over authenticated
HTTP, so an application (the BlockSniper Laravel app) can read pool state
without shell access to the pool host.

It lives in this repository **because it parses ckpool's output**: the log line
formats it greps for and the coinbase binary layout it decodes are defined by
`src/stratifier.c`. When those change, the parser has to change in the same
commit. Keeping it in a separate repo is how the two drift apart silently.

> This is **not** the same thing as [`CKPOOL_API_GUIDE.md`](../CKPOOL_API_GUIDE.md)
> in the repository root. That documents ckpool's own control interface — Unix
> domain sockets driven with `ckpmsg`, which can change pool state (set log
> level, disconnect a worker, shut the pool down). This service is read-only,
> speaks HTTP, and never touches those sockets.

## Endpoints

All require `Authorization: Bearer $CKPOOL_API_KEY` **except `/ping`**, which
is deliberately open to unauthenticated browser callers (see
[below](#get-ping)). Rate limited to 20 requests/minute per IP, responses
cached 60s — except `/shares`, which gets a wider 60 requests/minute per IP
budget of its own, sized for a single proxy polling it every 2–3s (see
`SharesRateLimitRequests` in `ckpool_api_server.go`), and `/ping`, which has
its own 60 requests/minute budget and no response cache.

| Endpoint | Purpose |
|---|---|
| `GET /health` | liveness, log presence and size |
| `GET /tail?lines=N` | tail the main log (capped at 1000 lines) |
| `GET /grep?pattern=` | search the main log |
| `GET /find-block?height=N` | the log burst for one solved height — finder, worker, hashrate at solve time, round shares — see [below](#get-find-block) |
| `GET /stats` | parsed pool stats — hashrate, workers, users, plus the node's own chain view |
| `GET /user-log?user=&lines=N` | tail one miner's log |
| `GET /user-file?user=` | one miner's full status file |
| `GET /coinbase?user=NAME` | decode the coinbase this miner is currently working on — see [COINBASE_API.md](COINBASE_API.md) |
| `GET /shares?since=&user=&limit=` | poll new sharelog records with a resumable cursor (`since`) — see [below](#get-shares) |
| `GET /metrics` | service-level counters |
| `GET /ping` | **unauthenticated** round-trip-time probe for a browser — see [below](#get-ping) |

`/user-log` and `/user-file` resolve both CashAddr forms of a username: the pool
names each stats file after the exact string the miner authorized with, so a
query for `qzxq…` also finds `bitcoincash:qzxq…` (and `bchtest:`/`bchreg:`), and
vice versa. An exact match always wins, and the response's `username` field
carries the name the stats actually live under.

### Worker names

ckpool stores a worker's name as the whole stratum auth string —
`bitcoincash:qzxq….nh` — because `authorise()` splits it on the first `.` or
`_` to derive the username (which is why a real username can never contain
either character). Repeating the 54-character address on every worker row is
useless to a dashboard, so `/user-file` and `/shares` both add a **`worker`**
field carrying just the segment after that separator:

| `workername` | `worker` |
|---|---|
| `bitcoincash:qzxq….nh` | `nh` |
| `bitcoincash:qzxq…_nh` | `nh` |
| `bitcoincash:qzxq….rig.1` | `rig.1` |
| `bitcoincash:qzxq…` (no suffix) | `""` |

This is **additive** — `workername` is still the verbatim string ckpool wrote,
so existing consumers are unaffected. `worker` is always derived at read time,
never taken from the sharelog line even if one somehow carries that key. A
miner who authorised with a bare address gets `""` rather than the address
echoed back, so a single unnamed rig can be rendered without re-matching the
identity.

### Probe worker filtering on `/user-file`

The `/coinbase` endpoint creates a temporary phantom worker (`<user>.ckpool-api`)
to authorize and fetch the current block template. When `/user-file` returns a
user's status, it automatically **filters out** these probe workers from the
`worker` array and subtracts the exact count of filtered entries from the file's
own `workers` field. The adjustment is never recomputed from the array length —
only what was filtered is subtracted, since ckpool's `workers` is an independent
decay counter that can be 0 even while workers are listed. Every other field
passes through byte-for-byte untouched.

### `GET /find-block`

`?height=N` (required, numeric). Returns the log burst ckpool emits when it
confirms a solve, so a consumer can recover **who found the block and at what
hashrate** — the "mining power when found" column on a block card.

```json
{
  "found": true,
  "height": "966569",
  "timestamp": 1788251130,
  "lines": [
    "[…] BLOCK ACCEPTED!",
    "[…] ZMQ block hash 00000000000000000021bb…4f3f from endpoint 0",
    "[…] Solved and confirmed block 966569 by bitcoincash:qzxq…4u.nh",
    "[…] User bitcoincash:qzxq…4u:{\"hashrate1m\": \"443T\", \"hashrate5m\": \"992T\", \"hashrate1hr\": \"8.47P\", \"hashrate1d\": \"9.09P\", \"hashrate7d\": \"3.24P\", \"shares\": 565974752369, \"authorised\": 1787906856}",
    "[…] Worker bitcoincash:qzxq…4u.nh:{\"hashrate1m\": \"443T\", …}",
    "[…] Block solved after 565977752369 shares at 128.0% diff"
  ]
}
```

`lines` is **raw log text**, deliberately — the endpoint is a context grep, not
a block parser, so a new ckpool log line shows up here without a server change.
The shape is stable because it comes from one function, `block_solve()` in
[`../src/stratifier.c`](../src/stratifier.c), which emits these `LOGWARNING`
calls back to back with nothing interleaved:

| Regex | Yields |
|---|---|
| `ZMQ block hash ([0-9a-f]{64})` | block hash |
| `Solved and confirmed block (\d+) by (\S+)` | height, workername |
| `^\[.*?\] User (\S+):(\{.*\})$` | account-wide hashrate snapshot (JSON) |
| `^\[.*?\] Worker (\S+):(\{.*\})$` | winning rig's hashrate snapshot (JSON) |
| `Block solved after ([\d.]+) shares at ([\d.]+)% diff` | round shares, luck % |
| `Possible block solve diff ([\d.]+)` | winning share difficulty |

Both JSON objects come from `user_stats()` / `worker_stats()` and carry
`hashrate1m`, `hashrate5m`, `hashrate1hr`, `hashrate1d`, `hashrate7d`, formatted
by ckpool's `suffix_string()` as a value plus a decimal (1000-based) SI suffix —
`K M G T P E`, three significant digits. Parse the suffix: `"443T"` is 443 TH/s,
`"8.47P"` is 8.47 PH/s. **Below 1000 H/s there is no suffix at all** — an idle
worker logs a bare `"0"`, so a parser that assumes a trailing letter will
mis-read it. `User` adds `shares` (lifetime accepted share
difficulty) and `authorised` (Unix seconds). The values are the decaying averages
**at confirmation time**, which is why they cannot be reconstructed later from
`/user-file` — that endpoint only ever reports *now*.

> **A username contains a colon.** `bitcoincash:qzxq…4u` is one CashAddr
> identity, and the stats line then appends a *second* colon before the JSON:
> `User bitcoincash:qzxq…4u:{…}`. A character class like `[\w.]+` stops at the
> first colon, so the pattern never matches and the hashrates come back empty —
> or worse, the block gets attributed to a miner named `bitcoincash`. Use
> `(\S+)` as above (greedy, so it backtracks to the *last* colon), or widen the
> class to include `:`. This has bitten two consumers already.

Split `workername` on its first `.` or `_` to get the rig label; see
[Worker names](#worker-names) above.

**Caveats.**

- It greps the live `ckpool.log` only. ckpool ships no rotation, so a height
  stays resolvable exactly as long as that file retains the lines. Treat the
  call as a one-shot enrichment fired soon after the solve, and persist the
  parsed row yourself.
- `found: false` therefore means *"not in the current log"*, never *"no such
  block"*.
- Found responses are cached in-process for the life of the service, so
  re-asking for the same height costs nothing.
- Three search patterns run and their context windows are concatenated, so
  `lines` may repeat a line. Deduplicate, or key each field off its first match.
- Pair with the [notifier](NOTIFIER.md): its `pool.block` event fires the
  instant ckpool confirms a solve and carries `height`/`workername` — use it as
  the trigger, and this endpoint as the detail fetch.

### `GET /shares`

Polls ckpool's per-block sharelog (`<logdir>/<height>/*.sharelog`) as a
resumable stream, rather than a fixed snapshot.

**Parameters** (all optional):

| Param | Meaning |
|---|---|
| `since` | Opaque resume cursor, copied verbatim from a previous response's `next_cursor`. Omit it for a bootstrap read of the most recent records instead of a resume. |
| `user` | Username filter — resolves both CashAddr spellings (bare and `bitcoincash:`/`bchtest:`/`bchreg:`-prefixed forms), just like `/user-log` and `/user-file`. Legacy base58 addresses are never folded into this set — a bare and prefixed CashAddr are treated as equivalent, but base58 is kept separate since ckpool itself keeps them as distinct accounting identities. |
| `limit` | Max records to return. Default and hard cap **1000**; anything larger is silently clamped, anything less than 1 is silently raised to 1. |

**Response:**

```json
{
  "shares": [
    {
      "hash": "0000000000000000...",
      "sdiff": 123456.78,
      "workername": "bitcoincash:qzxq....worker1",
      "worker": "worker1",
      "username": "bitcoincash:qzxq...",
      "createdate": "1735689600,123456789",
      "createinet": "127.0.0.1:3333",
      "result": true
    }
  ],
  "height": 900123,
  "next_cursor": "MDAwZGI3ZmJ8MDAwMDAwMDAwMDAwMDAwMS5zaGFyZWxvZ3wyMDQ4",
  "timestamp": 1735689600
}
```

Each entry in `shares` is a raw ckpool sharelog record — field names are kept
**verbatim** from `src/stratifier.c`'s own JSON output rather than
Go/JSON-cased, so the response matches the on-disk `.sharelog` line exactly
— with the single exception of `worker`, which ckpool does not write and this
API derives (see [Worker names](#worker-names)).
Beyond the ones shown above, every field ckpool writes is passed through
(`workinfoid`, `clientid`, `nonce`, `ntime`, `diff`, `errn`, `reject-reason`,
etc.) — a rejected share carries `"result": false` with a `reject-reason`
string explaining why.

**Cursor semantics:** poll again with the exact `next_cursor` value from the
last response to receive each share exactly once, in order — never a repeat,
never a gap. The cursor is anchored to a (height dir, file, byte offset)
triple rather than a bare counter, so it survives a block rollover: shares
before and after the pool moves to a new height dir come back seamlessly
across the same poll loop. An empty `shares` array with `next_cursor`
returned unchanged just means nothing new has landed yet — that is not an
error, keep polling with the same cursor.

A cursor's file component is **empty** while the pool is idle (`<dir>||0`):
ckpool creates the height dir for a new workbase before writing any share
into it, so there is nothing to anchor on yet. That is a valid, resumable
position — hand it straight back like any other. (Before `homolog`, the
server rejected its own idle cursor and silently downgraded that poll to a
bootstrap tail read, which dropped every share but the last `limit` of them
the moment mining resumed.)

**Rate budget:** 60 requests/minute per IP on this endpoint specifically (not
the default 20/minute — see the note above), sized for one proxy polling it
every 2–3 seconds.

> ⚠️ **Not for direct browser use.** Like every other endpoint here, `/shares`
> is authenticated with the bearer key alone; the BlockSniper Laravel app
> proxies it to the browser rather than the browser calling it directly, so
> the key never ships to a client that could leak or misuse it.

### `GET /ping`

The one deliberate exception to everything above: **unauthenticated**, so a
browser can call it directly with no key of its own. It exists so a visitor's
browser can measure its own round-trip time to this host — see the
browser latency ping specification for the full contract this
implements, including why `/health` cannot be reused for the same purpose
(it sits behind auth and returns real operational detail, this endpoint
returns neither).

```
GET /ping  →  204 No Content
```

- **Body:** always empty. **Headers:** `Cache-Control: no-store` (a cached
  response measures nothing) and `Access-Control-Allow-Origin: *` (no data,
  no credentials, so the open CORS policy is safe here).
- **Methods:** `GET` and `HEAD` only — anything else gets `405` with
  `Allow: GET, HEAD`.
- **Query string is ignored entirely** — the consumer appends `?t=<ts>` only
  to defeat HTTP caching, and the value is never read.
- **No redirects.** Answers on the exact requested path — a 301/302 would
  double every measurement.
- **Does zero work per request:** no body read, no disk I/O, no log line
  before responding. Any of those would be added to the number this endpoint
  exists to measure.
- **Its own rate-limit bucket**, separate from every authenticated route (see
  [below](#rate-limiting)) — a burst of browser samples can never eat the
  budget `/tail`, `/stats`, etc. depend on, and vice versa.
- **What this is NOT** (contract §6): not a liveness probe for stratum
  `:3333` — it proves the box and network path are reachable and how far
  away they are, nothing about the stratum daemon itself; not authenticated,
  and it must never carry data — its entire response is a status line; not
  a general CORS opening of the other, authenticated routes.
- **Client IP for the limiter** is `RemoteAddr`, except when `RemoteAddr` is
  loopback (a request arriving through a local reverse proxy), in which case
  the first entry of `X-Forwarded-For` is used instead — otherwise every
  browser behind the proxy would share one `127.0.0.1` bucket. `X-Forwarded-For`
  is trusted **only** in that loopback case, never from a direct caller, so a
  client cannot spoof its own bucket by setting the header itself.

## Rate limiting

Every endpoint is throttled **per client IP**, counted in a **fixed 60-second
window** that opens on that IP's first request and resets exactly 60 s later —
not a rolling average.

| Bucket | Endpoints | Default | Env |
|---|---|---|---|
| Default | everything except `/shares` and `/ping` | **20 req / 60 s** | `CKPOOL_RATE_LIMIT` |
| Shares | `/shares` only | **60 req / 60 s** | `CKPOOL_SHARES_RATE_LIMIT` |
| Ping | `/ping` only, unauthenticated | **60 req / 60 s** | `CKPOOL_PING_RATE_LIMIT` |

The three buckets are independent: spending the `/shares` budget does not
affect `/find-block` or `/ping`, and vice versa. On authenticated routes the
budget is charged **after** authentication, so a bad key never consumes it —
but it is charged **before** the response cache, so a cache hit still costs
one request. `/ping` has no authentication step to charge after; it has no
response cache either, since there is nothing to cache. Caching on the
client side is what saves budget; caching on the server side only saves disk.

**Every response advertises the budget**, so a client never has to infer it:

| Header | On | Meaning |
|---|---|---|
| `X-RateLimit-Limit` | all responses | the bucket's budget for this route |
| `X-RateLimit-Remaining` | all responses | requests left in the current window; `0` on a 429 |
| `X-RateLimit-Reset` | all responses | Unix seconds when the window resets |
| `Retry-After` | `429` only | whole seconds to wait; always ≥ 1, never `0` |

All four are listed in `Access-Control-Expose-Headers`, so a browser client can
read them cross-origin. A `429` carries the plain-text body `Too Many Requests`.

> **A `429` is not an outage.** A consumer that treats every non-2xx as a flat
> failure cannot tell "come back in 12 seconds" from "the pool is down", and
> will retry blind into a closed window. Branch on the status: sleep
> `Retry-After` on a 429, and reserve failure handling for everything else.

> **Sizing.** 20/60 s is sized for a dashboard polling live state, not for a
> backfill. If a consumer needs to sweep many historical heights, either raise
> `CKPOOL_RATE_LIMIT` for that host or — better — have the consumer cache
> **negative** results too. `/find-block` returns `found: false` permanently for
> any height that has aged out of the log (see [above](#get-find-block)), so a
> sweep that only caches successes re-requests every unresolvable height on
> every pass and spends the entire budget on work that can never succeed.

## Configuration

Environment only. There is no config file.

| Variable | Default | Notes |
|---|---|---|
| `CKPOOL_API_KEY` | — | **required**; the server aborts without it |
| `CKPOOL_LOG_PATH` | `~/ckpool/logs/ckpool.log` | must match the pool's `logdir` |
| `CKPOOL_USER_LOGS_PATH` | `~/ckpool/logs/users` | ditto |
| `CKPOOL_API_PORT` | `8888` | |
| `CKPOOL_CASHADDR_PREFIX` | `bitcoincash` | the CashAddr network prefix (no trailing `:`) that `/coinbase`'s probe prepends to a bare CashAddr username, so it authorises under the pool's canonical spelling instead of minting a shadow user; a regtest/testnet4 rig sets `bchreg`/`bchtest` |
| `CKPOOL_CONF_PATH` | `<logdir>/../ckpool.conf`, then `<logdir>/ckpool.conf` | the pool's own conf, read once at startup for the node RPC endpoint — see below |
| `CKPOOL_RATE_LIMIT` | `20` | per-IP request budget for all endpoints except `/shares`, in requests per 60-second window |
| `CKPOOL_SHARES_RATE_LIMIT` | `60` | per-IP request budget for `/shares` only, in requests per 60-second window (wider than default to accommodate a single proxy polling every 2–3 s) |
| `CKPOOL_PING_RATE_LIMIT` | `60` | per-IP request budget for `/ping` only, in requests per 60-second window — sized for a browser sampling several times per page load; see [`GET /ping`](#get-ping) |
| `DAYS_TO_KEEP` | `60` | sharelog directory retention: `clean-old-blocks.sh` removes height dirs older than this many days; managed by `ckpool-logprune.timer`/`.service` |

### Node RPC — read from `ckpool.conf`, not from this service's environment

`/stats` reports `block_height` and a `node` object from the BCH node itself,
via `getblockchaininfo`. The RPC credentials are **not** configured here: they
are read once at startup out of the pool's own `ckpool.conf`, from the first
`btcd` entry that has a `url`:

```json
{ "btcd": [{ "url": "127.0.0.1:8332", "auth": "…", "pass": "…", "notify": true }] }
```

ckpool cannot build work without those credentials, so on any host running a
pool they are already present, already correct, and rotate with the node —
whereas a second copy in this service's env file is one more thing to drift.
The conf is typically mode `0640` owned by the pool account, so run this API
as that account (the shipped unit's `User=`/`Group=`) or grant its group read.

Every failure on this path is **non-fatal**: an unreadable, absent, or
node-less conf, or a node that is down, only makes `/stats` less informative —
`node` is omitted and `block_height` falls back to the older log-scrape, which
reads whatever height appeared in the last 200 lines of `ckpool.log` and is
therefore stale on a quiet pool. A `getblockchaininfo` call is bounded at 3 s
and happens at most once per 30 s (the `/stats` cache TTL).

Generate a key with `openssl rand -hex 32`. The server refuses to start on an
empty key **or** on the placeholder constant compiled into it — otherwise an
operator who forgot the variable would get a running service authenticated by a
value published in this repository, with the startup banner still reporting
`API Key: [SET]`.

### Sharelog retention — `ckpool-logprune.timer` and `.service`

ckpool creates a new per-block sharelog directory (named `000XXXXX` where X is
hex height) on every new workbase and leaves old dirs in place indefinitely. The
systemd timer `ckpool-logprune.timer` runs `clean-old-blocks.sh` daily to prune
directories older than `DAYS_TO_KEEP` (default 60 days). The retention window is
tuned for /shares cursor resumption during the typical poll interval; adjust it
only if your retention use case differs.

`DAYS_TO_KEEP` is read from the environment at timer invocation (see
`ckpool-logprune.service`); the script accepts `--dry-run` to preview what would
be deleted without removing it.

> ⚠️ **The shipped unit is a template, and it prunes solves like anything else.**
> Two things to fix before you install it:
>
> 1. **Its paths and `User=` are one host's, not defaults** (`/mnt/data4tb/bch/…`,
>    user `bch`). On any other box they are wrong, and systemd tells you
>    nothing — a bad `ExecStart` fails at the first timer firing, hours later,
>    with nobody watching. Edit `User=`, `Group=`, `WorkingDirectory=`,
>    `Environment=CKPOOL_DIR=` and `ExecStart=`, then run it **once by hand**
>    (`systemctl start ckpool-logprune`) and read the journal before trusting
>    the timer.
> 2. **Never `chmod +x` your way out of `status=203/EXEC`.** On the *right* host
>    with every path correct, that error can still mean nothing worse than a
>    missing executable bit — and the reflex fix is one character. `clean-old-blocks.sh`
>    now ships mode `0755` *with* solve protection precisely so that bit is not
>    load-bearing; before it did, that bit was the only thing between a live solo
>    pool and an age-only `rm -rf` of its sharelog tree. A unit failing harmlessly
>    for weeks is not evidence that arming it is harmless, and a journal showing
>    no deletions may only be proof it never ran.

**Solve protection.** `clean-old-blocks.sh` builds a persistent, append-only
solve index (`$CKPOOL_DIR/solved-heights.idx`, overridable via `SOLVE_INDEX`)
before it prunes anything, skips every height in it, and **refuses to delete at
all** if the index cannot be built. The sharelog of a height you solved is the
per-share record behind a block you were paid for and has no second copy.

The index is persistent by design, not by accident: ckpool.log has a bounded
window — ckpool never rotates it, and this job prunes rotated copies after
`ROTATED_LOG_DAYS` — so a solve from months ago is no longer greppable anywhere.
An index rebuilt from scratch each run would silently shrink, and the next prune
would take exactly the directories the protection exists for. The index is
refreshed from *every* `ckpool.log*` including rotated copies, and always before
those copies are deleted.

**Verify protection on the DEPLOYED copy, not the one in your checkout.** The
protection lives in the script, so it exists only where that script has actually
been updated — a host still carrying a pre-protection copy is unprotected no
matter what this repo says. "Upstream fixed it" is not a deployment.

Run the path in the unit's `ExecStart=`, not `./clean-old-blocks.sh`. Note that
`--value` returns systemd's structured record, not a bare path — it looks like
`{ path=/usr/local/bin/foo ; argv[]=… ; ignore_errors=no ; … }` — so extract the
`path=` field rather than taking the first word:

```bash
P=$(systemctl show ckpool-logprune -p ExecStart --value | sed -n 's/.*path=\([^ ;]*\).*/\1/p')
"$P" --dry-run
```

**How you verify depends on whether the pool has ever solved a block.**

*On a pool with at least one solve* — the dry-run is real evidence. Protection is
live only if the output names the solved height:

```
PROTECTED (solved): 000ebfa9
```

No `PROTECTED` line here means you are running a pre-protection copy. Pull,
re-verify, and only then enable the timer.

*On a pool that has never solved* — **the dry-run proves nothing.** A protected
script and a pre-protection one emit byte-identical output when there is no solve
to protect, so a clean run is not a passing check, merely an uninformative one.
Confirm the deployed file's provenance instead: that it is the version you
intended to install, and that it implements a solve index at all. A check that
could not have failed is not evidence.

Deployed as:

```bash
sudo install -m 0644 ckpool-logprune.timer /etc/systemd/system/
sudo install -m 0644 ckpool-logprune.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ckpool-logprune.timer
```

Check status with `systemctl status ckpool-logprune.timer` and
`journalctl -u ckpool-logprune`.

## Build

**One direct dependency** — `github.com/go-zeromq/zmq4`, which the notifier uses
to subscribe to the node's block ZMQ feed — plus three indirects. All of it is
**vendored**, so `-mod=vendor` builds with no network and no module proxy.
(The HTTP server itself is still standard-library-only; the dependency arrived
with `notifier/`.)

```bash
cd api
go build -mod=vendor -ldflags="-s -w" -o ckpool-api .
```

Binaries are **not** committed — they are built on demand and copied to the pool
host.

### Cross-compiling is the practical path

The obvious advice would be "build on the target OS so the artifact is exercised
where it runs". **That is not currently available for the pool host**: Ubuntu
24.04 ships Go **1.22**, `go.mod` requires **1.26.1**, and an in-tree build there
fails on the version directive. Since Go 1.21 the default `GOTOOLCHAIN=auto`
would silently fetch the newer toolchain — but that needs proxy access the pool
host does not have, so it fails rather than rescuing the build. Either upgrade
the host toolchain from <https://go.dev/dl/>, or cross-compile:

```bash
cd api
gofmt -l . && go vet ./... && go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath \
    -ldflags="-s -w" -o ckpool-api-linux-amd64 .
```

`CGO_ENABLED=0` is safe despite `goczmq/v4` appearing in `go.mod`: it sits behind
the `czmq4` build tag and is never compiled, so the result is a **statically
linked** ELF with no libc or libzmq dependency on the pool host. Swap
`GOARCH=arm64` for an ARM host.

> **Checksum both ends.** The link to the pool host runs around 10 KB/s, so a
> 10 MB binary takes roughly 15 minutes and a truncated transfer is a live
> failure mode, not a theoretical one. `sha256sum` locally and on the host and
> compare before you install anything.

```bash
sha256sum ckpool-api-linux-amd64
scp ckpool-api-linux-amd64 solo:/tmp/ckpool-api
ssh solo 'sha256sum /tmp/ckpool-api'   # must match before proceeding
```

Keep the outgoing binary as a dated backup so a rollback is a `mv` and a
restart, never a rebuild:

```bash
ssh solo 'cp ~/ckpool-api/ckpool-api ~/ckpool-api/ckpool-api.bak.$(date +%Y%m%d) \
          && install -m 0755 /tmp/ckpool-api ~/ckpool-api/ckpool-api \
          && sudo systemctl restart ckpool-api'
```

Restarting `ckpool-api` does **not** touch `ckpool` itself — miners stay
connected and no shares are lost. Confirm that after any deploy by checking that
the stratum unit's `ActiveEnterTimestamp` is unchanged.

## Deploying

Prefer the release binary over building on the pool host.

```bash
# 1. Binary
sudo install -d -m 0755 /opt/ckpool-api
sudo install -m 0755 ckpool-api-linux-amd64 /opt/ckpool-api/ckpool-api

# 2. Key, root-only
sudo install -d -m 0755 /etc/ckpool-api
printf 'CKPOOL_API_KEY=%s\n' "$(openssl rand -hex 32)" \
    | sudo tee /etc/ckpool-api/ckpool-api.env >/dev/null
sudo chmod 0600 /etc/ckpool-api/ckpool-api.env

# 3. Point it at the pool's logs
sudo tee -a /etc/ckpool-api/ckpool-api.env >/dev/null <<'EOF'
CKPOOL_LOG_PATH=/home/ckpool/ckpool/logs/ckpool.log
CKPOOL_USER_LOGS_PATH=/home/ckpool/ckpool/logs/users
CKPOOL_API_PORT=8888
EOF

# 4. Unit — edit User=, WorkingDirectory= and ExecStart= first
sudo cp ckpool-api.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ckpool-api
```

Two things that reliably go wrong:

- **Run it as the user that runs ckpool.** ckpool creates its log directory mode
  `0750` (`src/ckpool.c`), so any other account gets `permission denied` on
  every endpoint while `/health` still answers.
- **Do not expose 8888 to the internet.** The key is the only authentication and
  it travels in cleartext over HTTP. Scope it in the firewall to the consuming
  application's address, exactly as the stratum host already does.

## Relationship to the Python server

`skaisser/ckpool_api` holds the original Python implementation, still installed
by `install-ckpool-api.sh` in the BlockSniper application repository. This Go
service supersedes it: one static binary, no interpreter, far lower latency and
memory, plus `/coinbase`, `/user-file` and `/metrics`, which Python never had.
