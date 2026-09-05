#!/usr/bin/env python3
"""Recheck the published BCH block record without accessing pool infrastructure."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent
API = 'https://api.blockchain.info/haskoin-store/bch/'


def fetch(path):
    with urllib.request.urlopen(API + path, timeout=30) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--offline', action='store_true', help='Check record structure and excerpt checksums only; no chain verification')
    args = parser.parse_args()
    data = json.loads((ROOT / 'blocks.json').read_text())
    blocks = data['blocks']
    assert len(blocks) == data['count'] == len({b['height'] for b in blocks})
    for block in blocks:
        assert len(block['hash']) == 64 and block['mainchain'] is True
        if block['local_solve_log'] == 'recovered':
            excerpt = (ROOT / block['solve_excerpt']).read_bytes()
            assert hashlib.sha256(excerpt).hexdigest() == block['solve_excerpt_sha256']
            assert f"Solved and confirmed block {block['height']} by [redacted]" in excerpt.decode()
        if not args.offline:
            candidates = fetch(f"block/height/{block['height']}")
            header = next(b for b in candidates if b['hash'] == block['hash'] and b['mainchain'])
            assert header['height'] == block['height']
            assert header['tx'][0] == block['coinbase_txid']
            tx = fetch('transaction/' + block['coinbase_txid'])
            assert tx['inputs'][0]['coinbase'] is True
            assert tx['inputs'][0]['sigscript'] == block['coinbase_script_hex']
            assert tx['block']['height'] == block['height']
        print(f"OK {block['height']}")
    print(f"{len(blocks)} records passed ({'offline structure and excerpt checks only' if args.offline else 'live BCH chain verification'}).")


if __name__ == '__main__':
    main()
