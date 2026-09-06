# Share / hashrate inflation via client-controlled difficulty

**Discovered:** 2026-08-29 · **Status:** patched on `homolog`, **not yet deployed** · **Severity:** critical
**Affects:** `blocksniper-ckpool` on `homolog` and `master`, from `bf4b72e8` (2026) to `be1ca841`
**Introduced by:** `bf4b72e8` "🐛 fix: CRITICAL - correct share validation logic" and `c3216d85` "⚡ perf: only reject shares below pool mindiff"
**Not present in:** upstream ckpool (ckolivas), asicseer-pool (cculianu), `asicsteer`, `elostratum`

---

## 1. Summary

Any miner that can connect to the stratum port can inflate its recorded share count and
hashrate by an arbitrary, self-chosen factor — up to **1,000,000×** using only the
difficulty values already present in the live configuration, and higher with a custom
password. The attack requires no special tooling: **a worker name is sufficient.**

Every downstream consumer is affected: per-worker and per-user hashrate, the pool
hashmeter, the sharelog on disk, and the `GET /shares` API.

Block *finding* is unaffected — a real block still requires real work. What is corrupted is
the accounting of who contributed, which is what a payout scheme would be computed from.

---

## 2. Root cause

Two independent defects that compose.

### 2.1 Acceptance is checked against the wrong difficulty

`src/stratifier.c:6465` (pre-patch):

```c
if (sdiff >= ckp->mindiff) {        /* pool-wide floor. Live value: 1 */
```

Upstream ckpool (`src/stratifier.c:7826`) and asicseer (`src/stratifier.c:7337`) both use:

```c
if (sdiff >= diff) {                /* the difficulty THIS CLIENT was assigned */
```

`sdiff` is the share's real difficulty; `diff` is what the pool told the client to mine at.
With `mindiff = 1`, our predicate accepts essentially **every** share a miner can produce.

### 2.2 …but credit is given at the assigned difficulty

`src/stratifier.c:6505` calls `add_submit(ckp, client, diff, result, submit)` — passing
`diff`, not `sdiff`. Inside (`src/stratifier.c:5838-5872`):

```c
ckp_sdata->stats.unaccounted_diff_shares += diff;
worker->shares += diff;
user->shares   += diff;
decay_client(client, diff, &now_t);
decay_worker(worker, diff, &now_t);
decay_user(user, diff, &now_t);
```

and the sharelog record at `:6510` writes `json_set_double(val, "diff", diff)`.

**The inflation factor is `client->diff / sdiff`, and the client controls `client->diff`.**

### 2.3 Why the original change was unnecessary

The commit rationale was *"never throw away valid work that could find blocks."* The code
does not support this:

- `test_blocksolve()` is called unconditionally at `src/stratifier.c:6186`, under its own
  comment *"Test we haven't solved a block regardless of share status."*
- `out_submit:` already forces `submit = true` for any `sdiff >= network_diff`
  (`src/stratifier.c:6444-6448`).

Block detection and upstream submission were never coupled to the accept/reject decision.
The change bought nothing and cost accounting integrity.

---

## 3. How an attacker does it

### Path A — worker name only (no custom software, works against the live config)

The live pool config contains:

```json
"mindiff_overrides": {
  "nicehash": 500000, "NiceHash": 500000,
  "MiningRigRentals": 1000000, "miningrigrentals": 1000000,
  "stratum-proxy": 10000, "bitaxe": 1000, "test": 1
}
```

`src/stratifier.c:5672-5689` (pre-patch) matched these as a **case-insensitive substring of
the full workername**, which is attacker-supplied, and applied the value **unclamped**:

```c
json_object_foreach(ckp->mindiff_overrides, pattern, diff_val) {
        if (strcasestr(client->workername, pattern)) {      /* substring of "<address>.<worker>" */
                client->suggest_diff = override_diff;        /* unclamped */
                client->diff = client->old_diff = override_diff;
```

So the attack is one line of miner configuration:

```
-u <attacker-bch-address>.MiningRigRentals -p x
```

1. `mining.authorize` arrives with that workername.
2. `strcasestr()` matches `MiningRigRentals` → `client->diff = 1000000`.
3. The miner ignores the assigned difficulty and submits difficulty-1 shares — the pool
   never required proof at 1,000,000.
4. Each share passes `sdiff (1) >= ckp->mindiff (1)`.
5. Each share is credited `diff` = **1,000,000**.

Result: **1,000,000× inflation** for 1/1,000,000th of the work, using a value the operator
put in the config for a legitimate purpose.

Note the same mechanism collides accidentally: a cashaddr is base32 over
`qpzry9x8gf2tvdw0s3jn54khce6mua7l`, so short keys like `test` or `bitaxe` can appear inside
an ordinary payout address by chance. The code already documented a production collision of
exactly this kind (`src/stratifier.c:5663-5667`, the `"s9"` case) — the symptom was patched
for rental clients, the mechanism was left in place.

### Path B — stratum password (unbounded, bypasses `maxdiff`)

`src/stratifier.c:5695-5723` (pre-patch) parsed `d=` / `diff=` from the password and stored
`client->suggest_diff` **before** applying the mindiff/maxdiff clamp — and the clamp only
mutated a local variable:

```c
client->suggest_diff = password_diff;                                  /* :5710 unclamped */
if (ckp->maxdiff && password_diff > ckp->maxdiff)
        password_diff = ckp->maxdiff;                                  /* :5715 local only */
client->diff = client->old_diff = password_diff;                       /* :5719 clamped */
```

`suggest_diff` is the vardiff floor (`src/stratifier.c:5914-5915`:
`if (client->suggest_diff) mindiff = client->suggest_diff;`), so it is re-applied on every
retarget and escapes `maxdiff` permanently:

```
-u <attacker-address>.rig -p d=999999999
```

Additionally, the `d=` probe used an unanchored `strstr(pass, "d=")`, so ordinary passwords
containing `worker_id=5`, `pwd=x` or `uuid=…` silently reassigned a client's difficulty.

### Path C — `mining.suggest_difficulty`

`src/stratifier.c:6763` accepts a client-suggested difficulty directly. Same outcome.

### Self-amplification

Vardiff retargets on `dsps` counters that are themselves computed from the inflated `diff`
(`decay_client()` above). Submitting many trivial shares raises the measured rate, which
raises the assigned difficulty, which raises the credit per share. The loop reinforces
itself even without Paths A–C.

---

## 4. Impact

| Consumer | Effect |
|---|---|
| `worker->shares`, `user->shares` | Inflated by the attacker's chosen factor |
| `client->dsps1/5/60/1440`, pool hashmeter | Inflated; reported hashrate is fiction |
| Sharelog `diff` field (`logs/<height>/`) | **Already-written logs are contaminated** for any period a client used a high assigned diff |
| `GET /shares` API | Serves the contaminated `diff` straight through |
| Block finding | **Not affected** — a real block still needs real work |

Because the pool runs `btcsolo` (`ckpool -B`), all accounting is local and terminal: there is
no upstream pool cross-checking the share stream, so nothing else would have caught this.

**Historical data:** any sharelog record whose `diff` greatly exceeds its `sdiff` is suspect.
That comparison is the detection query — both fields are written to every record
(`src/stratifier.c:6510-6511`), so contamination is measurable after the fact rather than
merely suspected.

---

## 5. The fix

Three changes on `homolog`, all in `src/stratifier.c` plus the error enum in `src/libckpool.h`.

**Fix 1 — restore the upstream predicate (this alone closes the vulnerability).**

```c
- if (sdiff >= ckp->mindiff) {
+ if (sdiff >= diff) {
```

plus the reject branch back to `SE_HIGH_DIFF` / `"Above target"` (`src/libckpool.h:281,300`).

With this in place, a client that requests difficulty 1,000,000 must actually produce shares
meeting difficulty 1,000,000 in order to be credited 1,000,000. Paths A, B and C stop being
exploits and become what they were meant to be: difficulty *requests*.

**Fix 2 — `mindiff_overrides` hardening (defence in depth).** Match only the worker-name
segment after the first `.` or `_`, never the address, and clamp the override into
`[mindiff, maxdiff]`.

**Fix 3 — password parser hardening (defence in depth).** Require `d=` to be a whole token
(start of string, or after `,` `;` space or tab), and store `client->suggest_diff` only
*after* the clamp so it can no longer exceed `maxdiff`.

Fixes 2 and 3 are not what stops the attack — Fix 1 is. They remove the ability to pin a
difficulty outside the operator's configured bounds, which is a correctness bug in its own
right.

---

## 6. Deployment

1. Build and restart `ckpool.service` on beast from the patched tree.
2. **Expect a visible drop in reported pool hashrate after the restart.** That drop is the
   fiction being removed, not a regression. The honest baseline is whatever remains.
3. Watch for a rise in `Above target` rejects. Miners that were mining below their assigned
   difficulty will now be rejected — which is correct, and is what every other ckpool does.
4. Verify with a known-good miner that shares are still accepted at the assigned difficulty
   before declaring success.

**Interim mitigation if the deploy must wait:** remove `MiningRigRentals`,
`miningrigrentals`, `nicehash`, `NiceHash` and `stratum-proxy` from `mindiff_overrides` in
the live `ckpool.conf` and restart. That removes the high-value Path A strings. It does
**not** close Paths B or C — only the code fix does that.
