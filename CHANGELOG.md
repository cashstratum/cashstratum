# Changelog

Notable changes to this fork. Newest first.

Entries record **what changed and what evidence backs it** — a fix with no observation behind it
says so rather than implying one.

---

## 1.2.2 — 2026-10-06

Adds BCHD and Flowee the Hub as supported nodes and refuses block templates without a usable
`coinbasevalue`. Behaviour on BCHN is unchanged.

### Added

- **BCHD and Flowee the Hub as the node.** Contributed by
  [@CyberAshven](https://github.com/CyberAshven), who runs CashStratum in a StartOS package where
  the operator picks the node. Every JSON-RPC request now carries an `id`: BCHD treats a request
  without one as a notification and never answers it. `getblocktemplate` no longer asks for the
  `coinbasetxn` capability, which makes BCHD answer with a coinbase transaction instead of
  `coinbasevalue` (or an error when it has no `--miningaddr`). `coinbaseaux` is optional, as
  BIP22 allows, because Flowee the Hub omits it. Chain detection recognises BCHD's network names
  (`mainnet`, `testnet3`). BCHN, which ignores the capability list and always sends
  `coinbaseaux`, parses to the same template as before. See
  [Supported nodes](docs/installation.md#supported-nodes).

### Security

- **A template without `coinbasevalue` is refused.** The missing field used to be read as zero,
  so a node answering with `coinbasetxn` alone (BCHD started with `--miningaddr` and asked for
  that capability) would have produced work whose coinbase paid nothing. Covered by
  `test/nodecompat.c`, which fails 17 checks against the previous request strings and parser.

---

## 1.2.1 — 2026-10-06

Security hardening of share accounting, difficulty handling, client framing and large BCH
templates, plus two operator fixes. Details and the finding-by-finding disposition are in
[`docs/security-hardening.md`](docs/security-hardening.md).

### Security

- **Difficulty requests honour configured bounds.** `mining.suggest_difficulty` and rental
  defaults could exceed `maxdiff`; password parsing accepted partial numbers and an embedded
  `diff=`. Requests are now clamped to the configured range and password tokens must be complete
  numbers.
- **Rejected and repeated shares no longer inflate rate estimates.** Rejected SV2 submissions and
  repeated stale SV1 submissions could feed hashrate accounting. Eligible rate credit is now
  separated from rejection handling and upstream forwarding; block detection and accepted
  latency-grace work are unchanged.
- **Share logs are append-only.** Writing by filename truncated the file despite an open append
  stream. Writes go through the append stream and write/close failures are checked.
- **Malformed client frames are rejected.** Parsing past a newline could accept a malformed
  frame or dispatch one request twice. The connector now parses exactly one complete frame,
  handles EOF, and keeps its buffer on resize failure.
- **Proxy merkle branches are validated.** Hexadecimal encoding is checked and branches share
  the common capacity.
- **Empty payout scripts are refused** at startup and when registering an address-based remote
  user, instead of producing work that pays nothing.

### Fixed

- **Large BCH templates keep every transaction.** A 65,535-transaction cap silently dropped the
  rest of a template. Merkle construction is now heap-backed with 32-level branches across the
  template and proxy paths, and malformed templates are rejected instead of replaced with empty
  work. `test/merklebranch` checks reconstructed branches against independently computed trees
  at 65,535, 65,536 and 300,000 transactions.
- **Chain detection fails closed.** An RPC failure or unknown chain used to select mainnet
  silently; the pool now retries until it identifies a supported chain.
- **Block notifications recover on their own.** Notification mode gains a five-second RPC
  tip-polling backstop, bounded ZMQ polling and failed-socket recovery.
- **`disableproxy` on a parent proxy takes its subproxies down.** Previously only the parent
  socket closed: subproxies kept serving and the stratifier kept recruiting more, so the active
  proxy never moved. A disabled parent now drops its subproxies and stops recruiting. Verified
  with two upstreams: `disableproxy {"id":0}` switched the active proxy to 1 in the same second
  and the served coinbase changed; `enableproxy {"id":0}` switched back.
- **Monitor and cleanup scripts find the install directory.** `monitor.sh`,
  `clean-old-blocks.sh` and the monitor written by `post-install.sh` guessed the directory from
  `$HOME`, which failed for installs under `/opt/cashstratum` or when run as another user. They
  now resolve from the script's own location (following symlinks) and pick the first candidate
  that holds a pool log. `CASHSTRATUM_DIR` and `CKPOOL_DIR` still take precedence.
  `post-install.sh` installs the home-directory shortcut as a symlink instead of a copy.

### Tests

- New regressions under `testing/` for connector framing, difficulty policy, network-prefix
  selection, notification recovery, remote payout registration and share accounting, plus
  `stratum_diffprobe.py`.
- BCH regtest gate: 89/89 assertions, including real payout blocks and difficulty
  rejection/clamping. All C unit tests pass on a clean Ubuntu build with SV2 enabled.

---

## 1.2.0 — 2026-09-13

First public CashStratum release. The version follows the upstream ckpool 1.2.0 rebase this tree
is built on (`configure.ac` has declared 1.2.0 since that rebase), so the shipped source and the
development tags agree.

### Release

- **Public source tree.** The C mining engine, Go operator API and notifier, installers, unit
  tests, the regtest money gate and the mirrored block proofs under `docs/proofs/` ship together.
  Deployment configuration, hostnames and operator-only runbooks do not.
- **Documentation scrubbed for publication.** Example addresses use RFC 5737 documentation
  ranges, example usernames are neutral, and comments no longer name private hosts or internal
  CI runs. No runtime behaviour changed for this.

### Since the 2026-09-05 development snapshot

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
  runners, and the full regtest money gate on a self-hosted runner. Split because it was
  measured: the e2e was **killed at 28m20s by the 30-minute job timeout** on a hosted runner and
  takes **~6–7 min on the self-hosted runner** — CPU mining against 2–4 cores versus 36.
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

- No deployment. Every result above is from a development host (Ubuntu 24.04, gcc 13.3) or CI, not from the
  production pool.
- The SV2 guard in `renotify_solo_client()` is argued from `stratum_broadcast()`'s identical
  guard, not observed: every build path here pins `--disable-sv2`.
- The atomic throttle is correct by construction; no test drives concurrent `sprocessor` threads
  through it.
- The `--enable-sv2` ckproxy change was verified by container builds, not by a full end-to-end
  install on a clean host.
