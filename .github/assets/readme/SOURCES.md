# README visual assets

The README uses paired light and dark images through HTML `picture` elements. Light artwork
is the fallback. Essential feature descriptions, examples and proof links remain ordinary
Markdown text so the images are not required to understand or use the project.
The feature overview has a single-column variant for screens up to 600 pixels wide.
Theme markup follows [GitHub's image guidance](https://github.blog/developer-skills/github/how-to-make-your-images-in-markdown-on-github-adjust-for-dark-mode-and-light-mode/).

## Illustrations

- `badge-{release,proofs,validation,license}.svg`: rounded vector badges with a shared
  mint surface and dark lettering for both GitHub themes. Release and block count refer to
  this README's dated snapshot. The testing badge links to the pre-publication validation
  process and coverage; it is not a live CI status indicator.
- `hero-{light,dark}.webp`: conceptual mining-infrastructure artwork. The layered assembly is
  an illustration, not a hardware product or a literal diagram of the engine's processes.
- `block-accepted-{light,dark}.svg`: selected lines from
  [`972535-context.log`](../../../docs/proofs/logs/972535-context.log), typeset for legibility.
  The date is shown once, timestamps are retained, and the hash notification is wrapped.
  The finder remains redacted. The original log and its checksums are unchanged.
- `architecture-{light,dark}.svg`: the documented Stratum V1 path, supported node interfaces,
  and log/status consumers. See [installation](../../../docs/installation.md),
  [monitor](../../../monitor.sh) and [API](../../../docs/operator-api.md).
- `feature-showcase-{light,dark}.svg`: feature overview. Rental detection recognizes
  `nicehash`, `miningrigrentals` and `mrr-hash` in the client identifier; the difficulty is
  bounded and sent after authorization. See [`src/stratifier.c`](../../../src/stratifier.c).
  The regional probe layout is illustrative; it does not describe a deployed region list.
  Each probe measures browser-to-host HTTP round-trip time. Regional infrastructure and
  the comparison interface are operator-managed.

## Node logos

Retrieved from the projects' own websites on 2026-10-12. The marks identify compatible
software; ownership remains with the respective projects. Their appearance does not imply
sponsorship or endorsement.

| Asset | Official source | Treatment |
|---|---|---|
| `bchn-light.svg`, `bchn-dark.svg` | [Bitcoin Cash Node media kit](https://bitcoincashnode.org/en/media), [original ZIP](https://bitcoincashnode.org/static/img/bchn-mediakit.zip) | Unmodified horizontal black and white SVG variants |
| `bchd.svg` | [BCHD](https://bchd.cash/), [original logo](https://bchd.cash/_assets/svg/logo.svg?0.0.3) | Original SVG, embedded unchanged in `bchd-badge.svg` on a dark surface |
| `flowee-the-hub.png` | [Flowee](https://flowee.org/), [original logo](https://flowee.org/images/logo-the-hub.png) | Original PNG, embedded unchanged in `flowee-the-hub-badge.svg` on a light surface |

The neutral badge surfaces keep the upstream logo colors legible in both GitHub themes.

## Node compatibility claims

The [v1.2.2 release](https://github.com/cashstratum/cashstratum/releases/tag/v1.2.2),
[`test/nodecompat.c`](../../../test/nodecompat.c), [`src/bitcoin.c`](../../../src/bitcoin.c)
and the [node setup guide](../../../docs/installation.md#supported-nodes) establish BCHD
and Flowee support. This work adapted CashStratum's RPC requests and template parsing.
It did not add a ZMQ implementation to BCHD. The documented configuration for those two
nodes uses RPC polling. BCHN is the reference for the end-to-end mining gate.
