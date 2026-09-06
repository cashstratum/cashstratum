# blocksniper-notifier — Deployment Runbook

## What it does

The `blocksniper-notifier` is a standalone Go binary that delivers sub-second block notifications from the pool host to the Laravel stack via signed webhooks. It subscribes directly to bitcoind's ZMQ `hashblock` socket and posts a notification to Laravel in milliseconds — typically faster than one cycle of the one-minute schedulers (`bch:monitor-new-blocks`, `pool:monitor-blocks`). It also tails `ckpool.log` to emit events for the three pool-solve moments ckpool records: block submission, confirmation, and rejection. Every 5 minutes it sends a heartbeat to prove liveness.

The every-minute Laravel schedulers continue unchanged. The notifier is a latency layer over that polling system — if a webhook is lost or delayed, the schedulers remain the safety net. Blocks never disappear.

## Environment variables

The notifier reads configuration from a single env file: `/etc/blocksniper-notifier/notifier.env` (mode 0600, owned by the run user).

| Variable | Required? | Notes |
|---|---|---|
| `BLOCKSNIPER_NOTIFY_URL` | Yes (unless `NOTIFY_DRY_RUN=true`) | Webhook URL on the Laravel side. Example: `https://blocksniper.ai/api/pool/notify` |
| `BLOCKSNIPER_NOTIFY_SECRET` | Yes (unless `NOTIFY_DRY_RUN=true`) | 32-byte random hex string. Shared with Laravel; never log it. See secret creation below. |
| `CKPOOL_CONF_PATH` | Yes | Path to `ckpool.conf`. Example: `/home/elo/ckpool/ckpool.conf` on production, `/mnt/data4tb/bch/ckpool/ckpool.conf` on test. |
| `CKPOOL_LOG_PATH` | Yes | Path to `ckpool.log`. Example: `/home/elo/ckpool/logs/ckpool.log` on production, `/mnt/data4tb/bch/ckpool/logs/ckpool.log` on test. |
| `NOTIFY_DRY_RUN` | No | **Defaults to `true`** (dry-run mode). Set to `false` to actually POST to the webhook URL. To enable: edit `notifier.env`, change `NOTIFY_DRY_RUN=false`, then `systemctl restart blocksniper-notifier`. |

## Secret creation

**On the pool host**, run (as `root` or with `sudo`):

```bash
# Create the config directory (pool-host-side).
sudo install -d -m 0755 /etc/blocksniper-notifier

# Generate a 32-byte random hex secret.
SECRET=$(openssl rand -hex 32)

# Write it to the env file, mode 0600, owned by the run user.
# On production (solo): run user is `elo`
# On test (beast): run user is `bch`
echo "BLOCKSNIPER_NOTIFY_SECRET=$SECRET" | sudo tee /etc/blocksniper-notifier/notifier.env >/dev/null
sudo chmod 0600 /etc/blocksniper-notifier/notifier.env
sudo chown elo:elo /etc/blocksniper-notifier/notifier.env    # production solo
# OR
sudo chown bch:bch /etc/blocksniper-notifier/notifier.env     # test beast
```

Mirror the secret into 1Password (`ClaudeCode` vault, item `blocksniper-notifier secret`) for recovery and to share with the Laravel team.

## Build

On the pool host, inside the repo clone:

```bash
cd api && go build -mod=vendor -o blocksniper-notifier ./notifier
```

This builds offline with vendored dependencies. No network access needed.

Copy the binary to `/usr/local/bin/`:

```bash
sudo cp blocksniper-notifier /usr/local/bin/
sudo chmod 0755 /usr/local/bin/blocksniper-notifier
```

## Deploy sequence

### Production host — `solo` (45.63.84.63)

- Run user: `elo`
- Source tree: `/home/elo/src/blocksniper-ckpool`
- ckpool config: `/home/elo/ckpool/ckpool.conf`
- ckpool logs: `/home/elo/ckpool/logs/ckpool.log`
- **CAUTION: `sudo` requires a password on `solo`.** Operator must run interactive commands.

**Steps:**

1. **[Operator-run]** Create the config directory and generate the secret:
   ```bash
   ssh solo
   sudo install -d -m 0755 /etc/blocksniper-notifier
   SECRET=$(openssl rand -hex 32)
   echo "BLOCKSNIPER_NOTIFY_SECRET=$SECRET" | sudo tee /etc/blocksniper-notifier/notifier.env >/dev/null
   sudo chmod 0600 /etc/blocksniper-notifier/notifier.env
   sudo chown elo:elo /etc/blocksniper-notifier/notifier.env
   ```
   Record the secret for 1Password.

2. Append the remaining env vars to the file (the secret is already there):
   ```bash
   cat >> /tmp/notifier-vars.env <<'EOF'
   BLOCKSNIPER_NOTIFY_URL=https://blocksniper.ai/api/pool/notify
   CKPOOL_CONF_PATH=/home/elo/ckpool/ckpool.conf
   CKPOOL_LOG_PATH=/home/elo/ckpool/logs/ckpool.log
   NOTIFY_DRY_RUN=true
   EOF
   cat /tmp/notifier-vars.env | sudo tee -a /etc/blocksniper-notifier/notifier.env >/dev/null
   ```

3. Build the binary locally (or on the pool host):
   ```bash
   cd /home/elo/src/blocksniper-ckpool/api
   go build -mod=vendor -o blocksniper-notifier ./notifier
   ```

4. **[Operator-run]** Copy the binary and install the systemd unit:
   ```bash
   sudo cp blocksniper-notifier /usr/local/bin/
   sudo chmod 0755 /usr/local/bin/blocksniper-notifier
   sudo cp /home/elo/src/blocksniper-ckpool/api/blocksniper-notifier.service /etc/systemd/system/
   ```

5. **[Operator-run]** Start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable blocksniper-notifier
   sudo systemctl start blocksniper-notifier
   ```

6. Verify it started:
   ```bash
   systemctl status blocksniper-notifier
   journalctl -u blocksniper-notifier -n 20
   ```

### Test host — `beast` (10.0.0.5)

- Run user: `bch`
- Source tree: `/mnt/data4tb/bch/ckpool-src`
- ckpool config: `/mnt/data4tb/bch/ckpool/ckpool.conf`
- ckpool logs: `/mnt/data4tb/bch/ckpool/logs/ckpool.log`
- **Passwordless sudo.** Agent/script can run unattended.

**Steps:**

1. **[Operator-run]** Create the config directory and generate the secret (same as above):
   ```bash
   ssh beast
   sudo install -d -m 0755 /etc/blocksniper-notifier
   SECRET=$(openssl rand -hex 32)
   echo "BLOCKSNIPER_NOTIFY_SECRET=$SECRET" | sudo tee /etc/blocksniper-notifier/notifier.env >/dev/null
   sudo chmod 0600 /etc/blocksniper-notifier/notifier.env
   sudo chown bch:bch /etc/blocksniper-notifier/notifier.env
   ```
   Record the secret for 1Password.

2. Append the remaining env vars (the secret is already there):
   ```bash
   cat >> /tmp/notifier-vars.env <<'EOF'
   BLOCKSNIPER_NOTIFY_URL=https://blocksniper.ai/api/pool/notify
   CKPOOL_CONF_PATH=/mnt/data4tb/bch/ckpool/ckpool.conf
   CKPOOL_LOG_PATH=/mnt/data4tb/bch/ckpool/logs/ckpool.log
   NOTIFY_DRY_RUN=true
   EOF
   cat /tmp/notifier-vars.env | sudo tee -a /etc/blocksniper-notifier/notifier.env >/dev/null
   ```

3. Build the binary:
   ```bash
   cd /mnt/data4tb/bch/ckpool-src/api
   go build -mod=vendor -o blocksniper-notifier ./notifier
   ```

4. Copy the binary and install the systemd unit:
   ```bash
   sudo cp blocksniper-notifier /usr/local/bin/
   sudo chmod 0755 /usr/local/bin/blocksniper-notifier
   sudo cp /mnt/data4tb/bch/ckpool-src/api/blocksniper-notifier.service /etc/systemd/system/
   ```

5. Start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable blocksniper-notifier
   sudo systemctl start blocksniper-notifier
   ```

6. Verify it started:
   ```bash
   systemctl status blocksniper-notifier
   journalctl -u blocksniper-notifier -n 20
   ```

## Dry-run mode and going live

The notifier ships with `NOTIFY_DRY_RUN=true` by default. In dry-run mode, it logs `WOULD POST` lines to the journal instead of actually sending webhooks. This is safe for testing and validation before going live.

To enable live webhooks:

1. Edit `/etc/blocksniper-notifier/notifier.env`:
   ```bash
   sudo nano /etc/blocksniper-notifier/notifier.env
   # Change:  NOTIFY_DRY_RUN=true
   # To:      NOTIFY_DRY_RUN=false
   ```

2. Restart the service:
   ```bash
   sudo systemctl restart blocksniper-notifier
   ```

3. Verify in the journal — you should now see successful HTTP POST lines instead of `WOULD POST`:
   ```bash
   journalctl -u blocksniper-notifier -f
   ```

## Reading the journal

View live logs:

```bash
journalctl -u blocksniper-notifier -f
```

View the last 50 lines:

```bash
journalctl -u blocksniper-notifier -n 50
```

### Log message types

- **`WOULD POST <url>` (dry-run mode):** A webhook would be sent, but dry-run is active. Full body follows.
- **`delivered <event> <url>` or `retried`:** Successful or retried delivery (live mode only).
- **`dropped: <reason>`:** Event dropped after retries exhausted or queue full.
- **`heartbeat`:** Periodic liveness ping (every 5 minutes).
- **Connection errors:** ZMQ or log tailer failures, usually transient.

### Example dry-run output

```
Aug 30 23:26:27 beast blocksniper-notifier[1234]: WOULD POST https://blocksniper.ai/api/pool/notify
{"event":"network.block","event_id":"018f3c9e-7a41-7b02-9c33-2d5e8f1a4b60","emitted_at":"2026-08-30T23:26:27.191Z","pool":"blocksniper","notifier_version":"1.0.0","hash":"00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744","height":968432,"block_time":1756598780,"enriched":true,"source":"zmq"}
```

## Wire format reference

See the integration contract for the complete wire format specification, including:
- HTTP headers and HMAC-SHA256 signatures
- Event type definitions (network.block, pool.block, pool.block.rejected, pool.block.submitted, notifier.heartbeat)
- Idempotency requirements
- Retry ladder (1 s / 4 s / 12 s)
- Laravel verification snippet
