# 69 BCH mainnet blocks

**Last updated / chain verification:** 2026-09-05T22:27:16Z

**Block dates:** 2025-07-15T16:42:49Z through 2026-09-01T03:50:05Z.

These 69 blocks belong to the production history of the BCH CKPool fork now being prepared
for release as CashStratum. All 69 hashes match the BCH main chain, and every coinbase carries
an EloPool or BlockSniper tag. This is historical deployment evidence, not a claim that the
newly branded CashStratum release was used for all 69 blocks.

## Evidence and method

- Selected the 69 confirmed records with audit references from the operator database snapshot.
- Queried the public Blockchain.com Haskoin BCH API by height; required the stored hash to match and `mainchain` to be true.
- Retrieved each first transaction and required a coinbase input and matching block height.
- Recorded the coinbase transaction ID, script and recognizable project tag.
- Recovered solve lines for **35 blocks** from local historical logs; redacted finder identities.
- **34 blocks have no local solve excerpt recovered in this collection.** Their on-chain identity and tag were verified independently of that gap.

Dates below are UTC block-header timestamps, not local pool log times. Log timestamps retain
their original format and have no asserted timezone. Verification timestamps describe this
snapshot and are not continuously refreshed.

A coinbase tag is self-declared and does not cryptographically attest a software build. Neither
these tags nor the solve logs establish the exact source commit, hashrate, performance, or
per-address payout mode for every historical block. No such claim is made here.

## Download and verify

- [Machine-readable records](blocks.json)
- [CSV block index](blocks.csv)
- [Recheck chain hashes and coinbase scripts](verify.py): `python3 verify.py` (Python 3 standard library, network required).
- Explorer links below lead to the individual BCH blocks.

## Block index

| Height / explorer | Block date (UTC) | Coinbase tag | Local solve excerpt | Last checked (UTC) |
|---|---|---|---|---|
| [966569](https://www.blockchain.com/explorer/blocks/bch/966569) | 2026-09-01T03:50:05Z | BlockSniper.ai | Not recovered | 2026-09-05T22:27:03Z |
| [928165](https://www.blockchain.com/explorer/blocks/bch/928165) | 2025-12-06T08:33:03Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:03Z |
| [928098](https://www.blockchain.com/explorer/blocks/bch/928098) | 2025-12-05T21:48:01Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:04Z |
| [916984](https://www.blockchain.com/explorer/blocks/bch/916984) | 2025-09-20T10:02:02Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:03Z |
| [916429](https://www.blockchain.com/explorer/blocks/bch/916429) | 2025-09-16T16:46:10Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:05Z |
| [916154](https://www.blockchain.com/explorer/blocks/bch/916154) | 2025-09-14T20:24:45Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:05Z |
| [915924](https://www.blockchain.com/explorer/blocks/bch/915924) | 2025-09-13T05:51:49Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:05Z |
| [915824](https://www.blockchain.com/explorer/blocks/bch/915824) | 2025-09-12T14:05:13Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [915321](https://www.blockchain.com/explorer/blocks/bch/915321) | 2025-09-09T00:18:10Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [914850](https://www.blockchain.com/explorer/blocks/bch/914850) | 2025-09-05T15:51:57Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [914754](https://www.blockchain.com/explorer/blocks/bch/914754) | 2025-09-05T04:27:58Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [914557](https://www.blockchain.com/explorer/blocks/bch/914557) | 2025-09-03T18:34:12Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [914527](https://www.blockchain.com/explorer/blocks/bch/914527) | 2025-09-03T12:31:03Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:06Z |
| [914486](https://www.blockchain.com/explorer/blocks/bch/914486) | 2025-09-03T06:58:03Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914439](https://www.blockchain.com/explorer/blocks/bch/914439) | 2025-09-03T00:21:32Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914407](https://www.blockchain.com/explorer/blocks/bch/914407) | 2025-09-02T18:31:08Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914239](https://www.blockchain.com/explorer/blocks/bch/914239) | 2025-09-01T18:20:42Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914226](https://www.blockchain.com/explorer/blocks/bch/914226) | 2025-09-01T15:30:33Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914122](https://www.blockchain.com/explorer/blocks/bch/914122) | 2025-08-31T22:13:50Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [914076](https://www.blockchain.com/explorer/blocks/bch/914076) | 2025-08-31T13:48:39Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:07Z |
| [913971](https://www.blockchain.com/explorer/blocks/bch/913971) | 2025-08-30T21:06:26Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:08Z |
| [913959](https://www.blockchain.com/explorer/blocks/bch/913959) | 2025-08-30T18:52:28Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:08Z |
| [913820](https://www.blockchain.com/explorer/blocks/bch/913820) | 2025-08-29T21:30:32Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:08Z |
| [913680](https://www.blockchain.com/explorer/blocks/bch/913680) | 2025-08-28T21:41:37Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:08Z |
| [913451](https://www.blockchain.com/explorer/blocks/bch/913451) | 2025-08-27T06:54:42Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:08Z |
| [913423](https://www.blockchain.com/explorer/blocks/bch/913423) | 2025-08-27T03:28:18Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:09Z |
| [913256](https://www.blockchain.com/explorer/blocks/bch/913256) | 2025-08-25T21:10:12Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:09Z |
| [912928](https://www.blockchain.com/explorer/blocks/bch/912928) | 2025-08-23T14:20:52Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:10Z |
| [912893](https://www.blockchain.com/explorer/blocks/bch/912893) | 2025-08-23T09:19:27Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:09Z |
| [912853](https://www.blockchain.com/explorer/blocks/bch/912853) | 2025-08-23T02:03:17Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:09Z |
| [912775](https://www.blockchain.com/explorer/blocks/bch/912775) | 2025-08-22T15:24:50Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:10Z |
| [912602](https://www.blockchain.com/explorer/blocks/bch/912602) | 2025-08-21T10:36:39Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:10Z |
| [912567](https://www.blockchain.com/explorer/blocks/bch/912567) | 2025-08-21T04:56:04Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:10Z |
| [912378](https://www.blockchain.com/explorer/blocks/bch/912378) | 2025-08-19T21:25:47Z | EloPool.cloud | Not recovered | 2026-09-05T22:27:10Z |
| [912116](https://www.blockchain.com/explorer/blocks/bch/912116) | 2025-08-18T01:23:17Z | EloPool.cloud | [Redacted log](logs/912116.log) | 2026-09-05T22:27:10Z |
| [912035](https://www.blockchain.com/explorer/blocks/bch/912035) | 2025-08-17T11:38:53Z | EloPool.cloud | [Redacted log](logs/912035.log) | 2026-09-05T22:27:11Z |
| [912009](https://www.blockchain.com/explorer/blocks/bch/912009) | 2025-08-17T07:22:41Z | EloPool.cloud | [Redacted log](logs/912009.log) | 2026-09-05T22:27:11Z |
| [911920](https://www.blockchain.com/explorer/blocks/bch/911920) | 2025-08-16T15:29:57Z | EloPool.cloud | [Redacted log](logs/911920.log) | 2026-09-05T22:27:11Z |
| [911903](https://www.blockchain.com/explorer/blocks/bch/911903) | 2025-08-16T13:18:47Z | EloPool.cloud | [Redacted log](logs/911903.log) | 2026-09-05T22:27:11Z |
| [911683](https://www.blockchain.com/explorer/blocks/bch/911683) | 2025-08-14T23:39:09Z | EloPool.cloud | [Redacted log](logs/911683.log) | 2026-09-05T22:27:11Z |
| [911569](https://www.blockchain.com/explorer/blocks/bch/911569) | 2025-08-14T03:52:09Z | EloPool.cloud | [Redacted log](logs/911569.log) | 2026-09-05T22:27:12Z |
| [911549](https://www.blockchain.com/explorer/blocks/bch/911549) | 2025-08-14T01:54:14Z | EloPool.cloud | [Redacted log](logs/911549.log) | 2026-09-05T22:27:12Z |
| [911502](https://www.blockchain.com/explorer/blocks/bch/911502) | 2025-08-13T17:50:32Z | EloPool.cloud | [Redacted log](logs/911502.log) | 2026-09-05T22:27:12Z |
| [911458](https://www.blockchain.com/explorer/blocks/bch/911458) | 2025-08-13T09:28:10Z | EloPool.cloud | [Redacted log](logs/911458.log) | 2026-09-05T22:27:12Z |
| [911379](https://www.blockchain.com/explorer/blocks/bch/911379) | 2025-08-12T19:25:34Z | EloPool.cloud | [Redacted log](logs/911379.log) | 2026-09-05T22:27:12Z |
| [911319](https://www.blockchain.com/explorer/blocks/bch/911319) | 2025-08-12T12:04:33Z | EloPool.cloud | [Redacted log](logs/911319.log) | 2026-09-05T22:27:13Z |
| [911228](https://www.blockchain.com/explorer/blocks/bch/911228) | 2025-08-11T21:37:48Z | EloPool.cloud | [Redacted log](logs/911228.log) | 2026-09-05T22:27:13Z |
| [911053](https://www.blockchain.com/explorer/blocks/bch/911053) | 2025-08-10T17:45:42Z | EloPool.cloud | [Redacted log](logs/911053.log) | 2026-09-05T22:27:13Z |
| [910620](https://www.blockchain.com/explorer/blocks/bch/910620) | 2025-08-07T16:16:38Z | EloPool.cloud | [Redacted log](logs/910620.log) | 2026-09-05T22:27:13Z |
| [910527](https://www.blockchain.com/explorer/blocks/bch/910527) | 2025-08-07T01:02:24Z | EloPool.cloud | [Redacted log](logs/910527.log) | 2026-09-05T22:27:13Z |
| [910381](https://www.blockchain.com/explorer/blocks/bch/910381) | 2025-08-06T01:36:21Z | EloPool.cloud | [Redacted log](logs/910381.log) | 2026-09-05T22:27:13Z |
| [910380](https://www.blockchain.com/explorer/blocks/bch/910380) | 2025-08-06T01:35:21Z | EloPool.cloud | [Redacted log](logs/910380.log) | 2026-09-05T22:27:14Z |
| [910200](https://www.blockchain.com/explorer/blocks/bch/910200) | 2025-08-04T18:36:27Z | EloPool.cloud | [Redacted log](logs/910200.log) | 2026-09-05T22:27:14Z |
| [910003](https://www.blockchain.com/explorer/blocks/bch/910003) | 2025-08-03T12:54:27Z | EloPool.cloud | [Redacted log](logs/910003.log) | 2026-09-05T22:27:14Z |
| [909933](https://www.blockchain.com/explorer/blocks/bch/909933) | 2025-08-03T00:47:04Z | EloPool.cloud | [Redacted log](logs/909933.log) | 2026-09-05T22:27:14Z |
| [909930](https://www.blockchain.com/explorer/blocks/bch/909930) | 2025-08-02T23:54:20Z | EloPool.cloud | [Redacted log](logs/909930.log) | 2026-09-05T22:27:14Z |
| [909890](https://www.blockchain.com/explorer/blocks/bch/909890) | 2025-08-02T18:12:32Z | EloPool.cloud | [Redacted log](logs/909890.log) | 2026-09-05T22:27:14Z |
| [909866](https://www.blockchain.com/explorer/blocks/bch/909866) | 2025-08-02T12:59:17Z | EloPool.cloud | [Redacted log](logs/909866.log) | 2026-09-05T22:27:14Z |
| [909802](https://www.blockchain.com/explorer/blocks/bch/909802) | 2025-08-02T02:57:20Z | EloPool.cloud | [Redacted log](logs/909802.log) | 2026-09-05T22:27:15Z |
| [909720](https://www.blockchain.com/explorer/blocks/bch/909720) | 2025-08-01T12:04:27Z | EloPool.cloud | [Redacted log](logs/909720.log) | 2026-09-05T22:27:15Z |
| [909701](https://www.blockchain.com/explorer/blocks/bch/909701) | 2025-08-01T08:55:07Z | EloPool.cloud | [Redacted log](logs/909701.log) | 2026-09-05T22:27:15Z |
| [909669](https://www.blockchain.com/explorer/blocks/bch/909669) | 2025-08-01T04:11:42Z | EloPool.cloud | [Redacted log](logs/909669.log) | 2026-09-05T22:27:15Z |
| [909494](https://www.blockchain.com/explorer/blocks/bch/909494) | 2025-07-30T22:30:45Z | EloPool.cloud | [Redacted log](logs/909494.log) | 2026-09-05T22:27:15Z |
| [909310](https://www.blockchain.com/explorer/blocks/bch/909310) | 2025-07-29T16:39:56Z | EloPool.cloud | [Redacted log](logs/909310.log) | 2026-09-05T22:27:16Z |
| [908385](https://www.blockchain.com/explorer/blocks/bch/908385) | 2025-07-23T13:05:56Z | EloPool.cloud | [Redacted log](logs/908385.log) | 2026-09-05T22:27:16Z |
| [908222](https://www.blockchain.com/explorer/blocks/bch/908222) | 2025-07-22T10:16:53Z | EloPool.Cloud | [Redacted log](logs/908222.log) | 2026-09-05T22:27:16Z |
| [907506](https://www.blockchain.com/explorer/blocks/bch/907506) | 2025-07-17T13:57:40Z | EloPool.Cloud | [Redacted log](logs/907506.log) | 2026-09-05T22:27:16Z |
| [907353](https://www.blockchain.com/explorer/blocks/bch/907353) | 2025-07-16T12:09:54Z | EloPool.Cloud | [Redacted log](logs/907353.log) | 2026-09-05T22:27:16Z |
| [907231](https://www.blockchain.com/explorer/blocks/bch/907231) | 2025-07-15T16:42:49Z | EloPool.Cloud | [Redacted log](logs/907231.log) | 2026-09-05T22:27:16Z |
