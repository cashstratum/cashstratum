# Caddy TLS front door for `/ping` — deployment runbook

## What it does

Terminates HTTPS for `solo.blocksniper.ai` on port 443 and reverse-proxies exactly one
path, `GET /ping`, to the unprivileged `ckpool-api` process already running on
`127.0.0.1:8888`. Everything else on port 443 gets a plain 404. This exists so a browser
on `blocksniper.ai` can measure real network latency to the pool host — see
the browser latency ping contract and documentation for the full "why" and
the design decisions (D1–D7).

Certificates come from Let's Encrypt via **TLS-ALPN-01** only — HTTP-01 is disabled
because port 80 is closed on this host and stays closed. Caddy's admin API is off; there
is no other config surface on this box for anyone to reach.

**Config file:** [`Caddyfile.solo`](Caddyfile.solo).

## Which host this is for

**Production only — `solo` (`45.63.84.63`).** `beast` (10.0.0.5, the test host) keeps its
existing Traefik setup and never gets this Caddyfile; the beast side of latency ping testing is
handler-level HTTP straight to `ckpool-api` over the LAN (`http://10.0.0.5:8888/ping`),
not through a TLS proxy — beast is behind double NAT and cannot hold a publicly-trusted
certificate for a real hostname. If you're deploying to beast, stop here; there is nothing
in this directory for you to install.

## Before you start

- `ckpool-api` must already be listening on `127.0.0.1:8888` on `solo` with the `/ping`
  handler and rate limiter deployed (see the Phase 1 work in this same plan). This runbook
  only covers the TLS front door, not the API itself.
- `solo.blocksniper.ai` DNS-only (not proxied) → `45.63.84.63` must already resolve
  correctly. It does today (verified 2026-09-04).
- **D7, said plainly:** this opens 443/tcp to the whole internet on the host that runs the
  pool. Nothing has ever listened on 80 or 443 there before. That is the point — a browser
  needs a publicly reachable TLS origin — but it is a new attack surface on the money host
  and should be treated as such (see Rollback below if you need to undo it).

## Install — production host `solo` (root, `production-solo-root`)

Run every step as root on `solo`. Commands are copy-pasteable in order.

1. **Install Caddy from the official apt repo** (not the Ubuntu-archive version, which
   lags):
   ```bash
   apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | \
     gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | \
     tee /etc/apt/sources.list.d/caddy-stable.list
   apt update
   apt install -y caddy
   ```
   Expected: `apt install` finishes with `Setting up caddy (...)` and no errors. This also
   creates the `caddy` system user/group and enables (but does not yet correctly
   configure) the `caddy.service` unit.

2. **Back up the packaged default Caddyfile**, then install this repo's config in its
   place:
   ```bash
   cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.packaged-default.bak
   cp /home/elo/src/blocksniper-ckpool/api/caddy/Caddyfile.solo /etc/caddy/Caddyfile
   caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
   ```
   Adjust the source path if the repo checkout on `solo` lives elsewhere (see
   `../NOTIFIER.md` for the tree layout convention used on this host).
   Expected: `caddy validate` ends with `Valid configuration` and no `Error:` line.

3. **Open 443 in ufw** for both address families (D7 — this is the new public listener):
   ```bash
   ufw allow 443/tcp comment 'browser latency ping (Caddy)'
   ufw status numbered | grep 443
   ```
   Expected: two `443/tcp` rules (or one dual-stack rule, depending on ufw's IPv6 setup) —
   `ALLOW IN` from `Anywhere` and `Anywhere (v6)`.

4. **Enable and start Caddy:**
   ```bash
   systemctl enable --now caddy
   systemctl status caddy --no-pager
   ```
   Expected: `Active: active (running)`.

5. **Watch certificate issuance:**
   ```bash
   journalctl -u caddy -f
   ```
   Expected within a few seconds: a line containing
   `"certificate obtained successfully"` and `"identifier":"solo.blocksniper.ai"`. If
   instead you see repeated `"challenge failed"` for `tls-alpn-01`, re-check that 443 is
   actually reachable from the internet (ufw rule from step 3, and that nothing upstream —
   e.g. a cloud-provider firewall — is also blocking it) before retrying. Ctrl-C once the
   success line appears; `journalctl -u caddy -n 50 --no-pager` afterward for a static
   look-back.

## Verify

Run these from your workstation, not from `solo` itself (they're testing the public path).

1. **`/ping` answers 204 with the right headers:**
   ```bash
   curl -sSI https://solo.blocksniper.ai/ping
   ```
   Expected:
   ```
   HTTP/2 204
   cache-control: no-store
   access-control-allow-origin: *
   x-ratelimit-limit: 60
   x-ratelimit-remaining: 59
   ```
   (Exact `x-ratelimit-*` values depend on the configured `CKPOOL_PING_RATE_LIMIT` and how
   many requests have already landed in the current window — the important thing is that
   the headers are present at all.)

2. **Everything else 404s at Caddy, never 401 from the API** — proves the API's other
   authenticated routes are not reachable through this proxy:
   ```bash
   curl -sSI https://solo.blocksniper.ai/health
   ```
   Expected: `HTTP/2 404` from Caddy. A `401` here would mean Caddy is proxying paths it
   shouldn't; go back and check the `@ping` matcher in `Caddyfile.solo`.

3. **Rate limit trips fast, with `Retry-After`** — a 61-request loop should hit `429`
   before the loop ends, and the `429` itself must return quickly (no slow rejection):
   ```bash
   for i in $(seq 1 61); do
     curl -sS -o /dev/null -w '%{http_code} '  https://solo.blocksniper.ai/ping
   done; echo
   curl -sSI https://solo.blocksniper.ai/ping | grep -i retry-after
   ```
   Expected: a run of `204 204 204 ...` followed by one or more `429` near the end of the
   61 requests, and a `retry-after:` header present on the last check.

4. **Certificate is publicly trusted (Let's Encrypt), not self-signed or a CF Origin CA
   cert:**
   ```bash
   openssl s_client -connect solo.blocksniper.ai:443 -servername solo.blocksniper.ai \
     </dev/null 2>/dev/null | openssl x509 -noout -issuer -dates
   ```
   Expected: `issuer=... O = Let's Encrypt` (or `O = (STAGING) Let's Encrypt` only if you
   are intentionally testing against the ACME staging environment — production issuance
   must show the non-staging issuer), plus `notBefore`/`notAfter` dates bracketing today.

## Rollback

If 443 needs to come back down (incident, or the decision gets reversed):

```bash
systemctl disable --now caddy
ufw delete allow 443/tcp
```

This stops Caddy and closes the port; `ckpool-api` itself is untouched and keeps serving
`8888` to the Forge box exactly as before this change. Restore the packaged default config
first if Caddy is ever reinstalled or re-enabled later:
`cp /etc/caddy/Caddyfile.packaged-default.bak /etc/caddy/Caddyfile`.

## Beast — the test host, in one paragraph

`beast` (10.0.0.5) already runs Traefik on 443, scoped to LAN + Tailscale only, and that
setup is unrelated to and unaffected by this change. `Caddyfile.solo` is not installed on
`beast` and never should be — beast is behind double NAT and cannot obtain a
publicly-trusted certificate for a real, browser-reachable hostname (contract §4, D4).
Beast's role in this plan is limited to the handler-level test of the Go `/ping` route
itself over the LAN (`http://10.0.0.5:8888/ping` from a machine on the same network), which
exercises the API and rate limiter without needing TLS at all. TLS exposure is proven only
on production, by Caddy's own certificate issuance plus a real browser fetch from
`blocksniper.ai`.
