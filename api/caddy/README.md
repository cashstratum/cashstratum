# HTTPS for the public latency probe

The operator API normally listens on loopback. A TLS proxy can expose `/ping`
without publishing authenticated pool-data routes. Replace `pool.example.com`
with a hostname you control; DNS must reach the host and TCP 443 must be available.
Use your existing proxy if another service already owns that port.

The following Caddy configuration uses TLS-ALPN-01 certificate validation on 443
and disables automatic HTTP redirects, so it does not require an HTTP listener
on port 80:

```caddyfile
{
    admin off
    auto_https disable_redirects
}

pool.example.com {
    tls {
        issuer acme {
            disable_http_challenge
        }
    }
    @ping path /ping
    handle @ping {
        reverse_proxy 127.0.0.1:8888
    }
    handle {
        respond 404
    }
}
```

Save the adapted configuration to the location used by your Caddy service,
validate it with `caddy validate --config /etc/caddy/Caddyfile`, and follow your
service's deployment procedure. No DNS, certificate or host migration is performed
by the CashStratum installers.

Verify the actual public endpoint after activation:

```bash
curl -sSI https://pool.example.com/ping    # 204
curl -sSI https://pool.example.com/health  # 404
curl -sSI https://pool.example.com/stats   # 404
```

The API accepts `GET` and `HEAD` for `/ping`; other methods receive 405. Its
loopback proxy handling uses the forwarded client address for rate limiting.
The HTTP measurement includes the proxy and network path; it does not measure
Stratum job latency. See the [operator API reference](../../docs/operator-api.md).
