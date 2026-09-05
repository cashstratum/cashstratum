# Contributing to CashStratum

Thanks for looking. CashStratum runs real money on real hardware, so the bar here is evidence:
a change that cannot be demonstrated does not land.

## Reporting a bug

Open an issue with:

- what you ran (exact command, config with secrets removed) and what happened;
- your BCH node (BCHN version, network) and mining hardware/software;
- the relevant log lines — not a screenshot of them;
- whether stock CKPool behaves the same way, if you can check.

A share-accounting or payout bug should include the block height or share log that shows it.
Security issues do **not** go in a public issue — see [SECURITY.md](SECURITY.md).

## Pull requests

- One logical change per PR, with a description of the problem before the solution.
- Match the style of the file you are editing. This codebase descends from CKPool and does not
  follow modern C conventions everywhere; consistency with the surrounding code beats consistency
  with your preference.
- Keep C99, no new external dependencies without discussion — the CashAddr implementation is pure
  C on purpose.
- Say how you tested it. "Compiles" is not a test; a testnet block, a share log, or a benchmark is.
- No formatting-only churn mixed into a functional change.

Releases are cut as complete source trees, so a merged PR ships in the next tagged release.

## Getting your pool listed

Listings in [`pools.yml`](pools.yml) are verified. Open an
[Add your pool](https://github.com/cashstratum/cashstratum/issues/new?template=add-your-pool.yml)
issue; we confirm two things before adding you:

1. **Domain control** — a DNS `TXT` record `cashstratum-verification=<issue-number>` on your apex
   domain, or the same string served at `https://<your-domain>/.well-known/cashstratum.txt`.
2. **The pool actually runs CashStratum** — at least one behaviour stock CKPool does not have:
   a CashAddr username accepted at authorize, password difficulty (`-p d=65536`) applied, explicit
   rejection of a bad-checksum address, or an on-chain coinbase showing your tag and the
   dual-output split.

We re-check listed endpoints periodically; a pool unreachable for more than 30 days is moved to an
inactive section rather than silently dropped.

## License

By contributing you agree your work is licensed under GPLv3, like the rest of the project.
