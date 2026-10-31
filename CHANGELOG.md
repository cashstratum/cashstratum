# Changelog

Notable changes to this fork. Newest first.

Entries record **what changed and what evidence backs it** — a fix with no observation behind it
says so rather than implying one.

---

## Unreleased

## 1.3.1 — 2026-10-31

Adds `cashtop`, a read-only terminal dashboard, plus `monitor.sh` improvements and new block
proofs. **The stratum server is unchanged:** nothing under `src/` or the C build differs from
1.3.0, so a running 1.3.0 pool needs no rebuild or restart. Rebuild the API directory (or re-run
the installer) to get `cashtop`.

- **`cashtop`, a terminal dashboard for the pool host.** A Go program (`api/cmd/cashtop`) that reads
  the pool's own log, `pool.status`, user files and sharelogs, plus the node's RPC.
  - **Read-only by construction.** It never writes inside the pool install. It calls only an
    allowlist of read-only node RPC methods, and a disallowed method fails before any network I/O.
    It keeps a small index under `$XDG_CACHE_HOME/cashtop/`, so restarts resume without rescanning
    the log.
  - **Tabs:** Dashboard (hashrate, round effort and odds, the Block Meter, best shares since the
    last block, recent blocks), Workers, Blocks (history with rewards, plus the sharelog behind each
    block), Feed (the `monitor.sh` view) and Network. An address view shows one miner's live
    submits and its share of the work in each round.
  - **On a block:** a full-screen celebration, the terminal bell and a desktop notification where
    the terminal supports one. `--quiet` turns these off; `--replay-last-block` shows it again.
  - **Health:** the dashboard flags a stopped pool (no log activity for 150s, or nothing listening
    on the configured port), shows the age of the last network block (red past 10 minutes) and an
    arrow on difficulty changes.
  - **Rewards** come from the block's coinbase transaction alone, so large blocks don't hit the
    RPC response cap.
  - **Install:** `install-cashstratum.sh` builds it next to the API when Go is present.
    `post-install.sh` links it as `/usr/local/bin/cashtop` and `~/cashtop`, so `cashtop` runs from
    any directory. An existing non-symlink file at `/usr/local/bin/cashtop` is left alone.
  - **Try it without a pool:** `cd api && go run ./cmd/cashtop --demo=showcase`. The README
    screenshots come from that demo data.
  - **Evidence:** unit, golden and fuzz tests under `api/cmd/cashtop/` and the new shared packages
    (`go test ./...`), a parity test of its feed rules against `monitor.sh`, and the installer
    regression. It ran on a production pool host through block 975301: rewards, stale detection,
    the cache across restarts and the block celebration.
- **Shared Go packages.** The log tailer, ZMQ subscriber, block-event parsing and node RPC client
  moved out of the notifier into `api/internal/`, and the API's response types into
  `api/internal/apitypes`. Notifier webhook payloads are byte-identical on the existing fixtures.
- `monitor.sh` shows round luck on every miner row and on the POOL row, e.g. `luck 1.63%`.
  - It is the pool's own `luck` field: work since the last block found, as a percent of network
    difficulty, so 100% is one expected block of work.
  - Older logs that carry no `luck` field show none.
- **`monitor.sh` rejoins split log lines.** The pool writes any message over 510 bytes as several
  lines, stamped up to a millisecond apart. A user's stats line has passed that size since 1.3.0, so
  the filtered view printed its tail as a raw JSON fragment (`…"lastrejectreason":"",…`) every
  minute. Fields cut into that tail, such as `rejectedcount`, were also invisible to the miner row.
  A join stage now glues the pieces back together before any other rule runs.
  - **Evidence:** a new `testing/monitor_regression.py` case splits a user line at 510 bytes, 1 ms
    apart, with `rejectedcount` in the tail. One formatted row comes out, showing `rej 3`. On a live
    v1.3.0 pool following the log (mawk 1.3.4), no fragments printed after the change.
- **Proofs:** 17 additional mainnet block proofs (heights 972548 → 975301) are mirrored under
  `docs/proofs/`, bringing the verified total to 98.

## 1.3.0 — 2026-10-30

Adds per-user and per-worker reject statistics, a cap on the difficulty credited to rejected shares,
a recorded reason for every client disconnect, and a filtered `monitor.sh`. The accepted-share path
is unchanged. The cap changes what the pool-wide `rejected` counter reports: read the second bullet
before upgrading dashboards.

- **Per-user and per-worker reject statistics.** Each user object in `logs/users/<address>` and each
  `worker[]` entry gains `rejected` (summed credited difficulty), `rejectedcount`,
  `rejects{stale,duplicate,lowdiff,ntime,badjob,malformed,worker}`, `lastreject`,
  `lastrejectreason` and `rejectrate1m`/`rejectrate5m`/`rejectrate1hr` (hashrate format).
  - Every SV1 reject reason is counted, plus the SV2 reject returns, including reasons the
    pool-wide counter never saw.
  - The counters persist across restarts; older files load as zero.
  - The sum of `rejected` over all users can exceed the pool-wide `rejected`, because the per-user
    counters see reasons the pool-wide one does not.
  - Documented in `api/README.md` under "Reject statistics on /user-file".
- **Behaviour change: reject difficulty is capped.** A rejected share is credited at the
  connection's pool-assigned starting difficulty until the connection has an accepted share, and
  afterwards at most the highest difficulty it has actually met. Counters saturate instead of
  overflowing. The cap applies to the pool `rejected` in `pool.status` and the `Pool:` log line, and
  to the new per-worker counters. Ports upstream CKPool `47a61470` and `e9b66549`.
  - Consequence: a miner that ignores the assigned difficulty no longer inflates `rejected` by the
    difficulty it asked for, so graphs of the pool `rejected` counter grow much more slowly after
    upgrading.
  - Alert on `rejectedcount` and on the reject percentage, not on summed difficulty.
- **Disconnect reasons.** `Dropped client …` log lines now end with
  ` reason=<reason>[ errno=<n>] age=<seconds>`; everything before that suffix is unchanged.
  Reasons are lower-case names, among them `peer_eof`, `peer_reset`, `epollhup`, `epollrdhup`,
  `epollerr`, `bad_json`, `msg_oversize`, `buf_overload`, `sv2_proto`, `sv2_encrypt`,
  `not_subscribed`, `role_refused`, `write_err`, `blocked_60s`, `sendq_limit`,
  `pre_session_idle`, `auth_timeout`, `idle_drop`, `invalid_shares`, `upstream_reject`,
  `auth_rejected_upstream`, `reconnect_ignored`, `dropall`, `mining_term`, `smsg_err` and `other`.
  - Each `worker[]` entry gains `connects`, `disconnects`, `lastdisconnect` and
    `lastdisconnectreason`. A pool-wide dropall is not counted as a disconnect.
- **API.** `/shares` now fills `reject-reason` from the share log's `error` field; it was always
  null before. `api/README.md` now documents the real route table, the typed `/shares` fields and
  the new `/user-file` keys. An absent key means an older pool, not zero. The API must be rebuilt
  to pick up the `/shares` change.
- **Build:** clean builds with and without `--enable-sv2` produce zero compiler warnings. Three
  dead declarations were removed: an unused CashAddr encoding table, an SV2 debug buffer outside
  Cap'n Proto builds, and an SV2-only variable in non-SV2 builds.
- **Tests:**
  - Regtest e2e scenario 15 (`testing/reject_stats_regression.py`) checks the reject counters,
    buckets, `Dropped` reasons and connect counts.
  - New unit test `test/reject_stats.c`.
  - The code-extracting regression harnesses were updated for the new counters.
  - The Stratum V2 Noise handshake is now checked against an independent initiator built on the
    reference implementation's `noise_sv2` crate (2.0.0), with a negative control that must reject
    a version 1 certificate. It runs as an opt-in automake test (`--enable-sv2-interop`), in the
    clean-host harness, and as a hosted CI job.
- **Proofs:** 16 additional mainnet block proofs (heights 972548 → 975091) are mirrored under
  `docs/proofs/`, bringing the verified total to 97.
- `monitor.sh` now shows a filtered view by default; `--all` keeps the previous every-line view.
  - **Miners:** active miners get one compact row each (address without the network prefix,
    1m/5m/1h hashrate, workers, best share). Users with no current hashrate and no rejects are
    hidden.
  - **Pool:** the three per-minute `Pool:` lines fold into one POOL row, which flags any rise in
    the pool `rejected` counter.
  - **Connects and drops:** each is one `+`/`-` line with the session length. A connect line also
    shows where its difficulty came from: the NiceHash/MiningRigRentals floor, a password `d=`,
    or a worker-name pattern.
  - **Hidden:** the pool's own `cashstratum-api` probes from 127.0.0.1.
  - **Flapping:** two or more sessions shorter than 5 s within a minute fold into one FLAP line
    with a count. The threshold is set by `CS_MONITOR_FLAP_SECS`.
  - **Highlighted:** blocks, submit failures and errors.
  - **Colours:** each address has a stable colour. An optional groups file (`--groups`,
    `CS_MONITOR_GROUPS`, or `monitor-groups.txt` in the install dir) gives chosen addresses a
    label and colour. `NO_COLOR` is honoured.
  - **How it works:** a single POSIX awk pass, where the old script forked up to 20 `grep`
    processes per log line. Log text is no longer passed through `echo -e`.
  - **Evidence:** `testing/monitor_regression.py` runs it over a synthetic log. On a live log it
    produced identical output under gawk and mawk.
- `post-install.sh` installs the repository's `monitor.sh` in place of an embedded copy, so
  reinstalls and upgrades no longer bring the old monitor back.

Operator note: restart the pool to load the new engine; miners reconnect. Restart the connector and
stratifier together, as they are one binary. The new counters start at zero. Builds configured with
`--disable-sv2` do not contain the SV2 counting.

Validation: Linux default build, `make check` 7/7, zero warnings. `--enable-sv2` build,
`make check` 16/16, zero warnings. `testing/regtest-e2e.sh`: 139/139 assertions, scenarios 1–15.
`go vet` and `go test ./...` in `api/` pass. The PR gate passes: build, unit and API tests including
the Python regression discovery; the self-hosted regtest money gate; SV2 build and interop.

## 1.2.5 — 2026-10-16

Engine release limited to the Stratum V2 Noise handshake. Builds configured with `--disable-sv2`
are functionally identical to 1.2.4.

- Stratum V2 Noise certificates now carry format version **0**, as the SV2 specification requires.
  The listener previously signed version **1**. Initiators that enforce the specification, such as
  SRI `noise_sv2` **2.0.0** and later, rejected the handshake with `InvalidCertificate`; they should
  now complete it (see Validation). The wire observation and the SRI rejection are documented in
  cashstratum/cashstratum#3. Upstream CKPool fixed the same defect in `528b9baf`.
- CashStratum's own SV2 client (ckproxy and the job-declarator client) now rejects any certificate
  version other than 0 before it checks the signature.
- Certificates served by a long-running SV2 listener are re-dated for each handshake when they are
  within one day of expiry. The validity window was previously fixed at key-load time, so a listener
  up for more than 365 days served an expired certificate. Ports upstream CKPool commit `3c45dbe8`.

Operator note: restart the pool server to serve the new certificate. It is signed under the same
authority key, so published `stratum2+tcp://host:port/<key>` URLs do not change. Roll out pool
servers before proxies and job-declarator clients: an updated client refuses an upstream that still
serves version 1.

Validation: an SV2-enabled Linux build passes `make check` (15/15), including the new negative
test that refuses correctly signed certificates with versions 1, 0x0100 and 0xffff and the
long-uptime renewal test. Interoperability with an SRI `noise_sv2` 2.0.0+ initiator was verified by
reading the SRI source, not by running an SRI client; a report from an operator on SRI is welcome
on cashstratum/cashstratum#3.

## 1.2.4 — 2026-10-12

- Refresh the README with light/dark feature artwork, linked node logos, real block evidence,
  a concise reference-deployment overview and custom rounded badges. Document rental detection,
  regional HTTP probes, direct-address and username-based mining, guided installation,
  private deployments and optional pool setup. Keep the extended candidate-payout, block-history
  and notification gallery in the website draft. Clarify regtest and testnet validation in
  development before public releases, with links to public CI and detailed chain coverage.
- Add verified blocks **972524**, **972529** and **972535**, with redacted solve contexts: **81 blocks**,
  **253.31946287 BCH**, including **12 blocks on CashStratum v1.2.1** and **48 recovered solve excerpts**.
- Use saved BlockSniper per-block price snapshots for the headline USD valuation: **72 saved
  prices, two separately marked BlockSniper screenshot prices, and seven historical Coinbase fallbacks**,
  approximately **$132,752.44**. Retain the independent two-exchange calculation and extend the offline
  verifier to check both methods. Distinguish the proof collection from account-history totals.

No mining-engine, API or installer behavior changes.

## 1.2.3 — 2026-10-12

Documentation and proof release. **No engine, API or installer code changed**; a 1.2.2 build
and a 1.2.3 build are functionally identical.

### Added

- **Nine blocks found by the released CashStratum 1.2.1 build** join the production proofs:
  971726, 971744, 971770, 971817, 971860, 972310, 972313, 972318 and 972517 (2026-10-06 →
  2026-10-12, 28.13863015 BCH gross). They are the first blocks found by a tagged CashStratum
  release rather than its predecessor fork. Each record carries a redacted solve excerpt with
  SHA-256 checksums for the excerpt and its source, and a two-exchange valuation.
- **Block 966569 now has its solve excerpt**, recovered from a preserved log; it was previously
  listed as not recovered.
- **`software` field** on proof records whose deployment record names the running release.

### Changed

- **Proof collection: 69 → 78 blocks, 215.80288934 → 243.94151949 BCH, ≈ $121,575.15 →
  ≈ $129,968.85** at historical prices. Solve excerpts: 35 → 45. All 78 re-verified against
  the public chain on 2026-10-12.
- **`verify-values.py`** reads the block count from the data instead of a hard-coded 69.
- **README** rebuilt around the evidence: headline figures, the proof timeline by era, a
  verify-it-yourself walkthrough and a documentation map.

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
