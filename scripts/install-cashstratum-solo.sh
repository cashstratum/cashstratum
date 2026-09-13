#!/usr/bin/env bash
# Install a BCH solo pool against an existing Bitcoin Cash Node (BCHN).
# This does not install, reconfigure, stop, or upgrade the node. Legacy pool
# directories are preserved; migrating their evidence is a separate operation.
set -euo pipefail

usage() {
    cat <<'HELP'
Usage: install-cashstratum-solo.sh --user USER --address CASHADDR --rpc-user USER [options]
  --rpc-url HOST:PORT    Existing BCHN HTTP RPC endpoint (127.0.0.1:8332)
  --source-dir PATH      Build this source checkout instead of cloning a release
  --ref TAG             Public release tag to clone (required without --source-dir)
  --install-dir PATH    Pool directory (/opt/cashstratum)
  --destdir PATH        Stage files for inspection; never touch host services
  --no-start            Install unit without enabling or starting it
  --help                Show this help
Set CASHSTRATUM_RPC_PASSWORD in the environment. RPC credentials are never printed.
Install build prerequisites first (see docs/installation.md): C/C++ compiler,
make, autoconf, automake, libtool, pkg-config, yasm, ZeroMQ development headers,
git and python3. BCHN must be running and support getblocktemplate.
Existing cashstratum.conf is preserved byte-for-byte and used for RPC validation.
A staged install writes the unit under DESTDIR/etc/systemd/system; copy/activate
only after inspecting the config and completing an explicit migration of old data.
HELP
}
INSTALL_DIR=/opt/cashstratum
SOURCE_DIR=''
RELEASE_REF=''
DESTDIR=''
SERVICE_USER=''
PAYOUT_ADDRESS=''
RPC_URL=127.0.0.1:8332
RPC_USER=''
START=true
while (($#)); do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --no-start) START=false; shift ;;
        --source-dir|--ref|--destdir|--install-dir|--user|--address|--rpc-url|--rpc-user)
            (($# >= 2)) || { echo "Missing value for $1" >&2; exit 2; }
            case "$1" in
                --source-dir) SOURCE_DIR=$2 ;; --ref) RELEASE_REF=$2 ;;
                --destdir) DESTDIR=$2 ;; --install-dir) INSTALL_DIR=$2 ;;
                --user) SERVICE_USER=$2 ;; --address) PAYOUT_ADDRESS=$2 ;;
                --rpc-url) RPC_URL=$2 ;; --rpc-user) RPC_USER=$2 ;;
            esac
            shift 2 ;;
        *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done
[[ "$INSTALL_DIR" =~ ^/[a-zA-Z0-9_./-]+$ && "$INSTALL_DIR" != / && "$INSTALL_DIR" != *..* ]] || { echo 'Use an absolute installation path without spaces or dot-dot.' >&2; exit 2; }
[[ "$SERVICE_USER" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || { echo '--user must name an existing service account.' >&2; exit 2; }
id "$SERVICE_USER" >/dev/null
if [[ -z "$DESTDIR" ]]; then
    ((EUID == 0)) || { echo 'Run with sudo, or use --destdir for an unprivileged staged install.' >&2; exit 2; }
    command -v systemctl >/dev/null
    if systemctl is-active --quiet ckpool.service; then
        echo 'Legacy ckpool.service is active. Complete the documented stopped-pool migration before installing CashStratum.' >&2
        exit 2
    fi
else
    [[ "$DESTDIR" == /* && "$DESTDIR" != / ]] || { echo '--destdir must be an absolute staging directory.' >&2; exit 2; }
fi
for dependency in python3 make autoconf automake pkg-config git; do
    command -v "$dependency" >/dev/null || { echo "Missing prerequisite: $dependency" >&2; exit 2; }
done
TARGET="$DESTDIR$INSTALL_DIR"
# Refuse symlink targets so a staged inspection cannot accidentally write outside it.
python3 - "$TARGET" <<'PY'
import pathlib,sys
p=pathlib.Path(sys.argv[1])
if any(x.is_symlink() for x in [p,*p.parents]):
    sys.exit('Installation directory and its parents must not be symlinks')
PY
WORK_DIR=$(mktemp -d)
# Set once the previous binaries have been saved and the live service may be
# stopped: from that point a failure must put the old install back, not just
# delete the build tree.
rollback_needed=false
service_was_active=false
cleanup() {
    status=$?
    trap - EXIT
    set +e
    if [[ "$rollback_needed" == true ]]; then
        recovery_failed=false
        for binary in cashstratum ckpmsg notifier; do
            saved="$WORK_DIR/previous/$binary"
            [[ -e "$saved" ]] || continue
            if ! cp -a "$saved" "$TARGET/$binary.restore" || ! mv -f "$TARGET/$binary.restore" "$TARGET/$binary"; then
                recovery_failed=true
            fi
        done
        if [[ "$service_was_active" == true ]]; then
            systemctl start cashstratum.service || recovery_failed=true
        fi
        if [[ "$recovery_failed" == true ]]; then
            echo "CashStratum installation failed and rollback needs operator attention." >&2
            echo "Previous binaries remain at $WORK_DIR/previous; inspect with: journalctl -u cashstratum -n 50" >&2
            # Leave the saved binaries in place for a manual restore.
            exit "$status"
        fi
        echo 'CashStratum installation failed; previous binaries and service state restored.' >&2
        echo 'Inspect the failed build with: journalctl -u cashstratum -n 50' >&2
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
if [[ -n "$SOURCE_DIR" ]]; then
    SOURCE_DIR=$(cd "$SOURCE_DIR" && pwd)
    # Build a copy: never modify the caller's checkout or running binaries.
    mkdir "$WORK_DIR/source"
    tar -C "$SOURCE_DIR" --exclude=.git -cf - . | tar -C "$WORK_DIR/source" -xf -
else
    [[ -n "$RELEASE_REF" && "$RELEASE_REF" != -* ]] || { echo '--ref release tag is required without --source-dir.' >&2; exit 2; }
    git clone --depth 1 --branch "$RELEASE_REF" https://github.com/cashstratum/cashstratum.git "$WORK_DIR/source"
fi
# Validate the node and new payout address before changing an installation.
export RPC_URL RPC_USER PAYOUT_ADDRESS
export CASHSTRATUM_RPC_PASSWORD="${CASHSTRATUM_RPC_PASSWORD:-}"
python3 - "$TARGET/cashstratum.conf" "$WORK_DIR/cashstratum.conf" "$INSTALL_DIR" <<'PY'
import base64,json,os,pathlib,sys,urllib.request,urllib.error,urllib.parse
existing,out,install=sys.argv[1:]
if pathlib.Path(existing).exists():
    config=json.loads(pathlib.Path(existing).read_text())
else:
    address=os.environ['PAYOUT_ADDRESS']
    if not address.startswith(('bitcoincash:', 'bchtest:', 'bchreg:')):
        sys.exit('--address must be a BCH CashAddr with network prefix')
    if not os.environ['RPC_USER'] or not os.environ['CASHSTRATUM_RPC_PASSWORD']:
        sys.exit('RPC username and CASHSTRATUM_RPC_PASSWORD are required')
    config={'btcd':[{'url':os.environ['RPC_URL'],'auth':os.environ['RPC_USER'],
                    'pass':os.environ['CASHSTRATUM_RPC_PASSWORD'],'notify':False}],
            'bchaddress':address,'btcsig':'CashStratum','pooladdress':address,'poolfee':0,
            'serverurl':['0.0.0.0:3333'],
            'logdir':install+'/logs','startdiff':10000,'mindiff':1}
try:
    node=config['btcd'][0]
    url=node['url']
    if '://' not in url: url='http://'+url
    parsed=urllib.parse.urlsplit(url)
    if (parsed.scheme != 'http' or not parsed.hostname or parsed.port is None
            or not 1 <= parsed.port <= 65535 or parsed.username is not None
            or parsed.password is not None or parsed.path or parsed.query or parsed.fragment
            or any(c.isspace() for c in url) or any(c in url for c in ('?', '#'))):
        raise ValueError('RPC requires HTTP host:port without userinfo, path, query or fragment')
    # Validate every configured endpoint against the same C transport contract.
    for other in config['btcd'][1:]:
        endpoint=other['url']
        if '://' not in endpoint: endpoint='http://'+endpoint
        part=urllib.parse.urlsplit(endpoint)
        if (part.scheme != 'http' or not part.hostname or part.port is None
                or not 1 <= part.port <= 65535 or part.username is not None
                or part.password is not None or part.path or part.query or part.fragment
                or any(c.isspace() for c in endpoint) or any(c in endpoint for c in ('?', '#'))):
            raise ValueError('Unsupported fallback RPC endpoint')
    authorization=base64.b64encode((node['auth']+':'+node['pass']).encode()).decode()
    def rpc(method, params=[]):
        request=urllib.request.Request(url,json.dumps({'id':'cashstratum-install',
            'method':method,'params':params}).encode(),{'Authorization':'Basic '+authorization,
            'Content-Type':'application/json'})
        with urllib.request.urlopen(request,timeout=15) as response: data=json.load(response)
        if data.get('error'): raise ValueError('RPC method rejected: '+method)
        return data['result']
    network=rpc('getnetworkinfo')
    if 'Bitcoin Cash Node' not in network.get('subversion',''):
        raise ValueError('endpoint is not Bitcoin Cash Node (BCHN)')
    chain=rpc('getblockchaininfo')['chain']
    address=config.get('bchaddress',config.get('btcaddress',''))
    prefix={'main':'bitcoincash:','test':'bchtest:','test4':'bchtest:','scale':'bchtest:','chip':'bchtest:','regtest':'bchreg:'}.get(chain)
    if not prefix or not address.startswith(prefix): raise ValueError('payout address does not match node chain')
    if not rpc('validateaddress',[address]).get('isvalid'): raise ValueError('invalid payout address')
    rpc('getblocktemplate',[{'capabilities':['coinbasetxn','workid','coinbase/append']}])
except Exception:
    sys.exit('BCHN validation failed: check node availability, credentials, chain, payout address and template readiness. No installation files changed.')
pathlib.Path(out).write_text(json.dumps(config,indent=2)+'\n')
pathlib.Path(out).chmod(0o600)
print('BCHN RPC, payout address and block template validated ('+chain+').')
PY
(
    cd "$WORK_DIR/source"
    if [[ -f Makefile ]]; then make distclean >/dev/null 2>&1 || true; fi
    ./autogen.sh
    ./configure --disable-sv2
    make -j"$(getconf _NPROCESSORS_ONLN)"
)
# Install only after the complete build and node validation have succeeded.
# Save the binaries we are about to replace so a failed activation can restore
# them: without this a bad build leaves the pool down with no way back.
mkdir -p "$WORK_DIR/previous"
for binary in cashstratum ckpmsg notifier; do
    if [[ -e "$TARGET/$binary" ]]; then cp -a "$TARGET/$binary" "$WORK_DIR/previous/$binary"; fi
done
if [[ -z "$DESTDIR" ]] && systemctl is-active --quiet cashstratum.service; then
    service_was_active=true
fi
rollback_needed=true
if [[ "$service_was_active" == true ]]; then
    systemctl stop cashstratum.service
fi
SYSTEMD_DIR="${SYSTEMD_DIR:-$DESTDIR/etc/systemd/system}"
mkdir -p "$TARGET"/{logs,users,pool,data} "$SYSTEMD_DIR"
for binary in ckpool ckpmsg notifier; do
    output=$binary
    [[ "$binary" != ckpool ]] || output=cashstratum
    install -m 755 "$WORK_DIR/source/src/$binary" "$TARGET/$output.new"
    mv -f "$TARGET/$output.new" "$TARGET/$output"
done
if [[ ! -e "$TARGET/cashstratum.conf" ]]; then
    install -m 600 "$WORK_DIR/cashstratum.conf" "$TARGET/cashstratum.conf"
fi
if [[ -f "$WORK_DIR/source/clean-old-blocks.sh" ]]; then
    install -m 755 "$WORK_DIR/source/clean-old-blocks.sh" "$TARGET/clean-old-blocks.sh"
fi
cat > "$SYSTEMD_DIR/cashstratum.service" <<UNIT
[Unit]
Description=CashStratum BCH solo mining pool
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/cashstratum -c $INSTALL_DIR/cashstratum.conf -n cashstratum -B -L
Restart=on-failure
RestartSec=10
NoNewPrivileges=true
LimitNOFILE=100000
[Install]
WantedBy=multi-user.target
UNIT
if [[ -z "$DESTDIR" ]]; then
    chown "$SERVICE_USER" "$TARGET" "$TARGET/cashstratum.conf" "$TARGET/logs" "$TARGET/users" "$TARGET/pool" "$TARGET/data"
    systemctl daemon-reload
    if [[ "$START" == true ]]; then
        systemctl enable cashstratum.service
        systemctl restart cashstratum.service
        sleep 2
        systemctl is-active --quiet cashstratum.service
    fi
fi
# Past this point the new install is live and validated; stop guarding it.
rollback_needed=false
printf 'CashStratum installed at %s. Existing configuration and legacy directories preserved.\n' "$TARGET"
printf 'Only Stratum V1 is configured (port 3333). No cleanup schedule was enabled.\n'
printf 'Optional Go API/notifier components are installed separately; see api/README.md and api/NOTIFIER.md.\n'
