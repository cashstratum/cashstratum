#!/usr/bin/env bash
# Build and install this checkout on clean distributions, never public HEAD.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
command -v docker >/dev/null || { echo "Docker is required; no checks ran." >&2; exit 2; }
command -v python3 >/dev/null || { echo "Python 3 is required." >&2; exit 2; }
docker info >/dev/null 2>&1 || { echo "Docker daemon is unavailable; no checks ran." >&2; exit 2; }
SNAPSHOT="$(mktemp -d)"
trap 'rm -rf "$SNAPSHOT"' EXIT
LOG_DIR="${CLEANHOST_LOG_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/cashstratum-cleanhost-logs.XXXXXX")}"
mkdir -p "$LOG_DIR"
LOG_DIR="$(cd "$LOG_DIR" && pwd)"

# Tracked files (including edits) plus new, nonignored files form the proposal.
# Skip git internals and ignored build outputs. Fetch the exact submodule pin
# inside the container, where dependencies and network access are isolated.
python3 - "$ROOT" "$SNAPSHOT" <<'PY'
import hashlib
from pathlib import Path
import shutil
import subprocess
import sys
root, target = map(Path, sys.argv[1:])
files = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z', '--cached', '--others', '--exclude-standard']).split(b'\0')
manifest = []
for value in sorted(set(files)):
    if not value:
        continue
    name = value.decode()
    source = root / name
    if not source.exists() or source.is_dir():
        continue
    destination = target / name
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination, follow_symlinks=False)
    if source.is_file():
        manifest.append(hashlib.sha256(source.read_bytes()).hexdigest() + '  ' + name)
(target / 'CHECKOUT-SHA256.txt').write_text('\n'.join(manifest) + '\n')
revision = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
(target / 'CHECKOUT-REVISION.txt').write_text(revision + '\n')
entry = subprocess.check_output(['git', '-C', str(root), 'ls-tree', 'HEAD', 'src/secp256k1'], text=True).split()
if len(entry) < 3 or entry[0] != '160000':
    raise SystemExit('No pinned secp256k1 submodule in this checkout')
(target / 'SECP-REVISION.txt').write_text(entry[2] + '\n')
PY
cp "$SNAPSHOT/CHECKOUT-SHA256.txt" "$SNAPSHOT/CHECKOUT-REVISION.txt" "$LOG_DIR/"
if [[ $# -eq 0 ]]; then set -- ubuntu:24.04 fedora:latest; fi

for distro in "$@"; do
  case "$distro" in ubuntu:*|fedora:*) ;; *) echo "Unsupported test image: $distro" >&2; exit 2 ;; esac
  mkdir -p "$LOG_DIR/${distro//[:\/]/-}-details"
  mounts=(-v "$SNAPSHOT:/checkout:ro" -v "$LOG_DIR/${distro//[:\/]/-}-details:/evidence")
  if [[ -n "${CLEANHOST_BCHN_DIR:-}" ]]; then
    [[ -x "$CLEANHOST_BCHN_DIR/bin/bitcoind" ]] || { echo "CLEANHOST_BCHN_DIR must contain bin/bitcoind" >&2; exit 2; }
    mounts+=(-v "$CLEANHOST_BCHN_DIR:/opt/bchn:ro")
  fi
  echo "Testing $distro; logs: $LOG_DIR"
  # pipefail propagates package, build, test, installer and Docker failures.
  docker run --rm -i "${mounts[@]}" "$distro" bash -s <<'CONTAINER' 2>&1 | tee "$LOG_DIR/${distro//[:\/]/-}.log"
set -Eeuo pipefail
trap 'echo "FAILED at line $LINENO: $BASH_COMMAND" >&2' ERR
collect_evidence() {
  result=$?
  mkdir -p /evidence
  cp /src/config.log /src/test/test-suite.log /tmp/daemon-help.txt /evidence/ 2>/dev/null || true
  tar -czf /evidence/runtime-logs.tar.gz --ignore-failed-read /tmp/install-node/regtest/debug.log /tmp/installed-pool.log /tmp/installed-proxy.log /tmp/interactive-pool.log /tmp/ckpool-e2e.* /solo-stage/opt/cashstratum/logs 2>/dev/null || true
  exit "$result"
}
trap collect_evidence EXIT
. /etc/os-release
case "$ID" in
  ubuntu)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y --no-install-recommends build-essential git autoconf automake libtool pkg-config yasm libzmq3-dev libsodium-dev libssl-dev libcap2-bin python3 jq curl ca-certificates libcurl4 libjansson4
    ;;
  fedora)
    # The minimal image omits the service-unit directory and account tools
    # supplied on an installed system; service state is still simulated below.
    dnf install -y gcc gcc-c++ make git autoconf automake libtool pkgconf-pkg-config yasm zeromq-devel libsodium-devel openssl-devel libcap python3 jq curl ca-certificates libcurl jansson systemd shadow-utils
    ;;
esac
mkdir /src
cp -a /checkout/. /src/
cd /src
sha256sum -c CHECKOUT-SHA256.txt >/dev/null
printf 'Source base revision: '; cat CHECKOUT-REVISION.txt
# No mount is writable; each run builds its own submodule and object files.
git clone -q https://github.com/bitcoin-core/secp256k1.git src/secp256k1
git -C src/secp256k1 checkout -q "$(cat SECP-REVISION.txt)"
./autogen.sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s testing -p '*regression*.py' -v
./configure --disable-sv2
make -j"$(nproc)"
make -C test check
make distcheck DISTCHECK_CONFIGURE_FLAGS=--disable-sv2
make DESTDIR=/stage install
for executable in cashstratum csproxy ckpmsg notifier; do
  test -x "/stage/usr/local/bin/$executable"
done
/stage/usr/local/bin/cashstratum --help > /tmp/daemon-help.txt 2>&1
grep -q -- '--name NAME' /tmp/daemon-help.txt
if [[ -x /opt/bchn/bin/bitcoind ]]; then
  export PATH="/opt/bchn/bin:$PATH"
  /opt/bchn/bin/bitcoind --version
  export CASHSTRATUM_RPC_PASSWORD='cleanhost-test-password'
  mkdir /tmp/install-node
  bitcoind -regtest -datadir=/tmp/install-node -rpcuser=cleanhost -rpcpassword="$CASHSTRATUM_RPC_PASSWORD" -rpcport=18443 -port=18444 -rpcbind=127.0.0.1 -rpcallowip=127.0.0.1 -allowunconnectedmining=1 -fallbackfee=0.0002 -daemon=1
  bch() { bitcoin-cli -regtest -datadir=/tmp/install-node -rpcuser=cleanhost -rpcpassword="$CASHSTRATUM_RPC_PASSWORD" -rpcport=18443 "$@"; }
  for attempt in $(seq 1 60); do bch getblockchaininfo >/dev/null 2>&1 && break; sleep 1; done
  bch getblockchaininfo >/dev/null
  bch createwallet cleanhost >/dev/null
  ADDRESS=$(bch -rpcwallet=cleanhost getnewaddress)
  bch generatetoaddress 101 "$ADDRESS" >/dev/null
  # A real BCHN validates the generated configuration before either install.
  solo_args=(--source-dir /src --destdir /solo-stage --install-dir /opt/cashstratum --user root --rpc-url 127.0.0.1:18443 --rpc-user cleanhost --address "$ADDRESS")
  bash scripts/install-cashstratum-solo.sh "${solo_args[@]}"
  cp /solo-stage/opt/cashstratum/cashstratum.conf /tmp/installed.conf
  bash scripts/install-cashstratum-solo.sh "${solo_args[@]}"
  cmp /tmp/installed.conf /solo-stage/opt/cashstratum/cashstratum.conf
  # Follow the generated unit's exact paths and options inside this disposable
  # container; verify external helpers can reach the installed daemon.
  ln -s /solo-stage/opt/cashstratum /opt/cashstratum
  /opt/cashstratum/cashstratum -c /opt/cashstratum/cashstratum.conf -n cashstratum -B -L >/tmp/installed-pool.log 2>&1 &
  pool_pid=$!
  for attempt in $(seq 1 30); do
    [[ -S /tmp/cashstratum/listener ]] && break
    kill -0 "$pool_pid"
    sleep 1
  done
  printf 'ping\n' | /opt/cashstratum/ckpmsg -n cashstratum | grep -q pong
  test -s /opt/cashstratum/logs/cashstratum.log
  # Install and exercise the real BCH proxy against the running solo pool.
  # Containers do not boot systemd: record service-management calls, then run
  # the generated unit's ExecStart ourselves and verify a Stratum handshake.
  test -d /etc/systemd/system
  command -v useradd
  command -v runuser
  mkdir /service-test-bin
  cat > /service-test-bin/systemctl <<'SYSTEMCTL'
#!/bin/sh
# Simulated service state only. The harness separately launches the actual
# daemon and checks its Stratum socket; this is not a systemd runtime test.
set -eu
state="${SYSTEMCTL_FIXTURE_DIR:-/service-test-state}"
mkdir -p "$state"
printf '%s\n' "$*" >> "${SYSTEMCTL_FIXTURE_LOG:-/evidence/systemctl-calls.txt}"
operation="${1:-}"
[ "$#" -gt 0 ] || exit 2
shift
unit=''
for argument in "$@"; do
    case "$argument" in --quiet) ;; -*) exit 2 ;; *) [ -z "$unit" ] || exit 2; unit="$argument" ;; esac
done
[ "$operation" != daemon-reload ] || exit 0
unit="${unit%.service}"
case "$unit" in ''|*[!a-zA-Z0-9_.@-]*) exit 2 ;; esac
case "$operation" in
    start|restart) : > "$state/$unit.active" ;;
    stop) rm -f "$state/$unit.active" ;;
    enable) : > "$state/$unit.enabled" ;;
    disable) rm -f "$state/$unit.enabled" ;;
    is-active) [ -f "$state/$unit.active" ] ;;
    is-enabled) [ -f "$state/$unit.enabled" ] ;;
    *) echo "unsupported simulated systemctl operation: $operation" >&2; exit 2 ;;
esac
SYSTEMCTL
  chmod +x /service-test-bin/systemctl
  printf '\n127.0.0.1:3333\n%s\nx\nn\n3334\n' "$ADDRESS" | PATH="/service-test-bin:$PATH" bash scripts/install-csproxy.sh --source-dir /src
  python3 -c 'import json; c=json.load(open("/etc/cashstratum/csproxy.conf")); assert c["proxy"][0]["url"] == "127.0.0.1:3333"'
  /usr/local/bin/csproxy -n csproxy -q -c /etc/cashstratum/csproxy.conf >/tmp/installed-proxy.log 2>&1 &
  proxy_pid=$!
  python3 - <<'PROXY'
import json, socket, time
for attempt in range(60):
    try:
        with socket.create_connection(('127.0.0.1', 3334), timeout=2) as connection:
            connection.sendall(b'{"id":1,"method":"mining.subscribe","params":["cleanhost"]}\n')
            stream = connection.makefile('rb')
            for _ in range(10):
                response=json.loads(stream.readline())
                if response.get('id') == 1:
                    assert response.get('result') and not response.get('error'), response
                    break
            else:
                raise RuntimeError('proxy did not answer subscription')
        break
    except (OSError, ValueError, RuntimeError):
        if attempt == 59: raise
        time.sleep(1)
print('Installed proxy accepted a Stratum subscription through the solo pool')
PROXY
  kill "$proxy_pid"
  wait "$proxy_pid" || true
  kill "$pool_pid"
  wait "$pool_pid" || true
  # The interactive installer promises an unprivileged install. Exercise it
  # as its own account, including a rerun and launch from a different cwd.
  useradd -m installer
  mkdir /interactive-src
  cp -a /src/. /interactive-src/
  chown -R installer:installer /interactive-src
  printf '/home/installer/cashstratum\n127.0.0.1:18443\ncleanhost\n%s\nbitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a\n' "$CASHSTRATUM_RPC_PASSWORD" | runuser -u installer -- bash -c 'cd /interactive-src && bash install-cashstratum.sh'
  cp /home/installer/cashstratum/cashstratum.conf /tmp/interactive.conf
  printf '/home/installer/cashstratum\n' | runuser -u installer -- bash -c 'cd /interactive-src && bash install-cashstratum.sh'
  cmp /tmp/interactive.conf /home/installer/cashstratum/cashstratum.conf
  python3 - <<'INTERACTIVE'
import json, os, pwd
path='/home/installer/cashstratum/regtest.conf'
config=json.load(open('/opt/cashstratum/cashstratum.conf'))
config['logdir']='/home/installer/cashstratum/logs'
with open(path,'w') as output: json.dump(config, output)
account=pwd.getpwnam('installer'); os.chown(path,account.pw_uid,account.pw_gid); os.chmod(path,0o600)
INTERACTIVE
  mv /tmp/cashstratum /tmp/solo-cashstratum-stopped
  runuser -u installer -- bash -c 'cd / && exec /home/installer/cashstratum/start-cashstratum.sh /home/installer/cashstratum/regtest.conf' >/tmp/interactive-pool.log 2>&1 &
  interactive_pid=$!
  for attempt in $(seq 1 30); do [[ -S /tmp/cashstratum/listener ]] && break; kill -0 "$interactive_pid"; sleep 1; done
  runuser -u installer -- bash -c 'printf "ping\n" | /home/installer/cashstratum/ckpmsg -n cashstratum' | grep -q pong
  kill "$interactive_pid"
  wait "$interactive_pid" || true
  bch stop >/dev/null
  for attempt in $(seq 1 30); do [[ ! -e /tmp/install-node/regtest/bitcoind.pid ]] && break; sleep 1; done
  # Reuse the full money gate, now with the SOLO INSTALLER'S artifacts.
  mkdir -p /installed/src /installed/testing
  cp /opt/cashstratum/cashstratum /installed/src/ckpool
  cp /opt/cashstratum/ckpmsg /installed/src/ckpmsg
  cp testing/regtest-e2e.sh testing/minerd /installed/testing/
  bash /installed/testing/regtest-e2e.sh
else
  echo 'NOT RUN: live solo install/start/rerun and mining gate (set CLEANHOST_BCHN_DIR).'
fi
make DESTDIR=/stage uninstall
test ! -e /stage/usr/local/bin/cashstratum
test ! -e /stage/usr/local/bin/csproxy
make distclean
./configure --enable-sv2
make -j"$(nproc)"
make -C test check
make DESTDIR=/sv2-stage install
test -x /sv2-stage/usr/local/bin/csproxy
echo "PASS: $ID exact-checkout builds, unit tests and installations"
CONTAINER
done
echo "All requested distributions passed. Evidence: $LOG_DIR"
