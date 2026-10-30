#!/usr/bin/env python3
"""Verify the additional website proof snapshot, without private pool access."""
import argparse
from datetime import datetime, timezone
from decimal import Decimal, ROUND_HALF_UP
import hashlib
import re
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent
API = "https://api.blockchain.info/haskoin-store/bch/"


def fetch(path):
    with urllib.request.urlopen(API + path, timeout=30) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--offline", action="store_true")
    args = parser.parse_args()
    snapshot = json.loads((ROOT / "recent-blocks.json").read_text())
    archived = json.loads((ROOT / "blocks.json").read_text())
    existing = {b["height"] for b in archived["blocks"]}
    records = snapshot["blocks"]
    assert len(records) == snapshot["count"] == len({b["height"] for b in records})
    # The miner-history source accounts for part of the records; later solve-log heights add the rest.
    assert snapshot["source_block_count"] - snapshot["already_in_archived_snapshot"] <= len(records)
    for b in records:
        assert b["height"] not in existing
        assert len(bytes.fromhex(b["hash"])) == 32 and b["mainchain"] is True
        assert b"BlockSniper.ai" in bytes.fromhex(b["coinbase_script_hex"])
        assert b["reward_satoshis"] == sum(b["coinbase_outputs_satoshis"])
        assert b["reward_satoshis"] == b["subsidy_satoshis"] + b["fees_satoshis"]
        assert Decimal(b["gross_reward_bch"]) == Decimal(b["reward_satoshis"]) / Decimal(100_000_000)
        excerpt = (ROOT / b["solve_excerpt"]).read_bytes()
        assert hashlib.sha256(excerpt).hexdigest() == b["solve_excerpt_sha256"]
        assert f"Solved and confirmed block {b['height']} by [redacted]" in excerpt.decode()
        assert "BLOCK ACCEPTED!" in excerpt.decode() and b["hash"] in excerpt.decode()
        assert not re.search(r"bitcoincash:|bchtest:|(?:\d{1,3}\.){3}\d{1,3}|bearer|password", excerpt.decode(), re.I)
        integrity = b["source_integrity"]
        if integrity["sidecar_sha256_matches"] is not None or integrity["database_sha256_matches"] is not None:
            assert integrity["sidecar_sha256_matches"] and integrity["database_sha256_matches"]
        valuation = b["valuation"]
        value = (Decimal(b["gross_reward_bch"]) * Decimal(valuation["bch_usd"])).quantize(Decimal(".01"), rounding=ROUND_HALF_UP)
        assert value == Decimal(valuation["gross_value_usd"]) == Decimal(b["historical_value_usd"])
        if not args.offline:
            header = next(h for h in fetch(f"block/height/{b['height']}") if h["mainchain"])
            assert header["hash"] == b["hash"] and header["tx"][0] == b["coinbase_txid"]
            timestamp = datetime.fromtimestamp(header["time"], timezone.utc).isoformat().replace("+00:00", "Z")
            assert timestamp == b["block_time_utc"]
            tx = fetch("transaction/" + b["coinbase_txid"])
            assert tx["inputs"][0]["coinbase"] is True and tx["block"]["height"] == b["height"]
            assert tx["inputs"][0]["sigscript"] == b["coinbase_script_hex"]
            assert [o["value"] for o in tx["outputs"]] == b["coinbase_outputs_satoshis"]
        print(f"OK {b['height']}")
    total = sum(Decimal(b["gross_reward_bch"]) for b in records)
    assert total == Decimal(snapshot["summary"]["gross_reward_bch"])
    value_total = sum(Decimal(b["historical_value_usd"]) for b in records)
    assert value_total == Decimal(snapshot["summary"]["historical_gross_value_usd"])
    print(f"{len(records)} additional records passed ({'offline structure and arithmetic' if args.offline else 'live BCH chain verification'}).")


if __name__ == "__main__":
    main()
