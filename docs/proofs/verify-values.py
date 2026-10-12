#!/usr/bin/env python3
"""Verify saved reward arithmetic, valuation totals and price time boundaries (offline)."""
import json
from decimal import Decimal as D, ROUND_HALF_UP
from pathlib import Path
from datetime import datetime

root = Path(__file__).resolve().parent
data = json.loads((root / 'valuations.json').read_text())
proofs = {b['height']: b for b in json.loads((root / 'blocks.json').read_text())['blocks']}
rows = data['blocks']
assert len(rows) == data['summary']['blocks'] == len({b['height'] for b in rows})
assert {b['height'] for b in rows} == set(proofs)
sats = subsidy = fees = 0
usd = secondary = D(0)
for row in rows:
    evidence = row['evidence']
    assert evidence['height'] == row['height']
    timestamp = int(datetime.fromisoformat(row['block_time_utc'].replace('Z', '+00:00')).timestamp())
    assert timestamp == evidence['block_timestamp']
    assert row['block_time_utc'] == proofs[row['height']]['block_time_utc']
    reward = evidence['reward_satoshis']
    assert reward == sum(evidence['coinbase_outputs_satoshis'])
    assert reward == evidence['subsidy_satoshis'] + evidence['fees_satoshis']
    assert D(row['gross_reward_bch']) == D(reward) / D(100000000)
    for source, price_field, value_field, time_field in [
        ('coinbase', 'bch_usd', 'gross_value_usd', 'start'),
        ('bitstamp', 'bitstamp_bch_usd', 'bitstamp_comparison_value_usd', 'timestamp'),
    ]:
        candle = evidence[source]['candle']
        assert int(candle[time_field]) + 60 <= timestamp
        price = D(candle['close'])
        assert price > 0 and price == D(row[price_field])
        value = (D(row['gross_reward_bch']) * price).quantize(D('.01'), rounding=ROUND_HALF_UP)
        assert value == D(row[value_field])
    assert D(evidence['coinbase']['candle']['volume']) > 0
    assert row['price_age_seconds'] == timestamp - int(evidence['coinbase']['candle']['start']) - 60
    sats += reward
    subsidy += evidence['subsidy_satoshis']
    fees += evidence['fees_satoshis']
    usd += D(row['gross_value_usd'])
    secondary += D(row['bitstamp_comparison_value_usd'])
summary = data['summary']
assert D(summary['gross_reward_bch']) == D(sats) / D(100000000)
assert D(summary['subsidy_bch']) == D(subsidy) / D(100000000)
assert D(summary['transaction_fees_bch']) == D(fees) / D(100000000)
assert D(summary['historical_gross_value_usd']) == usd
assert D(summary['bitstamp_comparison_total_usd']) == secondary
print(f"{len(rows)} exchange valuations verified: {summary['gross_reward_bch']} BCH; ${usd:,.2f} historical gross value.")

operator = json.loads((root / 'operator-valuations.json').read_text())
operator_rows = operator['blocks']
operator_summary = operator['summary']
exchange_by_height = {row['height']: row for row in rows}
assert len(operator_rows) == len(proofs) == operator_summary['blocks']
assert {row['height'] for row in operator_rows} == set(proofs)
totals = {'stored_snapshot': D(0), 'historical_exchange_fallback': D(0), 'screenshot_snapshot': D(0)}
counts = dict.fromkeys(totals, 0)
operator_bch = D(0)
for row in operator_rows:
    proof = proofs[row['height']]
    exchange = exchange_by_height[row['height']]
    assert row['hash'] == proof['hash']
    assert row['block_time_utc'] == proof['block_time_utc']
    assert D(row['gross_reward_bch']) == D(exchange['gross_reward_bch'])
    price = D(row['bch_usd'])
    assert price > 0
    exact = D(row['gross_reward_bch']) * price
    assert exact == D(row['unrounded_gross_value_usd'])
    assert exact.quantize(D('.01'), ROUND_HALF_UP) == D(row['gross_value_usd'])
    basis = row['basis']
    if basis == 'historical_exchange_fallback':
        assert price == D(exchange['bch_usd'])
        assert row['price_candle_end_utc'] == exchange['price_candle_end_utc']
        assert row['fallback_reason'] and row['price_source'] == 'Coinbase BCH-USD'
    elif basis == 'screenshot_snapshot':
        assert row['price_source'] in ('BlockSniper Telegram notification', 'BlockSniper website block card')
        assert row['price_quality'] == 'rounded_display_price'
        assert len(row['screenshot_sha256']) == 64
        assert all(c in '0123456789abcdef' for c in row['screenshot_sha256'])
        assert row['observation_note']
    else:
        assert basis == 'stored_snapshot'
        assert row['price_source'] and row['price_quality']
        assert row['price_observed_at_as_stored'] and row['observation_time_note']
    totals[basis] += exact
    counts[basis] += 1
    operator_bch += D(row['gross_reward_bch'])
assert operator_bch == D(operator_summary['gross_reward_bch'])
assert counts['stored_snapshot'] == operator_summary['stored_snapshots']
assert counts['historical_exchange_fallback'] == operator_summary['historical_exchange_fallbacks']
assert counts['screenshot_snapshot'] == operator_summary['screenshot_snapshots']
for basis, field in [('stored_snapshot', 'snapshot_subtotal_usd'),
                     ('historical_exchange_fallback', 'fallback_subtotal_usd'),
                     ('screenshot_snapshot', 'screenshot_subtotal_usd')]:
    assert totals[basis].quantize(D('.01'), ROUND_HALF_UP) == D(operator_summary[field])
operator_usd = sum(totals.values()).quantize(D('.01'), ROUND_HALF_UP)
assert operator_usd == D(operator_summary['historical_gross_value_usd'])
print(f"{len(operator_rows)} operator valuations verified: ${operator_usd:,.2f}; "
      f"{counts['stored_snapshot']} database prices, {counts['screenshot_snapshot']} screenshot prices, "
      f"{counts['historical_exchange_fallback']} historical fallbacks.")
