# Security policy

## Reporting a vulnerability

**Do not open a public issue.** CashStratum handles block templates, coinbase construction and
payout addresses on live mining pools; a public report is an exploit notice for every operator
running it.

Report privately through GitHub's
[security advisory form](https://github.com/cashstratum/cashstratum/security/advisories/new).

Please include a description of the impact, the affected version or commit, and reproduction
steps or a proof of concept.

## What we consider high severity

- Anything that redirects, splits or steals a block reward, or corrupts coinbase outputs.
- Address parsing or validation flaws that could pay a valid-looking but wrong address.
- Remote crashes or memory corruption reachable from an unauthenticated stratum connection.
- Share accounting flaws that let a miner claim work it did not do.

## Response

We aim to acknowledge a report within 72 hours and to ship a fix, with an advisory and credit to
the reporter (unless you prefer otherwise), as fast as a correct fix allows. Pool operators listed
in `pools.yml` are notified before public disclosure where the issue warrants it.

## Supported versions

The latest tagged release is supported. Because pool operators build from source, fixes are
delivered as new releases rather than backported patches.
