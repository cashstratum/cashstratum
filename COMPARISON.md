# blocksniper-ckpool vs asicseer-pool — code-level comparison

**Date:** 2026-08-29 · **Subject commit:** `be1ca841` (`homolog`) · **Method:** source-only. No claim in this document rests on a README, a changelog, or a commit message; every line reference was read in the tree it names.

---

## 0. Repository identity — read this first

The comparison request used the name "elo" for this fork and assumed `asicsteer`/`elostratum` might be the same lineage. They are not. Establishing this changes what "port a patch" even means.

| Local path | GitHub | Upstream lineage | Role |
|---|---|---|---|
| `infra/blocksniper-ckpool` **(this repo)** | `skaisser/blocksniper-ckpool` | **ckpool** (ckolivas) | "**ELO**" below. Rebranded to **BlockSniper.ai** in 2026 (formerly EloPool, `1b634e50`, `70eede6e`). Production runs on **solo.blocksniper.ai**; `beast` is the homelab test/build host. |
| `infra/asicsteer` | `skaisser/asicsteer` | **asicseer-pool** (cculianu) | "**SEER**" below |
| `infra/elostratum` | `skaisser/elostratum` | **asicseer-pool** (cculianu) | Same lineage as asicsteer; stopped + disabled on beast 2026-08-29 |
| `infra/ckpool` | `skaisser/ckpool` | ckpool | The public mirror of this fork, not an upstream |

`asicsteer` and `elostratum` share asicseer-pool's root commit (`fe2e464e Add license`); 3831 vs 3842 commits. **Despite the name, `elostratum` is the asicseer-derived pool, not the "elo" of this report.**

Two pristine baselines were cloned for neutral comparison:

- **`CKP_UP`** — `bitbucket.org/ckolivas/ckpool` — the true upstream of ELO.
- **`SEER_UP`** — `github.com/cculianu/asicseer-pool` — verified byte-identical to `asicsteer` in every area examined, so `asicsteer` carries no fork-local changes in the compared code.

The **`CKP_UP` column is the most important one in this document.** ELO forked ckpool years ago. Several defects attributed to "our fork" are inherited verbatim, and several ELO-local changes are genuine improvements over upstream. Conflating the two leads to porting the wrong patches.

**Deployment context** (from `beast`, live): BCHN 29.1.0 mainnet, single node, `ckpool -B -L` — **btcsolo mode**. Proxy/node/redirector modes are not in use, and `-B` is mutually exclusive with `proxy` (`src/ckpool.c:1639`). Live config: `mindiff 1`, `startdiff 42`, `maxdiff 0`, `blockpoll 50`, `update_interval 15`, `poolfee 1.0`, `btcd[0].notify = true` with `zmqnotify` set.

---

## 1. Verdict on the original report

The report that prompted this review made one claim. It is **factually correct and materially misattributed**, and it is not close to the most serious problem in this codebase.

| Claim | Status |
|---|---|
| SEER uses `GENWORK_MAX_MERKLE_DEPTH 32`; ELO hardcodes `[16]` | ✅ Correct — `SEER src/stratifier.h:22,72-73` vs `ELO src/stratifier.h:56-57` |
| SEER bounds-checks the merkle loop, ELO does not | ✅ Correct — `SEER src/stratifier.c:1942` vs `ELO src/stratifier.c:1421-1440` |
| "**The one real bug I found in your fork**" | ❌ **Wrong on both counts.** It is not *in* our fork — `CKP_UP src/stratifier.h:56-57` is byte-identical, so it is inherited from upstream ckpool. And it is not the *one* bug: it is roughly the fifth most serious finding here. |

**The finding that matters is not in that report at all: ELO's share-acceptance predicate was rewritten locally and inflates every share, hashrate, and payout number the pool produces, by a factor any miner chooses. It is live on beast now.** See §3.

---

## 2. Severity summary

Ordered by operational urgency for the live deployment, not by report section.

| # | Finding | Area | Provenance | Live? | Severity |
|---|---|---|---|---|---|
| 1 | Shares accepted at pool `mindiff` but credited at client `diff` | 2 | **ELO-introduced** | **YES** | **bug** |
| 2 | `mindiff_overrides` substring-matches the workername and sets an unclamped diff | 3 | **ELO-introduced** | **YES** | **bug** |
| 3 | Password `d=` parser: unanchored match + unclamped `suggest_diff` | 3 | **ELO-introduced** | **YES** | **bug** |
| 4 | Inline `live_server()` parks stratifier threads for a whole bitcoind outage | 5 | **ELO-introduced** | **YES** | **risk** |
| 5 | ZMQ `zmq_poll(..., -1)` with no idle detection or socket rebuild | 5 | ELO-introduced (multi-endpoint) | **YES** | **risk** |
| 6 | Merkle depth `[16]`, no bounds check — overflow at ≥65,536 txns | 1 | inherited (upstream has since fixed) | latent | **bug** |
| 7 | `alloca(txns*32+64)` on an 8 MB thread stack | 1 | inherited (upstream now bounds it) | latent | **risk** |
| 8 | `update_notify()` unbounded `strcpy` into `merklehash[i][68]` | 1 | inherited | **no** (proxy-only) | **bug** |
| 9 | Unbounded GBT `coinbaseaux.flags` → invalid blocks, then heap overflow | 4 | inherited (upstream has since fixed) | latent | **risk** |
| 10 | `address_to_txn()` returning 0 stored unchecked → burned block reward | 4 | ELO-introduced | latent | **risk** |
| 11 | Failed `getblockchaininfo` silently defaults to mainnet prefix | 5 | ELO-introduced | latent | **risk** |

"Live?" means reachable in btcsolo mode on the current beast config.

---

## 3. Area 2 — Share validation ⚠️ **THE HEADLINE**

### 3.1 Acceptance gated on the wrong difficulty

```c
/* ELO src/stratifier.c:6465 */
if (sdiff >= ckp->mindiff) {          /* pool-wide floor — live value: 1 */
```
```c
/* CKP_UP src/stratifier.c:7826  AND  SEER src/stratifier.c:7337 — identical */
if (sdiff >= diff) {                  /* the client's ASSIGNED difficulty */
```

A share is accepted whenever its real difficulty is ≥ 1. It is then credited at `diff` — the client's *assigned* difficulty — through `add_submit(ckp, client, diff, result, submit)` (`:6505`), which feeds:

- `worker->shares += diff`, `user->shares += diff` (`:5847-5849`)
- `ckp_sdata->stats.unaccounted_diff_shares += diff` (`:5840`)
- `decay_client()` / `decay_worker()` / `decay_user()` — every `dsps1/5/60/1440` counter (`:5866-5872`)
- the sharelog record: `json_set_double(val, "diff", diff)` (`:6510`) — **which is what the `GET /shares` endpoint serves**

Inflation factor is `client->diff / sdiff`, and the client controls `client->diff`.

**Provenance:** ELO-introduced by `bf4b72e8` ("fix: CRITICAL - correct share validation logic") and `c3216d85` ("perf: only reject shares below pool mindiff"). Present on `homolog` **and** `master`.

**The stated rationale does not hold.** The comment claims the change "ensures we never throw away valid work that could find blocks." But `test_blocksolve()` runs unconditionally at `:6186` — its own comment reads *"Test we haven't solved a block regardless of share status"* — and `out_submit:` already forces `submit = true` for any `sdiff >= network_diff` (`:6444-6448`). Block detection and submission were never coupled to the accept/reject decision. The change bought nothing and cost accounting integrity.

**Secondary effect:** `new_share()` now runs for every above-mindiff share, so the `sdata->shares` dupe hashtable grows by `client->diff / mindiff` — with `mindiff 1`, about five orders of magnitude — and is only purged on a new block (`:1148`). That is a memory and `share_lock` contention amplifier.

**Why btcsolo makes it worse, not better:** in proxy mode an upstream pool would cross-check the share stream. In btcsolo all accounting is local and terminal. Nothing downstream can detect the inflation.

- **Verdict for ELO:** worse · **Severity:** bug
- **Patch:** `src/stratifier.c:6465` → `if (sdiff >= diff)`; revert `SE_LOW_DIFF` / `"Below minimum difficulty"` to `SE_HIGH_DIFF` / `"Above target"` in `src/libckpool.h:281,300`. If low-diff shares must still be *observed*, use the existing `check_best_diff()` branch at `:6404-6411`, which runs before the accept decision — never `add_submit()`.

### 3.2 The delivery vehicles

Two ELO-local paths let a client raise its own `client->diff` with no proof of work at that difficulty. Either one, combined with §3.1, is a complete exploit.

**(a) `mindiff_overrides` substring-matches the workername** — `src/stratifier.c:5672-5689`:

```c
json_object_foreach(ckp->mindiff_overrides, pattern, diff_val) {
        if (strcasestr(client->workername, pattern)) {      /* :5677 substring, case-insensitive */
                client->suggest_diff = override_diff;       /* :5681 UNCLAMPED */
                client->diff = client->old_diff = override_diff;  /* :5683 */
```

`client->workername` is `<bchaddress>.<name>` and is fully attacker-chosen. The **live** config carries `MiningRigRentals: 1000000`, `nicehash: 500000`, `stratum-proxy: 10000`. So on beast, right now, any miner authorizing as `<their-address>.MiningRigRentals` is assigned diff 1,000,000, and — by §3.1 — is credited **1,000,000 per difficulty-1 share**. No custom tooling; a worker name is the entire exploit.

The code already documents a production collision from this same mechanism (`:5663-5667`: *"a short override key (e.g. \"s9\") can substring-match inside the address"*). The symptom was patched for rental clients via `client->rental_diff`; the mechanism was left in place. Note also that a cashaddr is base32 over `qpzry9x8gf2tvdw0s3jn54khce6mua7l` — `test`, `bitaxe` and similar short keys can collide inside an ordinary address by accident.

SEER keys `mindiff_overrides` on the **useragent prefix**, not the workername, and validates the value. Upstream ckpool has no such feature.

**(b) Password `d=` parser** — `src/stratifier.c:5695-5723`:

```c
const char *diff_str = strstr(pass, "diff=");
if (!diff_str) diff_str = strstr(pass, "d=");     /* :5699 UNANCHORED */
...
client->suggest_diff = password_diff;             /* :5710 written BEFORE the clamp */
if (ckp->mindiff && password_diff < ckp->mindiff) password_diff = ckp->mindiff;
if (ckp->maxdiff && password_diff > ckp->maxdiff) password_diff = ckp->maxdiff;   /* :5713-5716 */
client->diff = client->old_diff = password_diff;  /* :5719 clamped value */
```

Two defects. The `strstr(pass, "d=")` fallback has no delimiter check, so `worker_id=5`, `pwd=x`, `uuid=…` are all parsed as difficulty requests. And `client->suggest_diff` is stored **before** the clamp, which only mutates the local variable — so `suggest_diff` escapes `maxdiff` entirely.

That matters because `suggest_diff` is the vardiff floor: `src/stratifier.c:5914-5915` does `if (client->suggest_diff) mindiff = client->suggest_diff;`. A client sending `-p d=999999999` pins its own floor above the pool's configured ceiling permanently.

Neither upstream ckpool nor SEER parses difficulty from the password.

- **Verdict for ELO:** worse · **Severity:** bug (both)
- **Patch:** (a) key `mindiff_overrides` on useragent as SEER does, or at minimum require an exact match on the post-`.` worker segment and clamp the value to `[mindiff, maxdiff]`. (b) require a delimiter before `d=` (start of string, or after `,`/`;`/whitespace), and move the `client->suggest_diff` assignment to *after* the clamp.

### 3.3 Where ELO is already correct — do not "fix" toward SEER

**Diff-change grace window.** ELO uses `diff = MIN(diff, client->old_diff)` (`:6455-6456`, inherited from `CKP_UP`); SEER uses `diff = client->old_diff` unconditionally (`SEER:7334`). ELO/upstream is the more forgiving and more correct form. **Verdict: better. Do not port SEER's.**

---

## 4. Area 3 — Vardiff

| | ELO | SEER |
|---|---|---|
| Retarget trigger | 240 s / 72 shares, with a `dsps1` fast path | 30 s / 9 shares, `dsps5` only |
| Target ratio | `drr` 0.15–0.4 hysteresis; `optimal = dsps * 3.33`, or `* 2.4` when a client mindiff is set (`:5919-5925`) | same shape, different cadence |
| `mindiff_overrides` key | **workername substring** (`:5677`) | **useragent prefix** |
| Override value clamped? | **no** (`:5681`) | yes |

ELO's slower cadence is a deliberate, defensible choice for a solo pool. The keying and clamping of `mindiff_overrides` are not — see §3.2(a).

`mining.suggest_difficulty` (`ELO:6751-6778`) is equal to SEER and to ELO's merge-base upstream; both narrow to `json_is_integer`, so current upstream's `isfinite()` hardening (`CKP_UP:8100-8107`) is unreachable in either fork. **Verdict: equal · nice-to-have.**

---

## 5. Area 1 — Block template limits

### 5.1 Merkle depth (the originally reported bug)

```c
/* ELO src/stratifier.h:55-58 — identical to CKP_UP src/stratifier.h:55-57 */
int merkles;
char merklehash[16][68];      /* 1088 B */
char merklebin[16][32];       /*  512 B */
json_t *merkle_array;         /* <-- first field clobbered */
```

`binleft = txns + 1`; the loop at `:1421-1440` runs `ceil(log2(binleft))` times writing indices `0..n-1` with **no bounds check**. Index 16 is written when **`txns ≥ 65,536`**.

What happens then is specific and immediate. `memcpy(&wb->merklebin[16][0], hashbin+32, 32)` (`:1426`) writes 32 bytes of SHA-256 output across `merkle_array` (8) + `coinb1` (8) + `coinb1bin` (8) + `coinb1len` (4) + 4 bytes of `enonce1const` — the arrays are 8-aligned multiples of 8, so there is no padding to absorb it. Two lines later `json_array_append_new(wb->merkle_array, …)` (`:1428`) dereferences that hash-valued pointer inside jansson. `__bin2hex()` at `:1427` additionally writes 65 bytes over `merklebin[0..2]`, corrupting already-emitted branch entries.

**Reachability is real for BCH and only for BCH.** A 32 MB block at ~200 B/tx holds ~160,000 transactions. Upstream ckpool is BTC, where blocks cap out around 10–15k txns and 65,536 is unreachable — which is exactly why upstream never noticed, and why SEER (a BCH fork) fixed it.

SEER: `#define GENWORK_MAX_MERKLE_DEPTH 32` (`:22`), warn+break in the producer (`:1942-1946`), a defensive `&& i < GENWORK_MAX_MERKLE_DEPTH` clamp in `share_diff` (`:2668`), warn+clamp on the proxy notify path (`:3805-3808`).

**Upstream has since fixed this too — but their fix is wrong for us:**

```c
/* CKP_UP src/stratifier.c:420, 1457-1461 */
#define MAX_GBT_TXNS 65535
if (unlikely(wb->txns < 0 || wb->txns > MAX_GBT_TXNS)) {
        LOGWARNING("Invalid transaction count %d in wb_merkle_bin_txns, ignoring transactions");
        wb->txns = 0;                 /* falls through as an EMPTY template */
}
```

On BTC, discarding a >65,535-txn template costs nothing because it cannot happen. On BCH it would silently mine an **empty block** on exactly the busy blocks where fees are highest. **Do not port the upstream guard. Port SEER's approach** — raise the depth and heap-allocate — because on BCH those templates are legitimate.

- **Verdict for ELO:** worse than SEER, worse than current upstream, **equal to its own merge-base** · **Severity:** bug

### 5.2 The `alloca` — a second, independent bug in the same function

| | ELO `:1363-1373` | SEER `:1874-1885` |
|---|---|---|
| scratch buffer | `alloca(binlen + 32)` — **stack** | `ckalloc(binlen + 32L)` — heap, `free()`d at `:1966` |
| index arithmetic | `int` | `long` |
| txn cap | none | none (relies on 64-bit + depth 32) |

`wb_merkle_bin_txns` runs on a ckmsgq worker thread (`create_ckmsgq(ckp, "updater", &block_update)`, `:8948`). `create_pthread()` passes `NULL` for the attr (`src/libckpool.c:69-75`) and **no `pthread_attr_setstacksize` exists anywhere in `src/`** — so the thread gets the glibc default, 8 MB.

`alloca(txns*32 + 64)` therefore consumes ~5.1 MB (64% of the stack) on a typical 32 MB BCH block, ~6.8 MB (85%) on a block of minimal-size transactions, and overruns outright above ~250k txns or on any node configured past 32 MB. `alloca` cannot fail or return NULL, and a single guard page does not reliably catch a multi-megabyte jump — this is a stack-clash overrun, not a clean SIGSEGV.

The merkle overflow (§5.1) fires first at 65,536 txns, but both need fixing.

- **Verdict for ELO:** worse · **Severity:** risk
- **Patch:** adopt SEER's `ckalloc`/`free` + `long` arithmetic verbatim.

### 5.3 `update_notify()` — unbounded `strcpy` (proxy mode only)

```c
/* ELO src/stratifier.c:3030-3034 */
wb->merkles = json_array_size(wb->merkle_array);            /* unbounded count */
for (i = 0; i < wb->merkles; i++)
        strcpy(&wb->merklehash[i][0], json_string_value(json_array_get(wb->merkle_array, i)));
```

Three defects, all driven by the upstream pool in proxy mode: no cap on `i`; no NULL check (`json_string_value()` returns NULL for any non-string element → `strcpy(dst, NULL)`); and **no length check** on the element. The length hole is independent of the count hole — a single-element array carrying one long string walks `strcpy` through all 1600 bytes of both arrays and into the pointer fields. Fixing the count fixes nothing here.

`add_node_base()` has the same shape: `json_intcpy(&wb->merkles, val, "merkles")` (`:1831`) with no range check, from a trusted-remote workbase.

**SEER has this bug too** — its clamp at `:3805-3808` bounds only `i`; `SEER:3810-3813` does the same unvalidated `strcpy`. **Only upstream fixes it properly**, rejecting anything that is not exactly 64 hex chars before copying (`CKP_UP:3501-3505`). That is the site to port from.

**Not reachable on beast** — btcsolo excludes proxy mode (`src/ckpool.c:1639`). Latent.

- **Verdict for ELO:** worse than upstream, equal to SEER · **Severity:** bug (latent)

### 5.4 Max transactions / block size

Neither fork caps transaction count or block size, and both correctly defer the size limit to the node's GBT response — the right posture for a stratum layer. **Do not add a block-size knob.**

ELO's `int` arithmetic in `txn_data` (`:1388`) and `txn_hashes` (`wb->txns * 65 + 1`) overflows only at ~2 GB templates. SEER is 64-bit throughout and caches `wb->txn_data_len` (`:1936`), which lets its `process_block()` size the submission buffer in one `ckzalloc` instead of ELO's `realloc_strcat()` + repeated 64 MB `strlen`s on the block-submission critical path (`ELO:2016-2061`) — where latency is orphan risk.

ELO is **better** on one point: an explicit `cblen > (sizeof(hexcoinbase)-1)/2` reject at `:2029-2033` that SEER lacks structurally.

- **Verdict:** equal on policy, worse on serialisation latency, better on the `hexcoinbase` guard · **Severity:** nice-to-have

---

## 6. Area 4 — Coinbase construction

### 6.1 Output ordering and multi-output

ELO inherits ckpool's 2-output splice model (count byte in `coinb2bin`, generation value at the end of `coinb2`, fee output in `coinb3`): `[0]` generation, `[1]` pool-operator fee. SEER builds a real output list via `add_output_()` into a growable buffer with a back-patched CompactSize: pool fee → 2 dev donations → up to 150 PPLNS payouts → change → an `OP_RETURN` carrying the extranonce.

ELO renamed upstream's `donation`/`dontxnbin` to `poolfee`/`pooladdress`/`pooltxnbin` and **removed ckpool's dev donation entirely** (`grep DONATION src/*.c` → zero hits).

**The output-count byte was audited across all three branches (`:627`, `:631`, `:634`) and is consistent in every case — there is no off-by-one and no malformed coinbase.** `insert_witness` is hard-false for BCH (`:1482`), so the witness term never fires.

- **Verdict:** worse in capability (2 outputs vs ~154), **equal in correctness** · **Severity:** nice-to-have · **No patch required.** SEER's model cannot be bolted on without rewriting `__user_coinb2()` (`:6060-6104`) and the coinb1/2/3 contract.

### 6.2 Dust — and the one path that can burn a block reward

ELO has exactly one dust check, and it is ELO-local and correct: `if (d64 < 546)` (`:623`) drops the fee output, restores `g64`, and writes count `1`. ELO also **fixed a latent upstream footgun** — `CKP_UP:702` permanently zeroes `ckpool.donation` on the dust branch, so one small round disabled the fee forever; ELO gates that on `!poolfee_dust` (`:655-664`). ELO's `poolfee` clamp to `[0,50]` (`src/ckpool.c:1464-1474`) additionally prevents the unsigned underflow a `poolfee > 100` typo causes upstream.

**The real exposure is elsewhere.** `bch_address_to_script()` returns `0` on `BCH_ADDR_INVALID` (`src/libckpool.c:1832-1834`), and both call sites store it unchecked:

```c
sdata->txnlen     = address_to_txn(sdata->txnbin, ckp->btcaddress, …);      /* :8917 */
sdata->pooltxnlen = address_to_txn(sdata->pooltxnbin, ckp->pooladdress, …); /* :8923 */
```

`generator_checkaddr()` asks **bitcoind** whether the address is valid; `bch_address_to_script()` re-classifies it **locally**. Any address the node accepts but our parser does not classify — a token-aware or non-P2PKH/P2SH cashaddr type byte, for instance — yields `txnlen == 0`, and the coinbase carries an output with a `0x00` script length and no script: structurally valid, consensus-valid, and **permanently unspendable**. The full block reward is burned, silently.

Not triggered by the current P2PKH payout address. It is a one-line guard protecting a full block reward.

- **Verdict:** better than upstream, worse than SEER (which guards every output on a non-zero `scriptlen`) · **Severity:** risk
- **Patch:** reject `txnlen < 1` fatally for `btcaddress`; for `pooladdress`, warn and disable the fee. Apply the same to the per-user solo path (`:5503-5508`, `:7161-7163`).

### 6.3 scriptSig length — ELO is safe, but not for the reason it looks

**An over-long `btcsig` cannot produce a >100-byte scriptSig.** The budget:

```
scriptSig = 29 + flagslen + siglen          ⇒  siglen ≤ 71 - flagslen
```

`btcsig` is truncated to 38 bytes at config parse (`src/ckpool.c:1476-1478`) — **inherited verbatim** from `CKP_UP:1734-1736`. Worst case is a 67-byte scriptSig. Removing upstream's 7-byte `"\x0a" "ckpool"` prefix (`:586`) made ELO *safer*, widening the budget from 64 to 71. ELO also cannot underrun the 2-byte minimum.

**But `flagslen` is unbounded and node-controlled**, and that is a genuine gap:

```c
/* ELO src/bitcoin.c:152-154 — no length check, no validhex check */
flags = json_string_value(json_object_get(coinbase_aux, "flags"));
/* ELO src/stratifier.c:545-546 */
wb->coinb1  = ckzalloc(256);
wb->coinb1bin = ckzalloc(128);      /* "Set fixed length coinb1 arrays to be more than enough" */
/* ELO src/stratifier.c:560-563 */
len = strlen(wb->flags) / 2;
hex2bin(wb->coinb1bin + ofs, wb->flags, len);
```

At `flagslen ≥ ~33` the scriptSig exceeds 100 bytes and **every block found is rejected by the node, with no warning**. At `flagslen ≥ 70` `hex2bin`/`__bin2hex` overrun the 128/256-byte heap buffers.

**Upstream fixed this and ELO's fork predates it:** `CKP_UP src/bitcoin.h:19` defines `MAX_GBT_FLAGS_LEN 32`, enforced at parse time with a hex check (`CKP_UP src/bitcoin.c:250`) and again defensively in the builder (`CKP_UP:621-627`). `grep -rn MAX_GBT_FLAGS_LEN` over ELO returns nothing.

BCHN returns an empty `coinbaseaux`, so `flagslen == 0` today and this is latent, reachable only via a hostile or misbehaving node. SEER is structurally immune: it keeps the extranonce in an `OP_RETURN` output rather than the scriptSig, caps sigs at `MAX_USER_COINBASE_LEN 96`, computes `MIN(siglen, spaceLeft)` against the real remaining budget, and hard-`quit`s if the result still exceeds `MAX_COINBASE_SCRIPTSIG_LEN 100`.

- **Verdict:** worse than SEER, and — uniquely in this area — **worse than current upstream** · **Severity:** risk
- **Patch:** backport `MAX_GBT_FLAGS_LEN` from `CKP_UP` (both the parse-time reject and the builder re-clamp), and add a terminal assertion after `:600` that the scriptSig is within `2..100`.

### 6.4 Incidental: fork-point marker

ELO emits `nSequence = 0xffffffff` (`:605`) and `nLockTime = 0` (`:669`). Current upstream emits `0xfffffffe` and `nLockTime = height - 1` per BIP54 (`CKP_UP:672`, `:714-716`) — a **Bitcoin-only** consensus change with no BCH equivalent. **ELO's values are correct for BCH.** This is the same fork-point gap that explains the missing flags clamp.

---

## 7. Area 5 — bitcoind failover and ZMQ

### 7.1 Inline `live_server()` parks the stratifier ⚠️ live

ELO moved failover from asynchronous to **synchronous, on the caller's thread**:

```c
/* ELO src/generator.c:938-946 (generator_getbase); same at :971-979, :436-446 */
si->alive = cs->alive = false;
si = live_server(ckp, gdata);       /* <-- called on a STRATIFIER thread */
if (si) reconnect_generator(ckp);
```

`live_server()` never gives up:

```c
/* ELO src/generator.c:352-354 */
LOGWARNING("CRITICAL: No bitcoinds active!");
sleep(5);
goto retry;
```

Upstream and SEER mark the node dead, post an async `reconnect_generator()`, and return in microseconds — letting the generator thread, whose job is to block on reconnect, own the wait. ELO-introduced by `54421344` / `7fc91a0a`; `c8bb9841` already reverted the same treatment for `generator_checkaddr`.

**On this single-node deployment, a routine `systemctl restart bitcoind-bch` parks the entire work-update machinery** — updater worker, ZMQ notifier, block poller — for the whole outage. It does recover once bitcoind answers, but nothing is serviceable meanwhile and the only diagnostic is a 5-second `CRITICAL` heartbeat. With one btcd there is nothing to fail over *to*, so the latency benefit the commit was chasing does not exist here.

Second hazard: `server_alive()` does `extract_sockaddr()` / `dealloc(cs->auth)` (`:270-283`) **outside** the `connsock_t.sem` documented as serialising request/response (`src/ckpool.h:89-90`), while the generator thread may be formatting `cs->auth` into an HTTP header. SEER flags this exact hazard in-source (`SEER src/generator.c:207`) and avoids it by never calling `live_server()` off the generator thread.

- **Verdict:** worse · **Severity:** risk + latent use-after-free
- **Patch:** revert the three inline calls to the upstream async shape; address the original latency complaint with a two-line `!si->alive` early return in `generator_getbest()`/`getbase()`; keep `live_server()` reachable only from `gen_loop()` and `server_watchdog()`.

### 7.2 ZMQ ⚠️ live

ELO's multi-endpoint ZMQ is **ELO-introduced** (`e7591ca4`) and is genuinely better than upstream in one respect worth keeping: `ZMQ_SUBSCRIBE` with topic length **9** actually filters on `hashblock`, whereas both `CKP_UP:10393` and `SEER:5537` pass length 0 (subscribe-to-everything), where a 32-byte `hashtx` frame would fire a spurious `update_base`. **Keep ELO's 9. Do not copy SEER's 0.**

Where ELO is worse — and this is the "ZMQ dies but RPC stays up" case:

1. **`zmq_poll(poll_items, num_endpoints, -1)` (`:8814`) never times out.** libzmq's auto-reconnect covers a plain bitcoind restart over `tcp://`, but not a half-open socket surviving a host network reset, an endpoint rebound to another port, or a replaced `ipc://` inode. In those cases ELO goes **permanently silent with no log line at all**. SEER has a bounded poll plus an idle detector that tears down and rebuilds every socket (`SEER:5662-5673`); ELO has no equivalent.

2. **The live config hits the bad branch.** ELO reads `notify` and `zmqnotify` as *independent* keys (`src/ckpool.c:1276-1283`); SEER *derives* `notify` from ZMQ presence (`SEER src/asicseer-pool.c:2224-2229`). The live beast config has **`btcd[0].notify = true`** alongside `zmqnotify`. So `generator_getbest()` short-circuits at `src/generator.c:963-966`, `blockupdate()` hits `case GETBEST_NOTIFY: cksleep_ms(5000)` (`:4686-4688`) and **never polls**. With ZMQ silent, new-block detection falls back to the periodic template refresh — `update_interval`, live value **15 s**. That is up to 15 seconds mining on a stale block after every block, with no alarm. Setting `notify: false` restores the 50 ms `blockpoll` fallback.

3. **All-endpoints-fail spin.** On setup failure ELO leaves `subscribers[i] = NULL` and a zeroed `poll_items[i]` (`:8784-8804`); if every endpoint fails, `zmq_poll(items, N, -1)` has nothing to wait on and returns immediately — a hot spin on one core, silently. Upstream `quit(1)`s; SEER backs off 90 s then `quit(1)`s.

4. **`zmq_msg_more()` after `zmq_msg_close()`** (`:8865-8866`) — reads a struct closed on the previous line. **Inherited** (`CKP_UP:10434-10435`); SEER restructured it.

- **Verdict:** better than upstream, worse than SEER · **Severity:** risk + bug

### 7.3 Startup validation

ELO validates **more than upstream, less than SEER**. The premise that asicseer added checks upstream lacks is confirmed in source.

ELO-introduced and genuinely good: `detect_cashaddr_prefix()` (`:218-255`), and fatal aborts when `bchaddress` is missing (`src/ckpool.c:1787-1790`). That second one is load-bearing here — **upstream silently adopts a donation address in btcsolo mode when none is configured** (`CKP_UP src/generator.c:298-308`); ELO stripped that in `34d4430e`. An operator typo can no longer route block rewards to an upstream donation address.

Two gaps that bite this deployment:

1. **A missing or mistyped `-zmqpubhashblock` on the node is never detected.** `zmq_connect` is asynchronous and succeeds regardless, so ELO logs a cheerful `ZMQ connected to endpoint` (`:8806`) and sits in `zmq_poll` forever. `getzmqnotifications` appears nowhere in ELO. SEER checks protocol and port against what the node actually publishes and names the mismatch (`SEER src/bitcoin.c:469-540`). Combined with §7.2(2), this is a permanently 15-s-stale pool with a log claiming ZMQ is fine.

2. **A failed `getblockchaininfo` silently defaults to mainnet.** `detect_cashaddr_prefix()` warns and falls through to `bitcoincash` (`:225-228`, `:233-236`, `:245-247`), runs **once** (`:306`), and keeps the result for the process lifetime. A plausible systemd startup race — ckpool connecting while BCHN is still loading the block index — would classify payout addresses against the wrong network for as long as the process lives. SEER treats this as fatal (`SEER src/stratifier.c:9974-9977`).

**ELO is better than SEER on fail-back:** ELO's `server_watchdog` compares against the live `current_si` (`:3348-3374`, byte-identical to `CKP_UP`), whereas SEER compares against a stale-pinned `gdata->si` (`SEER:3181`) and after a second failure never fails back at all. **Do not port `gdata->si`.**

- **Verdict:** worse than SEER, better than upstream · **Severity:** risk

---

## 8. Area 6 — Config options

**All three codebases ignore unknown keys silently.** None iterates the config object to detect unrecognised members; a typo is accepted with no warning and zero effect. SEER's only exception is a hard `quit(1)` on the two obsolete names `btcaddress`/`btcsig` (`SEER src/asicseer-pool.c:1812-1815`).

### 8.1 In SEER, not in ELO (candidate ports)

| key | SEER | What it does | ELO verdict | Severity |
|---|---|---|---|---|
| `blocking_timeout` | `asicseer-pool.c:1896`, enforced `connector.c:774-778` | Disconnects a client whose socket has been write-blocked > N s (default 60). Without it a wedged peer holds an fd + client slot indefinitely. | worse | **risk** |
| btcd `zmq`/`zmqpubhashblock` aliases | `asicseer-pool.c:1505-1507` | Config-compat spellings | worse (minor) | nice-to-have |
| btcd zmq → auto-`notify` | `asicseer-pool.c:2224-2226` | Derives `notify` from ZMQ presence — see §7.2(2) | worse | **risk** |
| `fee_discounts` | `asicseer-pool.c:1869` | Per-username fee discount | worse | nice-to-have |
| `bchsig` as array | `asicseer-pool.c:1820` | Rotating coinbase sigs | worse (minor) | nice-to-have |
| `disable_dev_donation` | `asicseer-pool.c:1891` | Opt out of dev donation | **n/a — ELO has no dev donation at all** | — |

### 8.2 In ELO, not in SEER

`pooladdress` / `poolfee` (operator fee with dual-output split, `src/ckpool.c:1462-1473`) — **better**. `highdiff`, and the per-btcd `zmqnotify`.

### 8.3 ELO-local additions vs upstream

`bchaddress` (BCH rename, `btcaddress` kept as a deprecating alias, `:1458-1461`), `pooladdress`, `poolfee` + clamp, `mindiff_overrides`, per-btcd `zmqnotify`.

**Upstream keys ELO does NOT accept** — copying these from an upstream config produces no error and no effect: `dropidle`, `maxsendqueue`, `reconnect` (the operationally meaningful ones), plus `passthroughserver`, `maxusers`, `maxsubclients`, `donation`, `ipcmining`, and the `sv2*` cluster (StratumV2 — irrelevant to BCH).

### 8.4 Silently-ignored keys

**`log_shares` is confirmed not a config key in any of the three** — share logging is the `-L` CLI flag only (ELO: set `src/ckpool.c:1676`, consumed `src/stratifier.c:1103,1110,6534`). The operator's understanding is correct.

**ELO has zero orphan keys** — every key in every shipped `.conf` is parsed. The reverse gap exists instead: four parsed keys are documented in no sample config — **`highdiff`** (default 1,000,000, silently applied to high-port clients), **`mindiff_overrides`**, **`maxclients`**, and btcd-entry **`zmqnotify`**.

SEER ships two orphans, both fork-local to `asicsteer`: `log_shares` and `asicboost`, neither parsed anywhere.

---

## 9. Area 7 — Test coverage

**ELO is decisively ahead, and the gap is qualitative.**

**ELO** — `test/addrclassify.c` exercises the production address path (the code deciding where block rewards go) with independently derived ground truth and adversarial single-character typo vectors. `regtest-e2e.sh` is a genuine end-to-end money gate: real node, real stratum handshake, coinbase decoded back off-chain, satoshi-exact split arithmetic via `assert_split()`, plus *negative* wire assertions (no `mining.notify` to a rejected client, no spurious "Failed over").

**SEER** — seven unit cases, all library primitives (hex round-trip, one base58 address, SHA digest vectors). They test crypto plumbing and nothing about pool behaviour, are `OFF` by default, and are unregistered with CTest. Its two "regtest" scripts are interactive operator menus that assert nothing.

**ELO's real weakness is cadence, not content:** `release-gate.yml` fires only on `v*` tags and manual dispatch, so a regression merged to `homolog` sits undetected until release day.

**Zero coverage in ELO:** template building, **share validation**, **vardiff / `mindiff_overrides` / password `d=` parsing**, failover, all non-solo modes, config parsing. Coinbase is the one well-covered area.

Note that findings §3.1, §3.2 and §3.3 all sit in the two areas with zero tests.

- **Verdict:** better · **Patch:** add a cheap `push`/`pull_request` CI job running `make -C test check` (seconds, no BCHN download); leave the regtest on the tag trigger.

---

## 10. Prioritized patches to port into ELO

### Tier 0 — fix now, live and exploitable

1. **Restore the share predicate.** `src/stratifier.c:6465` → `if (sdiff >= diff)`; revert `SE_LOW_DIFF`/`"Below minimum difficulty"` → `SE_HIGH_DIFF`/`"Above target"` (`src/libckpool.h:281,300`). Reverts `bf4b72e8` + `c3216d85`. *(§3.1)*
2. **Fix `mindiff_overrides`.** Key on useragent as SEER does, or require an exact match on the post-`.` worker segment; clamp the value to `[mindiff, maxdiff]`. `src/stratifier.c:5672-5689`. **Until this ships, consider removing `MiningRigRentals`/`nicehash`/`stratum-proxy` from the live config** — those three keys are the highest-value exploit strings. *(§3.2a)*
3. **Fix the password `d=` parser.** Require a delimiter before the token; move `client->suggest_diff =` after the clamp. `src/stratifier.c:5699-5716`. *(§3.2b)*

### Tier 1 — operational robustness, live

4. **Revert inline `live_server()`** to async `reconnect_generator()`. `src/generator.c:938-946, 971-979, 436-446`. *(§7.1)*
5. **Bound the ZMQ poll + port SEER's idle socket-rebuild.** `src/stratifier.c:8814`; port `SEER:5662-5673`. Converts silent permanent ZMQ death into a ≤2 min self-heal with a log line. *(§7.2)*
6. **Derive `notify` from ZMQ presence and index-align `btcdzmq`.** Port `SEER src/asicseer-pool.c:2224-2229`. **Interim mitigation available today: set `btcd[0].notify = false` in the live config** to restore the 50 ms polling fallback. *(§7.2)*
7. **Port the `getzmqnotifications` startup check** (`SEER src/bitcoin.c:469-540`) into `server_alive()`. *(§7.3)*
8. **Make a failed `getblockchaininfo` fatal or retrying**, never a mainnet default. `src/generator.c:225-247`. *(§7.3)*

### Tier 2 — latent memory-safety

9. **Merkle depth → 32 + bounds check.** Port `GENWORK_MAX_MERKLE_DEPTH` and the warn+break from SEER. **Do not port upstream's `MAX_GBT_TXNS` clamp — it mines empty blocks on BCH.** *(§5.1)*
10. **`alloca` → `ckalloc`/`free`, `int` → `long`.** Port `SEER:1883`/`:1966`. *(§5.2)*
11. **Backport `MAX_GBT_FLAGS_LEN 32`** from `CKP_UP src/bitcoin.h:19` + `bitcoin.c:250` + `stratifier.c:621-627`, and assert the scriptSig is in `2..100`. *(§6.3)*
12. **Reject `txnlen < 1`** from `address_to_txn()` at `:8917`/`:8923` and the solo paths. *(§6.2)*
13. **Validate merkle elements in `update_notify()`** — port `CKP_UP:3501-3505` (exactly 64 hex chars), not SEER's count-only clamp. Proxy-only; do it if proxy mode is ever enabled. *(§5.3)*

### Tier 3 — quality

14. `blocking_timeout` (`SEER connector.c:774-778`). 15. `current_si_lock` (`SEER generator.c:187,293-295,328-334`). 16. Fix `zmq_msg_more()` after `zmq_msg_close()` (`:8865-8866`). 17. CI job on push/PR running `make -C test check`. 18. Document `highdiff`, `mindiff_overrides`, `maxclients`, `zmqnotify` in the sample config. 19. SEER's exact-sized `process_block()` for BCH-size blocks.

### Do **not** port

- Upstream's `MAX_GBT_TXNS` clamp — mines empty blocks on BCH *(§5.1)*
- SEER's `gdata->si` fail-back comparison — ELO's `current_si` form is correct *(§7.3)*
- SEER's `ZMQ_SUBSCRIBE` topic length 0 — ELO's 9 is correct *(§7.2)*
- SEER's `diff = client->old_diff` grace window — ELO's `MIN()` is more correct *(§3.3)*
- SEER's multi-output coinbase model — not portable without rewriting the coinb1/2/3 contract *(§6.1)*

---

## 11. ELO deviations from upstream ckpool that are or could be bugs

Answering the request's final question directly.

| Deviation | Site | Assessment |
|---|---|---|
| Share acceptance on `ckp->mindiff` instead of `diff` | `stratifier.c:6465` | **Bug.** The most serious finding in this review. |
| `mindiff_overrides` on workername substring, unclamped | `stratifier.c:5672-5689` | **Bug.** No upstream equivalent. |
| Password `d=` parsing, unanchored + unclamped | `stratifier.c:5695-5723` | **Bug.** No upstream equivalent. |
| Inline `live_server()` in RPC helpers | `generator.c:938-946, 971-979, 436-446` | **Risk.** Indefinite stall + unsynchronised free. |
| `detect_cashaddr_prefix()` defaults to mainnet on RPC failure | `generator.c:225-247` | **Risk.** Wrong-network address classification. |
| `address_to_txn()` result stored unchecked | `stratifier.c:8917, 8923` | **Risk.** Silent burned block reward. |
| Multi-endpoint ZMQ without idle detection | `stratifier.c:8750-8884` | **Risk.** Silent permanent ZMQ death. |
| Fork-point gaps: no `MAX_GBT_FLAGS_LEN`, no `MAX_GBT_TXNS` | `bitcoin.c:152`, `stratifier.c:1370` | **Risk.** Upstream fixed both after our fork point. |
| **Removal of upstream's silent donation-address fallback** | `34d4430e` | **Improvement.** Load-bearing for btcsolo. |
| **`poolfee` dust guard + `[0,50]` clamp** | `stratifier.c:623`, `ckpool.c:1464-1474` | **Improvement.** Fixes an upstream permanent-disable bug and an underflow. |
| **`hexcoinbase` overflow guard** | `stratifier.c:2029-2033` | **Improvement.** SEER lacks it. |
| **`ZMQ_SUBSCRIBE` topic length 9** | `stratifier.c:8800` | **Improvement.** Both upstream and SEER subscribe to everything. |
| **Removal of the 7-byte `"ckpool"` scriptSig prefix** | `stratifier.c:586` | **Improvement.** +7 bytes of scriptSig headroom. |
| `nSequence`/`nLockTime` unchanged vs upstream's BIP54 values | `stratifier.c:605, 669` | **Correct for BCH.** Not a bug. |

---

## Appendix — detailed working notes

Full per-area investigation records, including material not summarised above:

- `area1.md` — block template limits
- `area23.md` — share validation and vardiff (reject table with btcsolo reachability per condition)
- `area4-rest.md` — coinbase (full byte arithmetic, branch-by-branch output-count audit)
- `area5.md` — failover and ZMQ
- `area67.md` — config and test coverage

Baselines: `ckpool-upstream` (bitbucket.org/ckolivas/ckpool), `asicseer-upstream` (github.com/cculianu/asicseer-pool).
