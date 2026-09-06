# Coinbase API Endpoint

## Overview

The `/coinbase` endpoint connects to your CKPool stratum server, retrieves the **current candidate block template's coinbase transaction**, and returns detailed information about it including pool fee splits and block height.

**Important:** This endpoint returns the template the pool is currently working on, not proof of a found block. Each uncached request costs one short-lived stratum authorisation (a temporary phantom worker named `<user>.ckpool-api`). Concurrent matching requests for the same username share one probe; different candidate heights queue behind that username's active probe and resolve separately. On an idle pool with infrequent work broadcasts, the endpoint can take up to the pool's `update_interval` (default 30 seconds) plus 5 seconds of network slack to receive a job, and returns HTTP 504 (Gateway Timeout) with a JSON error body if the job never arrives.

Use this for display purposes — to show miners what they're working toward if a block is found right now — rather than for critical mining decisions.

## Endpoint

```
GET /coinbase?user=<username>&candidate_height=<height>
```

### Parameters

- `user` (optional): Username to display in the response and to authorize with the pool.
  The endpoint authorizes as `<user>.ckpool-api` to create a temporary, identifiable phantom
  worker for the block template fetch (see **Probe workers** below). Defaults to "anonymous" if
  not provided. This parameter is not validated or sanitized — it is safe only because it is
  JSON-encoded into the stratum message and never reaches a shell or file path.
- `candidate_height` (optional): Positive decimal height of the candidate block the caller is
  displaying. When supplied, a successful proof decoded at that same height is reusable for the
  username until the caller sends a different height (subject to bounded-cache eviction or an API
  restart). A malformed or zero value returns HTTP 400. Omitting this parameter preserves the
  legacy 10-second freshness window.

### Probe workers

Each uncached `/coinbase` probe creates a temporary phantom worker named `<user>.ckpool-api` that
appears in the miner's stats file under `worker[].workername`. When `/user-file` returns a
miner's status, it automatically filters out these probe entries and subtracts the exact count
from the file's `workers` field — so the phantom load from periodic `/coinbase` polls does not
inflate the visible worker count. Matching concurrent misses are coalesced, while different
candidate heights are serialized, so one username never creates simultaneous probe workers.

### Headers

```
Authorization: Bearer YOUR_API_KEY
```

## Response Format

```json
{
  "username": "anonymous",
  "coinbase_hex": "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff31037bbe0e00049889936a04ed2e391a0c4068936a000000000000000014456c6f506f6f6c2e636c6f75642f5b536f6c6f5dffffffff02eedb7612000000001976a91401c09ad61cb2ef44f812441153318c332d3b651088acfdbe2f00000000001976a91401c09ad61cb2ef44f812441153318c332d3b651088ac00000000",
  "coinbase_message": "",
  "outputs": [
    {
      "value": 309779438,
      "value_bch": "3.09779438",
      "address": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
      "type": "miner"
    },
    {
      "value": 3129085,
      "value_bch": "0.03129085",
      "address": "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS",
      "type": "pool_fee"
    }
  ],
  "total_value": 312908523,
  "total_value_bch": "3.12908523",
  "block_height": 966267,
  "network_bits": "1802762c",
  "timestamp": 1788053906
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `username` | string | The username parameter provided in the request |
| `coinbase_hex` | string | Full coinbase transaction in hexadecimal format |
| `coinbase_message` | string | Pool branding message extracted from coinbase (e.g., "BlockSniper.ai/[Solo]") |
| `outputs` | array | Array of coinbase outputs (miner reward + pool fee) |
| `total_value` | integer | Total block reward in satoshis |
| `total_value_bch` | string | Total block reward formatted in BCH |
| `block_height` | integer | Current block height being mined |
| `network_bits` | string | Network difficulty bits in hex format |
| `timestamp` | integer | Unix timestamp when the response was generated |
| `error` | string | Error message if request failed (only present on error) |

### Output Object

Each output in the `outputs` array contains:

| Field | Type | Description |
|-------|------|-------------|
| `value` | integer | Output value in satoshis |
| `value_bch` | string | Output value formatted in BCH (8 decimal places) |
| `address` | string | Bitcoin Cash address receiving the output (legacy Base58 format) |
| `type` | string | "miner" for main reward, "pool_fee" for operator fee |

**Note:** Address values are returned in **legacy Base58 format**. If your application uses CashAddr format (e.g., `bitcoincash:...`), you must convert the addresses before use.

## Example Usage

### cURL

```bash
curl -H "Authorization: Bearer your_api_key_here" \
  "http://localhost:8888/coinbase?user=myworker"
```

### JavaScript (fetch)

```javascript
const response = await fetch('http://localhost:8888/coinbase?user=myworker', {
  headers: {
    'Authorization': 'Bearer your_api_key_here'
  }
});

const data = await response.json();
console.log('Coinbase message:', data.coinbase_message);
console.log('Miner will receive:', data.outputs[0].value_bch, 'BCH');
console.log('Pool fee:', data.outputs[1].value_bch, 'BCH');
```

### Python

```python
import requests

headers = {'Authorization': 'Bearer your_api_key_here'}
response = requests.get(
    'http://localhost:8888/coinbase?user=myworker',
    headers=headers
)

data = response.json()
print(f"Mining to: {data['outputs'][0]['address']}")
print(f"Miner reward: {data['outputs'][0]['value_bch']} BCH")
print(f"Pool fee: {data['outputs'][1]['value_bch']} BCH")
```

## Caching and Performance

- With `candidate_height`, a successful proof is cached by **username plus the decoded block
  height**. Repeating the same height returns instantly without a time-based refresh. A changed
  height deterministically triggers a new probe.
- The server always indexes a new proof by the `block_height` decoded from the coinbase, never by
  an unverified requested height. If the chain advances during a request, the response contains
  the new actual height and is cached under that height; the stale requested height remains a miss.
  Each username retains only its latest successful candidate proof, so a new decoded candidate
  replaces the prior height rather than preserving stale-height history.
- Without `candidate_height`, successful responses keep the legacy **10-second per-username**
  cache window.
- Failed probes are not cached.
- Concurrent misses for the same username and candidate share one in-flight Stratum probe and
  receive the same result. Different candidate heights wait for that probe slot, then perform their
  own cache resolution/probe; they never receive a mismatched candidate's result.
- On an idle pool (no new work broadcast for many seconds), a cache miss can block for up to
  the pool's `update_interval` + 5 s (default 35 s total) waiting for a job, and returns HTTP 504
  if the timeout is reached
- Connection timeout to the stratum server is 5 seconds; if stratum itself is unreachable, the error
  is returned immediately

### Candidate-height freshness tradeoff

Block height is a deterministic invalidation boundary, but it is not a complete template generation
identifier. CKPool may update the coinbase within the same height as fees or candidate work change.
Using `candidate_height` intentionally favors eliminating repeat phantom probes over observing those
within-height changes. Consumers that must display the freshest template within a height should omit
`candidate_height` and use the legacy 10-second refresh path. The cached response's `timestamp` is the
time the proof was generated, not the time it was served from cache.

## Use Cases

### 1. Display Current Block Reward

Show miners exactly what they'll receive when a block is found:

```javascript
const coinbase = await getCoinbaseInfo('myworker');
console.log(`If you find a block now, you'll receive ${coinbase.outputs[0].value_bch} BCH`);
```

### 2. Verify Pool Fee

Allow miners to verify the pool is taking the correct fee percentage:

```javascript
const total = parseFloat(coinbase.total_value_bch);
const fee = parseFloat(coinbase.outputs[1].value_bch);
const feePercentage = (fee / total * 100).toFixed(2);
console.log(`Pool fee: ${feePercentage}%`);
```

### 3. Display Pool Branding

Show the pool's coinbase message to confirm identity:

```javascript
console.log(`Mining on: ${coinbase.coinbase_message}`);
```

### 4. Transaction Transparency

Display the full coinbase hex for miners who want to verify the exact transaction:

```html
<details>
  <summary>View Raw Coinbase Transaction</summary>
  <code>{coinbase.coinbase_hex}</code>
</details>
```

## Error Handling

If the request fails, the response will contain an `error` field. The HTTP status code indicates the failure class:

**HTTP 200 with `error` field:** Transport worked but the pool or authorization failed.

```json
{
  "username": "myworker",
  "timestamp": 1760031366,
  "error": "Pool did not authorise <user>.ckpool-api -- a username that is neither a valid BCH address nor a plain name is rejected outright"
}
```

**HTTP 504 Gateway Timeout with `error` field:** The stratum connection succeeded but no mining job arrived within the deadline (default: `update_interval` 30 s + 5 s slack). On an idle pool this is a real, expected outcome, not a hard fault.

```json
{
  "username": "myworker",
  "timestamp": 1760031366,
  "error": "Stratum read failed after authorisation, while waiting for a job: context deadline exceeded"
}
```

**HTTP 502 Bad Gateway with `error` field:** Connection to stratum failed or broke mid-conversation.

```json
{
  "username": "myworker",
  "timestamp": 1760031366,
  "error": "Failed to connect to pool: connection refused"
}
```

Common error scenarios:
- Stratum is unreachable — connection refused, connection timeout → HTTP 502
- Username is invalid (neither a BCH address nor a plain name) → HTTP 200 with error
- Pool is idle and no job broadcast within the deadline → HTTP 504
- Stratum breaks mid-conversation (e.g., pool restarts) → HTTP 502

## Configuration

The endpoint connects to the stratum server at `127.0.0.1:3333` by default. This can be changed by modifying `DefaultStratumHost` in the source code.

## Performance and Timeouts

- **Stratum connection timeout:** 5 seconds (if stratum is unreachable)
- **Stratum job deadline:** `update_interval` + 5 s (default: 35 seconds total). This is how long
  the handler waits for a mining job broadcast after authorization succeeds. On an idle pool this
  legitimately expires and the handler returns HTTP 504.
- **HTTP write deadline:** job deadline + 5 s extra (for encoding and sending the JSON response)
- **Response cache:** candidate-scoped when `candidate_height` is supplied; otherwise 10 seconds per
  username
- **Concurrent requests:** fully supported; matching requests share one Stratum connection and
  different candidate heights are serialized per username

## Security

- Requires valid API key in `Authorization` header, compared in constant time
- Subject to rate limiting (20 requests per 60 seconds per IP)
- Direct TCP connection to localhost only (not exposed externally)

The `user` parameter is **not** validated or sanitized — an earlier version of
this document claimed it was, which was never true. It is safe today only
because of where it goes, not because of what is done to it: it is `json.Marshal`ed
into the stratum request (so it cannot break out of the message) and concatenated
into a cache key (so an attacker can at worst occupy cache entries). It never
reaches a shell, a file path, or a SQL statement. **Validate it before using this
parameter anywhere else** — `handleUserFile` and `handleUserLog` deliberately run
their own `filepath.Base()` for exactly that reason.
