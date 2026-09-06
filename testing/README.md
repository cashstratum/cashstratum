# Testing Tools

This folder contains tools for testing CKPool functionality.

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
./minerd -a sha256d -o stratum+tcp://localhost:3333 -u skaisser.test -p x --coinbase-addr=bchreg:qqugw9vuyndj3wd8ewuxll8zs29j96mh3v93fxhygd

# For mainnet
./minerd -a sha256d -o stratum+tcp://localhost:3333 -u skaisser.rig1 -p x
```

### Notes
- This is a pre-compiled binary (64-bit Linux)
- Only depends on standard system libraries
- No need to recompile unless changing architectures
- Mainly used for testing, not efficient for actual mining

## regtest-e2e.sh

End-to-end regtest verification of ckpool's solo (`-B`) per-address payout
feature. It spins up a throwaway BCHN `bitcoind -regtest` node, builds and
drives `src/ckpool -B` against it with `testing/minerd`, and asserts every
auth/payout scenario against the plan's acceptance matrix.

### Running it
- **Linux only.** `src/ckpool` links `<sys/epoll.h>` and does not build on
  macOS/BSD, and `testing/minerd` is a pre-compiled 64-bit Linux ELF binary.
  Run it on the Ubuntu pool server, never on a Mac dev box.
- Build ckpool first: `./autogen.sh && ./configure && make` in the repo root
  (the script's prerequisite check fails fast with a clear message if
  `src/ckpool` or `src/ckpmsg` aren't built yet).
- Also requires `bitcoind`/`bitcoin-cli` (BCHN) and `jq` in `PATH`.
- `TMPDIR` controls where the throwaway regtest workdir is created
  (`mktemp -d "${TMPDIR:-/tmp}/ckpool-e2e.XXXXXX"`); set it if `/tmp` is
  constrained. On failure the workdir is preserved for inspection instead of
  being cleaned up; the last 40 lines of ckpool's log are printed too.
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