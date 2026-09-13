# BCH Stratum V1 proxy

Use [Installing CashStratum](docs/installation.md) for the supported source and dependency
requirements. Run `bash scripts/install-csproxy.sh --help` for proxy installation options.

The installer builds CashStratum from a selected checkout or release tag, configures a
Stratum V1 upstream and installs the `csproxy` service. It does not enable Stratum V2 or
install a Bitcoin node. Use your upstream pool's hostname, port and credentials; no
CashStratum-operated upstream is required.

The service invokes proxy mode with `-n csproxy`. Configuration and log locations are
shown by the installer and its generated unit. Inspect a staged installation before
activation, then use `systemctl status csproxy` and `journalctl -u csproxy` to verify it.
See [engine modes](README-CS_MODES.md#proxy-mode) for the `proxy` configuration fields.
