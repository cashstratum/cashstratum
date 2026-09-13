# CashStratum API Guide - Using ckpmsg

CashStratum doesn't use a traditional HTTP API. Instead, it uses Unix domain sockets accessed via the `ckpmsg` utility. This guide explains how to query and control your CashStratum instance.

## Prerequisites

- CashStratum must be running
- You need access to the `ckpmsg` binary (installed with cashstratum)
- Unix sockets must be accessible (typically in `/tmp/cashstratum/`)

## Basic Usage

```bash
printf '<command>\n' | ckpmsg -s <parent> -n <pool-name> -N <process>
```

Two things about `ckpmsg` are easy to get wrong, and both fail **silently**:

1. **The command is read from stdin, not argv** (`src/ckpmsg.c:121`). A trailing
   `ckpmsg ... stats` is ignored and you get empty output with exit status 0.
2. **The socket path is assembled from three flags** as `<-s>/<-n>/<-N>`
   (`src/ckpmsg.c:252-262`) — `-s` is a *parent* directory, not the socket itself.

A daemon launched with `-n cashstratum` uses `/tmp/cashstratum/stratifier`, so the
helper flags are `-s /tmp -n cashstratum -N stratifier`. The daemon's `-s` flag
sets the complete socket directory; a JSON `sockdir` field is not read by the engine.
For another socket directory, split it into parent and basename, or use `-n .`:

```bash
printf 'stats\n' | ckpmsg -s /var/run/mypool -n . -N stratifier
```

For an additional instance, select independent configuration, network ports, logs and
socket paths. For example, start the daemon with `-n cashstratum-test -s /tmp/cashstratum-test`
and query it with `ckpmsg -s /tmp -n cashstratum-test -N stratifier`. Do not rely on
a renamed executable alone to select the instance name.

### A shell helper you will want

`ckpmsg` is a debugging tool, not a JSON transport. It writes its logging to
**stdout**, mixed in with the reply, and it prints through `LOGMSGSIZ`, which
emits at most 510 characters per line — so any sizeable response arrives split
across several lines behind two lines of chatter. Piping it straight into `jq`
fails on anything bigger than a toy pool.

```bash
ckpmsg_json() {
    printf '%s\n' "$1" \
        | ckpmsg -s /tmp -n cashstratum -N "${2:-stratifier}" 2>/dev/null \
        | sed -n '/Received response: /,$p' \
        | sed '1s/^.*Received response: //' \
        | tr -d '\n'
}

ckpmsg_json stats | jq .
ckpmsg_json users | jq '.users | length'
```

> For anything programmatic — a dashboard, monitoring, a web app — prefer the
> read-only HTTP service in [`api/`](api/README.md). It returns clean JSON over
> an authenticated socket and does not require shell access to the pool host.

### `CKPOOL_CASHADDR_PREFIX` (HTTP API only)

The HTTP service's `/coinbase` endpoint runs a phantom stratum probe that
authorises internally as `<user>.ckpool-api` (see `api/README.md` for the
full `CKPOOL_*` environment variable table). `CKPOOL_CASHADDR_PREFIX` sets
this pool's CashAddr network prefix (no trailing `:`), which that probe
prepends to a bare CashAddr username so it authorises under the same
spelling cashstratum already knows the address by, instead of minting a second,
bare-spelling shadow user. Defaults to `bitcoincash` (production is
mainnet); a test host regtest/testnet4 rig sets it to `bchreg`/`bchtest`.

Where `<process>` is one of:
- `stratifier` - Main mining process (most commands)
- `connector` - Network connections
- `generator` - Block generation
- `pool` - Main pool process

## Common Commands

### 1. Pool Statistics

Get overall pool statistics:
```bash
printf 'stats\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

Returns JSON with:
- Current hashrate (1m, 5m, 15m, 1h, 1d, 7d)
- Number of connected workers and users
- Total shares submitted
- Pool uptime
- Share statistics

### 2. List All Users

Get a list of all users:
```bash
printf 'users\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

Returns JSON array with all users and their statistics.

### 3. List All Workers

Get detailed worker information:
```bash
printf 'workers\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

Returns JSON with all workers grouped by user.

### 4. Get Specific User Info

Get information about a specific user:
```bash
printf 'user.info=USERNAME\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

Example:
```bash
printf 'user.info=alice\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

### 5. Get Current Work

View the current work template:
```bash
printf 'current.workbase\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

### 6. Change Log Level

Adjust logging verbosity:
```bash
# Set to debug
printf 'loglevel=7\n' | ckpmsg -s /tmp -n cashstratum -N stratifier

# Set to notice (default)
printf 'loglevel=5\n' | ckpmsg -s /tmp -n cashstratum -N stratifier

# Set to warning only
printf 'loglevel=3\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

Log levels:
- 0: EMERG
- 1: ALERT
- 2: CRIT
- 3: ERR
- 4: WARNING
- 5: NOTICE
- 6: INFO
- 7: DEBUG

### 7. Disconnect User/Worker

Disconnect a specific user:
```bash
printf 'dropuser=USERNAME\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

### 8. Pool Summary

Get a quick summary:
```bash
printf 'summary\n' | ckpmsg -s /tmp -n cashstratum -N pool
```

### 9. Shutdown Pool

Gracefully shutdown the pool:
```bash
printf 'shutdown\n' | ckpmsg -s /tmp -n cashstratum -N pool
```

## Practical Examples

### Monitor Pool in Real-time

Create a monitoring script:
```bash
#!/bin/bash
while true; do
    clear
    echo "=== CashStratum Stats ==="
    ckpmsg_json stats stratifier | jq '.'
    sleep 5
done
```

### Get User Hashrate

Extract specific user's hashrate:
```bash
ckpmsg_json user.info=alice stratifier | jq '.hashrate1m'
```

### List Active Workers

Show all active workers with hashrate:
```bash
ckpmsg_json workers stratifier | jq '.workers[] | {user: .user, worker: .worker, hashrate: .hashrate1m}'
```

### Export Stats to JSON File

Save pool statistics:
```bash
printf 'stats\n' | ckpmsg -s /tmp -n cashstratum -N stratifier > pool_stats_$(date +%Y%m%d_%H%M%S).json
```

## Creating a Web API Wrapper

If you need HTTP access, create a simple wrapper:

```bash
#!/bin/bash
# api-server.sh - Simple HTTP wrapper for ckpmsg

# Requires socat
while true; do
    echo -e "HTTP/1.1 200 OK\nContent-Type: application/json\n"
    case "$REQUEST" in
        *"/stats"*)
            printf 'stats\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
            ;;
        *"/users"*)
            printf 'users\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
            ;;
        *"/workers"*)
            printf 'workers\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
            ;;
        *)
            echo '{"error":"Unknown endpoint"}'
            ;;
    esac
done | socat TCP-LISTEN:8080,reuseaddr,fork EXEC:"/bin/bash api-server.sh"
```

## Python Example

Query CashStratum from Python:
```python
import subprocess
import json

def cashstratum_command(socket, command):
    """Execute ckpmsg command and return parsed JSON"""
    cmd = ['ckpmsg', '-s', '/tmp', '-n', 'cashstratum', '-N', socket]
    # ckpmsg reads the command from stdin -- passing it in argv is ignored.
    result = subprocess.run(cmd, input=command + '\n',
                            capture_output=True, text=True)
    if result.returncode == 0:
        return json.loads(result.stdout)
    return None

# Get pool stats
stats = cashstratum_command('stratifier', 'stats')
print(f"Pool hashrate: {stats['hashrate1m']} GH/s")

# Get all users
users = cashstratum_command('stratifier', 'users')
for user in users['users']:
    print(f"User: {user['user']}, Hashrate: {user['hashrate1m']}")
```

## Troubleshooting

### Permission Denied
If you get permission errors:
```bash
ls -la /tmp/cashstratum/
# Check socket permissions
```

### No Such File
If sockets don't exist:
```bash
# Check if cashstratum is running
ps aux | grep cashstratum

# Check cashstratum logs
tail -f ~/cashstratum/logs/cashstratum.log
```

### Invalid JSON Response
Some commands may return text instead of JSON. Parse accordingly:
```bash
printf 'loglevel=7\n' | ckpmsg -s /tmp -n cashstratum -N stratifier 2>&1
```

## Advanced Usage

### Custom Queries
You can send custom JSON-RPC style queries:
```bash
echo '{"method":"stats","params":[]}' | printf '-\n' | ckpmsg -s /tmp -n cashstratum -N stratifier
```

### Monitoring Script
Create a comprehensive monitoring script:
```bash
#!/bin/bash
# monitor.sh

echo "CashStratum Monitor - $(date)"
echo "===================="

echo -e "\n📊 Pool Stats:"
ckpmsg_json stats stratifier | jq '{
    hashrate: .hashrate1m,
    workers: .workers,
    users: .users,
    shares: .accounted_shares,
    uptime: .elapsed
}'

echo -e "\n👥 Top Users by Hashrate:"
ckpmsg_json users stratifier | jq -r '.users | 
    sort_by(-.hashrate1m) | 
    .[0:5] | 
    .[] | 
    "\(.user): \(.hashrate1m) GH/s"'

echo -e "\n⚡ Recent Blocks:"
tail -n 5 ~/cashstratum/logs/cashstratum.log | grep "BLOCK FOUND"
```

## Notes

- All responses are in JSON format unless otherwise noted
- Some commands may require specific pool modes (solo vs proxy)
- Commands are processed asynchronously - responses may have slight delays
- For production monitoring, implement proper error handling and rate limiting