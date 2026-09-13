# CashStratum block notifier

The optional `cashstratum-notifier` Go service subscribes to a BCHN node's ZMQ
`hashblock` topic, enriches notifications through RPC, and tails the pool log for
submission, confirmation and rejection events. It sends a heartbeat every five
minutes. Delivery is best-effort: keep independent block reconciliation/polling.

This is separate from the C `notifier` helper, which sends an update command to a
local pool socket. For that helper, pass the pool name explicitly, for example
`notifier -n cashstratum`.

## Configuration

The process reads environment variables. Systemd's `EnvironmentFile` loads them
from the path configured in the unit; the binary does not discover or load env
files itself. New installations use `/etc/cashstratum-notifier/cashstratum-notifier.env`.

| Variable | Meaning |
|---|---|
| `CASHSTRATUM_NOTIFY_URL` | Receiving HTTPS webhook URL; required for live delivery |
| `CASHSTRATUM_NOTIFY_SECRET` | Shared HMAC secret; required for live delivery |
| `CASHSTRATUM_CONF_PATH` | Pool configuration with BCHN RPC/ZMQ settings |
| `CASHSTRATUM_LOG_PATH` | Pool's actual main log, such as `/opt/cashstratum/logs/cashstratum.log` |
| `NOTIFY_DRY_RUN` | Defaults to `true`; set `false` only after receiver verification |
| `NOTIFIER_POOL_NAME` | Wire envelope pool identifier; defaults to `blocksniper` for existing receivers |

Legacy `BLOCKSNIPER_NOTIFY_URL`, `BLOCKSNIPER_NOTIFY_SECRET`, `CKPOOL_CONF_PATH` and
`CKPOOL_LOG_PATH` remain environment fallbacks. New names take precedence. Without
an explicit config path, the process searches for `cashstratum.conf` then
`ckpool.conf` beside or above the configured log directory.

The `pool` field is a receiver contract, independent of the software's name.
Do not change it until the receiver accepts the new value: a receiver rejecting
it with HTTP 422 causes terminal event loss, not a queued retry.

## Build

From the source checkout, with the Go toolchain required by `api/go.mod` installed:

```bash
cd api
go test -race ./...
CGO_ENABLED=0 go build -mod=vendor -o cashstratum-notifier ./notifier
```

Dependencies are vendored; obtaining a missing Go toolchain still requires network
access. For a different target, set `GOOS=linux` and the appropriate `GOARCH`.

## Install a new service

Use the same unprivileged account as the pool so it can read the configuration and
logs. The paths below are examples; match the actual installation documented in
[Installing CashStratum](../docs/installation.md). Preserve existing secret files
when upgrading instead of running the initialization commands again.

From the checkout root, after building:

```bash
sudo install -m 0755 api/cashstratum-notifier /usr/local/bin/cashstratum-notifier
sudo install -d -m 0755 /etc/cashstratum-notifier
sudo install -m 0600 /dev/null /etc/cashstratum-notifier/cashstratum-notifier.env
printf 'CASHSTRATUM_NOTIFY_SECRET=%s\n' "$(openssl rand -hex 32)" \
    | sudo tee /etc/cashstratum-notifier/cashstratum-notifier.env >/dev/null
sudo tee -a /etc/cashstratum-notifier/cashstratum-notifier.env >/dev/null <<'VARS'
CASHSTRATUM_NOTIFY_URL=https://dashboard.example.com/api/pool/notify
CASHSTRATUM_CONF_PATH=/opt/cashstratum/cashstratum.conf
CASHSTRATUM_LOG_PATH=/opt/cashstratum/logs/cashstratum.log
NOTIFY_DRY_RUN=true
VARS
sudo cp api/cashstratum-notifier.service /etc/systemd/system/
sudoedit /etc/systemd/system/cashstratum-notifier.service
```

Replace `CHANGEME` in `User` and `Group`, verify `ExecStart`/`EnvironmentFile`, and
configure the receiver with the same secret through its own secret-management
process. Confirm its accepted pool identifier and signature verification before
changing `NOTIFY_DRY_RUN`.

```bash
sudo systemctl daemon-reload
sudo systemctl start cashstratum-notifier
sudo journalctl -u cashstratum-notifier -n 50
```

Check ZMQ connectivity, log tailing, heartbeat activity and dry-run `WOULD POST`
output. Then set `NOTIFY_DRY_RUN=false` in the env file, restart, and verify a
successful receiver response and downstream processing. Enable the service at
boot only after those checks. Restarting this companion does not restart the pool.

## Wire format

All events contain `event`, `event_id` (UUIDv7), `emitted_at` (UTC RFC3339 with
milliseconds), `pool` and `notifier_version`. Event-specific fields are defined
in [`notifier/event.go`](notifier/event.go). Event types are `network.block`,
`pool.block.submitted`, `pool.block`, `pool.block.rejected`, and `notifier.heartbeat`.

Both `X-CashStratum-*` and `X-Blocksniper-*` header families are sent with identical
values for `Event`, `Delivery`, `Timestamp` and `Signature`. The signature is
`sha256=` followed by hex HMAC-SHA256 of `timestamp + "." + exact_body_bytes`, using
the shared secret as its string bytes. Receivers should verify signatures with a
constant-time comparison, enforce an acceptable timestamp window and deduplicate
on the body's `event_id`.

HTTP 2xx is success. HTTP 4xx is terminal and is not retried. Transport errors and
5xx responses receive retries after 1, 4 and 12 seconds; retries retain the same
body, event ID, timestamp and signature. Queue capacity is bounded, and exhausted
or overflowing deliveries may be dropped. A notifier heartbeat does not prove
that every earlier block event was processed by the receiver.
