# Historical value at each block's price

**81 verified blocks · 253.31946287 BCH · approximately $132,752.44 USD.**

Database snapshot read: **2026-10-12T08:04:07+00:00**; screenshot observation added
**2026-10-12T08:47:42.393199+00:00**. These amounts cover exactly the heights in the
[proof index](README.md), including block 972535. They do not represent every block in a
BlockSniper account's mining history.

## Where the prices come from

| Price basis | Blocks | Historical gross value |
|---|---:|---:|
| Saved BlockSniper per-block price | 72 | $119,404.08 |
| Operator-supplied BlockSniper price screenshots | 2 | $1,784.89 |
| Historical Coinbase minute-price fallback | 7 | $11,563.47 |
| **Total** | **81** | **$132,752.44** |

The primary price is the immutable `price_usd` stored for each block in BlockSniper. Each
price is multiplied by that block's full on-chain coinbase reward, including transaction
fees. Today's BCH price is never applied to old rewards in this collection.

7 blocks have no saved price snapshot in this collection: 915321, 912116, 908385, 908222, 907506, 907353, 907231.
For these, use the previously recorded last completed, nonzero-volume Coinbase BCH/USD
minute candle at or before the UTC block-header timestamp. Every fallback is marked in the
table and machine-readable export.

Block **972529** uses the **$285.84/BCH** displayed in the operator-supplied BlockSniper
Telegram notification, recorded on 2026-10-12. Applied to its verified **3.12620441 BCH**
gross reward, this gives **$893.59**. This is a rounded display-price observation, not a
database-verified price. The notification's **$875.72** refers to the miner's **3.06368033 BCH**
output; the gross amount also includes the **0.06252408 BCH** operator fee.
The machine-readable record preserves the source screenshot's checksum and this distinction.

Block **972535** uses the **$891.30** shown on the BlockSniper website block card in a second
operator-supplied screenshot, recorded on 2026-10-12. Dividing it by the verified
**3.12528740 BCH** gross reward gives **$285.19/BCH**, which reproduces $891.30 exactly. It is
likewise rounded display evidence, not a database-verified price; its checksum is preserved.

The latest saved values match the supplied BlockSniper display: **972524 — $896.29;
972517 — $894.66; 972318 — $874.92; 972313 — $875.68**. The screenshot's **183 blocks,
572.41398136 BCH and $246,260.68** describe a broader account view, not this 81-block proof
collection. They are not substituted for the verified totals here.

## Calculation and limitations

- Use the verified coinbase amount, not the three-decimal reward label shown on a card.
- Preserve each saved price, source and quality label. Saved snapshots are observations near
  the block time; they are not required to be a completed exchange candle before the block.
- Sum the exact decimal products and round the final total half-up to cents, matching the
  account view's aggregation approach. Individually rounded rows can differ by a few cents
  from the total.
- Raw snapshot timestamps are retained verbatim. Older records used naive UTC strings and
  newer records use the application timezone; no uniform timezone is asserted for that
  field. The block dates in the table are independently verified UTC header timestamps.
- These are historical gross-value estimates before operator/miner splits and costs, not
  realized sale proceeds or profit. Database observations are operator-supplied evidence;
  the blockchain records BCH, not a USD exchange rate.

## Reproduce or compare

- [Per-block snapshot and fallback records](operator-valuations.json) · [CSV](operator-valuations.csv).
- [Independent Coinbase and Bitstamp calculation](EXCHANGE-VALUATION.md): approximately
  **$132,647.28** using only Coinbase prices.
  That method remains available with the original candles and time-boundary checks.
- Run `python3 verify-values.py` to check both calculations offline, including reward
  equality, price multiplication, totals and explicit fallback matching.

## Per-block values

| Block | Block date (UTC) | Gross BCH | BCH/USD | Gross USD estimate | Price basis |
|---|---|---|---|---|---|
| [972535](https://www.blockchain.com/explorer/blocks/bch/972535) | 2026-10-12T08:57:29Z | 3.12528740 | $285.19 | $891.30 | Website screenshot |
| [972529](https://www.blockchain.com/explorer/blocks/bch/972529) | 2026-10-12T08:20:18Z | 3.12620441 | $285.84 | $893.59 | Telegram screenshot |
| [972524](https://www.blockchain.com/explorer/blocks/bch/972524) | 2026-10-12T07:37:58Z | 3.12645157 | $286.68 | $896.29 | BlockSniper database |
| [972517](https://www.blockchain.com/explorer/blocks/bch/972517) | 2026-10-12T06:26:54Z | 3.12535292 | $286.26 | $894.66 | BlockSniper database |
| [972318](https://www.blockchain.com/explorer/blocks/bch/972318) | 2026-10-10T18:34:48Z | 3.12670989 | $279.82 | $874.92 | BlockSniper database |
| [972313](https://www.blockchain.com/explorer/blocks/bch/972313) | 2026-10-10T17:59:58Z | 3.12766941 | $279.98 | $875.68 | BlockSniper database |
| [972310](https://www.blockchain.com/explorer/blocks/bch/972310) | 2026-10-10T17:36:18Z | 3.12535461 | $280.08 | $875.35 | BlockSniper database |
| [971860](https://www.blockchain.com/explorer/blocks/bch/971860) | 2026-10-07T11:04:52Z | 3.12500998 | $304.03 | $950.10 | BlockSniper database |
| [971817](https://www.blockchain.com/explorer/blocks/bch/971817) | 2026-10-07T02:45:23Z | 3.12910966 | $306.00 | $957.51 | BlockSniper database |
| [971770](https://www.blockchain.com/explorer/blocks/bch/971770) | 2026-10-06T18:07:48Z | 3.12575307 | $315.79 | $987.08 | BlockSniper database |
| [971744](https://www.blockchain.com/explorer/blocks/bch/971744) | 2026-10-06T14:15:20Z | 3.12597374 | $315.71 | $986.90 | BlockSniper database |
| [971726](https://www.blockchain.com/explorer/blocks/bch/971726) | 2026-10-06T11:05:31Z | 3.12769687 | $317.43 | $992.82 | BlockSniper database |
| [966569](https://www.blockchain.com/explorer/blocks/bch/966569) | 2026-09-01T03:50:05Z | 3.12542747 | $248.17 | $775.64 | BlockSniper database |
| [928165](https://www.blockchain.com/explorer/blocks/bch/928165) | 2025-12-06T08:33:03Z | 3.12570713 | $584.80 | $1,827.91 | BlockSniper database |
| [928098](https://www.blockchain.com/explorer/blocks/bch/928098) | 2025-12-05T21:48:01Z | 3.12636404 | $564.17 | $1,763.80 | BlockSniper database |
| [916984](https://www.blockchain.com/explorer/blocks/bch/916984) | 2025-09-20T10:02:02Z | 3.12630717 | $600.87 | $1,878.50 | BlockSniper database |
| [916429](https://www.blockchain.com/explorer/blocks/bch/916429) | 2025-09-16T16:46:10Z | 3.12644433 | $599.36 | $1,873.87 | BlockSniper database |
| [916154](https://www.blockchain.com/explorer/blocks/bch/916154) | 2025-09-14T20:24:45Z | 3.12613920 | $603.27 | $1,885.91 | BlockSniper database |
| [915924](https://www.blockchain.com/explorer/blocks/bch/915924) | 2025-09-13T05:51:49Z | 3.12621588 | $601.14 | $1,879.29 | BlockSniper database |
| [915824](https://www.blockchain.com/explorer/blocks/bch/915824) | 2025-09-12T14:05:13Z | 3.12603710 | $590.93 | $1,847.27 | BlockSniper database |
| [915321](https://www.blockchain.com/explorer/blocks/bch/915321) | 2025-09-09T00:18:10Z | 3.12628694 | $588.05 | $1,838.41 | Coinbase fallback |
| [914850](https://www.blockchain.com/explorer/blocks/bch/914850) | 2025-09-05T15:51:57Z | 3.12508224 | $613.37 | $1,916.83 | BlockSniper database |
| [914754](https://www.blockchain.com/explorer/blocks/bch/914754) | 2025-09-05T04:27:58Z | 3.12661462 | $592.88 | $1,853.71 | BlockSniper database |
| [914557](https://www.blockchain.com/explorer/blocks/bch/914557) | 2025-09-03T18:34:12Z | 3.12508827 | $599.55 | $1,873.65 | BlockSniper database |
| [914527](https://www.blockchain.com/explorer/blocks/bch/914527) | 2025-09-03T12:31:03Z | 3.12934630 | $601.03 | $1,880.83 | BlockSniper database |
| [914486](https://www.blockchain.com/explorer/blocks/bch/914486) | 2025-09-03T06:58:03Z | 3.12538262 | $600.58 | $1,877.04 | BlockSniper database |
| [914439](https://www.blockchain.com/explorer/blocks/bch/914439) | 2025-09-03T00:21:32Z | 3.13660412 | $586.04 | $1,838.18 | BlockSniper database |
| [914407](https://www.blockchain.com/explorer/blocks/bch/914407) | 2025-09-02T18:31:08Z | 3.12650795 | $583.26 | $1,823.57 | BlockSniper database |
| [914239](https://www.blockchain.com/explorer/blocks/bch/914239) | 2025-09-01T18:20:42Z | 3.12502275 | $541.85 | $1,693.29 | BlockSniper database |
| [914226](https://www.blockchain.com/explorer/blocks/bch/914226) | 2025-09-01T15:30:33Z | 3.12770209 | $540.15 | $1,689.43 | BlockSniper database |
| [914122](https://www.blockchain.com/explorer/blocks/bch/914122) | 2025-08-31T22:13:50Z | 3.12823819 | $542.90 | $1,698.32 | BlockSniper database |
| [914076](https://www.blockchain.com/explorer/blocks/bch/914076) | 2025-08-31T13:48:39Z | 3.12511446 | $546.90 | $1,709.13 | BlockSniper database |
| [913971](https://www.blockchain.com/explorer/blocks/bch/913971) | 2025-08-30T21:06:26Z | 3.12886727 | $549.08 | $1,718.00 | BlockSniper database |
| [913959](https://www.blockchain.com/explorer/blocks/bch/913959) | 2025-08-30T18:52:28Z | 3.12535111 | $544.07 | $1,700.41 | BlockSniper database |
| [913820](https://www.blockchain.com/explorer/blocks/bch/913820) | 2025-08-29T21:30:32Z | 3.12591546 | $530.87 | $1,659.45 | BlockSniper database |
| [913680](https://www.blockchain.com/explorer/blocks/bch/913680) | 2025-08-28T21:41:37Z | 3.12629823 | $557.08 | $1,741.60 | BlockSniper database |
| [913451](https://www.blockchain.com/explorer/blocks/bch/913451) | 2025-08-27T06:54:42Z | 3.12507532 | $552.61 | $1,726.95 | BlockSniper database |
| [913423](https://www.blockchain.com/explorer/blocks/bch/913423) | 2025-08-27T03:28:18Z | 3.12612515 | $560.19 | $1,751.22 | BlockSniper database |
| [913256](https://www.blockchain.com/explorer/blocks/bch/913256) | 2025-08-25T21:10:12Z | 3.12562966 | $543.82 | $1,699.78 | BlockSniper database |
| [912928](https://www.blockchain.com/explorer/blocks/bch/912928) | 2025-08-23T14:20:52Z | 3.12593850 | $588.46 | $1,839.49 | BlockSniper database |
| [912893](https://www.blockchain.com/explorer/blocks/bch/912893) | 2025-08-23T09:19:27Z | 3.12594102 | $586.97 | $1,834.83 | BlockSniper database |
| [912853](https://www.blockchain.com/explorer/blocks/bch/912853) | 2025-08-23T02:03:17Z | 3.12819029 | $592.45 | $1,853.30 | BlockSniper database |
| [912775](https://www.blockchain.com/explorer/blocks/bch/912775) | 2025-08-22T15:24:50Z | 3.12507329 | $602.01 | $1,881.33 | BlockSniper database |
| [912602](https://www.blockchain.com/explorer/blocks/bch/912602) | 2025-08-21T10:36:39Z | 3.12708359 | $551.70 | $1,725.21 | BlockSniper database |
| [912567](https://www.blockchain.com/explorer/blocks/bch/912567) | 2025-08-21T04:56:04Z | 3.12595043 | $559.45 | $1,748.81 | BlockSniper database |
| [912378](https://www.blockchain.com/explorer/blocks/bch/912378) | 2025-08-19T21:25:47Z | 3.12527071 | $552.80 | $1,727.65 | BlockSniper database |
| [912116](https://www.blockchain.com/explorer/blocks/bch/912116) | 2025-08-18T01:23:17Z | 3.12665382 | $581.03 | $1,816.68 | Coinbase fallback |
| [912035](https://www.blockchain.com/explorer/blocks/bch/912035) | 2025-08-17T11:38:53Z | 3.12758278 | $589.28 | $1,843.02 | BlockSniper database |
| [912009](https://www.blockchain.com/explorer/blocks/bch/912009) | 2025-08-17T07:22:41Z | 3.12691253 | $591.40 | $1,849.26 | BlockSniper database |
| [911920](https://www.blockchain.com/explorer/blocks/bch/911920) | 2025-08-16T15:29:57Z | 3.12609699 | $578.28 | $1,807.76 | BlockSniper database |
| [911903](https://www.blockchain.com/explorer/blocks/bch/911903) | 2025-08-16T13:18:47Z | 3.12542799 | $580.11 | $1,813.09 | BlockSniper database |
| [911683](https://www.blockchain.com/explorer/blocks/bch/911683) | 2025-08-14T23:39:09Z | 3.12604270 | $594.94 | $1,859.81 | BlockSniper database |
| [911569](https://www.blockchain.com/explorer/blocks/bch/911569) | 2025-08-14T03:52:09Z | 3.12686461 | $614.62 | $1,921.83 | BlockSniper database |
| [911549](https://www.blockchain.com/explorer/blocks/bch/911549) | 2025-08-14T01:54:14Z | 3.13555915 | $625.17 | $1,960.26 | BlockSniper database |
| [911502](https://www.blockchain.com/explorer/blocks/bch/911502) | 2025-08-13T17:50:32Z | 3.16220546 | $612.05 | $1,935.43 | BlockSniper database |
| [911458](https://www.blockchain.com/explorer/blocks/bch/911458) | 2025-08-13T09:28:10Z | 3.12576798 | $607.51 | $1,898.94 | BlockSniper database |
| [911379](https://www.blockchain.com/explorer/blocks/bch/911379) | 2025-08-12T19:25:34Z | 3.12522579 | $618.08 | $1,931.64 | BlockSniper database |
| [911319](https://www.blockchain.com/explorer/blocks/bch/911319) | 2025-08-12T12:04:33Z | 3.12636851 | $592.49 | $1,852.34 | BlockSniper database |
| [911228](https://www.blockchain.com/explorer/blocks/bch/911228) | 2025-08-11T21:37:48Z | 3.12518867 | $580.04 | $1,812.73 | BlockSniper database |
| [911053](https://www.blockchain.com/explorer/blocks/bch/911053) | 2025-08-10T17:45:42Z | 3.12537820 | $570.35 | $1,782.56 | BlockSniper database |
| [910620](https://www.blockchain.com/explorer/blocks/bch/910620) | 2025-08-07T16:16:38Z | 3.12621511 | $577.41 | $1,805.11 | BlockSniper database |
| [910527](https://www.blockchain.com/explorer/blocks/bch/910527) | 2025-08-07T01:02:24Z | 3.12912408 | $569.48 | $1,781.97 | BlockSniper database |
| [910381](https://www.blockchain.com/explorer/blocks/bch/910381) | 2025-08-06T01:36:21Z | 3.12536822 | $556.79 | $1,740.17 | BlockSniper database |
| [910380](https://www.blockchain.com/explorer/blocks/bch/910380) | 2025-08-06T01:35:21Z | 3.12658577 | $556.79 | $1,740.85 | BlockSniper database |
| [910200](https://www.blockchain.com/explorer/blocks/bch/910200) | 2025-08-04T18:36:27Z | 3.12513980 | $559.65 | $1,748.98 | BlockSniper database |
| [910003](https://www.blockchain.com/explorer/blocks/bch/910003) | 2025-08-03T12:54:27Z | 3.12624401 | $540.63 | $1,690.14 | BlockSniper database |
| [909933](https://www.blockchain.com/explorer/blocks/bch/909933) | 2025-08-03T00:47:04Z | 3.12616799 | $528.44 | $1,651.99 | BlockSniper database |
| [909930](https://www.blockchain.com/explorer/blocks/bch/909930) | 2025-08-02T23:54:20Z | 3.12572107 | $528.12 | $1,650.76 | BlockSniper database |
| [909890](https://www.blockchain.com/explorer/blocks/bch/909890) | 2025-08-02T18:12:32Z | 3.12798370 | $527.64 | $1,650.45 | BlockSniper database |
| [909866](https://www.blockchain.com/explorer/blocks/bch/909866) | 2025-08-02T12:59:17Z | 3.12558933 | $531.27 | $1,660.53 | BlockSniper database |
| [909802](https://www.blockchain.com/explorer/blocks/bch/909802) | 2025-08-02T02:57:20Z | 3.13888283 | $539.55 | $1,693.58 | BlockSniper database |
| [909720](https://www.blockchain.com/explorer/blocks/bch/909720) | 2025-08-01T12:04:27Z | 3.12543440 | $556.59 | $1,739.59 | BlockSniper database |
| [909701](https://www.blockchain.com/explorer/blocks/bch/909701) | 2025-08-01T08:55:07Z | 3.12640144 | $555.20 | $1,735.78 | BlockSniper database |
| [909669](https://www.blockchain.com/explorer/blocks/bch/909669) | 2025-08-01T04:11:42Z | 3.12635187 | $564.77 | $1,765.67 | BlockSniper database |
| [909494](https://www.blockchain.com/explorer/blocks/bch/909494) | 2025-07-30T22:30:45Z | 3.12615255 | $583.63 | $1,824.52 | BlockSniper database |
| [909310](https://www.blockchain.com/explorer/blocks/bch/909310) | 2025-07-29T16:39:56Z | 3.12671192 | $566.03 | $1,769.81 | BlockSniper database |
| [908385](https://www.blockchain.com/explorer/blocks/bch/908385) | 2025-07-23T13:05:56Z | 3.12764478 | $518.32 | $1,621.12 | Coinbase fallback |
| [908222](https://www.blockchain.com/explorer/blocks/bch/908222) | 2025-07-22T10:16:53Z | 3.14416056 | $521.71 | $1,640.34 | Coinbase fallback |
| [907506](https://www.blockchain.com/explorer/blocks/bch/907506) | 2025-07-17T13:57:40Z | 3.12608344 | $495.29 | $1,548.32 | Coinbase fallback |
| [907353](https://www.blockchain.com/explorer/blocks/bch/907353) | 2025-07-16T12:09:54Z | 3.12655772 | $501.17 | $1,566.94 | Coinbase fallback |
| [907231](https://www.blockchain.com/explorer/blocks/bch/907231) | 2025-07-15T16:42:49Z | 3.13075067 | $489.23 | $1,531.66 | Coinbase fallback |
