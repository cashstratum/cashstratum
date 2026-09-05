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
assert len(rows) == 69 == len({b['height'] for b in rows})
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
print(f"69 valuations verified: {summary['gross_reward_bch']} BCH; ${usd:,.2f} historical gross value.")
