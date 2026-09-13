# Testing Tools

This folder contains tools for testing CashStratum functionality.

## minerd

A CPU miner for testing pool connectivity and mining operations.

### Usage
```bash
./minerd -a sha256d -o stratum+tcp://localhost:3333 -u username.worker -p x --coinbase-addr=<BCH_ADDRESS>
```

### Options
- `-a sha256d` - Use SHA256d algorithm (for Bitcoin Cash)
- `-o` - Pool URL (stratum+tcp://host:port)
- `-u` - Username.workername
- `-p` - Password (usually 'x' for most pools)
- `--coinbase-addr` - BCH address for solo mining

### Example for Testing
```bash
# For regtest
./minerd -a sha256d -o stratum+tcp://localhost:3333 -u alice.test -p x --coinbase-addr=bchreg:qqugw9vuyndj3wd8ewuxll8zs29j96mh3v93fxhygd

# For mainnet
./minerd -a sha256d -o stratum+tcp://localhost:3333 -u alice.rig1 -p x
```

### Notes
- This is a pre-compiled binary (64-bit Linux)
- Only depends on standard system libraries
- No need to recompile unless changing architectures
- Mainly used for testing, not efficient for actual mining

## regtest-e2e.sh

End-to-end regtest verification of CashStratum's solo (`-B`) per-address payout
feature. It spins up a throwaway BCHN `bitcoind -regtest` node, builds and
drives `src/ckpool -B` against it with `testing/minerd`, and asserts every
auth/payout scenario against the plan's acceptance matrix.

### Running it
- **Linux only.** `src/ckpool` links `<sys/epoll.h>` and does not build on
  macOS/BSD, and `testing/minerd` is a pre-compiled 64-bit Linux ELF binary.
  Run it on the Ubuntu pool server, never on a Mac dev box.
- Build CashStratum first: `./autogen.sh && ./configure && make` in the repo root
  (the script's prerequisite check fails fast with a clear message if
  `src/ckpool` or `src/ckpmsg` aren't built yet).
- Also requires `bitcoind`/`bitcoin-cli` (BCHN) and `jq` in `PATH`.
- `TMPDIR` controls where the throwaway regtest workdir is created
  (`mktemp -d "${TMPDIR:-/tmp}/ckpool-e2e.XXXXXX"`); set it if `/tmp` is
  constrained. On failure the workdir is preserved for inspection instead of
  being cleaned up; the last 40 lines of cashstratum's log are printed too.
- `E2E_MINE_TIMEOUT` overrides the per-block mining ceiling (auto-scaled from
  core count otherwise) if a box needs more time to solve regtest blocks.

### Exit codes
- `0` — every assertion passed.
- `1` — at least one assertion FAILed.
- `2` — missing prerequisites; nothing was started.

### Scenario 8's sensitivity guard
Scenario 8 (`scenario_8_per_user_round_independence`) proves the pool-wide
per-solve reset with a **behavioural clause** (the finder's post-solve
residual stays at or under a ceiling derived from its own winning share) and
a separate **sensitivity guard** (at least two solves were logged this run,
and the run's total solve diff strictly exceeds the post-solve residual —
proof the reset actually discarded accumulated diff rather than there being
nothing to discard). The guard is reported with its own `[e2e] scenario8:
sensitivity guard met/not met — …` log line and never counts as a failed
assertion: it used to be baked into the same `check` as the behavioural
clause via a fixed `total > ceiling * 3` multiplier, which depended on the
CPU miner's hashrate and went red on fast, many-core hardware for reasons
unrelated to the pool. Splitting it out keeps the guard purely count- and
ratio-based instead of hashrate-based.

## Installer and retention regression fixtures

Run `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s testing -p '*regression*.py' -v`.
These tests use disposable directories and fixture RPC servers; they never operate on
an installed pool. They exercise rejected upgrades, configuration preservation,
launcher arguments, historical solve indexes, compressed logs and deletion refusal.

## Clean Ubuntu and Fedora builds and installations

Run from the checkout being reviewed:

```bash
CLEANHOST_BCHN_DIR=/opt/bchn CLEANHOST_LOG_DIR=/tmp/cashstratum-install-evidence \
  bash testing/cleanhost-installer-test.sh ubuntu:24.04 fedora:latest
```

Docker and Python 3 are required. The harness snapshots tracked files including
uncommitted edits and new nonignored files, records their SHA-256 hashes, and mounts
that snapshot read-only. It fetches the pinned secp256k1 revision, then builds and
checks both `--disable-sv2` and `--enable-sv2`, including staged installation and
uninstallation. `make distcheck --disable-sv2` also builds and tests the generated
source archive outside Git. Any Docker, build, test or installer failure returns nonzero.

`CLEANHOST_BCHN_DIR` must contain `bin/bitcoind` and `bin/bitcoin-cli`. With this mount,
the harness starts an isolated regtest node, installs the solo pool, reruns the
installer to check config preservation, starts the installed daemon using its unit
arguments, checks the external message helper, installs and exercises a BCH Stratum proxy
against that pool, and verifies a real unprivileged interactive install and rerun.
It then runs the payout suite against the
installer-produced binaries. Without the mount, these live checks are explicitly
reported as **NOT RUN**; a build-only result is insufficient for installer approval.
Each container has its own network and node data. Console output, the source manifest,
build logs and runtime logs remain in `CLEANHOST_LOG_DIR`, including on failure.

The cleanhost harness does not activate host systemd units. Inside the disposable
container, proxy service-management calls are recorded by a stub; the actual proxy
binary is launched with the generated unit arguments and must serve a Stratum
subscription. A real-systemd smoke test
remains a separate release check; inspect the generated unit and verify helper socket
access under its actual service account before deploying.
