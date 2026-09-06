# Changelog

Notable changes to this fork. Newest first.

Entries record **what changed and what evidence backs it** — a fix with no observation behind it
says so rather than implying one.

---

## Unreleased — 2026-09-05

The upstream ckpool 1.2.0 rebase, six fixes from the 2026-09-04 rejected-shares incident, and
this repo's first pull-request CI. 34 commits.

### Security

- **Closed an out-of-bounds read reachable before authentication.** `cashaddr_decode_checked()`
  indexed `CHARSET_REV[128]` with a raw byte, so any byte ≥ 0x80 in a username read past the
  array. Proven against the *unfixed* file under AddressSanitizer —
  `global-buffer-overflow ... READ of size 1 ... 0 bytes after global variable 'CHARSET_REV'` —
  rather than inferred from the code. A test that only passes after a fix proves nothing about a
  memory error; ASAN on the old build is what showed the read was real.
- **Closed log injection on the same pre-auth path.** Three `LOGDEBUG` sites printed a raw
  client-supplied username, letting a client write CR to overwrite the line just logged, LF to
  forge whole entries, or a terminal escape that acts on the operator's terminal when the log is
  `cat`'d. Added `sanitise_addr()`: non-printable bytes become `?`, length bounded to 64.
- **Stopped the solo path guessing a coinbase.** When a client submitted a share against a
  workbase it had no per-user coinbase for, the code spliced in the *pool's own* payout script
  and validated against that — so a share was scored against a coinbase the miner never saw. The
  share is now rejected with `No user coinbase`. Reproduced on both builds with a temporary
  `getenv()` hook: the old build mislabelled it `"Above target"` with
  `sdiff 4.7e-10 ≈ 2⁻³¹`, the uniform-random-hash signature seen in the incident's rejected
  shares.
- **Guarded `__generate_userwb()` against a zero-length payout script.** With `user->txnlen == 0`
  it still wrote the length byte, emitting an output paying *nothing*. `generate_userwbs()`
  already guarded this; the two single-user call sites did not.

### Fixed

- **A third `mining.set_difficulty` in the subscribe → authorize window.** Measured on the live
  pool, then on the fix: **3 → 2**, last-announced value byte-identical. The review called this a
  regression from the port; diffing against *current* upstream rather than the fork point showed
  two of the three sends are upstream's and only the rental-motivated third is ours.
- **`live_server()` no longer runs inline on the stratifier thread.** A/B under a real `bitcoind`
  outage: pre-fix **0** `update_base` retries in 45 s across 4 distinct 5 s-loop phases (threads
  piled up inside `live_server()`); post-fix **10 retries + 2 give-ups** in 1 phase, updater back
  on its 30 s cadence.
- **Version-mask rejections now reach the log.** `SE_INVALID_VERSION_MASK` `goto out`s before the
  sharelog record is built, so these rejections appeared in **no** sink. Two investigations had
  cited "zero such records in 39,805 sharelog rows" as evidence clients were not sending
  out-of-mask bits — a corpus that structurally could not contain them.

### Added

- **Coinbase finality check before block submit** (`src/coinbase_final.{c,h}`, `test/cbfinal`,
  20 vectors). **Diagnostic only**: on a non-final coinbase it logs `LOGEMERG` and *still
  submits*, deliberately deviating from "refuse", because a bug in the predicate must never cost
  a real block.
- **`version_mask` in the `/shares` API**, emitted as a string to match the sharelog.
- **Pull-request CI** (`.github/workflows/pr-gate.yml`) — the repo previously had **none**;
  `release-gate.yml` fires only on tags. Build + `make check` + `go test` on GitHub-hosted
  runners, and the full regtest money gate on a self-hosted runner on beast. Split because it was
  measured: the e2e was **killed at 28m20s by the 30-minute job timeout** on a hosted runner and
  takes **~6–7 min on beast** — CPU mining against 2–4 cores versus 36.
- `api/` to CI at all. `make check` never reached it, so the whole Go module was untested while
  the gate showed green.

### Changed

- **Rebased the C tree onto upstream ckpool 1.2.0**, then re-applied each BCH feature as its own
  reviewable commit: cashaddr identity, address validation, the pool-fee dual coinbase output,
  BCHN chain detection and RPC failover, rental difficulty floors, finder attribution and
  per-user luck.
- **`install-ckproxy.sh` now builds `--enable-sv2` and installs libsodium.** It clones *upstream*
  ckpool, not this fork, so the "SV2 is Bitcoin-only, we are BCH" reasoning behind
  `--disable-sv2` elsewhere does not apply. Its package list never installed libsodium, so
  `enable_sv2=auto` always resolved to **no** while the script prompted for an SV2 upstream URL it
  could never serve. Verified on clean Ubuntu 24.04 and Fedora containers.
- **Reconciled master's MRR-Hash hotfix** (`1418ab7b`) into `homolog`. It conflicted in four
  `stratifier.c` hunks after the rebase; all resolved to `homolog`, which is a strict functional
  superset — both the `mrr-hash` useragent match and the `rental_diff` floor on `d=` survive,
  plus the globalisation, `PRId64`, and a configurable MRR default that the hotfix hardcoded.

### Test reliability

Two assertions in the money gate were failing on **provably unchanged binaries** — found only
once the e2e started running automatically on every PR:

- `stratum_probe()` sent `mining.authorize` without waiting for the subscribe reply. Stratum
  requires subscribe-first and ckpool enforces it, so the pool correctly answered
  `"Failed subscription"`. The test client was violating the protocol.
- Scenario 8 asserted the finder's post-solve `bestshare` equals the winning share's diff. It
  resets to 0 and is then set by whatever share arrives next — a random draw — so it failed in
  **both** directions (7.415 vs 1.072; 2.735 vs 10.472). A replacement `bestever >= bestshare`
  "high-water mark" assertion was tried and is **also wrong**: `best_ever` is `int64`
  (`stratifier.c:6217`) while `bestshare` is a double, so any fractional part breaks it
  (4.9005 vs 4). The value assertion is now **removed**. This is a **coverage reduction**, stated
  plainly: the reset itself remains asserted by `roundshares` dropping to a computed ceiling and
  `bestever` surviving non-zero, which are the properties the scenario exists to test.

### Known issues

Tracked, pending resolution.

- The finality check is bypassed by the node (`stratifier.c:2623`) and remote
  (`:9419`) block paths. Diagnostic-only everywhere, so the gap is a missing `LOGEMERG`, not a
  missing safety gate.
- Solo `mining.notify` is sent before the authorise response.
- `startdiff != 0` is an unenforced invariant that `sauth_process()` now depends on.
- `reconnect` messages accumulate while `gen_loop()` is blocked during a long outage.
- Scenario 10 (`block was found within timeout`) failed once and has not been diagnosed.

### Not verified

- No deployment. Every result above is from beast (Ubuntu 24.04, gcc 13.3) or CI, not from the
  production pool.
- The SV2 guard in `renotify_solo_client()` is argued from `stratum_broadcast()`'s identical
  guard, not observed: every build path here pins `--disable-sv2`.
- The atomic throttle is correct by construction; no test drives concurrent `sprocessor` threads
  through it.
- The `--enable-sv2` ckproxy change was verified by container builds, not by a full end-to-end
  install on a clean host.
