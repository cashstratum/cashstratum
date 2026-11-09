# Installing CashStratum

CashStratum serves Bitcoin Cash miners over Stratum V1. It requires an existing,
synced BCH full node; [Supported nodes](#supported-nodes) lists which ones. The
installers do not provision Bitcoin Core, change a node's configuration, download
its blockchain, or enable Stratum V2.

## Supported nodes

| Node | Status | Notes |
|---|---|---|
| [Bitcoin Cash Node (BCHN)](https://bitcoincashnode.org) | **Reference** | Used by the solo installer, the regtest mining gate and the reference deployment. Use ZMQ block notifications (`"notify": true` with `zmqblock`). |
| [BCHD](https://github.com/gcash/bchd) | Supported | Configure the pool by hand; the solo installer accepts BCHN only. The pool speaks plain HTTP, so start BCHD with `--notls`, which BCHD permits only for an RPC listener on localhost: run the pool on the same host. BCHD has no ZMQ, so set `"notify": false` on its `btcd` entry and the pool polls for new blocks every `blockpoll` milliseconds (100 by default). `--miningaddr` is not needed: the pool builds its own coinbase. |
| [Flowee the Hub](https://flowee.org) | Supported | Configure the pool by hand, as for BCHD, with `"notify": false` on its `btcd` entry. |

The pool needs `getblocktemplate`, `submitblock`, `getblockchaininfo`,
`getblockcount`, `getblockhash` and `getbestblockhash` over HTTP JSON-RPC. It
requests a template that carries `coinbasevalue`, and refuses one that does
not, rather than build a coinbase that pays nothing. BCHD and Flowee support is
covered by unit tests of those requests and the template parser (`test/nodecompat.c`)
built from their documented RPC behaviour; the end-to-end mining gate runs on BCHN.
Example `btcd` entry for a node without ZMQ:

```json
"btcd" : [
	{
		"url" : "127.0.0.1:8334",
		"auth" : "rpcuser",
		"pass" : "rpcpassword",
		"notify" : false
	}
]
```

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

## After upgrading from 1.3.1 or earlier

Up to 1.3.1, the share that solved a block could be kept as the finder's
round best (`bestshare`), so a users file can carry a round best at or above the
network difficulty. The daemon now drops any restored round best at or above the
network difficulty on its own when the first workbase arrives after start, for
every user and worker. All-time bests (`bestever`) are left alone.

To also start every round afresh, reset the round once after the upgrade. Wait
for the first stats write after the restart (about one minute; the users files
under `logs/users/` are rewritten), then run, from the installation directory:

```sh
printf 'resetshares\n' | ./ckpmsg -n cashstratum -s /tmp -N stratifier
```

`-n` is the instance name the service runs with (`-n cashstratum` in the
generated unit) and `-s` is the parent of its socket directory (`/tmp` unless the
daemon was started with its own `-s`). The reset applies to **every user**: each
user's and worker's round best and round shares go to zero, so per-user luck
restarts for everyone, and the pool-wide round counters reset too. `bestever`,
total shares and hashrates are untouched. The change reaches the users files at
the next stats write, so do not restart the daemon again within that minute.

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

## Running a second instance

A host can run a second pool beside the first, for example a pro-rata pool with a fee next to a
plain solo pool. Each instance is a separate daemon process with its own configuration file, name,
socket directory, log directory and stratum ports. The two share only the Bitcoin Cash node, which
must accept RPC from both.

Give each instance its own values for these settings:

| Setting | Where | Why it must differ |
|---|---|---|
| `-n <name>` | command line | names the instance. The log is `<logdir>/<name>.log`, and the default socket directory is `/tmp/<name>/`. |
| `-s <dir>` | command line | the socket and pid directory. The daemon creates it if it is missing. `ckpmsg` and the daemon's `-k` (`killold`) find an instance through its socket and pid file here. |
| `-c <file>` | command line | the configuration file. |
| `logdir` | configuration | the log directory, relative to the working directory the daemon is started from. |
| `serverurl` | configuration | a stratum port that no other process on the host listens on. |

The `sockdir` configuration key is **not read**. Set the socket directory with `-s` only.

Two other points:

- `-B` selects solo mode on the command line. It is not a configuration key, so start every
  solo-mode instance with it.
- `-L` (`--log-shares`) writes a sharelog for each share, under `<logdir>/<height>/`. The log-prune
  timer applies the `DAYS_TO_KEEP` retention (60 days by default) to sharelogs. Check that it covers
  the second instance's log directory before relying on it. Turn `-L` on for an instance whose
  payouts you audit from the share records.

### Example: a pro-rata pool with a finder's fee

This is a generic example of a shared pool that pays a 50% finder's fee to a payroll address. Every
address in it is a placeholder. The daemon refuses to start while `pooladdress` is a placeholder,
which is the intended behaviour of `poolfee_strict` (on by default). Replace both addresses with
valid addresses for your network before the first start.

Save as `cashstratum-prop.conf` in the installation directory:

```json
{
	"btcd" : [
		{
			"url" : "127.0.0.1:8334",
			"auth" : "rpcuser",
			"pass" : "rpcpassword",
			"notify" : false
		}
	],
	"serverurl" : [ "0.0.0.0:3334" ],
	"logdir" : "logs-prop",
	"btcaddress" : "bitcoincash:qp...fallback-placeholder",
	"poolfee" : 50,
	"pooladdress" : "bitcoincash:qp...payroll-placeholder",
	"poolfee_strict" : true,
	"mindiff" : 1000,
	"startdiff" : 1000,
	"update_interval" : 30
}
```

`btcaddress` is the fallback that solo mode needs for a username that is not an address. The daemon
requires it with `-B`.

`cashtop` shows the PROP view when the conf has `poolfee` 50 with a `pooladdress` set. Solo mode
comes from `-B` on the command line, which is why the example conf has no solo key.

Start it from the installation directory, so the relative `logdir` resolves there. The `-n` name
matches the conf file's name, so `cashtop` finds the log without `--log`:

```sh
cd /opt/cashstratum
./cashstratum -B -L -c cashstratum-prop.conf -n cashstratum-prop -s /tmp/cashstratum-prop
printf 'stats\n' | ./ckpmsg -n cashstratum-prop -s /tmp/cashstratum-prop -N stratifier
./cashtop --conf cashstratum-prop.conf
```

If `-n` differs from the conf file's name, for example `-n prop` with `cashstratum-prop.conf`,
pass `--log` with the actual log path:

```sh
./cashtop --conf cashstratum-prop.conf --log /opt/cashstratum/logs-prop/prop.log
```

`cashtop` derives the log path only from the conf's file name, so `--log` is needed whenever the
two names differ. Run the daemon and `cashtop` from the same installation directory.

Give the round API its own state. Its defaults sit beside the log directory (see
[Configuration](operator-api.md#configuration) in the operator API reference). A second instance
whose `logdir` shares a parent with the first shares that state unless you set
`CASHSTRATUM_INDEX_DIR` and `CASHSTRATUM_ROUNDS_DIR` for it.

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

## Stratum V2 certificates

The server signs its Stratum V2 Noise certificate under the authority key at startup, with
certificate format version 0, and re-dates the validity window automatically when it is within a
day of expiry. The client side, ckproxy and the job-declarator client, accepts only format version 0.
After upgrading, a restart is sufficient: the authority key and miner URLs do not change. Upgrade
pool servers before proxies and job-declarator clients, because an updated client refuses an
upstream still serving version 1.
