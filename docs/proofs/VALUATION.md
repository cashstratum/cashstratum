# Historical BCH rewards and USD valuation

**Updated:** 2026-09-05T22:37:38Z

**69 blocks · 215.80288934 BCH · approximately $121,575.15 USD at historical prices.**

The BCH total consists of **215.62500000 BCH subsidy** and **0.17788934 BCH transaction fees**. It is the gross coinbase reward, including all outputs, before any operator/miner split.

## Method

1. Sum all outputs of each verified coinbase transaction, in integer satoshis. Require that total to equal the chain API subsidy plus transaction fees.
2. Anchor the valuation to the public UTC block-header timestamp. It may differ from the pool log timestamp or actual discovery time.
3. Use the last completed Coinbase BCH/USD one-minute candle with nonzero trading volume at or before that timestamp. No later candle is used.
4. Record the Bitstamp BCH/USD last completed one-minute candle separately as a comparison. Some Bitstamp candles have zero volume and carry a previous price; their volumes are preserved in JSON. Do not average differently traded market observations.
5. Multiply exact BCH by the primary price using decimal arithmetic, round each USD result half-up to cents, then sum those displayed values.

All 69 blocks have both sources. Maximum primary candle-end age: **147 seconds**. Maximum quote difference: **0.4658%**. Using Bitstamp instead produces **$121,598.10**.

These are historical gross-value estimates, not sale proceeds, profit, present value or a
guarantee of earnings. No electricity, rental, operating or exchange costs are deducted.
The count describes the predecessor deployment of this modified CKPool lineage.

## Sources and reproducibility

- [Coinbase public candles documentation](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-product-candles)
- [Bitstamp OHLC API documentation](https://www.bitstamp.net/api/)
- [Full valuation records and exact endpoint URLs](valuations.json), including source candles and coinbase output amounts.
- [CSV download](valuations.csv)
- Run `python3 verify-values.py` to reproduce arithmetic and check time boundaries from the stored observations. This does not make new market-data requests.

## Per-block values

| Block | Date/time (UTC) | Gross BCH | BCH/USD — Coinbase | Gross USD estimate | BCH/USD — Bitstamp |
|---|---|---|---|---|---|
| [966569](https://www.blockchain.com/explorer/blocks/bch/966569) | 2026-09-01T03:50:05Z | 3.12542747 | $247.68 | $774.11 | $247.60 |
| [928165](https://www.blockchain.com/explorer/blocks/bch/928165) | 2025-12-06T08:33:03Z | 3.12570713 | $573.61 | $1,792.94 | $573.75 |
| [928098](https://www.blockchain.com/explorer/blocks/bch/928098) | 2025-12-05T21:48:01Z | 3.12636404 | $561.7 | $1,756.08 | $561.93 |
| [916984](https://www.blockchain.com/explorer/blocks/bch/916984) | 2025-09-20T10:02:02Z | 3.12630717 | $601.58 | $1,880.72 | $601.43 |
| [916429](https://www.blockchain.com/explorer/blocks/bch/916429) | 2025-09-16T16:46:10Z | 3.12644433 | $597.45 | $1,867.89 | $597.35 |
| [916154](https://www.blockchain.com/explorer/blocks/bch/916154) | 2025-09-14T20:24:45Z | 3.12613920 | $594.67 | $1,859.02 | $595.69 |
| [915924](https://www.blockchain.com/explorer/blocks/bch/915924) | 2025-09-13T05:51:49Z | 3.12621588 | $601.54 | $1,880.54 | $601.89 |
| [915824](https://www.blockchain.com/explorer/blocks/bch/915824) | 2025-09-12T14:05:13Z | 3.12603710 | $590.55 | $1,846.08 | $590.50 |
| [915321](https://www.blockchain.com/explorer/blocks/bch/915321) | 2025-09-09T00:18:10Z | 3.12628694 | $588.05 | $1,838.41 | $588.07 |
| [914850](https://www.blockchain.com/explorer/blocks/bch/914850) | 2025-09-05T15:51:57Z | 3.12508224 | $611.28 | $1,910.30 | $611.75 |
| [914754](https://www.blockchain.com/explorer/blocks/bch/914754) | 2025-09-05T04:27:58Z | 3.12661462 | $594.14 | $1,857.65 | $593.50 |
| [914557](https://www.blockchain.com/explorer/blocks/bch/914557) | 2025-09-03T18:34:12Z | 3.12508827 | $597.77 | $1,868.08 | $597.32 |
| [914527](https://www.blockchain.com/explorer/blocks/bch/914527) | 2025-09-03T12:31:03Z | 3.12934630 | $603.15 | $1,887.47 | $603.31 |
| [914486](https://www.blockchain.com/explorer/blocks/bch/914486) | 2025-09-03T06:58:03Z | 3.12538262 | $594.96 | $1,859.48 | $595.18 |
| [914439](https://www.blockchain.com/explorer/blocks/bch/914439) | 2025-09-03T00:21:32Z | 3.13660412 | $583.66 | $1,830.71 | $583.69 |
| [914407](https://www.blockchain.com/explorer/blocks/bch/914407) | 2025-09-02T18:31:08Z | 3.12650795 | $582.96 | $1,822.63 | $584.01 |
| [914239](https://www.blockchain.com/explorer/blocks/bch/914239) | 2025-09-01T18:20:42Z | 3.12502275 | $545.49 | $1,704.67 | $546.28 |
| [914226](https://www.blockchain.com/explorer/blocks/bch/914226) | 2025-09-01T15:30:33Z | 3.12770209 | $545.03 | $1,704.69 | $544.79 |
| [914122](https://www.blockchain.com/explorer/blocks/bch/914122) | 2025-08-31T22:13:50Z | 3.12823819 | $546.77 | $1,710.43 | $545.94 |
| [914076](https://www.blockchain.com/explorer/blocks/bch/914076) | 2025-08-31T13:48:39Z | 3.12511446 | $546.79 | $1,708.78 | $546.73 |
| [913971](https://www.blockchain.com/explorer/blocks/bch/913971) | 2025-08-30T21:06:26Z | 3.12886727 | $545.79 | $1,707.70 | $545.88 |
| [913959](https://www.blockchain.com/explorer/blocks/bch/913959) | 2025-08-30T18:52:28Z | 3.12535111 | $543.39 | $1,698.28 | $543.53 |
| [913820](https://www.blockchain.com/explorer/blocks/bch/913820) | 2025-08-29T21:30:32Z | 3.12591546 | $532.44 | $1,664.36 | $529.96 |
| [913680](https://www.blockchain.com/explorer/blocks/bch/913680) | 2025-08-28T21:41:37Z | 3.12629823 | $552.56 | $1,727.47 | $552.93 |
| [913451](https://www.blockchain.com/explorer/blocks/bch/913451) | 2025-08-27T06:54:42Z | 3.12507532 | $556.5 | $1,739.10 | $557.63 |
| [913423](https://www.blockchain.com/explorer/blocks/bch/913423) | 2025-08-27T03:28:18Z | 3.12612515 | $547.3 | $1,710.93 | $547.03 |
| [913256](https://www.blockchain.com/explorer/blocks/bch/913256) | 2025-08-25T21:10:12Z | 3.12562966 | $540.23 | $1,688.56 | $539.51 |
| [912928](https://www.blockchain.com/explorer/blocks/bch/912928) | 2025-08-23T14:20:52Z | 3.12593850 | $591.19 | $1,848.02 | $592.20 |
| [912893](https://www.blockchain.com/explorer/blocks/bch/912893) | 2025-08-23T09:19:27Z | 3.12594102 | $589.45 | $1,842.59 | $589.55 |
| [912853](https://www.blockchain.com/explorer/blocks/bch/912853) | 2025-08-23T02:03:17Z | 3.12819029 | $593.95 | $1,857.99 | $593.75 |
| [912775](https://www.blockchain.com/explorer/blocks/bch/912775) | 2025-08-22T15:24:50Z | 3.12507329 | $598.33 | $1,869.83 | $598.09 |
| [912602](https://www.blockchain.com/explorer/blocks/bch/912602) | 2025-08-21T10:36:39Z | 3.12708359 | $556.96 | $1,741.66 | $557.20 |
| [912567](https://www.blockchain.com/explorer/blocks/bch/912567) | 2025-08-21T04:56:04Z | 3.12595043 | $561.29 | $1,754.56 | $561.65 |
| [912378](https://www.blockchain.com/explorer/blocks/bch/912378) | 2025-08-19T21:25:47Z | 3.12527071 | $555.19 | $1,735.12 | $555.42 |
| [912116](https://www.blockchain.com/explorer/blocks/bch/912116) | 2025-08-18T01:23:17Z | 3.12665382 | $581.03 | $1,816.68 | $581.10 |
| [912035](https://www.blockchain.com/explorer/blocks/bch/912035) | 2025-08-17T11:38:53Z | 3.12758278 | $591.38 | $1,849.59 | $590.87 |
| [912009](https://www.blockchain.com/explorer/blocks/bch/912009) | 2025-08-17T07:22:41Z | 3.12691253 | $588.41 | $1,839.91 | $589.02 |
| [911920](https://www.blockchain.com/explorer/blocks/bch/911920) | 2025-08-16T15:29:57Z | 3.12609699 | $585.75 | $1,831.11 | $585.76 |
| [911903](https://www.blockchain.com/explorer/blocks/bch/911903) | 2025-08-16T13:18:47Z | 3.12542799 | $588.28 | $1,838.63 | $588.77 |
| [911683](https://www.blockchain.com/explorer/blocks/bch/911683) | 2025-08-14T23:39:09Z | 3.12604270 | $595.07 | $1,860.21 | $594.47 |
| [911569](https://www.blockchain.com/explorer/blocks/bch/911569) | 2025-08-14T03:52:09Z | 3.12686461 | $623.25 | $1,948.82 | $623.70 |
| [911549](https://www.blockchain.com/explorer/blocks/bch/911549) | 2025-08-14T01:54:14Z | 3.13555915 | $623.76 | $1,955.84 | $623.51 |
| [911502](https://www.blockchain.com/explorer/blocks/bch/911502) | 2025-08-13T17:50:32Z | 3.16220546 | $612.75 | $1,937.64 | $612.60 |
| [911458](https://www.blockchain.com/explorer/blocks/bch/911458) | 2025-08-13T09:28:10Z | 3.12576798 | $607.62 | $1,899.28 | $607.60 |
| [911379](https://www.blockchain.com/explorer/blocks/bch/911379) | 2025-08-12T19:25:34Z | 3.12522579 | $618.74 | $1,933.70 | $618.69 |
| [911319](https://www.blockchain.com/explorer/blocks/bch/911319) | 2025-08-12T12:04:33Z | 3.12636851 | $592.07 | $1,851.03 | $592.31 |
| [911228](https://www.blockchain.com/explorer/blocks/bch/911228) | 2025-08-11T21:37:48Z | 3.12518867 | $577.98 | $1,806.30 | $577.50 |
| [911053](https://www.blockchain.com/explorer/blocks/bch/911053) | 2025-08-10T17:45:42Z | 3.12537820 | $572.24 | $1,788.47 | $572.14 |
| [910620](https://www.blockchain.com/explorer/blocks/bch/910620) | 2025-08-07T16:16:38Z | 3.12621511 | $576.04 | $1,800.82 | $577.60 |
| [910527](https://www.blockchain.com/explorer/blocks/bch/910527) | 2025-08-07T01:02:24Z | 3.12912408 | $570.57 | $1,785.38 | $570.65 |
| [910381](https://www.blockchain.com/explorer/blocks/bch/910381) | 2025-08-06T01:36:21Z | 3.12536822 | $550.15 | $1,719.42 | $550.30 |
| [910380](https://www.blockchain.com/explorer/blocks/bch/910380) | 2025-08-06T01:35:21Z | 3.12658577 | $549.75 | $1,718.84 | $550.30 |
| [910200](https://www.blockchain.com/explorer/blocks/bch/910200) | 2025-08-04T18:36:27Z | 3.12513980 | $566.45 | $1,770.24 | $566.35 |
| [910003](https://www.blockchain.com/explorer/blocks/bch/910003) | 2025-08-03T12:54:27Z | 3.12624401 | $541.62 | $1,693.24 | $541.30 |
| [909933](https://www.blockchain.com/explorer/blocks/bch/909933) | 2025-08-03T00:47:04Z | 3.12616799 | $521.34 | $1,629.80 | $522.08 |
| [909930](https://www.blockchain.com/explorer/blocks/bch/909930) | 2025-08-02T23:54:20Z | 3.12572107 | $519.9 | $1,625.06 | $520.58 |
| [909890](https://www.blockchain.com/explorer/blocks/bch/909890) | 2025-08-02T18:12:32Z | 3.12798370 | $526.86 | $1,648.01 | $527.08 |
| [909866](https://www.blockchain.com/explorer/blocks/bch/909866) | 2025-08-02T12:59:17Z | 3.12558933 | $536.83 | $1,677.91 | $536.81 |
| [909802](https://www.blockchain.com/explorer/blocks/bch/909802) | 2025-08-02T02:57:20Z | 3.13888283 | $540.9 | $1,697.82 | $540.60 |
| [909720](https://www.blockchain.com/explorer/blocks/bch/909720) | 2025-08-01T12:04:27Z | 3.12543440 | $558.81 | $1,746.52 | $558.86 |
| [909701](https://www.blockchain.com/explorer/blocks/bch/909701) | 2025-08-01T08:55:07Z | 3.12640144 | $555.72 | $1,737.40 | $556.51 |
| [909669](https://www.blockchain.com/explorer/blocks/bch/909669) | 2025-08-01T04:11:42Z | 3.12635187 | $568.37 | $1,776.92 | $568.29 |
| [909494](https://www.blockchain.com/explorer/blocks/bch/909494) | 2025-07-30T22:30:45Z | 3.12615255 | $567.21 | $1,773.18 | $569.26 |
| [909310](https://www.blockchain.com/explorer/blocks/bch/909310) | 2025-07-29T16:39:56Z | 3.12671192 | $563.58 | $1,762.15 | $565.16 |
| [908385](https://www.blockchain.com/explorer/blocks/bch/908385) | 2025-07-23T13:05:56Z | 3.12764478 | $518.32 | $1,621.12 | $518.13 |
| [908222](https://www.blockchain.com/explorer/blocks/bch/908222) | 2025-07-22T10:16:53Z | 3.14416056 | $521.71 | $1,640.34 | $521.41 |
| [907506](https://www.blockchain.com/explorer/blocks/bch/907506) | 2025-07-17T13:57:40Z | 3.12608344 | $495.29 | $1,548.32 | $494.49 |
| [907353](https://www.blockchain.com/explorer/blocks/bch/907353) | 2025-07-16T12:09:54Z | 3.12655772 | $501.17 | $1,566.94 | $500.85 |
| [907231](https://www.blockchain.com/explorer/blocks/bch/907231) | 2025-07-15T16:42:49Z | 3.13075067 | $489.23 | $1,531.66 | $489.20 |
