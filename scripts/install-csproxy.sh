#!/bin/bash

# Installs csproxy from the latest git source and sets it up as a systemd
# service, prompting for upstream pool(s) and the local port to bind to.

# Exit on errors, unset variables, and failures anywhere in a pipeline. Without
# pipefail the source-copy pipe below reports only the receiving tar's status,
# so a failed sender would leave a silently incomplete build tree.
set -euo pipefail

# Public source for explicitly selected CashStratum releases.
GIT_URL="https://github.com/cashstratum/cashstratum.git"
LOCAL_SOURCE=""
RELEASE_REF=""
while (($#)); do
    case "$1" in
        --source-dir|--ref)
            (($# >= 2)) || { echo "Missing argument for $1" >&2; exit 2; }
            if [[ "$1" == --source-dir ]]; then LOCAL_SOURCE=$2; else RELEASE_REF=$2; fi
            shift 2 ;;
        --help|-h)
            echo "Usage: sudo $0 --source-dir CHECKOUT | --ref RELEASE_TAG"
            echo "Interactive BCH Stratum V1 proxy installer. Existing config replacement requires confirmation."
            exit 0 ;;
        *) echo "Unknown argument: $1" >&2; exit 2 ;;
    esac
done
if [[ -z "$LOCAL_SOURCE" && ( -z "$RELEASE_REF" || "$RELEASE_REF" == -* ) ]]; then
    echo "Choose --source-dir CHECKOUT or an explicit --ref RELEASE_TAG." >&2
    exit 2
fi
SRC_DIR="/opt/cashstratum"
CONF_DIR="/etc/cashstratum"
CONF_FILE="$CONF_DIR/csproxy.conf"
LOG_DIR="/var/log/cashstratum"
SERVICE_FILE="/etc/systemd/system/csproxy.service"

# Function to detect distro and set package manager. Derivatives (mint, pop,
# devuan, raspbian, kali, rocky, alma, oracle, amazon...) use the package names
# of the distro they are built from, so match on ID first and fall back to the
# ID_LIKE list that os-release advertises for exactly this purpose.
detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        DISTRO=$ID
        DISTRO_LIKE=${ID_LIKE:-}
    else
        echo "Unsupported distribution. Exiting."
        exit 1
    fi
    case " $DISTRO $DISTRO_LIKE " in
        *" ubuntu "*|*" debian "*)
            PKG_MANAGER="apt"
            INSTALL_CMD="apt install -y"
            UPDATE_CMD="apt update"
            # Build dependencies for the BCH Stratum V1 proxy.
            PACKAGES="build-essential git autoconf automake libtool pkg-config yasm libzmq3-dev python3"
            ;;
        *" fedora "*|*" rhel "*|*" centos "*)
            PKG_MANAGER="dnf"
            INSTALL_CMD="dnf install -y"
            UPDATE_CMD="dnf check-update || true"
            PACKAGES="gcc gcc-c++ make git autoconf automake libtool pkgconf-pkg-config yasm zeromq-devel python3"
            ;;
        *)
            echo "Unsupported distribution: $DISTRO. Exiting."
            echo "Debian and Red Hat based distributions are supported; this one declares"
            echo "neither in its /etc/os-release ID or ID_LIKE."
            exit 1
            ;;
    esac
    if [ "$DISTRO" != "$DISTRO_LIKE" ] && [ -n "$DISTRO_LIKE" ]; then
        echo "Detected $DISTRO, installing $PKG_MANAGER packages for $DISTRO_LIKE."
    fi
}

# Parse a pool url into PARSED_HOST / PARSED_PORT / PARSED_SV2.
# Accepts host:port, stratum+tcp://host:port, host:port/KEY and
# stratum2+tcp://host:port/KEY forms. A path component means SV2.
parse_pool_url() {
    local url="$1" rest hostport
    rest="${url#*://}"
    if [ "${rest%%/*}" != "$rest" ]; then
        PARSED_SV2=true
        hostport="${rest%%/*}"
    else
        PARSED_SV2=false
        hostport="$rest"
    fi
    PARSED_HOST="${hostport%:*}"
    PARSED_PORT="${hostport##*:}"
    if [ -z "$PARSED_HOST" ] || [ "$PARSED_HOST" = "$PARSED_PORT" ] || \
       ! [[ "$PARSED_PORT" =~ ^[0-9]+$ ]] || [ "$PARSED_PORT" -lt 1 ] || [ "$PARSED_PORT" -gt 65535 ]; then
        return 1
    fi
    return 0
}

# Wait for the correlated Stratum subscription response. Upstream pools may
# send difficulty/notify messages before answering the request.
test_pool() {
    local host="$1" port="$2" sv2="$3"
    [[ "$sv2" == false ]] || { echo "This installer supports Stratum V1 only."; return 1; }
    echo "Testing stratum connection to $host:$port..."
    if python3 - "$host" "$port" <<'PREFLIGHT'
import json, re, socket, sys, time
try:
    deadline = time.monotonic() + 10
    with socket.create_connection((sys.argv[1], int(sys.argv[2])), timeout=5) as connection:
        connection.settimeout(max(0.001, deadline - time.monotonic()))
        connection.sendall(b'{"id":1,"method":"mining.subscribe","params":["csproxy-installer"]}\n')
        buffered = b''
        for _ in range(64):
            while b'\n' not in buffered:
                if len(buffered) > 65536:
                    raise ValueError('subscription frame exceeds 64 KiB')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError('subscription response timed out')
                connection.settimeout(remaining)
                chunk = connection.recv(min(4096, 65537 - len(buffered)))
                if not chunk:
                    raise ValueError('connection closed before subscription response')
                buffered += chunk
            line, buffered = buffered.split(b'\n', 1)
            if len(line) > 65536:
                raise ValueError('subscription frame exceeds 64 KiB')
            response = json.loads(line)
            if not isinstance(response, dict):
                raise ValueError('invalid Stratum response')
            if type(response.get('id')) is not int or response['id'] != 1:
                continue
            result = response.get('result')
            if response.get('error') is not None:
                raise ValueError('upstream rejected subscription')
            if (not isinstance(result, list) or len(result) != 3
                or not isinstance(result[0], list) or not isinstance(result[1], str)
                or re.fullmatch(r'(?:[0-9a-fA-F]{2}){0,15}', result[1]) is None
                or type(result[2]) is not int or not 1 <= result[2] <= 8
                or not any(isinstance(item, list) and len(item) >= 2
                           and item[0] == 'mining.notify' and isinstance(item[1], str)
                           for item in result[0])):
                raise ValueError('invalid subscription result')
            break
        else:
            raise ValueError('too many frames without a subscription response')
except (OSError, ValueError, UnicodeError) as error:
    print('Stratum subscription failed: ' + str(error), file=sys.stderr)
    sys.exit(1)
PREFLIGHT
    then
        echo "Connection to $host:$port succeeded (valid subscription response received)."
        return 0
    fi
    echo "Could not verify a Stratum V1 subscription at $host:$port."
    return 1
}

# Escape a string for embedding in JSON
json_escape() {
    printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read())[1:-1])'
}

# Check if sudo
if [ "$EUID" -ne 0 ]; then
    echo "Please run with sudo or as root."
    exit 1
fi

# csproxy is installed as a systemd service, and some supported derivatives
# (devuan, antix, mx without systemd) do not have it.
if ! command -v systemctl >/dev/null 2>&1; then
    echo "systemctl not found. This installer sets up csproxy as a systemd service,"
    echo "which this system does not use. Build cashstratum and run src/csproxy manually"
    echo "instead, see README-CS_MODES.md for the proxy configuration."
    exit 1
fi

# Detect previous installation
if [ -f "$SERVICE_FILE" ] || [ -f "$CONF_FILE" ]; then
    read -p "Previous csproxy installation detected. Overwrite existing config and service? (y/N, default: no): " overwrite_answer
    if [[ ! "$overwrite_answer" =~ ^[Yy]$ ]]; then
        echo "Installation aborted."
        exit 0
    fi
    echo "Overwriting previous installation..."
fi

echo "Starting installation of csproxy. This requires sudo privileges."

# Prompt for service user (default to current sudo user)
current_user=${SUDO_USER:-root}
echo "Optionally, choose a user to run csproxy as (instead of $current_user)."
read -p "Enter existing username, or 'create' to make a new 'cashstratum' user (leave blank for $current_user): " input_user
if [ "$input_user" = "create" ]; then
    if id cashstratum >/dev/null 2>&1; then
        service_user="cashstratum"
    else
        useradd -m -s /bin/bash cashstratum
        service_user="cashstratum"
    fi
elif [ -z "$input_user" ]; then
    service_user="$current_user"
else
    if id "$input_user" >/dev/null 2>&1; then
        service_user="$input_user"
    else
        echo "User $input_user does not exist. Exiting."
        exit 1
    fi
fi

detect_distro
echo "Installing dependencies..."
eval $UPDATE_CMD
$INSTALL_CMD $PACKAGES

# Build a disposable copy; leave any existing /opt tree untouched.
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT
if [[ -n "$LOCAL_SOURCE" ]]; then
    mkdir "$BUILD_DIR/source"
    tar -C "$LOCAL_SOURCE" --exclude=.git -cf - . | tar -C "$BUILD_DIR/source" -xf -
else
    git clone --depth 1 --branch "$RELEASE_REF" "$GIT_URL" "$BUILD_DIR/source"
fi
SRC_DIR="$BUILD_DIR/source"

# Build and install
cd "$SRC_DIR"
if [[ -f Makefile ]]; then make distclean >/dev/null 2>&1 || true; fi
./autogen.sh
# BCH proxy installations use Stratum V1.
./configure --disable-sv2
make -j"$(nproc)"

# Prompt for upstream pools
echo
echo "Enter the upstream pool(s) for csproxy to connect to. Pools are tried in"
echo "order with automatic failover. URL formats:"
echo "  host:port                     Stratum V1 (e.g. stratum.cashstratum.com:3333)"
proxy_entries=""
pool_count=0
while true; do
    echo
    read -p "Upstream pool URL: " pool_url
    if [ -z "$pool_url" ]; then
        if [ $pool_count -eq 0 ]; then
            echo "At least one upstream pool is required."
            continue
        fi
        break
    fi
    if ! parse_pool_url "$pool_url"; then
        echo "Invalid pool URL '$pool_url'. Expected host:port (with optional scheme and SV2 key)."
        continue
    fi
    if [[ "$PARSED_SV2" == true || "$pool_url" == stratum2* ]]; then
        echo "This BCH proxy supports Stratum V1 only; use host:port or stratum+tcp://host:port."
        continue
    fi
    if ! test_pool "$PARSED_HOST" "$PARSED_PORT" "$PARSED_SV2"; then
        read -p "Connection test failed. Add this pool anyway? (y/N, default: no): " keep_answer
        if [[ ! "$keep_answer" =~ ^[Yy]$ ]]; then
            echo "Pool discarded."
            continue
        fi
    fi
    read -p "Username / BCH address for this pool: " pool_auth
    read -p "Password for this pool (often unused, default: x): " pool_pass
    if [ -z "$pool_pass" ]; then pool_pass="x"; fi
    if [ -n "$proxy_entries" ]; then
        proxy_entries+=$',\n'
    fi
    proxy_entries+=$'\t{\n'
    proxy_entries+=$'\t\t"url" : "'"$(json_escape "$pool_url")"$'",\n'
    proxy_entries+=$'\t\t"auth" : "'"$(json_escape "$pool_auth")"$'",\n'
    proxy_entries+=$'\t\t"pass" : "'"$(json_escape "$pool_pass")"$'"\n'
    proxy_entries+=$'\t}'
    pool_count=$((pool_count + 1))
    read -p "Add another (failover) pool? (y/N, default: no): " another_answer
    if [[ ! "$another_answer" =~ ^[Yy]$ ]]; then
        break
    fi
done

# Prompt for local bind port
echo
while true; do
    read -p "Local port for miners to connect to (default: 3334): " bind_port
    if [ -z "$bind_port" ]; then bind_port=3334; fi
    if ! [[ "$bind_port" =~ ^[0-9]+$ ]] || [ "$bind_port" -lt 1 ] || [ "$bind_port" -gt 65535 ]; then
        echo "Invalid port '$bind_port'."
        continue
    fi
    if ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":$bind_port\$"; then
        read -p "Port $bind_port appears to be in use. Use it anyway? (y/N, default: no): " port_answer
        if [[ ! "$port_answer" =~ ^[Yy]$ ]]; then
            continue
        fi
    fi
    break
done

# Prepare private configuration before changing the running installation.
umask 077
cat << EOF > "$BUILD_DIR/csproxy.conf"
{
"proxy" : [
$proxy_entries
],
"serverurl" : [
	"0.0.0.0:$bind_port"
],
"mindiff" : 1,
"startdiff" : 10000,
"logdir" : "$LOG_DIR"
}
EOF
python3 -m json.tool "$BUILD_DIR/csproxy.conf" >/dev/null
"$SRC_DIR/src/ckpool" -p -n csproxy -T -c "$BUILD_DIR/csproxy.conf" >/dev/null

# Create systemd service
cat << EOF > "$BUILD_DIR/csproxy.service"
[Unit]
Description=CashStratum Stratum Proxy
After=network-online.target
Wants=network-online.target

[Service]
User=$service_user
ExecStart=/usr/local/bin/csproxy -n csproxy -q -c $CONF_FILE
StandardOutput=journal
StandardError=journal
Restart=always
RestartSec=5
LimitNOFILE=100000
LimitNPROC=65536

[Install]
WantedBy=multi-user.target
EOF

# Keep a rollback copy of every replaced artifact, including a prior symlink.
# Save directory metadata before changing ownership for a new service account.
python3 - "$LOG_DIR" "$BUILD_DIR/log-metadata.json" <<'PYLOG'
import json,os,pathlib,stat,sys
path,out=sys.argv[1:]
metadata=None
if os.path.lexists(path):
    if os.path.islink(path) or not os.path.isdir(path):
        sys.exit('Proxy log path must be a directory, not a symlink')
    info=os.stat(path)
    metadata={'uid':info.st_uid,'gid':info.st_gid,'mode':stat.S_IMODE(info.st_mode)}
pathlib.Path(out).write_text(json.dumps(metadata))
PYLOG
BINARY_FILE=/usr/local/bin/csproxy
was_active=false
was_enabled=false
systemctl is-active --quiet csproxy && was_active=true
systemctl is-enabled --quiet csproxy && was_enabled=true
for entry in binary config service; do
    case "$entry" in binary) path=$BINARY_FILE ;; config) path=$CONF_FILE ;; service) path=$SERVICE_FILE ;; esac
    if [[ -e "$path" || -L "$path" ]]; then cp -a "$path" "$BUILD_DIR/old-$entry"; fi
done
rollback_needed=false
cleanup() {
    status=$?
    trap - EXIT
    set +e
    if [[ "$rollback_needed" == true ]]; then
        recovery_failed=false
        # Stop only what is actually running. The forward path starts csproxy
        # before `is-active` can fail, so a fresh install can reach rollback
        # with the unit live even though $was_active is false -- guarding on
        # $was_active alone would swap the binary under a running process.
        # Guarding on is-active covers both cases and avoids reporting
        # "operator attention needed" for stopping a unit that never loaded.
        if systemctl is-active --quiet csproxy; then
            systemctl stop csproxy >/dev/null 2>&1 || recovery_failed=true
        fi
        for entry in binary config service; do
            case "$entry" in binary) path=$BINARY_FILE ;; config) path=$CONF_FILE ;; service) path=$SERVICE_FILE ;; esac
            rm -f "$path.new"
            if [[ -e "$BUILD_DIR/old-$entry" || -L "$BUILD_DIR/old-$entry" ]]; then
                if ! cp -a "$BUILD_DIR/old-$entry" "$path.new" || ! mv -f "$path.new" "$path"; then
                    recovery_failed=true
                fi
            else
                rm -f "$path" || recovery_failed=true
            fi
        done
        if ! python3 - "$LOG_DIR" "$BUILD_DIR/log-metadata.json" <<'PYLOG'
import json,os,pathlib,sys
path,saved=sys.argv[1:]
metadata=json.loads(pathlib.Path(saved).read_text())
if metadata is not None:
    os.chown(path,metadata['uid'],metadata['gid'])
    os.chmod(path,metadata['mode'])
PYLOG
        then
            recovery_failed=true
        fi
        systemctl daemon-reload || recovery_failed=true
        if [[ "$was_enabled" == true ]]; then systemctl enable csproxy || recovery_failed=true
        else systemctl disable csproxy >/dev/null 2>&1 || recovery_failed=true; fi
        if [[ "$was_active" == true ]]; then systemctl start csproxy || recovery_failed=true; fi
        if [[ "$recovery_failed" == true ]]; then
            echo "Proxy installation failed and rollback needs operator attention; backups remain at $BUILD_DIR." >&2
            exit "$status"
        fi
        echo 'Proxy installation failed; prior artifacts, log permissions and service state restored.' >&2
    fi
    rm -rf "$BUILD_DIR"
    exit "$status"
}
trap cleanup EXIT
# The service stop is deliberately after all prompts, build and validation.
rollback_needed=true
if [[ "$was_active" == true ]]; then systemctl stop csproxy; fi
mkdir -p "$CONF_DIR" "$LOG_DIR" "$(dirname "$BINARY_FILE")"
install -m 755 "$SRC_DIR/src/ckpool" "$BINARY_FILE.new"
mv -f "$BINARY_FILE.new" "$BINARY_FILE"
install -m 600 -o "$service_user" "$BUILD_DIR/csproxy.conf" "$CONF_FILE.new"
mv -f "$CONF_FILE.new" "$CONF_FILE"
install -m 644 "$BUILD_DIR/csproxy.service" "$SERVICE_FILE.new"
mv -f "$SERVICE_FILE.new" "$SERVICE_FILE"
chown "$service_user" "$LOG_DIR"
systemctl daemon-reload
systemctl start csproxy
sleep 2
systemctl is-active --quiet csproxy
systemctl enable csproxy
rollback_needed=false

echo
echo "Installation complete! csproxy is running with $pool_count upstream pool(s)."
echo "Connect miners to: stratum+tcp://[machine IP]:$bind_port"
echo "Monitor logs:"
echo "  - journalctl -u csproxy -f"
echo "  - tail -f $LOG_DIR/csproxy.log"
echo "Edit $CONF_FILE if needed, then restart with: systemctl restart csproxy"
