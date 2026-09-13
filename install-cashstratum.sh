#!/bin/bash

# CashStratum Installation Script
# Builds from current working directory and installs to ~/cashstratum

set -e

echo "======================================"
echo "CashStratum Installation"
echo "======================================"
echo

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Check if running as root
if [ "$EUID" -eq 0 ]; then
   echo -e "${RED}Please don't run this script as root${NC}"
   exit 1
fi

# Detect OS
OS="unknown"
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="mac"
fi

if [[ "$OS" != linux ]]; then
    echo "CashStratum requires Linux (epoll and Linux process APIs). Build in a Linux host or container." >&2
    exit 1
fi

# Check and install dependencies
echo "Checking dependencies..."
MISSING_DEPS=()

check_command() {
    if ! command -v "$1" &> /dev/null; then
        MISSING_DEPS+=("$1")
        return 1
    fi
    return 0
}

# Required build tools
check_command autoconf || true
check_command automake || true
check_command libtoolize || true
check_command make || true
check_command gcc || true
check_command pkg-config || true
check_command python3 || true

# Optional but recommended
check_command yasm || true

# Check for required libraries
check_library() {
    if [[ "$OS" == "linux" ]]; then
        if ! ldconfig -p | grep -q "$1"; then
            return 1
        fi
    elif [[ "$OS" == "mac" ]]; then
        if ! brew list --formula | grep -q "$2"; then
            return 1
        fi
    fi
    return 0
}

# Check libraries
if [[ "$OS" == "linux" ]]; then
    check_library "libzmq" && true || MISSING_DEPS+=("libzmq-dev")
    check_library "libssl" && true || MISSING_DEPS+=("libssl-dev")
elif [[ "$OS" == "mac" ]]; then
    check_library "" "zeromq" && true || MISSING_DEPS+=("zeromq")
    check_library "" "openssl" && true || MISSING_DEPS+=("openssl")
fi

if [ ${#MISSING_DEPS[@]} -gt 0 ]; then
    echo -e "${YELLOW}Missing dependencies detected: ${MISSING_DEPS[*]}${NC}"
    echo

    if [[ "$OS" == "linux" ]]; then
        echo "Install with:"
        if command -v apt-get &> /dev/null; then
            echo "  sudo apt-get update"
            echo "  sudo apt-get install -y build-essential autoconf automake libtool pkg-config libzmq3-dev libssl-dev yasm python3"
        elif command -v yum &> /dev/null; then
            echo "  sudo yum groupinstall 'Development Tools'"
            echo "  sudo yum install -y autoconf automake libtool pkgconfig zeromq-devel openssl-devel yasm python3"
        fi
    elif [[ "$OS" == "mac" ]]; then
        echo "Install with Homebrew:"
        echo "  brew install autoconf automake libtool pkg-config zeromq openssl yasm python3"
    fi

    echo
    read -p "Would you like to install these dependencies now? (y/n) " -n 1 -r
    echo

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if [[ "$OS" == "linux" ]]; then
            if command -v apt-get &> /dev/null; then
                sudo apt-get update
                sudo apt-get install -y build-essential autoconf automake libtool pkg-config libzmq3-dev libssl-dev yasm python3
            elif command -v yum &> /dev/null; then
                sudo yum groupinstall -y 'Development Tools'
                sudo yum install -y autoconf automake libtool pkgconfig zeromq-devel openssl-devel yasm python3
            else
                echo -e "${RED}Unsupported package manager. Please install dependencies manually.${NC}"
                exit 1
            fi
        elif [[ "$OS" == "mac" ]]; then
            if ! command -v brew &> /dev/null; then
                echo -e "${RED}Homebrew not found. Please install from https://brew.sh${NC}"
                exit 1
            fi
            brew install autoconf automake libtool pkg-config zeromq openssl yasm python3
        fi
        echo -e "${GREEN}✓ Dependencies installed${NC}"
    else
        echo -e "${RED}Cannot proceed without dependencies${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ All dependencies satisfied${NC}"
fi

echo

# Prompt for installation directory
echo -e "${YELLOW}Where would you like to install CashStratum?${NC}"
read -e -r -p "Installation directory (default: $HOME/cashstratum): " USER_INSTALL_DIR
INSTALL_DIR="${USER_INSTALL_DIR:-$HOME/cashstratum}"
CURRENT_DIR=$(pwd)

echo
echo "Building from: $CURRENT_DIR"
echo "Installing to: $INSTALL_DIR"
echo

# Build from current directory
echo "Building CashStratum..."

# Clean any previous build attempts
make clean 2>/dev/null || true

# Build
autoreconf -fiv
# --disable-sv2: Stratum V2 is Bitcoin-only and its job-declaration protocol
# has no BCH counterpart, so leave it out rather than have the build flip
# depending on whether libsodium and the src/secp256k1 submodule happen to be
# present on the host. Cap'n Proto mining IPC is likewise auto-detected and
# stays off on a normal BCH box.
./configure --prefix="$INSTALL_DIR/build" --disable-sv2
make -j"$(getconf _NPROCESSORS_ONLN)"

if [ ! -f "src/ckpool" ]; then
    echo -e "${RED}Build failed!${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Build successful${NC}"

# Install
echo "Installing to $INSTALL_DIR..."
make install

# Create installation directory structure
mkdir -p "$INSTALL_DIR"/{logs,users,pool,data}
mkdir -p "$INSTALL_DIR/logs/shares"

# Copy binaries to main directory for easy access
echo "Copying binaries..."
cp src/ckpool "$INSTALL_DIR/cashstratum"
cp src/ckpmsg "$INSTALL_DIR/"
cp src/notifier "$INSTALL_DIR/"
[ -d "build/bin" ] && cp build/bin/* "$INSTALL_DIR/" 2>/dev/null || true
[ -f "clean-old-blocks.sh" ] && cp clean-old-blocks.sh "$INSTALL_DIR/" 2>/dev/null || true

# Make binaries executable
chmod +x "$INSTALL_DIR"/{cashstratum,ckpmsg,notifier} 2>/dev/null || true
[ -f "$INSTALL_DIR/clean-old-blocks.sh" ] && chmod +x "$INSTALL_DIR/clean-old-blocks.sh" 2>/dev/null || true

echo -e "${GREEN}✓ Installation complete${NC}"

# Preserve configured nodes and payouts when rerunning to add components.
if [ ! -f "$INSTALL_DIR/cashstratum.conf" ]; then
    read -r -p "BCH node RPC endpoint (default 127.0.0.1:8332): " RPC_URL
    RPC_URL="${RPC_URL:-127.0.0.1:8332}"
    read -r -p "BCH node RPC username: " RPC_USER
    read -r -s -p "BCH node RPC password: " RPC_PASSWORD; echo
    read -r -p "Your fallback/operator BCH CashAddr (bitcoincash:...): " PAYOUT_ADDRESS
    if [[ "$PAYOUT_ADDRESS" != bitcoincash:* || -z "$RPC_USER" || -z "$RPC_PASSWORD" ]]; then
        echo "A mainnet CashAddr and node credentials are required." >&2
        exit 1
    fi
    # Escape JSON strings without putting credentials in process arguments.
    json_string() { printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'; }
    RPC_URL_JSON=$(json_string "$RPC_URL")
    RPC_USER_JSON=$(json_string "$RPC_USER")
    RPC_PASSWORD_JSON=$(json_string "$RPC_PASSWORD")
    PAYOUT_JSON=$(json_string "$PAYOUT_ADDRESS")
# Create mainnet configuration
echo "Creating mainnet configuration..."
(
umask 077
cat > "$INSTALL_DIR/cashstratum.conf" << CONF_EOF
{
    "btcd": [{
        "url": $RPC_URL_JSON,
        "auth": $RPC_USER_JSON,
        "pass": $RPC_PASSWORD_JSON,
        "notify": true,
        "zmqnotify": "tcp://127.0.0.1:28333"
    }],
    "bchaddress": $PAYOUT_JSON,
    "btcsig": "CashStratum",
    "pooladdress": $PAYOUT_JSON,
    "poolfee": 0.0,
    "blockpoll": 50,
    "update_interval": 15,
    "serverurl": ["0.0.0.0:3333"],
    "logdir": "logs",
    "node_warning": false,
    "log_shares": true,
    "asicboost": true,
    "version_mask": "1fffe000",
    "maxclients": 10000,
    "mindiff": 500000,
    "startdiff": 500000,
    "maxdiff": 0,
    "mindiff_overrides": {
        "nicehash": 500000,
        "NiceHash": 500000,
        "MiningRigRentals": 1000000,
        "miningrigrentals": 1000000,
        "bitaxe": 1,
        "test": 1
    }
}
CONF_EOF
)
chmod 600 "$INSTALL_DIR/cashstratum.conf"
fi

# Create start script
cat > "$INSTALL_DIR/start-cashstratum.sh" << 'START_EOF'
#!/bin/bash

CONFIG="${1:-cashstratum.conf}"

echo "Starting CashStratum with config: $CONFIG"

cd "$(dirname "$0")"

# Start cashstratum
exec ./cashstratum -c "$CONFIG" -n cashstratum -L -B

echo "CashStratum started. Check logs/cashstratum.log"
START_EOF

chmod +x "$INSTALL_DIR/start-cashstratum.sh"

# Create stop script
cat > "$INSTALL_DIR/stop-cashstratum.sh" << STOP_EOF
#!/bin/bash

echo "Stopping CashStratum..."
pkill -TERM -x cashstratum || true
sleep 2

if pgrep -x "cashstratum" > /dev/null; then
    echo "Force stopping..."
    pkill -9 -x cashstratum 2>/dev/null || true
fi

echo "CashStratum stopped."
STOP_EOF

chmod +x "$INSTALL_DIR/stop-cashstratum.sh"

# ---------------------------------------------------------------------------
# Go log API & Notifier components (see api/README.md and api/NOTIFIER.md)
#
# Built here, as the unprivileged install user, because they must RUN as the same
# account as cashstratum: cashstratum creates its logdir mode 0750, so any other user gets
# "permission denied" on every endpoint while /health still cheerfully answers.
#
# The systemd unit and the firewall rule need root, so they belong to
# post-install.sh. This step only produces the binary and its environment file.
# ---------------------------------------------------------------------------
API_PORT="${CASHSTRATUM_API_PORT:-8888}"
API_DIR="$INSTALL_DIR/api"
API_BUILT=false
NOTIFIER_BUILT=false

if [ -d "$CURRENT_DIR/api" ]; then
echo
echo "Building Go components (API & Notifier)..."
if ! command -v go &> /dev/null; then
    echo -e "${YELLOW}⚠ Go not installed - skipping Go components.${NC}"
    echo "  Ubuntu's apt package is Go 1.22 and too old to build it."
    echo "  Install from https://go.dev/dl/ and re-run this script to add it."
elif ! command -v openssl &> /dev/null; then
    echo -e "${YELLOW}⚠ openssl not found - skipping Go components (needed for keys).${NC}"
else
    mkdir -p "$API_DIR"
    if (cd "$CURRENT_DIR/api" && go build -ldflags="-s -w" -o "$API_DIR/cashstratum-api" .); then
        chmod +x "$API_DIR/cashstratum-api"
        cp "$API_DIR/cashstratum-api" "$INSTALL_DIR/" 2>/dev/null || true
        API_BUILT=true
        echo -e "${GREEN}✓ Built $API_DIR/cashstratum-api${NC}"

        if [ -f "$API_DIR/cashstratum-api.env" ]; then
            echo -e "${YELLOW}  Existing cashstratum-api.env kept - your API key is unchanged${NC}"
        else
            (
umask 077
KEY=$(openssl rand -hex 32)
cat > "$API_DIR/cashstratum-api.env" <<ENVEOF
# Generated by install-cashstratum.sh. Keep this file at mode 0600.
#
# CASHSTRATUM_API_KEY is the ONLY authentication on this API and it travels in
# cleartext over HTTP. Never expose the port to the internet -- scope it in
# the firewall to the address of the application that consumes it.
CASHSTRATUM_API_KEY=$KEY
CASHSTRATUM_LOG_PATH=$INSTALL_DIR/logs/cashstratum.log
CASHSTRATUM_USER_LOGS_PATH=$INSTALL_DIR/logs/users
CASHSTRATUM_API_PORT=$API_PORT
ENVEOF
            )
            chmod 600 "$API_DIR/cashstratum-api.env"
            echo -e "${GREEN}✓ Generated $API_DIR/cashstratum-api.env with a fresh 32-byte key${NC}"
        fi
    else
        echo -e "${RED}✗ Go API build failed - continuing without it${NC}"
    fi

    if [ -d "$CURRENT_DIR/api/notifier" ]; then
        if (cd "$CURRENT_DIR/api/notifier" && go build -ldflags="-s -w" -o "$API_DIR/cashstratum-notifier" .); then
            chmod +x "$API_DIR/cashstratum-notifier"
            cp "$API_DIR/cashstratum-notifier" "$INSTALL_DIR/" 2>/dev/null || true
            NOTIFIER_BUILT=true
            echo -e "${GREEN}✓ Built $API_DIR/cashstratum-notifier${NC}"

            if [ -f "$API_DIR/cashstratum-notifier.env" ]; then
                echo -e "${YELLOW}  Existing cashstratum-notifier.env kept${NC}"
            else
                (
umask 077
SECRET=$(openssl rand -hex 32)
cat > "$API_DIR/cashstratum-notifier.env" <<NOTIFYEOF
# Generated by install-cashstratum.sh. Keep this file at mode 0600.
CASHSTRATUM_NOTIFY_URL=http://127.0.0.1:8000/api/v1/pool/notify
CASHSTRATUM_NOTIFY_SECRET=$SECRET
CASHSTRATUM_CONF_PATH=$INSTALL_DIR/cashstratum.conf
CASHSTRATUM_LOG_PATH=$INSTALL_DIR/logs/cashstratum.log
NOTIFY_DRY_RUN=true
NOTIFYEOF
                )
                chmod 600 "$API_DIR/cashstratum-notifier.env"
                echo -e "${GREEN}✓ Generated $API_DIR/cashstratum-notifier.env${NC}"
            fi
        else
            echo -e "${RED}✗ CashStratum Notifier build failed - continuing without it${NC}"
        fi
    fi
fi
fi

echo
echo "======================================"
echo -e "${GREEN}Installation Complete!${NC}"
echo "======================================"
echo
echo "Installed to: $INSTALL_DIR"
echo
echo "Configuration files created:"
echo "  • cashstratum.conf - Mainnet configuration (port 3333)"

echo
echo -e "${YELLOW}Before starting:${NC}"
echo "1. Edit the config file with your BCH node credentials"
echo "2. Update bchaddress with your mining address (fallback for non-address usernames)"
echo "3. Update pooladdress with your pool operator fee address"
echo "4. Set poolfee to desired percentage (e.g., 2.0 for 2%, configurable 0-50)"
echo "5. Additional instances require distinct -n NAME and -s /tmp/NAME arguments (see docs/installation.md)"
echo
echo "To start:"
echo "  cd $INSTALL_DIR"
echo "  ./start-cashstratum.sh                               # Uses cashstratum.conf (mainnet)"

echo
echo "To monitor:"
echo "  tail -f $INSTALL_DIR/logs/cashstratum.log"
echo
echo "To stop:"
echo "  ./stop-cashstratum.sh"
echo
if [ "$API_BUILT" = true ]; then
echo -e "${GREEN}Go log API:${NC}"
echo "  Read-only HTTP over CashStratum's logs. Built, but NOT yet running."
echo
echo "  1. Install the service and open the port (needs root):"
echo "       sudo ./post-install.sh"
echo
echo "  2. Read your API key when you need it (never echo it into a shared log):"
echo "       sudo grep CASHSTRATUM_API_KEY $API_DIR/cashstratum-api.env"
echo
echo "  3. Reach it - all endpoints need the bearer token:"
echo "       KEY=\$(sudo sed -n 's/^CASHSTRATUM_API_KEY=//p' $API_DIR/cashstratum-api.env)"
echo "       curl -H "Authorization: Bearer \$KEY" http://127.0.0.1:$API_PORT/health"
echo "       curl -H "Authorization: Bearer \$KEY" http://127.0.0.1:$API_PORT/stats"
echo
echo "  Endpoints: /health /stats /tail /grep /find-block /user-log /user-file"
echo "             /coinbase /metrics          (see api/README.md)"
echo
echo -e "${YELLOW}  Port $API_PORT must NOT face the internet.${NC} The key is the only"
echo "  authentication and HTTP sends it in cleartext. post-install.sh will ask"
echo "  which address may reach it and scope the firewall rule to that host."
echo
fi
if [ "$NOTIFIER_BUILT" = true ]; then
echo -e "${GREEN}CashStratum Notifier:${NC}"
echo "  Sub-second block notifications via ZMQ & stratum log tailer."
echo "  Configure: $API_DIR/cashstratum-notifier.env"
echo "  Install service via: sudo ./post-install.sh"
echo
fi
echo -e "${GREEN}Verified Features:${NC}"
echo "✓ Native CashAddr support (bitcoincash:, bchtest:, bchreg:)"
echo "✓ Legacy Base58 addresses supported"
echo "✓ Pool operator fee with automatic coinbase splitting"
echo "✓ Multi-difficulty management (password, useragent, pattern)"
echo "✓ Password-based difficulty: -p d=500000"
echo "✓ Auto-detection: NiceHash, MiningRigRentals"
echo "✓ Successfully tested on BCH testnet (10+ blocks mined)"
