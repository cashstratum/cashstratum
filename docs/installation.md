# Installing CashStratum

CashStratum serves Bitcoin Cash miners over Stratum V1. It requires an existing,
configured Bitcoin Cash Node (BCHN). The installers do not provision Bitcoin Core,
change a node's configuration, download its blockchain, or enable Stratum V2.

## Prerequisites

Use Linux with systemd for a managed installation. Install build tools before
running the solo installer:

```sh
# Ubuntu / Debian
sudo apt-get update
sudo apt-get install build-essential git autoconf automake libtool pkg-config yasm libzmq3-dev python3

# Fedora
sudo dnf install gcc gcc-c++ make git autoconf automake libtool pkgconf-pkg-config yasm zeromq-devel python3
```

BCHN must allow authenticated RPC from the pool host and return a usable
`getblocktemplate`. Use a synced node, an address matching its network, and a
private HTTP RPC endpoint in `HOST:PORT` or `http://HOST:PORT` format.
TLS, URL paths, queries, fragments and embedded credentials are unsupported by
the daemon and rejected by the solo installer. Create an existing unprivileged service account:

```sh
sudo useradd --system --create-home cashstratum
```

## Solo pool

Build the checkout you have reviewed, replacing the example endpoint, username
and address with your own. Read the password without putting it in shell history:

```sh
read -r -s -p 'BCHN RPC password: ' CASHSTRATUM_RPC_PASSWORD; echo
export CASHSTRATUM_RPC_PASSWORD
sudo --preserve-env=CASHSTRATUM_RPC_PASSWORD \
  bash scripts/install-cashstratum-solo.sh \
  --source-dir "$PWD" --user cashstratum \
  --rpc-url 127.0.0.1:8332 --rpc-user pool \
  --address 'bitcoincash:YOUR_VALID_CASHADDR' --no-start
unset CASHSTRATUM_RPC_PASSWORD
```

Without `--source-dir`, provide `--ref vX.Y.Z` to build an explicitly chosen public
release. The installer validates the BCHN implementation, chain, payout address,
and block-template availability before changing installation files. It builds a
temporary source copy and installs the daemon, `ckpmsg`, and `notifier` into
`/opt/cashstratum`. Override that location with `--install-dir /absolute/path`.

The generated configuration enables per-address solo mode through the service's
`-B` argument. The operator address is a fallback for non-address usernames;
`poolfee` starts at zero. Inspect `cashstratum.conf` and the unit before starting:

```sh
sudo systemctl enable --now cashstratum.service
sudo systemctl status cashstratum.service
sudo journalctl -u cashstratum.service -n 50
```

The pool binds port 3333. Scope RPC, API and miner firewall rules separately.
`--no-start` leaves service activation to the operator; if updating an already
running CashStratum service, the installer stops it before replacing binaries
and leaves it stopped. Without that flag it enables, restarts and checks the unit.
The Go log API and event notifier are separate optional components: see
[API installation](../api/README.md) and [notifier configuration](../api/NOTIFIER.md).

For inspection or container tests, append `--destdir /absolute/staging/root
--no-start`. This requires no root, does not invoke systemd, and places the pool
and unit under that staging root. Configuration and unit paths still refer to the
final installation directory. A staged directory is an artifact to inspect, not
a relocated runtime installation. Its parent paths must not be symlinks.

## Existing installations

Rerunning the solo installer preserves existing `cashstratum.conf` byte-for-byte
and validates its node/address instead of replacing it with command-line values.
It preserves logs and indexes and replaces binaries atomically after a successful
build. It does not delete or alias `/opt/ckpool`, rename historical logs, migrate
credentials, or enable cleanup. An active `ckpool.service` blocks a live install.

Treat migration as a separate stopped-pool operation: disable any pruning timer,
copy the complete old tree and solve index to safe storage, verify the historical
height/directory sets, and explicitly adapt the new configuration. Keep the old
tree until those checks and runtime verification succeed. Installers do not prove
that a historical protection index is complete. See the cleanup script's help
before importing a verified index and scheduling deletion.

## Interactive installation from a checkout

`bash install-cashstratum.sh` builds the current directory and prompts for an
installation directory, node credentials, and an operator BCH address. It preserves
an existing mainnet configuration on reruns. It does not create a sample testnet
configuration with third-party addresses. The generated start script accepts a
configuration filename and works when invoked from another directory:

```sh
~/cashstratum/start-cashstratum.sh cashstratum.conf
```

Configure any additional network explicitly with its own address, log directory
and port. Socket paths are command-line options, not JSON configuration keys.
For a second instance, bypass the default launcher and supply a distinct instance
name and socket directory; use the same values with its helper clients:

```sh
cd ~/cashstratum
./cashstratum -c second.conf -B -L -n cashstratum-second -s /tmp/cashstratum-second
printf 'stats\n' | ./ckpmsg -n cashstratum-second -s /tmp -N stratifier
```

Systemd setup and optional API/notifier activation are
provided by `sudo ./post-install.sh`. For RPC-validated, reproducible installations,
prefer the solo installer above.

## Stratum proxy

The proxy installer builds CashStratum's Stratum V1 proxy, prompts for upstream
BCH pools, and installs `csproxy.service`:

```sh
sudo bash scripts/install-csproxy.sh --source-dir "$PWD"
# Or choose an explicit public release:
sudo bash scripts/install-csproxy.sh --ref vX.Y.Z
```

It rejects SV2 URLs. Its source build uses a temporary directory and does not
remove any existing `/opt` tree. Replacing an existing proxy configuration requires
an interactive confirmation. All prompts and validation complete before stopping the
service. Configuration is written atomically with mode 0600. A failed activation
restores the previous executable, configuration, unit and service state. The
installer replaces only `/usr/local/bin/csproxy`, leaving other pool binaries intact.
It creates no installer-specific compatibility service or configuration aliases.
The upstream build retains the `ckpool` binary and `ckproxy` entry point alongside
CashStratum names; these are build compatibility interfaces.
Use the helper clients with an explicit instance name: `ckpmsg -n cashstratum`,
`notifier -n cashstratum`, or `-n csproxy` when addressing the proxy.

## Binding privileged ports

`make install` no longer grants `CAP_NET_BIND_SERVICE` automatically. Ordinary Stratum
ports (3333 and above) need no file capability, so the default is off. To bind a
privileged port (below 1024) directly, opt in during a privileged, non-staged install:

```sh
sudo make install INSTALL_BIND_CAPABILITY=yes
```
