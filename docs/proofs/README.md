# 81 BCH mainnet blocks

**Proof collection updated:** 2026-10-12T09:00:18Z

**Block dates:** 2025-07-15T16:42:49Z through 2026-10-12T08:57:29Z.

These 81 blocks belong to the production history of this modified CKPool lineage, from the
EloPool deployment in 2025 to CashStratum today. All 81 hashes match the BCH main chain, and
every coinbase carries an EloPool or BlockSniper tag.

**12 of these blocks were found by the released CashStratum v1.2.1 build** (2026-10-06 → 2026-10-12), running on the reference deployment. Every one of them has a recovered, redacted solve excerpt with SHA-256 checksums.

| Era | Blocks | Dates (UTC) | Gross BCH |
|---|---|---|---|
| EloPool | 68 | 2025-07-15 → 2025-12-06 | 212.67746187 |
| BlockSniper | 1 | 2026-09-01 → 2026-09-01 | 3.12542747 |
| CashStratum v1.2.1 | 12 | 2026-10-06 → 2026-10-12 | 37.51657353 |

## 17 additional blocks since this collection

**98 verified blocks in total.** [`recent-blocks.json`](recent-blocks.json) adds seventeen more
heights found by the reference deployment between **2026-10-12 and 2026-10-31** (972548 → 975301).
They are kept in a separate file so the 81-block collection above stays unchanged.

The first nine were found between **2026-10-12 and 2026-10-23**, all on CashStratum: two on the
released **v1.2.1** build and seven on the released **v1.2.5** build. Their source is the reference deployment's public miner history, which
lists 21 blocks for that miner. 12 are already in the collection above, which leaves these 9. Each
has a redacted solve excerpt with SHA-256 checksums for the excerpt and its source, and a saved
price snapshot.

The eight later heights (974758 → 975301, 2026-10-27 → 2026-10-31) were recovered from the
reference deployment's solve logs: seven on the released **v1.2.1** build and one on the released
**v1.3.0** build. Each has a redacted
solve excerpt with a SHA-256 checksum for the excerpt only: there is no sidecar or database-hash
check for them. No saved BlockSniper price exists for them, so they have no saved historical
valuation; their values use the documented historical fallback: the last completed, nonzero-volume Coinbase BCH/USD minute candle
at or before the block-header timestamp.

Every one of the seventeen was checked the same way: mainchain header, coinbase, pool tag and gross
reward against the public BCH API.

| Height | Block time (UTC) | Software | Gross BCH | Value |
|---|---|---|---|---|
| [975301](https://www.blockchain.com/explorer/blocks/bch/975301) | 2026-10-31T09:11:19Z | CashStratum v1.3.0 | 3.12529870 | $902.12 (fallback) |
| [975091](https://www.blockchain.com/explorer/blocks/bch/975091) | 2026-10-29T19:59:43Z | CashStratum v1.2.1 | 3.12545225 | $895.66 (fallback) |
| [975010](https://www.blockchain.com/explorer/blocks/bch/975010) | 2026-10-29T07:16:52Z | CashStratum v1.2.1 | 3.12544835 | $871.50 (fallback) |
| [975008](https://www.blockchain.com/explorer/blocks/bch/975008) | 2026-10-29T07:02:57Z | CashStratum v1.2.1 | 3.12500690 | $871.60 (fallback) |
| [974927](https://www.blockchain.com/explorer/blocks/bch/974927) | 2026-10-28T16:17:52Z | CashStratum v1.2.1 | 3.12501735 | $911.19 (fallback) |
| [974790](https://www.blockchain.com/explorer/blocks/bch/974790) | 2026-10-27T18:55:51Z | CashStratum v1.2.1 | 3.12513231 | $902.91 (fallback) |
| [974769](https://www.blockchain.com/explorer/blocks/bch/974769) | 2026-10-27T17:21:51Z | CashStratum v1.2.1 | 3.12510852 | $906.63 (fallback) |
| [974758](https://www.blockchain.com/explorer/blocks/bch/974758) | 2026-10-27T16:46:46Z | CashStratum v1.2.1 | 3.12563929 | $907.34 (fallback) |
| [974153](https://www.blockchain.com/explorer/blocks/bch/974153) | 2026-10-23T09:59:33Z | CashStratum v1.2.5 | 3.12597618 | $892.75 |
| [973785](https://www.blockchain.com/explorer/blocks/bch/973785) | 2026-10-21T01:56:47Z | CashStratum v1.2.5 | 3.12555473 | $898.78 |
| [973750](https://www.blockchain.com/explorer/blocks/bch/973750) | 2026-10-20T21:07:52Z | CashStratum v1.2.5 | 3.12536752 | $884.85 |
| [973663](https://www.blockchain.com/explorer/blocks/bch/973663) | 2026-10-20T08:33:36Z | CashStratum v1.2.5 | 3.12903949 | $859.52 |
| [973603](https://www.blockchain.com/explorer/blocks/bch/973603) | 2026-10-19T21:16:49Z | CashStratum v1.2.5 | 3.12944510 | $870.49 |
| [973559](https://www.blockchain.com/explorer/blocks/bch/973559) | 2026-10-19T13:17:50Z | CashStratum v1.2.5 | 3.12525575 | $872.98 |
| [973557](https://www.blockchain.com/explorer/blocks/bch/973557) | 2026-10-19T13:08:25Z | CashStratum v1.2.5 | 3.12600389 | $875.81 |
| [972557](https://www.blockchain.com/explorer/blocks/bch/972557) | 2026-10-12T11:46:11Z | CashStratum v1.2.1 | 3.12605355 | $879.27 |
| [972548](https://www.blockchain.com/explorer/blocks/bch/972548) | 2026-10-12T10:32:12Z | CashStratum v1.2.1 | 3.12563354 | $883.43 |
| **Total** | | | **53.14043342** | **$15,086.83** |

Values are gross of fees and splits: the saved price snapshot for the first nine, and the marked
Coinbase fallback for the eight later blocks (its candle URL is kept in each record). Excerpts are
under `logs/<height>-context.log`. To re-check: `python3 verify-recent.py` (live chain) or
`python3 verify-recent.py --offline` (checksums and arithmetic only).

## Historical reward value

**253.31946287 BCH gross rewards**, valued at approximately
**$132,752.44 USD** using each block's saved BlockSniper price
where available: **72 database snapshots, 2 separately marked
BlockSniper screenshot prices and 7 historical Coinbase fallbacks**.
Includes transaction fees; before operator/miner splits and operating costs.
[Per-block values, provenance and the independent exchange comparison](VALUATION.md).

## Evidence and method

- Selected confirmed records with audit references from the operator database.
- Queried the public Blockchain.com Haskoin BCH API by height; required the stored hash to match and `mainchain` to be true.
- Retrieved each first transaction and required a coinbase input and matching block height.
- Recorded the coinbase transaction ID, script and recognizable project tag.
- Recovered solve context for **48 blocks** from preserved pool logs; redacted finder identities.
- **33 blocks have no local solve excerpt recovered in this collection.** Their on-chain identity and tag were verified independently of that gap.

Dates below are UTC block-header timestamps, not local pool log times. Log timestamps retain
their original format and have no asserted timezone. Verification timestamps describe each
snapshot and are not continuously refreshed.

A coinbase tag is self-declared and does not cryptographically attest a software build. The
`software` field on the CashStratum-era records comes from the operator's deployment record of
the release that was running; for older blocks the exact source revision is not attested.
Neither the tags nor the solve logs establish hashrate, performance, or per-address payout mode
for every historical block. No such claim is made here.

## Context excerpts

Each excerpt keeps up to 20 source lines before and 30 after the confirmation. The candidate,
submission, acceptance, work-template and tip/ZMQ events are retained when present. Miner
identities and all messages outside a strict event allowlist are replaced with explicit
redaction markers. Timestamps and line order are preserved; missing events are not invented.
A nearby notification may refer to a later tip, not necessarily the solved block.

`logs/<height>.log` holds the one-line confirmation and `logs/<height>-context.log` the context
edition. JSON records preserve checksums for both, the source-file checksum, the source line
range and the proof-update time. Raw source logs remain private.

## Download and verify

- [Machine-readable records](blocks.json)
- [CSV block index](blocks.csv)
- [Recheck chain hashes and coinbase scripts](verify.py): `python3 verify.py` (Python 3 standard library, network required).
- [Recheck reward arithmetic and price boundaries](verify-values.py): `python3 verify-values.py` (offline).
- Explorer links below lead to the individual BCH blocks.

## Block index

| Height / explorer | Block date (UTC) | Era | Coinbase tag | Local solve excerpt | Last checked (UTC) |
|---|---|---|---|---|---|
| [972535](https://www.blockchain.com/explorer/blocks/bch/972535) | 2026-10-12T08:57:29Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972535-context.log) | 2026-10-12T09:00:19Z |
| [972529](https://www.blockchain.com/explorer/blocks/bch/972529) | 2026-10-12T08:20:18Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972529-context.log) | 2026-10-12T08:44:10Z |
| [972524](https://www.blockchain.com/explorer/blocks/bch/972524) | 2026-10-12T07:37:58Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972524-context.log) | 2026-10-12T08:03:12Z |
| [972517](https://www.blockchain.com/explorer/blocks/bch/972517) | 2026-10-12T06:26:54Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972517-context.log) | 2026-10-12T06:58:24Z |
| [972318](https://www.blockchain.com/explorer/blocks/bch/972318) | 2026-10-10T18:34:48Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972318-context.log) | 2026-10-12T06:58:23Z |
| [972313](https://www.blockchain.com/explorer/blocks/bch/972313) | 2026-10-10T17:59:58Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972313-context.log) | 2026-10-12T06:58:22Z |
| [972310](https://www.blockchain.com/explorer/blocks/bch/972310) | 2026-10-10T17:36:18Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/972310-context.log) | 2026-10-12T06:58:20Z |
| [971860](https://www.blockchain.com/explorer/blocks/bch/971860) | 2026-10-07T11:04:52Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/971860-context.log) | 2026-10-12T06:58:19Z |
| [971817](https://www.blockchain.com/explorer/blocks/bch/971817) | 2026-10-07T02:45:23Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/971817-context.log) | 2026-10-12T06:58:18Z |
| [971770](https://www.blockchain.com/explorer/blocks/bch/971770) | 2026-10-06T18:07:48Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/971770-context.log) | 2026-10-12T06:58:17Z |
| [971744](https://www.blockchain.com/explorer/blocks/bch/971744) | 2026-10-06T14:15:20Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/971744-context.log) | 2026-10-12T06:58:15Z |
| [971726](https://www.blockchain.com/explorer/blocks/bch/971726) | 2026-10-06T11:05:31Z | CashStratum v1.2.1 | BlockSniper.ai · AI Smart Solo Mining | [Redacted log](logs/971726-context.log) | 2026-10-12T06:58:14Z |
| [966569](https://www.blockchain.com/explorer/blocks/bch/966569) | 2026-09-01T03:50:05Z | BlockSniper | BlockSniper.ai · [Solo] | [Redacted log](logs/966569-context.log) | 2026-09-05T22:27:03Z |
| [928165](https://www.blockchain.com/explorer/blocks/bch/928165) | 2025-12-06T08:33:03Z | EloPool | EloPool.cloud | Not recovered | 2026-09-05T22:27:03Z |
| [928098](https://www.blockchain.com/explorer/blocks/bch/928098) | 2025-12-05T21:48:01Z | EloPool | EloPool.cloud | Not recovered | 2026-09-05T22:27:04Z |
| [916984](https://www.blockchain.com/explorer/blocks/bch/916984) | 2025-09-20T10:02:02Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:03Z |
| [916429](https://www.blockchain.com/explorer/blocks/bch/916429) | 2025-09-16T16:46:10Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:05Z |
| [916154](https://www.blockchain.com/explorer/blocks/bch/916154) | 2025-09-14T20:24:45Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:05Z |
| [915924](https://www.blockchain.com/explorer/blocks/bch/915924) | 2025-09-13T05:51:49Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:05Z |
| [915824](https://www.blockchain.com/explorer/blocks/bch/915824) | 2025-09-12T14:05:13Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [915321](https://www.blockchain.com/explorer/blocks/bch/915321) | 2025-09-09T00:18:10Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [914850](https://www.blockchain.com/explorer/blocks/bch/914850) | 2025-09-05T15:51:57Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [914754](https://www.blockchain.com/explorer/blocks/bch/914754) | 2025-09-05T04:27:58Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [914557](https://www.blockchain.com/explorer/blocks/bch/914557) | 2025-09-03T18:34:12Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [914527](https://www.blockchain.com/explorer/blocks/bch/914527) | 2025-09-03T12:31:03Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:06Z |
| [914486](https://www.blockchain.com/explorer/blocks/bch/914486) | 2025-09-03T06:58:03Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914439](https://www.blockchain.com/explorer/blocks/bch/914439) | 2025-09-03T00:21:32Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914407](https://www.blockchain.com/explorer/blocks/bch/914407) | 2025-09-02T18:31:08Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914239](https://www.blockchain.com/explorer/blocks/bch/914239) | 2025-09-01T18:20:42Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914226](https://www.blockchain.com/explorer/blocks/bch/914226) | 2025-09-01T15:30:33Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914122](https://www.blockchain.com/explorer/blocks/bch/914122) | 2025-08-31T22:13:50Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [914076](https://www.blockchain.com/explorer/blocks/bch/914076) | 2025-08-31T13:48:39Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:07Z |
| [913971](https://www.blockchain.com/explorer/blocks/bch/913971) | 2025-08-30T21:06:26Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:08Z |
| [913959](https://www.blockchain.com/explorer/blocks/bch/913959) | 2025-08-30T18:52:28Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:08Z |
| [913820](https://www.blockchain.com/explorer/blocks/bch/913820) | 2025-08-29T21:30:32Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:08Z |
| [913680](https://www.blockchain.com/explorer/blocks/bch/913680) | 2025-08-28T21:41:37Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:08Z |
| [913451](https://www.blockchain.com/explorer/blocks/bch/913451) | 2025-08-27T06:54:42Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:08Z |
| [913423](https://www.blockchain.com/explorer/blocks/bch/913423) | 2025-08-27T03:28:18Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:09Z |
| [913256](https://www.blockchain.com/explorer/blocks/bch/913256) | 2025-08-25T21:10:12Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:09Z |
| [912928](https://www.blockchain.com/explorer/blocks/bch/912928) | 2025-08-23T14:20:52Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:10Z |
| [912893](https://www.blockchain.com/explorer/blocks/bch/912893) | 2025-08-23T09:19:27Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:09Z |
| [912853](https://www.blockchain.com/explorer/blocks/bch/912853) | 2025-08-23T02:03:17Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:09Z |
| [912775](https://www.blockchain.com/explorer/blocks/bch/912775) | 2025-08-22T15:24:50Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:10Z |
| [912602](https://www.blockchain.com/explorer/blocks/bch/912602) | 2025-08-21T10:36:39Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:10Z |
| [912567](https://www.blockchain.com/explorer/blocks/bch/912567) | 2025-08-21T04:56:04Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:10Z |
| [912378](https://www.blockchain.com/explorer/blocks/bch/912378) | 2025-08-19T21:25:47Z | EloPool | EloPool.cloud · [Solo] | Not recovered | 2026-09-05T22:27:10Z |
| [912116](https://www.blockchain.com/explorer/blocks/bch/912116) | 2025-08-18T01:23:17Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/912116-context.log) | 2026-09-05T22:27:10Z |
| [912035](https://www.blockchain.com/explorer/blocks/bch/912035) | 2025-08-17T11:38:53Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/912035-context.log) | 2026-09-05T22:27:11Z |
| [912009](https://www.blockchain.com/explorer/blocks/bch/912009) | 2025-08-17T07:22:41Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/912009-context.log) | 2026-09-05T22:27:11Z |
| [911920](https://www.blockchain.com/explorer/blocks/bch/911920) | 2025-08-16T15:29:57Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911920-context.log) | 2026-09-05T22:27:11Z |
| [911903](https://www.blockchain.com/explorer/blocks/bch/911903) | 2025-08-16T13:18:47Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911903-context.log) | 2026-09-05T22:27:11Z |
| [911683](https://www.blockchain.com/explorer/blocks/bch/911683) | 2025-08-14T23:39:09Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911683-context.log) | 2026-09-05T22:27:11Z |
| [911569](https://www.blockchain.com/explorer/blocks/bch/911569) | 2025-08-14T03:52:09Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911569-context.log) | 2026-09-05T22:27:12Z |
| [911549](https://www.blockchain.com/explorer/blocks/bch/911549) | 2025-08-14T01:54:14Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911549-context.log) | 2026-09-05T22:27:12Z |
| [911502](https://www.blockchain.com/explorer/blocks/bch/911502) | 2025-08-13T17:50:32Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911502-context.log) | 2026-09-05T22:27:12Z |
| [911458](https://www.blockchain.com/explorer/blocks/bch/911458) | 2025-08-13T09:28:10Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911458-context.log) | 2026-09-05T22:27:12Z |
| [911379](https://www.blockchain.com/explorer/blocks/bch/911379) | 2025-08-12T19:25:34Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911379-context.log) | 2026-09-05T22:27:12Z |
| [911319](https://www.blockchain.com/explorer/blocks/bch/911319) | 2025-08-12T12:04:33Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911319-context.log) | 2026-09-05T22:27:13Z |
| [911228](https://www.blockchain.com/explorer/blocks/bch/911228) | 2025-08-11T21:37:48Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911228-context.log) | 2026-09-05T22:27:13Z |
| [911053](https://www.blockchain.com/explorer/blocks/bch/911053) | 2025-08-10T17:45:42Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/911053-context.log) | 2026-09-05T22:27:13Z |
| [910620](https://www.blockchain.com/explorer/blocks/bch/910620) | 2025-08-07T16:16:38Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910620-context.log) | 2026-09-05T22:27:13Z |
| [910527](https://www.blockchain.com/explorer/blocks/bch/910527) | 2025-08-07T01:02:24Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910527-context.log) | 2026-09-05T22:27:13Z |
| [910381](https://www.blockchain.com/explorer/blocks/bch/910381) | 2025-08-06T01:36:21Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910381-context.log) | 2026-09-05T22:27:13Z |
| [910380](https://www.blockchain.com/explorer/blocks/bch/910380) | 2025-08-06T01:35:21Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910380-context.log) | 2026-09-05T22:27:14Z |
| [910200](https://www.blockchain.com/explorer/blocks/bch/910200) | 2025-08-04T18:36:27Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910200-context.log) | 2026-09-05T22:27:14Z |
| [910003](https://www.blockchain.com/explorer/blocks/bch/910003) | 2025-08-03T12:54:27Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/910003-context.log) | 2026-09-05T22:27:14Z |
| [909933](https://www.blockchain.com/explorer/blocks/bch/909933) | 2025-08-03T00:47:04Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909933-context.log) | 2026-09-05T22:27:14Z |
| [909930](https://www.blockchain.com/explorer/blocks/bch/909930) | 2025-08-02T23:54:20Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909930-context.log) | 2026-09-05T22:27:14Z |
| [909890](https://www.blockchain.com/explorer/blocks/bch/909890) | 2025-08-02T18:12:32Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909890-context.log) | 2026-09-05T22:27:14Z |
| [909866](https://www.blockchain.com/explorer/blocks/bch/909866) | 2025-08-02T12:59:17Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909866-context.log) | 2026-09-05T22:27:14Z |
| [909802](https://www.blockchain.com/explorer/blocks/bch/909802) | 2025-08-02T02:57:20Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909802-context.log) | 2026-09-05T22:27:15Z |
| [909720](https://www.blockchain.com/explorer/blocks/bch/909720) | 2025-08-01T12:04:27Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909720-context.log) | 2026-09-05T22:27:15Z |
| [909701](https://www.blockchain.com/explorer/blocks/bch/909701) | 2025-08-01T08:55:07Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909701-context.log) | 2026-09-05T22:27:15Z |
| [909669](https://www.blockchain.com/explorer/blocks/bch/909669) | 2025-08-01T04:11:42Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909669-context.log) | 2026-09-05T22:27:15Z |
| [909494](https://www.blockchain.com/explorer/blocks/bch/909494) | 2025-07-30T22:30:45Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909494-context.log) | 2026-09-05T22:27:15Z |
| [909310](https://www.blockchain.com/explorer/blocks/bch/909310) | 2025-07-29T16:39:56Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/909310-context.log) | 2026-09-05T22:27:16Z |
| [908385](https://www.blockchain.com/explorer/blocks/bch/908385) | 2025-07-23T13:05:56Z | EloPool | EloPool.cloud · [Solo] | [Redacted log](logs/908385-context.log) | 2026-09-05T22:27:16Z |
| [908222](https://www.blockchain.com/explorer/blocks/bch/908222) | 2025-07-22T10:16:53Z | EloPool | EloPool.Cloud · ckpool | [Redacted log](logs/908222-context.log) | 2026-09-05T22:27:16Z |
| [907506](https://www.blockchain.com/explorer/blocks/bch/907506) | 2025-07-17T13:57:40Z | EloPool | EloPool.Cloud · ckpool | [Redacted log](logs/907506-context.log) | 2026-09-05T22:27:16Z |
| [907353](https://www.blockchain.com/explorer/blocks/bch/907353) | 2025-07-16T12:09:54Z | EloPool | EloPool.Cloud · ckpool | [Redacted log](logs/907353-context.log) | 2026-09-05T22:27:16Z |
| [907231](https://www.blockchain.com/explorer/blocks/bch/907231) | 2025-07-15T16:42:49Z | EloPool | EloPool.Cloud · ckpool | [Redacted log](logs/907231-context.log) | 2026-09-05T22:27:16Z |
